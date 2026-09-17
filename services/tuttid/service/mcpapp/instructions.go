package mcpapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
)

// Server instructions: the same second MCP client, asked a different question.
//
// codex (app-server) never puts an MCP server's initialize `instructions` into
// the model prompt, so the codex adapter asks tuttid for them and delivers them
// through its own developer-instructions channel (agentruntime
// mcp_server_instructions.go). Only initialize runs here: no tools/list, no
// resources/read, never tools/call.

const (
	instructionsLogEvent    = "mcp_server_instructions.resolve"
	instructionsProcessName = "mcp-instructions-probe"
	// instructionsDeadlineMargin is kept back from the caller's deadline so the
	// probe's own timeout fires first and the failure is attributed to (and
	// negatively cached for) the server, not silently dropped as "caller gave up".
	instructionsDeadlineMargin = 100 * time.Millisecond
	// instructionsMinProbe is the shortest probe ever launched. A caller with
	// less time left than this still gets its answer later from the cache: the
	// probe outlives the caller that started it.
	instructionsMinProbe = 500 * time.Millisecond
)

var _ agentruntime.MCPServerInstructionsResolver = (*Resolver)(nil)

type instructionsEntry struct {
	text      string
	err       error
	expiresAt time.Time
}

// instructionsCall is one in-flight probe shared by every caller of the same
// fingerprint (singleflight).
type instructionsCall struct {
	done chan struct{}
	text string
	err  error
}

// ServerInstructions returns the server's initialize instructions. Every probe
// outcome — success, or failure including the probe's own timeout — is cached
// per fingerprint (TTL / FailureTTL), so a hung or slow server costs at most one
// launch per FailureTTL rather than one per turn. Concurrent callers of the same
// fingerprint share one probe. The caller blocks for at most its ctx.
func (r *Resolver) ServerInstructions(ctx context.Context, server agentruntime.MCPAppServer) (string, error) {
	if r == nil {
		return "", errors.New("MCP server instructions resolver is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := server.Fingerprint
	r.mu.Lock()
	if entry, ok := r.instructions[key]; ok && r.now().Before(entry.expiresAt) {
		r.mu.Unlock()
		return entry.text, entry.err
	}
	if r.instructionsInflight == nil {
		r.instructionsInflight = make(map[string]*instructionsCall)
	}
	call, running := r.instructionsInflight[key]
	if !running {
		call = &instructionsCall{done: make(chan struct{})}
		r.instructionsInflight[key] = call
	}
	r.mu.Unlock()

	if !running {
		// Detached from ctx: the probe's outcome is cached even if this caller
		// stops waiting, which is what keeps the next turn from paying again.
		go r.runInstructionsProbe(server, call, r.instructionsProbeTimeout(ctx))
	}
	select {
	case <-call.done:
		return call.text, call.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// instructionsProbeTimeout = min(resolver timeout, caller's remaining time −
// margin), floored at instructionsMinProbe.
func (r *Resolver) instructionsProbeTimeout(ctx context.Context) time.Duration {
	timeout := r.timeout()
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) - instructionsDeadlineMargin; remaining < timeout {
			timeout = remaining
		}
	}
	if timeout < instructionsMinProbe {
		timeout = instructionsMinProbe
	}
	return timeout
}

func (r *Resolver) runInstructionsProbe(server agentruntime.MCPAppServer, call *instructionsCall, timeout time.Duration) {
	started := r.now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	text, err := r.fetchInstructions(ctx, server)

	ttl := r.ttl()
	if err != nil {
		ttl = r.failureTTL()
		slog.Warn("mcp_server_instructions.resolve failed",
			"event", instructionsLogEvent,
			"server", server.Name,
			"command", server.Command,
			"timeout_ms", timeout.Milliseconds(),
			"duration_ms", r.now().Sub(started).Milliseconds(),
			"error", err,
		)
	} else {
		slog.Info("mcp_server_instructions.resolve completed",
			"event", instructionsLogEvent,
			"server", server.Name,
			"instructions_bytes", len(text),
			"duration_ms", r.now().Sub(started).Milliseconds(),
		)
	}

	r.mu.Lock()
	if r.instructions == nil {
		r.instructions = make(map[string]instructionsEntry)
	}
	now := r.now()
	for cached, entry := range r.instructions {
		if !now.Before(entry.expiresAt) {
			delete(r.instructions, cached)
		}
	}
	if len(r.instructions) >= defaultMaxEntries {
		for cached := range r.instructions {
			delete(r.instructions, cached)
			if len(r.instructions) < defaultMaxEntries {
				break
			}
		}
	}
	r.instructions[server.Fingerprint] = instructionsEntry{text: text, err: err, expiresAt: now.Add(ttl)}
	delete(r.instructionsInflight, server.Fingerprint)
	call.text, call.err = text, err
	r.mu.Unlock()
	close(call.done)
}

func (r *Resolver) fetchInstructions(ctx context.Context, server agentruntime.MCPAppServer) (string, error) {
	slots := r.concurrencySlots()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return "", fmt.Errorf("wait for probe slot: %w", ctx.Err())
	}
	defer func() { <-slots }()

	_, closeClient, raw, err := openInitializedClient(ctx, r.Transport, server, instructionsClientRole)
	if err != nil {
		return "", err
	}
	closeClient(false)
	var result struct {
		Instructions string `json:"instructions"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			return "", fmt.Errorf("decode initialize: %w", err)
		}
	}
	return result.Instructions, nil
}
