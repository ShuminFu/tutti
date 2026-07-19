package claudecli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	terminateGrace = 2 * time.Second
	killGrace      = 5 * time.Second
)

var jsEntrySuffixes = []string{".js", ".mjs", ".tsx", ".ts", ".jsx"}

// transport owns the claude CLI process and its stdio streams.
type transport struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu   sync.Mutex
	inputOnce sync.Once

	lines chan string

	exitMu   sync.Mutex
	exited   bool
	exitErr  error
	exitedCh chan struct{}

	closeOnce sync.Once
}

func startTransport(options *Options) (*transport, error) {
	command, args := resolveSpawnCommand(options)
	cmd := exec.Command(command, args...)
	if options.CWD != "" {
		cmd.Dir = options.CWD
	}
	cmd.Env = buildProcessEnv(options.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		//nolint:staticcheck // Mirrors the Node SDK's exact error text.
		return nil, fmt.Errorf("Failed to spawn Claude Code process: %w", err)
	}
	t := &transport{
		cmd:      cmd,
		stdin:    stdin,
		lines:    make(chan string, 64),
		exitedCh: make(chan struct{}),
	}
	debug := debugEnabled(options.Env)
	stderrDone := make(chan struct{})
	go consumeStderr(stderr, debug, stderrDone)
	go t.readLines(stdout, stderrDone)
	return t, nil
}

func resolveSpawnCommand(options *Options) (string, []string) {
	args := options.cliArgs()
	path := strings.TrimSpace(options.PathToClaudeCodeExecutable)
	if path == "" {
		return "claude", args
	}
	for _, suffix := range jsEntrySuffixes {
		if strings.HasSuffix(path, suffix) {
			return "node", append([]string{path}, args...)
		}
	}
	return path, args
}

func buildProcessEnv(env map[string]string) []string {
	merged := map[string]string{}
	if env == nil {
		for _, item := range os.Environ() {
			key, value, ok := strings.Cut(item, "=")
			if ok {
				merged[key] = value
			}
		}
	} else {
		for key, value := range env {
			merged[key] = value
		}
	}
	if merged["CLAUDE_CODE_ENTRYPOINT"] == "" {
		merged["CLAUDE_CODE_ENTRYPOINT"] = "sdk-ts"
	}
	delete(merged, "NODE_OPTIONS")
	if truthy(merged["DEBUG_CLAUDE_AGENT_SDK"]) {
		merged["DEBUG"] = "1"
	} else {
		delete(merged, "DEBUG")
	}
	result := make([]string, 0, len(merged))
	for key, value := range merged {
		result = append(result, key+"="+value)
	}
	return result
}

func debugEnabled(env map[string]string) bool {
	if env != nil {
		return truthy(env["DEBUG_CLAUDE_AGENT_SDK"])
	}
	return truthy(os.Getenv("DEBUG_CLAUDE_AGENT_SDK"))
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func consumeStderr(stderr io.Reader, forward bool, done chan<- struct{}) {
	defer close(done)
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if forward {
			fmt.Fprintln(os.Stderr, scanner.Text())
		}
	}
}

func (t *transport) readLines(stdout io.Reader, stderrDone <-chan struct{}) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		t.lines <- line
	}
	<-stderrDone
	err := t.cmd.Wait()
	t.exitMu.Lock()
	t.exited = true
	t.exitErr = processExitError(err)
	t.exitMu.Unlock()
	close(t.exitedCh)
	close(t.lines)
}

func processExitError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if signal := exitSignalName(exitErr); signal != "" {
			//nolint:staticcheck // Mirrors the Node SDK's exact error text.
			return fmt.Errorf("Claude Code process terminated by signal %s", signal)
		}
		//nolint:staticcheck // Mirrors the Node SDK's exact error text.
		return fmt.Errorf("Claude Code process exited with code %d", exitErr.ExitCode())
	}
	return err
}

func (t *transport) write(data []byte) error {
	t.exitMu.Lock()
	exited := t.exited
	t.exitMu.Unlock()
	if exited {
		//nolint:staticcheck // Mirrors the Node SDK's exact error text.
		return errors.New("Cannot write to terminated process")
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_, err := t.stdin.Write(data)
	return err
}

func (t *transport) endInput() {
	t.inputOnce.Do(func() {
		_ = t.stdin.Close()
	})
}

// exitError reports the recorded process exit failure, if any.
func (t *transport) exitError() error {
	t.exitMu.Lock()
	defer t.exitMu.Unlock()
	return t.exitErr
}

// close ends stdin and escalates SIGTERM then SIGKILL on the SDK's timings.
func (t *transport) close() {
	t.closeOnce.Do(func() {
		t.endInput()
		go func() {
			if t.waitExited(terminateGrace) {
				return
			}
			if t.cmd.Process != nil {
				_ = terminateProcess(t.cmd.Process)
			}
			if t.waitExited(killGrace) {
				return
			}
			if t.cmd.Process != nil {
				_ = t.cmd.Process.Kill()
			}
		}()
	})
}

func (t *transport) waitExited(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-t.exitedCh:
		return true
	case <-timer.C:
		return false
	}
}
