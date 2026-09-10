package runtimeprep

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCodexConfigWithModelPlanEndpointRewritesProvider(t *testing.T) {
	t.Parallel()

	endpoint := &ModelEndpointConfig{
		PlanID:   "mp-1",
		PlanName: "Volc Coding Plan",
		Protocol: "openai",
		BaseURL:  "https://relay.example/v1",
		APIKey:   "sk-secret",
		WireAPI:  "responses",
		Model:    "seed-code",
	}
	content := "model = \"gpt-5\"\nmodel_provider = \"openai\"\n\n[tutti]\nconversationDetailMode = \"standard\"\n"
	next, changed := codexConfigWithModelPlanEndpoint(content, endpoint)
	if !changed {
		t.Fatalf("codexConfigWithModelPlanEndpoint() changed = false")
	}
	if !strings.Contains(next, "model_provider = \"tutti-model-plan\"") {
		t.Fatalf("model_provider not pinned:\n%s", next)
	}
	if strings.Contains(next, "model_provider = \"openai\"") {
		t.Fatalf("stale model_provider remains:\n%s", next)
	}
	if !strings.Contains(next, "model = \"seed-code\"") || strings.Contains(next, "model = \"gpt-5\"") {
		t.Fatalf("model not replaced:\n%s", next)
	}
	if !strings.Contains(next, "[model_providers.tutti-model-plan]") {
		t.Fatalf("provider table missing:\n%s", next)
	}
	if !strings.Contains(next, "env_key = \"TUTTI_MODEL_PLAN_API_KEY\"") {
		t.Fatalf("env_key missing:\n%s", next)
	}
	if !strings.Contains(next, "wire_api = \"responses\"") {
		t.Fatalf("responses wire_api missing:\n%s", next)
	}
	if strings.Contains(next, "sk-secret") {
		t.Fatalf("credential leaked into config:\n%s", next)
	}

	// Anthropic-protocol plans must not rewrite Codex config.
	if _, changed := codexConfigWithModelPlanEndpoint(content, &ModelEndpointConfig{Protocol: "anthropic", BaseURL: "https://x", APIKey: "k"}); changed {
		t.Fatalf("anthropic endpoint should not change codex config")
	}
}

func TestCodexConfigWithModelPlanEndpointPreservesWebSearch(t *testing.T) {
	t.Parallel()

	content := "web_search = \"live\"\n"
	next, changed := codexConfigWithModelPlanEndpoint(content, &ModelEndpointConfig{
		Protocol: "openai",
		BaseURL:  "https://relay.example/v1",
		APIKey:   "sk-secret",
		Model:    "seed-code",
	})
	if !changed {
		t.Fatalf("codexConfigWithModelPlanEndpoint() changed = false")
	}
	if !strings.Contains(next, "web_search = \"live\"") {
		t.Fatalf("web_search should be preserved:\n%s", next)
	}
}

func TestCodexConfigWithModelPlanEndpointPinsHostCatalog(t *testing.T) {
	t.Parallel()

	catalogPath := filepath.Join(t.TempDir(), "model_catalog.json")
	if err := os.WriteFile(catalogPath, []byte(`{"models":[]}`), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	endpoint := &ModelEndpointConfig{
		Protocol:         "openai",
		BaseURL:          "https://relay.example/v1",
		APIKey:           "sk-secret",
		Model:            "seed-code",
		ModelCatalogPath: catalogPath,
	}
	next, changed := codexConfigWithModelPlanEndpoint("model = \"gpt-5\"\n", endpoint)
	if !changed {
		t.Fatalf("codexConfigWithModelPlanEndpoint() changed = false")
	}
	want := "model_catalog_json = " + strconv.Quote(catalogPath)
	if !strings.Contains(next, want) {
		t.Fatalf("host catalog not pinned (%s):\n%s", want, next)
	}
	if strings.Index(next, "model_catalog_json") > strings.Index(next, "[model_providers.") {
		t.Fatalf("model_catalog_json must stay a top-level key:\n%s", next)
	}

	// A path Codex cannot read is fatal at startup and it does not fall back to
	// the bundled list, so the key must be omitted rather than written.
	for name, candidate := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "absent.json"),
		"relative":  "model_catalog.json",
		"directory": t.TempDir(),
	} {
		unusable := *endpoint
		unusable.ModelCatalogPath = candidate
		plain, _ := codexConfigWithModelPlanEndpoint("model = \"gpt-5\"\n", &unusable)
		if strings.Contains(plain, "model_catalog_json") {
			t.Fatalf("%s catalog path must be omitted from config:\n%s", name, plain)
		}
	}
}

func TestModelEndpointClaudeEnvSelectsAuthShape(t *testing.T) {
	t.Parallel()

	relay := modelEndpointClaudeEnv(&ModelEndpointConfig{
		Protocol: "anthropic",
		BaseURL:  "https://relay.example/api/anthropic",
		APIKey:   "sk-relay",
	})
	joined := strings.Join(relay, "\n")
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=https://relay.example/api/anthropic") {
		t.Fatalf("relay env = %v", relay)
	}
	if !strings.Contains(joined, "ANTHROPIC_AUTH_TOKEN=sk-relay") || !strings.HasSuffix(joined, "ANTHROPIC_API_KEY=") {
		t.Fatalf("relay should use bearer auth token: %v", relay)
	}

	official := modelEndpointClaudeEnv(&ModelEndpointConfig{
		Protocol: "anthropic",
		BaseURL:  "https://api.anthropic.com/v1",
		APIKey:   "sk-ant",
	})
	joined = strings.Join(official, "\n")
	if !strings.Contains(joined, "ANTHROPIC_API_KEY=sk-ant") || !strings.HasSuffix(joined, "ANTHROPIC_AUTH_TOKEN=") {
		t.Fatalf("official endpoint should use api key: %v", official)
	}
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=https://api.anthropic.com") {
		t.Fatalf("official base url should drop /v1 suffix: %v", official)
	}

	kimiCoding := modelEndpointClaudeEnv(&ModelEndpointConfig{
		Protocol: "anthropic",
		BaseURL:  "https://api.kimi.com/coding/",
		APIKey:   "sk-kimi",
	})
	joined = strings.Join(kimiCoding, "\n")
	if !strings.Contains(joined, "ANTHROPIC_API_KEY=sk-kimi") || !strings.HasSuffix(joined, "ANTHROPIC_AUTH_TOKEN=") {
		t.Fatalf("Kimi Coding should use api key auth: %v", kimiCoding)
	}
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=https://api.kimi.com/coding/") {
		t.Fatalf("Kimi Coding base url should be preserved: %v", kimiCoding)
	}

	if env := modelEndpointClaudeEnv(&ModelEndpointConfig{Protocol: "openai", BaseURL: "https://x", APIKey: "k"}); env != nil {
		t.Fatalf("openai endpoint should not produce claude env: %v", env)
	}
	if env := modelEndpointClaudeEnv(nil); env != nil {
		t.Fatalf("nil endpoint should not produce claude env: %v", env)
	}
}
