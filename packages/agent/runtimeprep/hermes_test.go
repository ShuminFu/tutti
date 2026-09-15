package runtimeprep

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func hermesRuntimePrep() *ExtensionRuntimePrep {
	return &ExtensionRuntimePrep{
		InstructionsFile: "AGENTS.md",
		Home: &ExtensionRuntimeHome{
			EnvVar:             "HERMES_HOME",
			DirName:            "hermes",
			SourceEnvVar:       "HERMES_HOME",
			SourceDefaultRel:   ".hermes",
			CopyFiles:          []string{"config.yaml", "auth.json", ".env"},
			ConfigFile:         "config.yaml",
			ConfigFormat:       "yaml",
			ExternalDirsKey:    []string{"skills", "external_dirs"},
			UserHomeSkillDir:   "skills",
			IncludeSkillRoots:  true,
			IncludeUserHomeDir: true,
		},
	}
}

// TestExtensionRuntimePreparerAddsDeclaredSkillRootsToSessionConfig 守住 hermes
// extension 的 skill 加载根因修复。hermes-agent 从 $HERMES_HOME/config.yaml 的
// skills.external_dirs 发现外部 skill，因此 extension runtime overlay 复用 extension
// profile 声明的 workspace root 推导 session-scoped root，把 tutti skills 稳定物化
// 到该 root，再把 root 加入 per-session config overlay。AGENTS.md 仍写到 cwd。
func TestExtensionRuntimePreparerPrepareTwiceIsIdempotent(t *testing.T) {
	globalHome := t.TempDir()
	t.Setenv("HERMES_HOME", globalHome)
	if err := os.WriteFile(filepath.Join(globalHome, "config.yaml"), []byte("skills:\n  external_dirs:\n    - \"/user/root\"\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	stateDir := t.TempDir()
	cwd := t.TempDir()
	prep := NewDefaultPreparer(stateDir)
	prep.CommandCatalog = staticCommandCatalog(nil)
	input := PrepareInput{
		WorkspaceID:          "workspace-1",
		AgentSessionID:       "session-1",
		AgentTargetID:        "local:hermes",
		Provider:             "acp:hermes",
		Cwd:                  cwd,
		ExtensionRuntimePrep: hermesRuntimePrep(),
		ExtensionSkillRoots:  []string{".agent_context/skills"},
	}
	if _, err := prep.Prepare(t.Context(), input); err != nil {
		t.Fatalf("first Prepare() error = %v", err)
	}
	runtimeRoot, err := LocalStore{StateDir: stateDir}.RuntimeRoot("workspace-1", "session-1")
	if err != nil {
		t.Fatalf("RuntimeRoot() error = %v", err)
	}
	sessionSkillRoot := filepath.Join(runtimeRoot, "extension-skills", ".agent_context", "skills")
	stalePath := filepath.Join(sessionSkillRoot, tuttiSkillName, "stale.txt")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale skill file: %v", err)
	}

	if _, err := prep.Prepare(t.Context(), input); err != nil {
		t.Fatalf("second Prepare() error = %v", err)
	}

	entries, err := os.ReadDir(sessionSkillRoot)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, skillName := range []string{tuttiHandoffSkillName, tuttiSkillName} {
		matches := 0
		for _, entry := range entries {
			if entry.IsDir() && (entry.Name() == skillName || strings.HasPrefix(entry.Name(), skillName+"-")) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("managed skill %q directory count = %d, want 1", skillName, matches)
		}
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale managed skill file should be replaced on retry, err=%v", err)
	}
	config, err := os.ReadFile(filepath.Join(runtimeRoot, "hermes", "config.yaml"))
	if err != nil {
		t.Fatalf("session config.yaml missing: %v", err)
	}
	if strings.Count(string(config), sessionSkillRoot) != 1 {
		t.Fatalf("session skill root should appear once after retry:\n%s", config)
	}
}

func TestExtensionRuntimePreparerIsolatesSkillRootsPerSession(t *testing.T) {
	globalHome := t.TempDir()
	t.Setenv("HERMES_HOME", globalHome)
	if err := os.WriteFile(filepath.Join(globalHome, "config.yaml"), []byte("model: test\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	stateDir := t.TempDir()
	cwd := t.TempDir()
	prep := NewDefaultPreparer(stateDir)
	prep.CommandCatalog = staticCommandCatalog(testCommandCapabilities())
	baseInput := PrepareInput{
		WorkspaceID:          "workspace-1",
		AgentTargetID:        "local:hermes",
		Provider:             "acp:hermes",
		Cwd:                  cwd,
		ExtensionRuntimePrep: hermesRuntimePrep(),
		ExtensionSkillRoots:  []string{".agent_context/skills"},
	}

	sessionRoots := map[string]string{}
	for _, sessionID := range []string{"session-a", "session-b"} {
		input := baseInput
		input.AgentSessionID = sessionID
		prepared, err := prep.Prepare(t.Context(), input)
		if err != nil {
			t.Fatalf("Prepare(%s) error = %v", sessionID, err)
		}
		hermesHome := preparedEnvValue(prepared.Env, "HERMES_HOME")
		if hermesHome == "" {
			t.Fatalf("Prepare(%s) missing HERMES_HOME: %v", sessionID, prepared.Env)
		}
		runtimeRoot, err := LocalStore{StateDir: stateDir}.RuntimeRoot("workspace-1", sessionID)
		if err != nil {
			t.Fatalf("RuntimeRoot(%s) error = %v", sessionID, err)
		}
		sessionSkillRoot := filepath.Join(runtimeRoot, "extension-skills", ".agent_context", "skills")
		sessionRoots[sessionID] = sessionSkillRoot

		config, err := os.ReadFile(filepath.Join(hermesHome, "config.yaml"))
		if err != nil {
			t.Fatalf("session %s config.yaml missing: %v", sessionID, err)
		}
		if !strings.Contains(string(config), sessionSkillRoot) {
			t.Fatalf("session %s config missing isolated skill root %q:\n%s", sessionID, sessionSkillRoot, config)
		}
		skill, err := os.ReadFile(filepath.Join(sessionSkillRoot, tuttiSkillName, "SKILL.md"))
		if err != nil {
			t.Fatalf("session %s tutti skill missing: %v", sessionID, err)
		}
		if !strings.Contains(string(skill), "Read the current AgentGUI session id from `$TUTTI_AGENT_SESSION_ID`") {
			t.Fatalf("session %s skill should resolve its session id at command time", sessionID)
		}
		if strings.Contains(string(skill), sessionID) {
			t.Fatalf("session %s skill should not embed its session id", sessionID)
		}
	}

	if sessionRoots["session-a"] == sessionRoots["session-b"] {
		t.Fatalf("sessions must not share extension skill roots: %v", sessionRoots)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agent_context", "skills")); !os.IsNotExist(err) {
		t.Fatalf("Hermes runtimePrep must not write shared cwd extension skill roots, err=%v", err)
	}
}

func TestExtensionRuntimePreparerMaterializesBrowserUseSkillWhenEnabled(t *testing.T) {
	t.Setenv("HERMES_HOME", t.TempDir())
	stateDir := t.TempDir()
	cwd := t.TempDir()
	prep := NewDefaultPreparer(stateDir)
	prep.CommandCatalog = staticCommandCatalog(testCommandCapabilities())
	if _, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:          "workspace-1",
		AgentSessionID:       "session-1",
		AgentTargetID:        "local:hermes",
		Provider:             "acp:hermes",
		Cwd:                  cwd,
		BrowserUse:           true,
		ExtensionRuntimePrep: hermesRuntimePrep(),
		ExtensionSkillRoots:  []string{".agent_context/skills"},
	}); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	runtimeRoot, err := LocalStore{StateDir: stateDir}.RuntimeRoot("workspace-1", "session-1")
	if err != nil {
		t.Fatalf("RuntimeRoot() error = %v", err)
	}
	skillPath := filepath.Join(runtimeRoot, "extension-skills", ".agent_context", "skills", browserUseSkillName, "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("browser-use SKILL.md missing in extension skill root: %v", err)
	}
}

func preparedEnvValue(env []string, name string) string {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func TestExtensionRuntimePreparerSkipsGlobalCopiesWhenHomeUnavailable(t *testing.T) {
	t.Setenv("HERMES_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	stateDir := t.TempDir()
	cwd := t.TempDir()
	for _, name := range []string{"config.yaml", "auth.json", ".env"} {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte("must-not-copy"), 0o600); err != nil {
			t.Fatalf("write cwd %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(cwd, "skills", "native"), 0o755); err != nil {
		t.Fatalf("write cwd skills: %v", err)
	}
	prep := NewDefaultPreparer(stateDir)
	prep.CommandCatalog = staticCommandCatalog(nil)
	prepared, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:          "workspace-1",
		AgentSessionID:       "session-1",
		AgentTargetID:        "local:hermes",
		Provider:             "acp:hermes",
		Cwd:                  cwd,
		ExtensionRuntimePrep: hermesRuntimePrep(),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	hermesHome := ""
	for _, env := range prepared.Env {
		if strings.HasPrefix(env, "HERMES_HOME=") {
			hermesHome = strings.TrimPrefix(env, "HERMES_HOME=")
		}
	}
	for _, name := range []string{"config.yaml", "auth.json", ".env"} {
		if _, err := os.Stat(filepath.Join(hermesHome, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not be copied from cwd when global home is unavailable, err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(hermesHome, "skills")); !os.IsNotExist(err) {
		t.Fatalf("cwd skills should not be copied into hermes home, err=%v", err)
	}
}

func TestACPExtensionWithoutRuntimePrepDoesNotUseProviderSpecificHome(t *testing.T) {
	t.Setenv("HERMES_HOME", t.TempDir())
	stateDir := t.TempDir()
	cwd := t.TempDir()
	prep := NewDefaultPreparer(stateDir)
	prep.CommandCatalog = staticCommandCatalog(nil)
	prepared, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:         "workspace-1",
		AgentSessionID:      "session-1",
		AgentTargetID:       "local:hermes",
		Provider:            "acp:hermes",
		Cwd:                 cwd,
		ExtensionSkillRoots: []string{".agent_context/skills"},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if slices.ContainsFunc(prepared.Env, func(env string) bool { return strings.HasPrefix(env, "HERMES_HOME=") }) {
		t.Fatalf("acp:hermes without runtime prep should not receive provider-specific env, env=%v", prepared.Env)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agent_context", "skills", tuttiSkillName, "SKILL.md")); err != nil {
		t.Fatalf("generic acp extension skill missing: %v", err)
	}
}

func TestMergeYAMLStringListPreservesUserPrecedenceAndDedupes(t *testing.T) {
	got, err := mergeYAMLStringList(`model: test
skills: {enabled: true, external_dirs: ["/user/first"]}
other: value
`, []string{"skills", "external_dirs"}, []string{"/tutti/root", "/user/first", "/home/.hermes/skills", "/tutti/root"})
	if err != nil {
		t.Fatalf("mergeYAMLStringList() error = %v", err)
	}
	var parsed struct {
		Skills struct {
			ExternalDirs []string `yaml:"external_dirs"`
		} `yaml:"skills"`
	}
	if err := yaml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("merged YAML should parse: %v\n%s", err, got)
	}
	want := []string{
		"/user/first",
		filepath.Clean(filepath.FromSlash("/tutti/root")),
		filepath.Clean(filepath.FromSlash("/home/.hermes/skills")),
	}
	if !slices.Equal(parsed.Skills.ExternalDirs, want) {
		t.Fatalf("external_dirs = %#v, want %#v\n%s", parsed.Skills.ExternalDirs, want, got)
	}
}

func TestMergeYAMLStringListRejectsInvalidConfig(t *testing.T) {
	if _, err := mergeYAMLStringList("skills: nope\n", []string{"skills", "external_dirs"}, []string{"/tutti/root"}); err == nil {
		t.Fatal("mergeYAMLStringList() error = nil, want invalid skills mapping error")
	}
}
