package agentruntime

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar"
)

// claudeSDKBuiltinSidecarCommand marks the built-in Go sidecar. The daemon
// runs it in-process over the same versioned NDJSON protocol an external
// sidecar would speak on stdio, so the protocol seam stays intact while no
// Node runtime is required.
const claudeSDKBuiltinSidecarCommand = "builtin:claude-sdk-sidecar"

func (a *ClaudeCodeSDKAdapter) startSidecarConnection(ctx context.Context, spec ProcessSpec) (ProcessConnection, error) {
	if len(spec.Command) > 0 && spec.Command[0] == claudeSDKBuiltinSidecarCommand && usesLocalProcessTransport(a.transport) {
		return newInProcessSidecarConnection(spec), nil
	}
	return a.transport.Start(ctx, spec)
}

// usesLocalProcessTransport reports whether the adapter would spawn local
// processes. Only then does the builtin command run in-process; injected
// transports (scripted tests, remote transports) keep receiving Start calls.
func usesLocalProcessTransport(transport ProcessTransport) bool {
	_, isLocal := transport.(localProcessTransport)
	return isLocal
}

// inProcessSidecarConnection adapts the Go sidecar server to the daemon's
// ProcessConnection seam: requests written with Send are served sequentially
// (like the TypeScript sidecar's stdin loop), and emitted events surface as
// stdout frames.
type inProcessSidecarConnection struct {
	requestWriter *io.PipeWriter
	frames        chan ProcessFrame
	closing       chan struct{}
	served        chan struct{}

	closeMu   sync.Mutex
	closed    bool
	inputMu   sync.Mutex
	inputDone bool
}

func newInProcessSidecarConnection(spec ProcessSpec) *inProcessSidecarConnection {
	// The daemon spawns sidecars with IS_SANDBOX=1; the embedded sidecar gets
	// the equivalent marker directly.
	if envValueFromList(spec.Env, "IS_SANDBOX") != "" {
		claudesidecar.SetSandboxed(true)
	}
	requestReader, requestWriter := io.Pipe()
	conn := &inProcessSidecarConnection{
		requestWriter: requestWriter,
		frames:        make(chan ProcessFrame, 16),
		closing:       make(chan struct{}),
		served:        make(chan struct{}),
	}
	var options []claudesidecar.ServerOption
	if envValueFromList(spec.Env, claudeSDKSidecarTestDriverEnv) == "1" {
		options = append(options, claudesidecar.WithTestDriver())
	}
	server := claudesidecar.NewServer(inProcessSidecarFrameWriter{conn: conn}, options...)
	go func() {
		defer close(conn.served)
		_ = server.Serve(requestReader)
		exitCode := 0
		select {
		case conn.frames <- ProcessFrame{ExitCode: &exitCode}:
		case <-conn.closing:
		}
		close(conn.frames)
	}()
	return conn
}

type inProcessSidecarFrameWriter struct {
	conn *inProcessSidecarConnection
}

func (w inProcessSidecarFrameWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	chunk := append([]byte(nil), data...)
	select {
	case w.conn.frames <- ProcessFrame{Stdout: chunk}:
		return len(data), nil
	case <-w.conn.closing:
		// Closing intentionally discards unread output, matching the local
		// process transport's shutdown behavior.
		return len(data), nil
	}
}

func (c *inProcessSidecarConnection) Send(data []byte) error {
	c.closeMu.Lock()
	closed := c.closed
	c.closeMu.Unlock()
	if closed {
		return io.ErrClosedPipe
	}
	_, err := c.requestWriter.Write(data)
	return err
}

func (c *inProcessSidecarConnection) Recv() (ProcessFrame, error) {
	frame, ok := <-c.frames
	if !ok {
		return ProcessFrame{}, io.EOF
	}
	return frame, nil
}

func (c *inProcessSidecarConnection) RecvContext(ctx context.Context) (ProcessFrame, error) {
	select {
	case <-ctx.Done():
		return ProcessFrame{}, ctx.Err()
	case frame, ok := <-c.frames:
		if !ok {
			return ProcessFrame{}, io.EOF
		}
		return frame, nil
	}
}

func (c *inProcessSidecarConnection) Close() error {
	c.closeMu.Lock()
	if !c.closed {
		c.closed = true
		close(c.closing)
	}
	c.closeMu.Unlock()
	_ = c.CloseInput()
	// A request handler stuck on a hung claude process cannot be killed the
	// way an external sidecar can; bound the wait instead of hanging Close.
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-c.served:
	case <-timer.C:
	}
	return nil
}

func (c *inProcessSidecarConnection) CloseInput() error {
	c.inputMu.Lock()
	defer c.inputMu.Unlock()
	if c.inputDone {
		return nil
	}
	c.inputDone = true
	return c.requestWriter.Close()
}

func (c *inProcessSidecarConnection) Terminate() error {
	return c.CloseInput()
}

func (c *inProcessSidecarConnection) Kill() error {
	return c.CloseInput()
}
