package runtimeprep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionRuntimePreparerInjectsDeclaredModelEndpointIntoSessionOnly(t *testing.T) {
	globalHome := t.TempDir()
	t.Setenv("TEST_AGENT_HOME", globalHome)
	globalConfig := "model:\n  provider: personal\n  default: personal-model\n  base_url: https://personal.example/v1\nkeep: unchanged\n"
	if err := os.WriteFile(filepath.Join(globalHome, "config.yaml"), []byte(globalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalHome, ".env"), []byte("OPENAI_API_KEY=personal-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimePrep := &ExtensionRuntimePrep{
		Home: &ExtensionRuntimeHome{
			EnvVar:       "TEST_AGENT_HOME",
			DirName:      "test-agent",
			SourceEnvVar: "TEST_AGENT_HOME",
			CopyFiles:    []string{"config.yaml", ".env"},
			ConfigFile:   "config.yaml",
			ConfigFormat: "yaml",
		},
		ModelEndpoint: &ExtensionModelEndpoint{
			Protocol:           "openai",
			WireAPI:            "chat",
			WireAPIConfigValue: "chat_completions",
			APIKeyEnv:          "OPENAI_API_KEY",
			ProviderValue:      "enterprise",
			ConfigKeys: ExtensionModelEndpointConfigKeys{
				Provider:  []string{"model", "provider"},
				Model:     []string{"model", "default"},
				BaseURL:   []string{"providers", "enterprise", "api"},
				APIKeyEnv: []string{"providers", "enterprise", "key_env"},
				WireAPI:   []string{"providers", "enterprise", "transport"},
			},
		},
	}
	stateDir := t.TempDir()
	prep := NewDefaultPreparer(stateDir)
	prep.CommandCatalog = staticCommandCatalog(nil)
	prepared, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:          "workspace",
		AgentSessionID:       "session-enterprise",
		AgentTargetID:        "extension:test-agent",
		Provider:             "acp:test-agent",
		Cwd:                  t.TempDir(),
		ExtensionRuntimePrep: runtimePrep,
		ModelEndpoint: &ModelEndpointConfig{
			Protocol: "openai",
			WireAPI:  "chat",
			BaseURL:  "http://127.0.0.1:18799/llmproxy/test-agent/v1/",
			APIKey:   "temporary-loopback-key",
			Model:    "enterprise-chat",
		},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	sessionHome := preparedEnvValue(prepared.Env, "TEST_AGENT_HOME")
	config, err := os.ReadFile(filepath.Join(sessionHome, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"provider: enterprise",
		"default: enterprise-chat",
		"enterprise:",
		"api: http://127.0.0.1:18799/llmproxy/test-agent/v1",
		"key_env: OPENAI_API_KEY",
		"transport: chat_completions",
		"keep: unchanged",
	} {
		if !strings.Contains(string(config), want) {
			t.Fatalf("session config missing %q:\n%s", want, config)
		}
	}
	if got := preparedEnvValue(prepared.Env, "OPENAI_API_KEY"); got != "temporary-loopback-key" {
		t.Fatalf("OPENAI_API_KEY = %q, want temporary key; env=%v", got, prepared.Env)
	}
	copiedDotEnv, err := os.ReadFile(filepath.Join(sessionHome, ".env"))
	if err != nil || string(copiedDotEnv) != "OPENAI_API_KEY=personal-key\n" {
		t.Fatalf("copied .env = %q, %v", copiedDotEnv, err)
	}
	unchanged, err := os.ReadFile(filepath.Join(globalHome, "config.yaml"))
	if err != nil || string(unchanged) != globalConfig {
		t.Fatalf("global config changed = %q, %v", unchanged, err)
	}

	personal, err := prep.Prepare(t.Context(), PrepareInput{
		WorkspaceID:          "workspace",
		AgentSessionID:       "session-personal",
		AgentTargetID:        "extension:test-agent",
		Provider:             "acp:test-agent",
		Cwd:                  t.TempDir(),
		ExtensionRuntimePrep: runtimePrep,
	})
	if err != nil {
		t.Fatalf("Prepare(personal) error = %v", err)
	}
	personalHome := preparedEnvValue(personal.Env, "TEST_AGENT_HOME")
	personalConfig, err := os.ReadFile(filepath.Join(personalHome, "config.yaml"))
	if err != nil || string(personalConfig) != globalConfig {
		t.Fatalf("personal session config = %q, %v", personalConfig, err)
	}
	if got := preparedEnvValue(personal.Env, "OPENAI_API_KEY"); got != "" {
		t.Fatalf("personal session should not inject an API key, got %q", got)
	}
}
