package claudesidecar

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

func fakeExecutable(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	return path
}

func TestExplicitClaudeCodeExecutableAlwaysWins(t *testing.T) {
	resolved := resolveClaudeCodeExecutablePath(map[string]string{
		"CLAUDE_CODE_EXECUTABLE":                "/custom/claude",
		"TUTTI_CLAUDE_CODE_FALLBACK_EXECUTABLE": fakeExecutable(t, "claude"),
	}, t.TempDir())
	if resolved != "/custom/claude" {
		t.Fatalf("resolved = %q", resolved)
	}
}

func TestNativeDevTreeBinaryWinsOverFallback(t *testing.T) {
	root := t.TempDir()
	keys := nativePlatformPackageKeys()
	if len(keys) == 0 {
		t.Skip("no native package key for this platform")
	}
	binaryName := "claude"
	if goruntime.GOOS == "windows" {
		binaryName = "claude.exe"
	}
	packageDir := filepath.Join(root, "node_modules", "@anthropic-ai", "claude-agent-sdk-"+keys[0])
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	native := filepath.Join(packageDir, binaryName)
	if err := os.WriteFile(native, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write native binary: %v", err)
	}
	nested := filepath.Join(root, "project", "workdir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	resolved := resolveClaudeCodeExecutablePath(map[string]string{
		"TUTTI_CLAUDE_CODE_FALLBACK_EXECUTABLE": fakeExecutable(t, "claude"),
	}, nested)
	if resolved != native {
		t.Fatalf("resolved = %q, want %q", resolved, native)
	}
}

func TestFallbackExecutableUsedWithoutNativeTree(t *testing.T) {
	fallback := fakeExecutable(t, "claude")
	resolved := resolveClaudeCodeExecutablePath(map[string]string{
		"TUTTI_CLAUDE_CODE_FALLBACK_EXECUTABLE": fallback,
	}, t.TempDir())
	if resolved != fallback {
		t.Fatalf("resolved = %q, want %q", resolved, fallback)
	}
}

func TestMissingFallbackFileIsIgnored(t *testing.T) {
	resolved := resolveClaudeCodeExecutablePath(map[string]string{
		"TUTTI_CLAUDE_CODE_FALLBACK_EXECUTABLE": "/nonexistent/claude",
	}, t.TempDir())
	if resolved != "" {
		t.Fatalf("resolved = %q, want empty", resolved)
	}
}

func TestBlankEnvValuesAreIgnored(t *testing.T) {
	resolved := resolveClaudeCodeExecutablePath(map[string]string{
		"CLAUDE_CODE_EXECUTABLE":                "   ",
		"TUTTI_CLAUDE_CODE_FALLBACK_EXECUTABLE": "",
	}, t.TempDir())
	if resolved != "" {
		t.Fatalf("resolved = %q, want empty", resolved)
	}
}
