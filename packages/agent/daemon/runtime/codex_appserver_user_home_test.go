package agentruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestCodexStartAppliesUserHomeOverridesBeforeThread(t *testing.T) {
	t.Parallel()

	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	root := t.TempDir()
	instructionsPath := filepath.Join(root, "developer-instructions.md")
	if err := os.WriteFile(instructionsPath, []byte("dock policy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillRoot := filepath.Join(root, "skills")
	overrides, err := json.Marshal([]string{"project_root_markers=[]"})
	if err != nil {
		t.Fatal(err)
	}
	extraRoots, err := json.Marshal([]string{skillRoot})
	if err != nil {
		t.Fatal(err)
	}
	session := testAppServerSession()
	session.Env = []string{
		"SESSION_ENV=1",
		runtimeprep.CodexConfigOverridesEnv + "=" + string(overrides),
		runtimeprep.CodexDeveloperInstructionsFileEnv + "=" + instructionsPath,
		runtimeprep.CodexExtraSkillRootsEnv + "=" + string(extraRoots),
	}
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(transport.specs) != 1 {
		t.Fatalf("process starts = %d, want 1", len(transport.specs))
	}
	wantCommand := []string{"codex", "-c", "project_root_markers=[]", "app-server"}
	if strings.Join(transport.specs[0].Command, "\x00") != strings.Join(wantCommand, "\x00") {
		t.Fatalf("command = %#v", transport.specs[0].Command)
	}
	for _, key := range []string{
		runtimeprep.CodexConfigOverridesEnv,
		runtimeprep.CodexDeveloperInstructionsFileEnv,
		runtimeprep.CodexExtraSkillRootsEnv,
	} {
		if _, found := lastEnvironmentValue(transport.specs[0].Env, key); found {
			t.Fatalf("internal %s leaked to child env", key)
		}
	}
	if !containsString(transport.specs[0].Env, "SESSION_ENV=1") {
		t.Fatalf("process env lost ordinary session env: %#v", transport.specs[0].Env)
	}
	params := appServerRequestParams(t, transport.conn, appServerMethodSkillsExtraRootsSet)
	if got := appServerStringSlice(params["extraRoots"]); len(got) != 1 || got[0] != skillRoot {
		t.Fatalf("skills/extraRoots/set roots = %#v", got)
	}
	methods := appServerSentMethods(t, transport.conn)
	assertAppServerMethodOrder(t, methods,
		appServerMethodInitialize,
		appServerMethodInitialized,
		appServerMethodSkillsExtraRootsSet,
		appServerMethodThreadStart,
	)
	thread := appServerRequestParams(t, transport.conn, appServerMethodThreadStart)
	if thread["developerInstructions"] != "dock policy" {
		t.Fatalf("developerInstructions = %#v", thread["developerInstructions"])
	}
}

func TestCodexResumeCarriesDeveloperInstructions(t *testing.T) {
	t.Parallel()

	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	instructionsPath := filepath.Join(t.TempDir(), "developer-instructions.md")
	if err := os.WriteFile(instructionsPath, []byte("dock policy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	session.Env = []string{runtimeprep.CodexDeveloperInstructionsFileEnv + "=" + instructionsPath}
	if err := adapter.Resume(context.Background(), session); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	thread := appServerRequestParams(t, transport.conn, appServerMethodThreadResume)
	if thread["developerInstructions"] != "dock policy" {
		t.Fatalf("developerInstructions = %#v", thread["developerInstructions"])
	}
	if _, found := lastEnvironmentValue(transport.specs[0].Env, runtimeprep.CodexDeveloperInstructionsFileEnv); found {
		t.Fatal("developer instructions path leaked to child env")
	}
}

func TestCodexConfigOverrideFailureStopsBeforeSpawn(t *testing.T) {
	t.Parallel()

	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.Env = []string{runtimeprep.CodexConfigOverridesEnv + "=not-json"}
	if _, err := adapter.Start(context.Background(), session); err == nil {
		t.Fatal("Start error = nil, want override decode failure")
	}
	if len(transport.specs) != 0 {
		t.Fatalf("process starts = %d, want validation before spawn", len(transport.specs))
	}
}

func TestCodexTurnAppendsDockDeveloperInstructionsToCollaborationMode(t *testing.T) {
	t.Parallel()

	instructionsPath := filepath.Join(t.TempDir(), "developer-instructions.md")
	if err := os.WriteFile(instructionsPath, []byte("dock policy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := testAppServerSession()
	session.Settings = &SessionSettings{Model: "gpt-5"}
	session.Env = []string{runtimeprep.CodexDeveloperInstructionsFileEnv + "=" + instructionsPath}
	mask := map[string]any{
		"mode":                   "default",
		"model":                  "gpt-5",
		"developer_instructions": "base instructions",
	}
	params := appServerTurnStartParams(session, "thread-1", nil, nil, mask, "", "", "", false)
	mode, _ := params["collaborationMode"].(map[string]any)
	settings, _ := mode["settings"].(map[string]any)
	got := asString(settings["developer_instructions"])
	if !strings.Contains(got, "base instructions") || !strings.Contains(got, "dock policy") {
		t.Fatalf("collaboration developer_instructions = %q", got)
	}
}
