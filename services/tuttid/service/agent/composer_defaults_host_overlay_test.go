package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

// Codex on a company gateway advertises host overlay models (and 1M rows) in
// the picker, but defaults used to validate the native CLI catalog. That is
// how deepseek-flash[1m] was offered then refused as invalid_value.

func TestValidateAgentComposerDefaultsPatchAcceptsHostOverlayOneMillionModel(t *testing.T) {
	installCodexHostOverlayWithDeepSeekFlash(t, true)
	service := newCodexNativeCatalogService(t)

	for _, model := range []string{"deepseek-flash", "deepseek-flash[1m]", "deepseek-flash[1M]"} {
		reason, err := validateCodexModelDefault(service, model)
		if err != nil {
			t.Fatalf("ValidateAgentComposerDefaultsPatch(%q) error = %v", model, err)
		}
		if reason != "" {
			t.Fatalf("ValidateAgentComposerDefaultsPatch(%q) reason = %q, want applied", model, reason)
		}
	}
}

func TestValidateAgentComposerDefaultsPatchAcceptsHostOverlayMarkedModelWithoutWindowTable(t *testing.T) {
	// Session create accepts X[1m] whenever X is in the overlay, even when the
	// host did not publish a 1M row. Defaults must not be stricter.
	installCodexHostOverlayWithDeepSeekFlash(t, false)
	service := newCodexNativeCatalogService(t)

	reason, err := validateCodexModelDefault(service, "deepseek-flash[1m]")
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() error = %v", err)
	}
	if reason != "" {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() reason = %q, want applied", reason)
	}
}

func TestValidateAgentComposerDefaultsPatchRejectsNativeOnlyModelWhenHostOverlayOwnsPicker(t *testing.T) {
	installCodexHostOverlayWithDeepSeekFlash(t, true)
	service := newCodexNativeCatalogService(t)

	reason, err := validateCodexModelDefault(service, "gpt-native")
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() error = %v", err)
	}
	if reason != AgentComposerDefaultsReasonInvalidValue {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() reason = %q, want %q", reason, AgentComposerDefaultsReasonInvalidValue)
	}
}

func TestValidateAgentComposerDefaultsPatchStillValidatesNativeCatalogWithoutHostOverlay(t *testing.T) {
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, "")
	t.Setenv(runtimeprep.HostModelEndpointsEnv, "")
	service := newCodexNativeCatalogService(t)

	reason, err := validateCodexModelDefault(service, "gpt-native")
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch(gpt-native) error = %v", err)
	}
	if reason != "" {
		t.Fatalf("ValidateAgentComposerDefaultsPatch(gpt-native) reason = %q, want applied", reason)
	}

	reason, err = validateCodexModelDefault(service, "deepseek-flash[1m]")
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch(deepseek-flash[1m]) error = %v", err)
	}
	if reason != AgentComposerDefaultsReasonInvalidValue {
		t.Fatalf("ValidateAgentComposerDefaultsPatch(deepseek-flash[1m]) reason = %q, want %q", reason, AgentComposerDefaultsReasonInvalidValue)
	}
}

func TestAdvertisedComposerModelValuesSkipRequestedBootstrapEcho(t *testing.T) {
	t.Parallel()
	got := advertisedComposerModelValues([]ComposerConfigOptionValue{
		{Value: "echoed", Requested: true},
		{Value: "deepseek-flash"},
		{ID: "deepseek-flash[1m]", Value: "deepseek-flash[1m]"},
		{Value: "deepseek-flash"},
		{ID: "id-only"},
	})
	want := []string{"deepseek-flash", "deepseek-flash[1m]", "id-only"}
	if len(got) != len(want) {
		t.Fatalf("advertised = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("advertised = %#v, want %#v", got, want)
		}
	}
}

func TestComposerModelIDInCatalogTreatsOneMillionMarkerAsBareID(t *testing.T) {
	t.Parallel()
	available := []string{"deepseek-flash", "gpt-native"}
	for _, model := range []string{"deepseek-flash", "deepseek-flash[1m]", "deepseek-flash[1M]"} {
		if !composerModelIDInCatalog(model, available) {
			t.Fatalf("composerModelIDInCatalog(%q) = false, want true", model)
		}
	}
	if composerModelIDInCatalog("gpt-6-astra", available) {
		t.Fatal("composerModelIDInCatalog(gpt-6-astra) = true, want false")
	}
}

func newCodexNativeCatalogService(t *testing.T) *Service {
	t.Helper()
	service := newTestService(newFakeRuntime())
	service.ModelCatalog = &recordingModelCatalog{}
	return service
}

func validateCodexModelDefault(service *Service, model string) (string, error) {
	result, err := service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		agenttargetbiz.IDLocalCodex,
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldModel: &model,
		},
	)
	if err != nil {
		return "", err
	}
	for _, rejected := range result.Rejected {
		return rejected.ReasonCode, nil
	}
	return "", nil
}

func installCodexHostOverlayWithDeepSeekFlash(t *testing.T, declareOneMillionWindow bool) {
	t.Helper()
	payload := `{
		"version":1,
		"providers":{
			"codex":{
				"planName":"DinTal Runtime LLM Proxy",
				"protocol":"openai",
				"baseURL":"http://127.0.0.1:18799/llmproxy/openai/v1",
				"apiKey":"loopback",
				"wireAPI":"responses",
				"model":"gpt-6-astra",
				"models":[
					{"id":"gpt-6-astra","name":"GPT-6 Astra"},
					{"id":"deepseek-flash","name":"DeepSeek Flash"}
				]
			}
		}`
	if declareOneMillionWindow {
		payload += `,"modelContext":{"deepseek-flash":1000000}`
	}
	payload += `}`
	path := filepath.Join(t.TempDir(), "host-model-endpoints.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, path)
	t.Setenv(runtimeprep.HostModelEndpointsEnv, "")
}
