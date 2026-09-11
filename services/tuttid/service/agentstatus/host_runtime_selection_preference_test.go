package agentstatus

import (
	"context"
	"path/filepath"
	"testing"

	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

// claudeSelectionService 造一个最小的 claude 服务：宿主投影走 env（本文件里由 t.Setenv 给），
// PATH 与内联覆盖走 service.Environ。
func claudeSelectionService(t *testing.T, home string, environ []string) Service {
	t.Helper()
	service := probeTestService(home)
	service.Environ = func() []string { return environ }
	return service
}

// 宿主说用哪条 codex，Tutti 就用哪条——**即使它不在 Tutti 自己的 catalog 里**。
//
// 这是 codex 与 claude 对齐的关键：以前宿主的路径不在 catalog 里会被硬拒
// （SelectCodexRuntime 报「not in the Tutti catalog」），codex 于是起不来，而宿主唯一的自救
// 手段是「用 Tutti 探测到的路径回写注册行」——那正是把用户的选择悄悄换掉的那个动作。
func TestHostSelectionProvidesCodexRuntimeOutsideTheCatalog(t *testing.T) {
	home := t.TempDir()
	hostCodex := writeCodexVersionFixture(t, filepath.Join(home, "nvm", "bin", "codex"), "0.145.0")
	service := probeTestService(home)
	// PATH 上没有 codex：discovery 一条候选都产不出来，只有宿主投影能给出这条路径。
	service.Environ = func() []string { return []string{"PATH=" + filepath.Join(home, "empty")} }
	service.CodexRuntimeSelectionStore = &memoryCodexRuntimeSelectionStore{}
	service.CodexProtocolProbe = func(_ context.Context, _ []string, _ []string) CodexProbeEvidence {
		return CodexProbeEvidence{CommandStarted: true, ProtocolReady: true}
	}
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"codex":{"binPath":"`+hostCodex+`"}}}`))

	command, err := service.ResolveProviderCommand(context.Background(), agentproviderbiz.Codex)
	if err != nil || len(command.Command) == 0 || command.Command[0] != hostCodex {
		t.Fatalf("ResolveProviderCommand() = %#v, %v; want the host-selected launcher %q", command, err, hostCodex)
	}
	// 状态探测必须与执行同源：两处读的是同一次解析结果，否则又出现「探测一个、执行另一个」。
	specs, err := service.selectProviderSpecs(context.Background(), []string{agentproviderbiz.Codex}, true)
	if err != nil {
		t.Fatalf("selectProviderSpecs() error = %v", err)
	}
	runtime := service.resolveProviderRuntime(context.Background(), specs[0])
	if runtime.AdapterPath != hostCodex || len(runtime.AdapterCommand) == 0 || runtime.AdapterCommand[0] != hostCodex {
		t.Fatalf("status runtime = %#v; want the host-selected launcher", runtime)
	}
	// 面板读的 catalog 也只有这一条：看到的就是要跑的那条。
	catalog, err := service.GetCodexRuntimeCatalog(context.Background(), agentproviderbiz.Codex)
	if err != nil {
		t.Fatalf("GetCodexRuntimeCatalog() error = %v", err)
	}
	if catalog.Selection.State != CodexRuntimeSelectionSelected || catalog.Selection.LauncherPath != hostCodex {
		t.Fatalf("catalog selection = %#v; want the host-selected launcher", catalog.Selection)
	}
	if len(catalog.Candidates) != 1 || catalog.Candidates[0].LauncherPath != hostCodex {
		t.Fatalf("catalog candidates = %#v; want only the host-selected launcher", catalog.Candidates)
	}
}

// 宿主给的路径不可用时**不吞掉**：落到 Tutti 自己的现发现 + 现验证，codex 还能起来。
// （与 claude 侧刻意不同：claude 的 cleared 是让调用方跳过冻住的旧值，而 codex 的回落是活的。）
func TestHostSelectionCodexFallsBackToTuttisOwnDiscovery(t *testing.T) {
	home := t.TempDir()
	discovered := writeCodexVersionFixture(t, filepath.Join(home, "bin", "codex"), "0.145.0")
	service := probeTestService(home)
	service.Environ = func() []string { return []string{"PATH=" + filepath.Dir(discovered)} }
	service.CodexRuntimeSelectionStore = &memoryCodexRuntimeSelectionStore{}
	// 桩要如实反映"只有真的那份二进制讲得通 ACP"：不分路径一律 ready 的话，一条不存在的路径
	// 也会被判成可用，这个用例就测不到回落了。
	service.CodexProtocolProbe = func(_ context.Context, command []string, _ []string) CodexProbeEvidence {
		if len(command) > 0 && command[0] == discovered {
			return CodexProbeEvidence{CommandStarted: true, ProtocolReady: true}
		}
		return CodexProbeEvidence{CommandStarted: true, Category: "acp_adapter_launch_failed"}
	}
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"codex":{"binPath":"`+filepath.Join(home, "gone", "codex")+`"}}}`))

	command, err := service.ResolveProviderCommand(context.Background(), agentproviderbiz.Codex)
	if err != nil || len(command.Command) == 0 || command.Command[0] != discovered {
		t.Fatalf("ResolveProviderCommand() = %#v, %v; want Tutti's own discovery %q", command, err, discovered)
	}
}

// 宿主说「没有选定项」（空 binPath）时，Tutti 自己持久化的那份选择照旧生效。
func TestHostSelectionClearedLeavesTuttisOwnCodexChoiceAlone(t *testing.T) {
	home := t.TempDir()
	other := writeCodexVersionFixture(t, filepath.Join(home, "other", "codex"), "0.145.0")
	chosen := writeCodexVersionFixture(t, filepath.Join(home, "chosen", "codex"), "0.145.0")
	service := probeTestService(home)
	service.Environ = func() []string {
		return []string{"PATH=" + filepath.Dir(other) + string(filepath.ListSeparator) + filepath.Dir(chosen)}
	}
	service.CodexRuntimeSelectionStore = &memoryCodexRuntimeSelectionStore{
		selection: agentproviderbiz.RuntimeSelection{Provider: agentproviderbiz.Codex, LauncherPath: chosen},
		found:     true,
	}
	service.CodexProtocolProbe = func(_ context.Context, _ []string, _ []string) CodexProbeEvidence {
		return CodexProbeEvidence{CommandStarted: true, ProtocolReady: true}
	}
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"codex":{"binPath":""}}}`))

	command, err := service.ResolveProviderCommand(context.Background(), agentproviderbiz.Codex)
	if err != nil || len(command.Command) == 0 || command.Command[0] != chosen {
		t.Fatalf("ResolveProviderCommand() = %#v, %v; want the persisted choice %q", command, err, chosen)
	}
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
