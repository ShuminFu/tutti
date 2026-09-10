package runtimeprep

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostModelEndpointReadsProviderScopedContract(t *testing.T) {
	t.Setenv(HostModelEndpointsEnv, `{"version":1,"providers":{"codex":{"planName":"DinTal LLM Proxy","protocol":"openai","baseURL":"http://127.0.0.1:18799/llmproxy/openai/v1/","apiKey":"loopback","wireAPI":"responses","model":"gpt-5","models":[{"id":"gpt-5","name":"GPT-5"},{"id":"gpt-5"}]}}}`)

	endpoint := HostModelEndpoint("codex")
	if endpoint == nil {
		t.Fatal("HostModelEndpoint(codex) = nil")
	}
	if endpoint.BaseURL != "http://127.0.0.1:18799/llmproxy/openai/v1" || endpoint.APIKey != "loopback" || endpoint.WireAPI != "responses" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
	if len(endpoint.Models) != 1 || endpoint.Models[0].ID != "gpt-5" {
		t.Fatalf("models = %#v", endpoint.Models)
	}
	if got := HostModelEndpoint("opencode"); got != nil {
		t.Fatalf("unconfigured provider = %#v, want nil", got)
	}
}

func TestHostModelEndpointPrefersUpdatablePrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-model-endpoints.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"providers":{"opencode":{"protocol":"openai","baseURL":"http://127.0.0.1:18799/llmproxy/opencode/v1","apiKey":"file-key","model":"gpt-5"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HostModelEndpointsFileEnv, path)
	t.Setenv(HostModelEndpointsEnv, `{"version":1,"providers":{"opencode":{"protocol":"openai","baseURL":"https://stale.example","apiKey":"stale"}}}`)
	endpoint := HostModelEndpoint("opencode")
	if endpoint == nil || endpoint.APIKey != "file-key" || endpoint.Model != "gpt-5" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
}

func TestHostModelEndpointRejectsInvalidContract(t *testing.T) {
	for _, value := range []string{
		`not-json`,
		`{"version":2,"providers":{"codex":{"protocol":"openai","baseURL":"http://127.0.0.1","apiKey":"key"}}}`,
		`{"version":1,"providers":{"codex":{"protocol":"unknown","baseURL":"http://127.0.0.1","apiKey":"key"}}}`,
	} {
		t.Setenv(HostModelEndpointsEnv, value)
		if got := HostModelEndpoint("codex"); got != nil {
			t.Fatalf("HostModelEndpoint() = %#v for %q, want nil", got, value)
		}
	}
}
