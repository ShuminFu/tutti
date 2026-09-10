package runtimeprep

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtensionRuntimePreparerWritesManagedHomeFileWithoutChangingUserHome(t *testing.T) {
	userHome := t.TempDir()
	userSoul := []byte("You are Personal Agent.\n")
	if err := os.WriteFile(filepath.Join(userHome, "SOUL.md"), userSoul, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_AGENT_HOME", userHome)

	prep := NewDefaultPreparer(t.TempDir())
	prep.CommandCatalog = staticCommandCatalog(nil)
	prepared, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:    "workspace",
		AgentSessionID: "session",
		AgentTargetID:  "extension:test-agent",
		Provider:       "acp:test-agent",
		Cwd:            t.TempDir(),
		ExtensionRuntimePrep: &ExtensionRuntimePrep{Home: &ExtensionRuntimeHome{
			EnvVar:       "TEST_AGENT_HOME",
			DirName:      "test-agent",
			SourceEnvVar: "TEST_AGENT_HOME",
			CopyFiles:    []string{"SOUL.md"},
			ManagedFiles: []ExtensionRuntimeManagedFile{{
				Path:    "SOUL.md",
				Content: "You are DinTalDock Agent.\n",
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	sessionHome := preparedEnvValue(prepared.Env, "TEST_AGENT_HOME")
	got, err := os.ReadFile(filepath.Join(sessionHome, "SOUL.md"))
	if err != nil || string(got) != "You are DinTalDock Agent.\n" {
		t.Fatalf("managed SOUL.md = %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(userHome, "SOUL.md"))
	if err != nil || string(got) != string(userSoul) {
		t.Fatalf("user SOUL.md changed = %q, %v", got, err)
	}
}

func TestValidateExtensionRuntimePrepRejectsUnsafeManagedHomeFile(t *testing.T) {
	err := ValidateExtensionRuntimePrep(ExtensionRuntimePrep{Home: &ExtensionRuntimeHome{
		EnvVar:       "TEST_AGENT_HOME",
		DirName:      "test-agent",
		ManagedFiles: []ExtensionRuntimeManagedFile{{Path: "../SOUL.md"}},
	}})
	if err == nil {
		t.Fatal("unsafe managed file path must be rejected")
	}
}
