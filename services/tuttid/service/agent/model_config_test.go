package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadClaudeCodeConfiguredDefaultModelKeepsConcreteModel(t *testing.T) {
	configDir := t.TempDir()
	settingsPath := filepath.Join(configDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model":"claude-opus-4-6"}`), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if got := readClaudeCodeConfiguredDefaultModel(); got != "claude-opus-4-6" {
		t.Fatalf("readClaudeCodeConfiguredDefaultModel() = %q, want claude-opus-4-6", got)
	}
}

// TestReadCodexConfiguredDefaultModelIgnoresUnownedHome pins the rule that keeps
// the host user's personal Codex CLI config out of the platform's model
// resolution: without a CODEX_HOME it owns, the daemon reports no configured
// default. Before this, the read fell through to ~/.codex/config.toml and a
// gateway-bound session picked up whatever that file said.
func TestReadCodexConfiguredDefaultModelIgnoresUnownedHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	personalHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(personalHome, 0o700); err != nil {
		t.Fatalf("create personal codex home: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(personalHome, "config.toml"),
		[]byte("model = \"gpt-6-astra\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write personal config.toml: %v", err)
	}

	if got := readCodexConfiguredDefaultModel(); got != "" {
		t.Fatalf("readCodexConfiguredDefaultModel() = %q, want empty", got)
	}
}

func TestReadCodexConfiguredDefaultModelReadsOwnedHome(t *testing.T) {
	ownedHome := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(ownedHome, "config.toml"),
		[]byte("model_provider = \"tutti-model-plan\"\nmodel = \"deepseek-flash\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write owned config.toml: %v", err)
	}
	t.Setenv("CODEX_HOME", ownedHome)

	if got := readCodexConfiguredDefaultModel(); got != "deepseek-flash" {
		t.Fatalf("readCodexConfiguredDefaultModel() = %q, want deepseek-flash", got)
	}
}
