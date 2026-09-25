package runtimeprep

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Existing sandbox tests describe the isolated fallback. User-home tests opt in.
	os.Setenv(CodexHomeModeEnv, CodexHomeModeIsolated)
	os.Exit(m.Run())
}

func TestCodexUserHomeLeavesPersonalFilesUntouched(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	userHome := filepath.Join(home, ".codex")
	config := "model_provider = \"official\"\nservice_tier = \"flex\"\ndeveloper_instructions = \"keep-user-instructions\"\n"
	auth := "{\"token\":\"personal\"}\n"
	agents := "# personal agents\n"
	skill := "---\nname: personal\n---\nPersonal\n"
	plugin := "plugin-marker\n"
	writeSidecarTestFile(t, filepath.Join(userHome, "config.toml"), config)
	writeSidecarTestFile(t, filepath.Join(userHome, "auth.json"), auth)
	writeSidecarTestFile(t, filepath.Join(userHome, "AGENTS.md"), agents)
	writeSidecarTestFile(t, filepath.Join(userHome, "skills", "personal", "SKILL.md"), skill)
	writeSidecarTestFile(t, filepath.Join(userHome, "plugins", "cache", "marker.txt"), plugin)

	stateDir := t.TempDir()
	cwd := t.TempDir()
	projector := &countingAuthProjector{}
	preparer := newTestPreparer(stateDir)
	preparer.RegisterProvider(CodexPreparer{AuthProjector: projector})
	prepared, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session-user-home",
		Provider: "codex", Cwd: cwd,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if projector.calls != 0 {
		t.Fatalf("auth projector calls = %d, want 0", projector.calls)
	}
	if got := envValue(prepared.Env, "CODEX_HOME"); got != userHome {
		t.Fatalf("CODEX_HOME = %q, want %q", got, userHome)
	}
	if prepared.Cwd != cwd {
		t.Fatalf("cwd = %q, want %q", prepared.Cwd, cwd)
	}
	assertFileEquals(t, filepath.Join(userHome, "config.toml"), config)
	assertFileEquals(t, filepath.Join(userHome, "auth.json"), auth)
	assertFileEquals(t, filepath.Join(userHome, "AGENTS.md"), agents)
	assertFileEquals(t, filepath.Join(userHome, "skills", "personal", "SKILL.md"), skill)
	assertFileEquals(t, filepath.Join(userHome, "plugins", "cache", "marker.txt"), plugin)
	if _, err := os.Stat(filepath.Join(userHome, "rules", "default.rules")); !os.IsNotExist(err) {
		t.Fatalf("user approval rules appeared: %v", err)
	}
	root := testRuntimeRoot(t, stateDir, "session-user-home")
	instructions, err := os.ReadFile(filepath.Join(root, codexOverlayDirectory, "developer-instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(instructions), "keep-user-instructions") || !strings.Contains(string(instructions), "Mention Routing") {
		t.Fatalf("developer instructions = %q", instructions)
	}
	overrides := decodeStringListEnv(t, prepared.Env, CodexConfigOverridesEnv)
	if !containsString(overrides, "project_root_markers=[]") {
		t.Fatalf("overrides = %#v", overrides)
	}
	roots := decodeStringListEnv(t, prepared.Env, CodexExtraSkillRootsEnv)
	if len(roots) != 1 || roots[0] != filepath.Join(root, codexOverlayDirectory, "skills") {
		t.Fatalf("extra skill roots = %#v", roots)
	}
}

func TestCodexUserHomeSkillNameYieldsToUserSkill(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	userSkill := "---\nname: tutti-cli\n---\nUser tutti skill\n"
	writeSidecarTestFile(t, filepath.Join(home, ".codex", "skills", "tutti-cli", "SKILL.md"), userSkill)

	stateDir := t.TempDir()
	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session-skill-name",
		Provider: "codex", Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(home, ".codex", "skills", "tutti-cli", "SKILL.md"), userSkill)
	root := testRuntimeRoot(t, stateDir, "session-skill-name")
	overlaySkills := filepath.Join(root, codexOverlayDirectory, "skills")
	if _, err := os.Stat(filepath.Join(overlaySkills, "tutti-cli")); !os.IsNotExist(err) {
		t.Fatalf("overlay reused the user skill name: %v", err)
	}
	tuttiSkill, err := os.ReadFile(filepath.Join(overlaySkills, "tutti-cli-tutti", "SKILL.md"))
	if err != nil {
		t.Fatalf("tutti fallback skill missing: %v", err)
	}
	if !strings.Contains(string(tuttiSkill), "`tutti <scope> --help`") {
		t.Fatalf("tutti fallback skill = %q", tuttiSkill)
	}
	if envValue(prepared.Env, "CODEX_HOME") != filepath.Join(home, ".codex") {
		t.Fatalf("CODEX_HOME = %q", envValue(prepared.Env, "CODEX_HOME"))
	}
}

func TestCodexUserHomeSaverRoleStaysOutsideUserConfig(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	config := "model = \"user\"\n"
	writeSidecarTestFile(t, filepath.Join(home, ".codex", "config.toml"), config)

	stateDir := t.TempDir()
	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session-saver",
		Provider: "codex", Cwd: t.TempDir(), CodexSaverMode: true,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(home, ".codex", "config.toml"), config)
	if _, err := os.Stat(filepath.Join(home, ".codex", "agents", "luna_worker.toml")); !os.IsNotExist(err) {
		t.Fatalf("saver role written into user home: %v", err)
	}
	root := testRuntimeRoot(t, stateDir, "session-saver")
	rolePath := filepath.Join(root, codexOverlayDirectory, "agents", "luna_worker.toml")
	if _, err := os.Stat(rolePath); err != nil {
		t.Fatal(err)
	}
	overrides := decodeStringListEnv(t, prepared.Env, CodexConfigOverridesEnv)
	if !containsString(overrides, "agents.default.config_file="+strconv.Quote(rolePath)) {
		t.Fatalf("overrides = %#v, want role %s", overrides, rolePath)
	}
	instructions, err := os.ReadFile(filepath.Join(root, codexOverlayDirectory, "developer-instructions.md"))
	if err != nil || !strings.Contains(string(instructions), "Codex Saver Mode") {
		t.Fatalf("saver policy missing: %q err=%v", instructions, err)
	}
}

func TestCodexExplicitIsolatedHomeIgnoresUserModeEnv(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	writeSidecarTestFile(t, filepath.Join(home, ".codex", "config.toml"), "model = \"user\"\n")

	stateDir := t.TempDir()
	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session-explicit-isolated",
		Provider: "codex", Cwd: t.TempDir(), CodexHomeMode: CodexHomeModeIsolated,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	want := filepath.Join(testRuntimeRoot(t, stateDir, "session-explicit-isolated"), codexHomeDirectory)
	if got := envValue(prepared.Env, "CODEX_HOME"); got != want {
		t.Fatalf("CODEX_HOME = %q, want %q", got, want)
	}
	assertFileEquals(t, filepath.Join(home, ".codex", "config.toml"), "model = \"user\"\n")
}

func TestCodexModelEndpointStaysIsolatedWhenUserModeRequested(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	writeSidecarTestFile(t, filepath.Join(home, ".codex", "auth.json"), "{\"token\":\"personal\"}\n")
	writeSidecarTestFile(t, filepath.Join(home, ".codex", "config.toml"), "model_provider = \"chatgpt\"\n")

	stateDir := t.TempDir()
	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session-endpoint",
		Provider: "codex", Cwd: t.TempDir(),
		ModelEndpoint: &ModelEndpointConfig{
			PlanName: "DinTal Runtime LLM Proxy", Protocol: "openai",
			BaseURL: "http://127.0.0.1:18799/llmproxy/openai/v1", APIKey: "loopback",
			WireAPI: "responses", Model: "gpt-5",
		},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	codexHome := envValue(prepared.Env, "CODEX_HOME")
	want := filepath.Join(testRuntimeRoot(t, stateDir, "session-endpoint"), codexHomeDirectory)
	if codexHome != want {
		t.Fatalf("CODEX_HOME = %q, want %q", codexHome, want)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("personal auth reached isolated home: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `model_provider = "tutti-model-plan"`) || strings.Contains(string(config), "chatgpt") {
		t.Fatalf("isolated config = %s", config)
	}
	assertFileEquals(t, filepath.Join(home, ".codex", "config.toml"), "model_provider = \"chatgpt\"\n")
}

func TestCodexManagedHomeStaysIsolatedWhenUserModeRequested(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	template := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(template, []byte("model = \"managed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(managedCodexRuntimeEnv, "1")
	t.Setenv(managedCodexConfigTemplateEnv, template)

	stateDir := t.TempDir()
	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session-managed-user-env",
		Provider: "codex", Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	want := filepath.Join(testRuntimeRoot(t, stateDir, "session-managed-user-env"), codexHomeDirectory)
	if got := envValue(prepared.Env, "CODEX_HOME"); got != want {
		t.Fatalf("CODEX_HOME = %q, want %q", got, want)
	}
}

func TestCodexLegacyRolloutKeepsIsolatedHome(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	writeSidecarTestFile(t, filepath.Join(home, ".codex", "config.toml"), "model = \"user\"\n")

	stateDir := t.TempDir()
	const sessionID = "session-legacy-rollout"
	root := testRuntimeRoot(t, stateDir, sessionID)
	writeTestCodexRollout(t, filepath.Join(root, codexHomeDirectory, "sessions", "2026", "09", "25", "rollout-old.jsonl"), "old-thread")

	prepared, err := newTestPreparer(stateDir).Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: sessionID,
		Provider: "codex", Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	want := filepath.Join(root, codexHomeDirectory)
	if got := envValue(prepared.Env, "CODEX_HOME"); got != want {
		t.Fatalf("CODEX_HOME = %q, want %q", got, want)
	}
	assertFileEquals(t, filepath.Join(home, ".codex", "config.toml"), "model = \"user\"\n")
}

func TestCodexEmptyHomeModeUsesUserHome(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv(CodexHomeModeEnv, "")
	t.Setenv("CODEX_HOME", "")

	prepared, err := newTestPreparer(t.TempDir()).Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session-default-user",
		Provider: "codex", Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if got, want := envValue(prepared.Env, "CODEX_HOME"), filepath.Join(home, ".codex"); got != want {
		t.Fatalf("CODEX_HOME = %q, want %q", got, want)
	}
}

func TestCodexUserHomeConfigOverrides(t *testing.T) {
	priority := codexUserHomeConfigOverrides("service_tier = \"priority\"\n", PrepareInput{}, false, false)
	if !containsString(priority, `service_tier="fast"`) || !containsString(priority, "project_root_markers=[]") {
		t.Fatalf("priority overrides = %#v", priority)
	}
	for _, content := range []string{
		"service_tier = \"flex\"\n",
		"service_tier = \"fast\"\n",
		"service_tier = \"default\"\n",
		"service_tier = \"standard\"\n",
		"[profile]\nservice_tier = \"priority\"\n",
	} {
		overrides := codexUserHomeConfigOverrides(content, PrepareInput{}, false, false)
		if containsString(overrides, `service_tier="fast"`) {
			t.Fatalf("content %q produced tier override %#v", content, overrides)
		}
	}
	windows := codexUserHomeConfigOverrides("", PrepareInput{}, true, false)
	if !containsString(windows, `windows.sandbox="unelevated"`) {
		t.Fatalf("windows overrides = %#v", windows)
	}
	if containsString(codexUserHomeConfigOverrides("", PrepareInput{}, false, false), `windows.sandbox="unelevated"`) {
		t.Fatal("non-windows override pinned the sandbox")
	}
	budgeted := "max_context_tokens = 100\n"
	if got := codexUserHomeSkillsBudgetOverride("[skills]\n" + budgeted); got != "" {
		t.Fatalf("existing skills budget override = %q", got)
	}
	if got := codexUserHomeSkillsBudgetOverride(""); got != "skills.max_context_tokens=10000" {
		t.Fatalf("skills budget override = %q", got)
	}
	connector := codexUserHomeConfigOverrides("", PrepareInput{MCPServers: []MCPServerBinding{{
		Name: "connector", Type: "http", URL: "http://127.0.0.1:9/mcp",
		Headers: map[string]string{"Authorization": "Bearer test"},
	}}}, false, true)
	if !containsString(connector, `mcp_servers.connector.url="http://127.0.0.1:9/mcp"`) ||
		!containsString(connector, "features.apps=false") ||
		!containsString(connector, `mcp_servers.connector.http_headers={"Authorization" = "Bearer test"}`) {
		t.Fatalf("connector overrides = %#v", connector)
	}
}

func TestCodexUserHomeForkCopiesLegacyRollout(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	userHome := filepath.Join(home, "codex-user")
	t.Setenv("CODEX_HOME", userHome)
	config := "model = \"user\"\n"
	writeSidecarTestFile(t, filepath.Join(userHome, "config.toml"), config)

	stateDir := t.TempDir()
	preparer := NewDefaultPreparer(stateDir)
	store := LocalStore{StateDir: stateDir}
	sourceRoot, err := store.RuntimeRoot("workspace", "source-session")
	if err != nil {
		t.Fatal(err)
	}
	targetRoot, err := store.RuntimeRoot("workspace", "target-session")
	if err != nil {
		t.Fatal(err)
	}
	relativePath := filepath.Join("sessions", "2026", "09", "25", "rollout-2026-09-25T00-00-00-target-thread.jsonl")
	sourcePath := filepath.Join(sourceRoot, codexHomeDirectory, relativePath)
	writeTestCodexRollout(t, sourcePath, "target-thread")

	input := SessionForkProviderStateBindingInput{
		WorkspaceID:             "workspace",
		Provider:                "codex",
		SourceAgentSessionID:    "source-session",
		TargetAgentSessionID:    "target-session",
		SourceProviderSessionID: "source-thread",
		TargetProviderSessionID: "target-thread",
	}
	if err := preparer.BindSessionForkProviderState(context.Background(), input); err != nil {
		t.Fatalf("BindSessionForkProviderState() error = %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(userHome, relativePath))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != string(source) {
		t.Fatalf("copied rollout = %q", copied)
	}
	assertFileEquals(t, filepath.Join(userHome, "config.toml"), config)
	if _, err := os.Stat(filepath.Join(targetRoot, codexHomeDirectory, relativePath)); !os.IsNotExist(err) {
		t.Fatalf("fork copied into the target per-session home: %v", err)
	}
	if err := os.WriteFile(sourcePath, append(source, []byte("{\"type\":\"event_msg\"}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := preparer.BindSessionForkProviderState(context.Background(), input); err != nil {
		t.Fatalf("second bind error = %v", err)
	}
	again, err := os.ReadFile(filepath.Join(userHome, relativePath))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(source) {
		t.Fatal("second bind rewrote the shared rollout")
	}
}

func TestCodexUserHomeForkLeavesExistingRollout(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	useCodexUserHome(t)
	userHome := filepath.Join(home, "codex-user")
	t.Setenv("CODEX_HOME", userHome)
	config := "model = \"user\"\n"
	writeSidecarTestFile(t, filepath.Join(userHome, "config.toml"), config)
	relativePath := filepath.Join("sessions", "2026", "09", "25", "rollout-2026-09-25T00-00-00-target-thread.jsonl")
	writeTestCodexRollout(t, filepath.Join(userHome, relativePath), "target-thread")
	original, err := os.ReadFile(filepath.Join(userHome, relativePath))
	if err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	sourceRoot, err := (LocalStore{StateDir: stateDir}).RuntimeRoot("workspace", "source-session")
	if err != nil {
		t.Fatal(err)
	}
	writeTestCodexRollout(t, filepath.Join(sourceRoot, codexHomeDirectory, relativePath), "target-thread")
	if err := os.WriteFile(filepath.Join(sourceRoot, codexHomeDirectory, relativePath), append(original, []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"extra\"}}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewDefaultPreparer(stateDir).BindSessionForkProviderState(context.Background(), SessionForkProviderStateBindingInput{
		WorkspaceID:             "workspace",
		Provider:                "codex",
		SourceAgentSessionID:    "source-session",
		TargetAgentSessionID:    "target-session",
		SourceProviderSessionID: "source-thread",
		TargetProviderSessionID: "target-thread",
	}); err != nil {
		t.Fatalf("BindSessionForkProviderState() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(userHome, relativePath), string(original))
	assertFileEquals(t, filepath.Join(userHome, "config.toml"), config)
}

func useCodexUserHome(t *testing.T) {
	t.Helper()
	t.Setenv(CodexHomeModeEnv, CodexHomeModeUser)
	t.Setenv("CODEX_HOME", "")
}

func testRuntimeRoot(t *testing.T, stateDir string, sessionID string) string {
	t.Helper()
	root, err := (LocalStore{StateDir: stateDir}).RuntimeRoot("workspace", sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func assertFileEquals(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func decodeStringListEnv(t *testing.T, env []string, key string) []string {
	t.Helper()
	var values []string
	if err := json.Unmarshal([]byte(envValue(env, key)), &values); err != nil {
		t.Fatalf("decode %s: %v", key, err)
	}
	return values
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
