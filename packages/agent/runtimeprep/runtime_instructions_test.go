package runtimeprep

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hermesInstructionPrepareInput(cwd string) PrepareInput {
	return PrepareInput{
		WorkspaceID:         "workspace-1",
		AgentSessionID:      "session-1",
		AgentTargetID:       "local:hermes",
		Provider:            "acp:hermes",
		Cwd:                 cwd,
		CLICommand:          "tutti-dev",
		ExtensionSkillRoots: []string{".agent_context/skills"},
	}
}

func expectedTuttiCLIPolicy(t *testing.T, input PrepareInput) string {
	t.Helper()
	policy, err := tuttiCLIPolicy(testResolvedInput(t, input))
	if err != nil {
		t.Fatalf("tuttiCLIPolicy() error = %v", err)
	}
	return policy
}

func loadSidecarManifest(t *testing.T, runtimeRoot string) Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(runtimeRoot, SidecarManifestFileName))
	if err != nil {
		t.Fatalf("read sidecar manifest: %v", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode sidecar manifest: %v", err)
	}
	return manifest
}

func TestDefaultPreparerWritesSessionRuntimeInstructionsForHermes(t *testing.T) {
	stateDir := t.TempDir()
	cwd := t.TempDir()
	input := hermesInstructionPrepareInput(cwd)
	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), input)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	runtimeRoot, err := LocalStore{StateDir: stateDir}.RuntimeRoot(input.WorkspaceID, input.AgentSessionID)
	if err != nil {
		t.Fatalf("RuntimeRoot() error = %v", err)
	}
	path := filepath.Join(runtimeRoot, runtimeInstructionsFileName)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("runtime-instructions.md missing: %v", err)
	}
	want := expectedTuttiCLIPolicy(t, input) + "\n"
	if string(got) != want {
		t.Fatalf("runtime-instructions.md content mismatch")
	}
	if envValue(prepared.Env, runtimeInstructionsFileEnv) != path {
		t.Fatalf("prepared env = %#v, want %s=%s", prepared.Env, runtimeInstructionsFileEnv, path)
	}
	manifest := loadSidecarManifest(t, runtimeRoot)
	found := false
	for _, file := range manifest.ManagedFiles {
		if file.Kind == "runtime-instructions" && file.Path == path {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("manifest missing kind runtime-instructions: %#v", manifest.ManagedFiles)
	}
}

func TestDefaultPreparerClaudeCodeSkipsSessionRuntimeInstructions(t *testing.T) {
	stateDir := t.TempDir()
	cwd := t.TempDir()
	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), PrepareInput{
		WorkspaceID:    "workspace-1",
		AgentSessionID: "session-1",
		AgentTargetID:  "local:claude-code",
		Provider:       "claude-code",
		Cwd:            cwd,
		CLICommand:     "tutti-dev",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if value := envValue(prepared.Env, runtimeInstructionsFileEnv); value != "" {
		t.Fatalf("claude-code env has %s=%s, want absent", runtimeInstructionsFileEnv, value)
	}
	runtimeRoot, err := LocalStore{StateDir: stateDir}.RuntimeRoot("workspace-1", "session-1")
	if err != nil {
		t.Fatalf("RuntimeRoot() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, runtimeInstructionsFileName)); !os.IsNotExist(err) {
		t.Fatalf("claude-code wrote runtime-instructions.md, err=%v", err)
	}
}

func TestDefaultPreparerCwdWriteGateSkipsAgentsFile(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		t.Setenv(runtimeInstructionsCwdWriteEnv, "off")
		var logs bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		t.Cleanup(func() { slog.SetDefault(previous) })

		stateDir := t.TempDir()
		cwd := t.TempDir()
		input := hermesInstructionPrepareInput(cwd)
		input.Metadata = map[string]any{"clientSubmitId": "submit-cwd-gate"}
		if _, err := newTestPreparer(stateDir).Prepare(t.Context(), input); err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(cwd, "AGENTS.md")); !os.IsNotExist(err) {
			t.Fatalf("cwd AGENTS.md exists when cwd write is off, err=%v", err)
		}
		if !strings.Contains(logs.String(), "cwd_write_skipped") {
			t.Fatalf("trace missing cwd_write_skipped: %s", logs.String())
		}
		skillPath := filepath.Join(cwd, ".agent_context", "skills", tuttiSkillName, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Fatalf("skills should still materialize: %v", err)
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Setenv(runtimeInstructionsCwdWriteEnv, "")
		stateDir := t.TempDir()
		cwd := t.TempDir()
		input := hermesInstructionPrepareInput(cwd)
		if _, err := newTestPreparer(stateDir).Prepare(t.Context(), input); err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		content, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
		if err != nil {
			t.Fatalf("cwd AGENTS.md missing: %v", err)
		}
		if !strings.Contains(string(content), "<!-- BEGIN TUTTI-RUNTIME") {
			t.Fatalf("cwd AGENTS.md missing managed block: %s", content)
		}
	})
}
