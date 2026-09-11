package agentstatus

import (
	"context"
	"path/filepath"
	"testing"
)

// claudeSelectionService 造一个最小的 claude 服务：宿主投影走 env（本文件里由 t.Setenv 给），
// PATH 与内联覆盖走 service.Environ。
func claudeSelectionService(t *testing.T, home string, environ []string) Service {
	t.Helper()
	service := probeTestService(home)
	service.Environ = func() []string { return environ }
	return service
}

func claudeSelectionSpec() ProviderSpec {
	return ProviderSpec{Provider: "claude-code", BinaryNames: []string{"claude"}}
}

func writeClaudeStub(t *testing.T, path, version string) string {
	t.Helper()
	writeExecutable(t, path, "#!/bin/sh\necho '"+version+" (Claude Code)'\n")
	return path
}

// The whole point of the host projection: it is read at use time, so a path the
// operator saved after the sidecar started wins over the inline override that
// was frozen into this process's environment at spawn.
func TestHostSelectionBeatsInlineEnvironment(t *testing.T) {
	home := t.TempDir()
	hostClaude := writeClaudeStub(t, filepath.Join(home, "host", "claude"), "2.1.268")
	envClaude := writeClaudeStub(t, filepath.Join(home, "env", "claude"), "2.1.19")
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"claude-code":{"binPath":"`+hostClaude+`","version":"2.1.268"}}}`))
	service := claudeSelectionService(t, home, []string{
		"PATH=" + filepath.Join(home, "empty"),
		claudeCodeExecutableEnv + "=" + envClaude,
	})

	resolved := service.resolveProviderRuntime(context.Background(), service.withPreferredClaudeCodeRuntime(context.Background(), claudeSelectionSpec()))
	if resolved.CLIPath != hostClaude {
		t.Fatalf("CLIPath = %q, want the host projection %q (inline was %q)", resolved.CLIPath, hostClaude, envClaude)
	}
	if got := envValueForKey(resolved.Env, claudeCodeExecutableEnv); got != hostClaude {
		t.Fatalf("%s = %q, want %q", claudeCodeExecutableEnv, got, hostClaude)
	}
	if first := filepath.SplitList(envValueForKey(resolved.Env, "PATH"))[0]; first != filepath.Dir(hostClaude) {
		t.Fatalf("PATH first dir = %q, want %q", first, filepath.Dir(hostClaude))
	}
}

// Steady state must stay byte-identical: when the projection agrees with the
// inline override the value is already in the child's environment, so nothing is
// appended to AdapterEnv (which would also move the ACP adapter's PATH head).
func TestHostSelectionSkippedWhenEqualToInlineEnvironment(t *testing.T) {
	home := t.TempDir()
	agreed := writeClaudeStub(t, filepath.Join(home, "both", "claude"), "2.1.268")
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"claude-code":{"binPath":"`+agreed+`"}}}`))
	service := claudeSelectionService(t, home, []string{
		"PATH=" + filepath.Join(home, "empty"),
		claudeCodeExecutableEnv + "=" + agreed,
	})

	spec := service.withPreferredClaudeCodeRuntime(context.Background(), claudeSelectionSpec())
	if len(spec.AdapterEnv) != 0 {
		t.Fatalf("AdapterEnv = %#v, want the spec left untouched when both channels agree", spec.AdapterEnv)
	}
}

// A projection pointing at something unusable is not a failure: it falls through
// to the inline override exactly like an invalid inline value would.
func TestHostSelectionFallsBackWhenProjectionIsUnusable(t *testing.T) {
	home := t.TempDir()
	envClaude := writeClaudeStub(t, filepath.Join(home, "env", "claude"), "2.1.19")
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"claude-code":{"binPath":"`+filepath.Join(home, "host", "absent")+`"}}}`))
	service := claudeSelectionService(t, home, []string{
		"PATH=" + filepath.Join(home, "empty"),
		claudeCodeExecutableEnv + "=" + envClaude,
	})

	resolved := service.resolveProviderRuntime(context.Background(), service.withPreferredClaudeCodeRuntime(context.Background(), claudeSelectionSpec()))
	if resolved.CLIPath != envClaude {
		t.Fatalf("CLIPath = %q, want the inline fallback %q", resolved.CLIPath, envClaude)
	}
}

// An empty binPath is the host saying "this provider has no selected runtime".
// The inline override is stale by definition, so it must not win — otherwise
// deleting the row would need a sidecar restart to take effect.
func TestHostSelectionClearedSkipsInlineEnvironment(t *testing.T) {
	home := t.TempDir()
	envClaude := writeClaudeStub(t, filepath.Join(home, "env", "claude"), "2.1.19")
	discovered := writeClaudeStub(t, filepath.Join(home, "bin", "claude"), "2.1.201")
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"claude-code":{"binPath":""}}}`))
	service := claudeSelectionService(t, home, []string{
		"PATH=" + filepath.Dir(discovered),
		claudeCodeExecutableEnv + "=" + envClaude,
	})

	resolved := service.resolveProviderRuntime(context.Background(), service.withPreferredClaudeCodeRuntime(context.Background(), claudeSelectionSpec()))
	if resolved.CLIPath == envClaude {
		t.Fatalf("CLIPath = %q, want the cleared state to ignore the stale inline override", resolved.CLIPath)
	}
	if resolved.CLIPath != discovered {
		t.Fatalf("CLIPath = %q, want PATH discovery %q", resolved.CLIPath, discovered)
	}
}

// Without the projection (older host, standalone) nothing changes: the inline
// override keeps winning, which is exactly today's behaviour.
func TestHostSelectionAbsentKeepsLegacyChain(t *testing.T) {
	home := t.TempDir()
	envClaude := writeClaudeStub(t, filepath.Join(home, "env", "claude"), "2.1.19")
	service := claudeSelectionService(t, home, []string{
		"PATH=" + filepath.Join(home, "empty"),
		claudeCodeExecutableEnv + "=" + envClaude,
	})

	resolved := service.resolveProviderRuntime(context.Background(), service.withPreferredClaudeCodeRuntime(context.Background(), claudeSelectionSpec()))
	if resolved.CLIPath != envClaude {
		t.Fatalf("CLIPath = %q, want the legacy inline override %q", resolved.CLIPath, envClaude)
	}
}
