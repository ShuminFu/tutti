package runtimeprep

import (
	"os"
	"path/filepath"
	"testing"
)

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
