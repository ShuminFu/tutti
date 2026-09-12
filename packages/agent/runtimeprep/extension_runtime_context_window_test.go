package runtimeprep

import (
	"strings"
	"testing"
)

func deepSeekJSONDeclaration() *ExtensionModelEndpoint {
	return &ExtensionModelEndpoint{
		Protocol: "openai", WireAPI: "chat", WireAPIConfigValue: "chat_completions",
		APIKeyEnv: "DINTAL_DSH_GATEWAY_TOKEN", ProviderValue: "dintal-gateway",
		ConfigKeys: ExtensionModelEndpointConfigKeys{
			Provider: []string{"model", "provider"}, Model: []string{"model", "default"},
			BaseURL:   []string{"providers", "dintal-gateway", "api"},
			APIKeyEnv: []string{"providers", "dintal-gateway", "key_env"},
			WireAPI:   []string{"providers", "dintal-gateway", "transport"},
			Models:    []string{"gateway", "models"},
		},
	}
}

func deepSeekJSONEndpoint(contextWindow int64) *ModelEndpointConfig {
	return &ModelEndpointConfig{
		Protocol: "openai", WireAPI: "chat", BaseURL: "http://127.0.0.1:18799/v1",
		APIKey: "loopback-token", Model: "deepseek-chat", ContextWindow: contextWindow,
		Models: []ModelEndpointModel{
			{ID: "deepseek-chat", Name: "DeepSeek Chat"},
			{ID: "deepseek-reasoner"},
		},
	}
}

// The 1M window reaches a JSON runtime through its own per-model catalog entry,
// which is the only channel the standard ACP session model selection cannot
// carry.
func TestJSONModelCatalogCarriesTheContextWindowOnTheSelectedModel(t *testing.T) {
	t.Parallel()
	config, err := mergeJSONExtensionRuntimeConfig(
		"", deepSeekJSONEndpoint(1_000_000), deepSeekJSONDeclaration(),
	)
	if err != nil {
		t.Fatalf("mergeJSONExtensionRuntimeConfig() error = %v", err)
	}
	for _, want := range []string{
		`"id": "deepseek-chat"`,
		`"contextWindow": 1000000`,
		`"model": {`, `"default": "deepseek-chat"`,
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("JSON config missing %q:\n%s", want, config)
		}
	}
	// Only the selected model's entry carries the window: the runtime sizes its
	// compaction against the entry it resolves, not against the whole catalog.
	if strings.Count(config, "contextWindow") != 1 {
		t.Fatalf("contextWindow appears %d times, want only on the selected model:\n%s",
			strings.Count(config, "contextWindow"), config)
	}
}

func TestJSONModelCatalogOmitsTheWindowWhenNoneWasRequested(t *testing.T) {
	t.Parallel()
	config, err := mergeJSONExtensionRuntimeConfig(
		"", deepSeekJSONEndpoint(0), deepSeekJSONDeclaration(),
	)
	if err != nil {
		t.Fatalf("mergeJSONExtensionRuntimeConfig() error = %v", err)
	}
	if strings.Contains(config, "contextWindow") {
		t.Fatalf("JSON config advertises a window nobody asked for:\n%s", config)
	}
}

// A window for a model the catalog does not expose must not be invented onto
// some other entry.
func TestJSONModelCatalogIgnoresAWindowForAnUnlistedModel(t *testing.T) {
	t.Parallel()
	endpoint := deepSeekJSONEndpoint(1_000_000)
	endpoint.Model = "deepseek-not-in-catalog"
	config, err := mergeJSONExtensionRuntimeConfig("", endpoint, deepSeekJSONDeclaration())
	if err != nil {
		t.Fatalf("mergeJSONExtensionRuntimeConfig() error = %v", err)
	}
	if strings.Contains(config, "contextWindow") {
		t.Fatalf("JSON config attributed the window to an unlisted model:\n%s", config)
	}
}
