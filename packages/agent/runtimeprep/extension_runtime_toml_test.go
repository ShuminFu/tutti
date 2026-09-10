package runtimeprep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeTOMLNestedTablesBoolIntAndEscapes(t *testing.T) {
	doc := tomlTable{
		"cli":      tomlTable{"auto_update": false},
		"features": tomlTable{"telemetry": false},
		"models": tomlTable{
			"default": "gpt-5.5",
			"count":   int64(2),
		},
		"model": tomlTable{
			`foo"bar\baz`: tomlTable{
				"model":    `foo"bar\baz`,
				"base_url": `http://127.0.0.1:18799/llmproxy/grok/v1/"quoted"\path`,
				"enabled":  true,
				"window":   int64(200000),
			},
		},
	}
	first := encodeTOML(doc)
	second := encodeTOML(doc)
	if first != second {
		t.Fatalf("TOML encoding is unstable:\n%s\n---\n%s", first, second)
	}
	for _, want := range []string{
		"[cli]\nauto_update = false\n",
		"[features]\ntelemetry = false\n",
		"[models]\ncount = 2\ndefault = \"gpt-5.5\"\n",
		`[model."foo\"bar\\baz"]`,
		`model = "foo\"bar\\baz"`,
		`base_url = "http://127.0.0.1:18799/llmproxy/grok/v1/\"quoted\"\\path"`,
		"enabled = true\n",
		"window = 200000\n",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("TOML missing %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, `foo"bar\baz`) && !strings.Contains(first, `foo\"bar\\baz`) {
		t.Fatalf("TOML left quote/backslash unescaped:\n%s", first)
	}
}

func grokExtensionRuntimePrep() *ExtensionRuntimePrep {
	return &ExtensionRuntimePrep{
		Home: &ExtensionRuntimeHome{
			EnvVar:                           "GROK_HOME",
			DirName:                          "grok",
			SourceEnvVar:                     "GROK_HOME",
			ConfigFile:                       "config.toml",
			ConfigFormat:                     "toml",
			IncludeUserHomeDir:               false,
			SourceDefaultRel:                 ".grok",
			PassthroughSourceWithoutEndpoint: true,
			ConfigValues: map[string]any{
				"cli":      map[string]any{"auto_update": false},
				"features": map[string]any{"telemetry": false},
			},
		},
		ModelEndpoint: &ExtensionModelEndpoint{
			Protocol:           "openai",
			WireAPI:            "chat",
			WireAPIConfigValue: "chat_completions",
			APIKeyEnv:          "DINTAL_LLM_KEY",
			ConfigKeys: ExtensionModelEndpointConfigKeys{
				Model:     []string{"models", "default"},
				BaseURL:   []string{"model", "base_url"},
				APIKeyEnv: []string{"model", "env_key"},
				WireAPI:   []string{"model", "api_backend"},
				Models:    []string{"model"},
			},
		},
	}
}

func TestExtensionRuntimePreparerWritesGrokTOMLFromHostLoopback(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("GROK_HOME", userHome)
	t.Setenv("XAI_API_KEY", "personal-xai-secret")
	if err := os.WriteFile(filepath.Join(userHome, "config.toml"), []byte("secret = \"should-not-copy\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	const escapedModel = `grok-"quote"\id`
	const escapedBase = `http://127.0.0.1:18799/llmproxy/grok/v1/"quoted"\path`
	prep := NewDefaultPreparer(t.TempDir())
	prep.CommandCatalog = staticCommandCatalog(nil)
	prepared, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:          "workspace",
		AgentSessionID:       "session-grok",
		AgentTargetID:        "extension:grok",
		Provider:             "acp:grok",
		Cwd:                  t.TempDir(),
		ExtensionRuntimePrep: grokExtensionRuntimePrep(),
		ModelEndpoint: &ModelEndpointConfig{
			Protocol: "openai",
			WireAPI:  "chat",
			BaseURL:  escapedBase + "/",
			APIKey:   "loopback-grok-key",
			Model:    escapedModel,
			Models: []ModelEndpointModel{
				{ID: escapedModel, Name: `Grok "Quote"\Name`},
				{ID: "gpt-5.5", Name: "GPT-5.5"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	sessionHome := preparedEnvValue(prepared.Env, "GROK_HOME")
	if sessionHome == "" || sessionHome == userHome {
		t.Fatalf("GROK_HOME = %q, want isolated session home (not %q); env=%v", sessionHome, userHome, prepared.Env)
	}
	config, err := os.ReadFile(filepath.Join(sessionHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(config)
	for _, want := range []string{
		"[cli]\nauto_update = false\n",
		"[features]\ntelemetry = false\n",
		`[model."grok-\"quote\"\\id"]`,
		`[model."gpt-5.5"]`,
		`base_url = "http://127.0.0.1:18799/llmproxy/grok/v1/\"quoted\"\\path"`,
		`env_key = "DINTAL_LLM_KEY"`,
		`api_backend = "chat_completions"`,
		`default = "grok-\"quote\"\\id"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("config.toml missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "loopback-grok-key") || strings.Contains(body, "personal-xai-secret") {
		t.Fatalf("config.toml persisted a secret:\n%s", body)
	}
	if strings.Contains(body, escapedBase) {
		t.Fatalf("config.toml left base URL unescaped:\n%s", body)
	}
	if got := preparedEnvValue(prepared.Env, "DINTAL_LLM_KEY"); got != "loopback-grok-key" {
		t.Fatalf("DINTAL_LLM_KEY = %q; env=%v", got, prepared.Env)
	}
	if got := preparedEnvValue(prepared.Env, "XAI_API_KEY"); got != "" {
		t.Fatalf("XAI_API_KEY leaked into prepared env: %q", got)
	}
}

func TestExtensionRuntimePreparerPassesThroughUserGrokHomeWithoutHostEndpoint(t *testing.T) {
	userHome := filepath.Join(t.TempDir(), "user-grok")
	if err := os.MkdirAll(userHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userHome, "config.toml"), []byte("[model.personal]\nbase_url = \"https://personal.example/v1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_HOME", userHome)
	t.Setenv("XAI_API_KEY", "personal-xai-secret")

	runtimeRoot := t.TempDir()
	prep := NewDefaultPreparer(runtimeRoot)
	prep.CommandCatalog = staticCommandCatalog(nil)
	prepared, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:          "workspace",
		AgentSessionID:       "session-personal",
		AgentTargetID:        "extension:grok",
		Provider:             "acp:grok",
		Cwd:                  t.TempDir(),
		ExtensionRuntimePrep: grokExtensionRuntimePrep(),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	// No gateway endpoint: the user's own GROK_HOME rides through untouched
	// (personal credentials keep working), and no session config is written.
	if got := preparedEnvValue(prepared.Env, "GROK_HOME"); got != userHome {
		t.Fatalf("GROK_HOME = %q, want passthrough of user home %q; env=%v", got, userHome, prepared.Env)
	}
	if got := preparedEnvValue(prepared.Env, "DINTAL_LLM_KEY"); got != "" {
		t.Fatalf("DINTAL_LLM_KEY injected without a host endpoint: %q", got)
	}
	if body, err := os.ReadFile(filepath.Join(userHome, "config.toml")); err != nil || !strings.Contains(string(body), "personal.example") {
		t.Fatalf("user config.toml was touched: %v %q", err, string(body))
	}
	matches, _ := filepath.Glob(filepath.Join(runtimeRoot, "*", "*", "grok", "config.toml"))
	if len(matches) != 0 {
		t.Fatalf("session config written despite passthrough: %v", matches)
	}
}

func TestExtensionRuntimePreparerIsolatesGrokHomeWhenNoUserHomeExists(t *testing.T) {
	// Passthrough is opt-in on a *present* personal home. With neither
	// GROK_HOME nor ~/.grok the runtime still gets an isolated session home.
	t.Setenv("GROK_HOME", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XAI_API_KEY", "personal-xai-secret")

	prep := NewDefaultPreparer(t.TempDir())
	prep.CommandCatalog = staticCommandCatalog(nil)
	prepared, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:          "workspace",
		AgentSessionID:       "session-fresh",
		AgentTargetID:        "extension:grok",
		Provider:             "acp:grok",
		Cwd:                  t.TempDir(),
		ExtensionRuntimePrep: grokExtensionRuntimePrep(),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	sessionHome := preparedEnvValue(prepared.Env, "GROK_HOME")
	if sessionHome == "" || strings.HasPrefix(sessionHome, os.Getenv("HOME")) {
		t.Fatalf("GROK_HOME = %q, want isolated session home; env=%v", sessionHome, prepared.Env)
	}
	if got := preparedEnvValue(prepared.Env, "DINTAL_LLM_KEY"); got != "" {
		t.Fatalf("DINTAL_LLM_KEY injected without a host endpoint: %q", got)
	}
}

func TestValidateExtensionRuntimePrepAcceptsGrokTOML(t *testing.T) {
	if err := ValidateExtensionRuntimePrep(*grokExtensionRuntimePrep()); err != nil {
		t.Fatalf("ValidateExtensionRuntimePrep(grok) error = %v", err)
	}
}
