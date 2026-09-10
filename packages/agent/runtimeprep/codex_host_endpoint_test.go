package runtimeprep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type countingAuthProjector struct {
	calls int
}

func (p *countingAuthProjector) Project(context.Context, AuthFileProjection) (func(context.Context) error, error) {
	p.calls++
	return nil, nil
}

func TestCodexHostEndpointDoesNotExposePersonalCredentials(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	personalHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(personalHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(personalHome, "auth.json"), []byte(`{"tokens":{"access_token":"personal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(personalHome, "config.toml"), []byte(`model_provider = "chatgpt"`), 0o600); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	projector := &countingAuthProjector{}
	preparer := newTestPreparer(stateDir)
	preparer.RegisterProvider(CodexPreparer{AuthProjector: projector})
	result, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session", AgentTargetID: "local:codex", Provider: "codex", Cwd: t.TempDir(),
		ModelEndpoint: &ModelEndpointConfig{PlanName: "DinTal Runtime LLM Proxy", Protocol: "openai", BaseURL: "http://127.0.0.1:18799/llmproxy/openai/v1", APIKey: "loopback", WireAPI: "responses", Model: "gpt-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projector.calls != 0 {
		t.Fatalf("personal auth projector calls = %d, want 0", projector.calls)
	}
	codexHome := envValue(result.Env, "CODEX_HOME")
	if _, err := os.Lstat(filepath.Join(codexHome, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("personal auth reached managed Codex home: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(content)
	if !strings.Contains(config, `model_provider = "tutti-model-plan"`) || !strings.Contains(config, `base_url = "http://127.0.0.1:18799/llmproxy/openai/v1"`) || strings.Contains(config, "chatgpt") {
		t.Fatalf("managed config = %s", config)
	}
}
