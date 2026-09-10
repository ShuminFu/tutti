package agentstatus

import "testing"

func TestClaudeACPWindowsBatchShimUsesBundledNativeCLI(t *testing.T) {
	spec := ProviderSpec{Provider: "claude-code", ExternalRegistryID: "claude-acp"}
	for _, path := range []string{
		`C:\Users\PC\AppData\Roaming\npm\claude.cmd`,
		`C:\tools\claude.BAT`,
	} {
		if got := claudeCodeAdapterExecutable("windows", spec, path); got != "" {
			t.Fatalf("claudeCodeAdapterExecutable(windows, %q) = %q, want empty override", path, got)
		}
	}
}

func TestClaudeACPWindowsNativeExecutableIsPreserved(t *testing.T) {
	spec := ProviderSpec{Provider: "claude-code", ExternalRegistryID: "claude-acp"}
	path := `C:\tools\claude.exe`
	if got := claudeCodeAdapterExecutable("windows", spec, path); got != path {
		t.Fatalf("claudeCodeAdapterExecutable(windows, %q) = %q", path, got)
	}
}

func TestClaudeBatchShimIsPreservedOutsideWindowsACP(t *testing.T) {
	path := `/usr/local/bin/claude`
	if got := claudeCodeAdapterExecutable("darwin", ProviderSpec{ExternalRegistryID: "claude-acp"}, path); got != path {
		t.Fatalf("Darwin executable = %q, want %q", got, path)
	}
	windowsShim := `C:\tools\claude.cmd`
	if got := claudeCodeAdapterExecutable("windows", ProviderSpec{}, windowsShim); got != windowsShim {
		t.Fatalf("non-ACP Windows executable = %q, want %q", got, windowsShim)
	}
}

func TestPreferClaudeCodeExecutableClearsWindowsACPBatchOverride(t *testing.T) {
	spec := ProviderSpec{Provider: "claude-code", ExternalRegistryID: "claude-acp"}
	path := `C:\Users\PC\AppData\Roaming\npm\claude.cmd`
	resolved := preferClaudeCodeExecutableForPlatform("windows", spec, path, []string{`PATH=C:\Windows\System32`})
	if got := envValueForKey(resolved.AdapterEnv, claudeCodeExecutableEnv); got != "" {
		t.Fatalf("%s = %q, want empty override", claudeCodeExecutableEnv, got)
	}
}
