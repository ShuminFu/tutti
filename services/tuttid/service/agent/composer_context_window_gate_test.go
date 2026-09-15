package agent

import (
	"reflect"
	"testing"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// The 1M rows are gated on the host's window table, and these tests are the
// contract for that gate: a marked row appears only for a model the host
// published as a >= 1M window, never from the composer's own guess. The sibling
// file (composer_context_window_options_test.go) covers how a row is spelled
// once the gate has admitted it.
//
// These tests cannot call t.Parallel: the host contract arrives through the
// environment (see setHostModelEndpointContractWithModelContext).

// TestContextWindowModelVariantsRequireHostWindow is the P0 shape: the gateway
// catalog advertises deepseek-flash, the CLI was never told a 1M window for it,
// and the composer used to offer `deepseek-flash[1m]` anyway -- a row that
// resolves to plain 200k on the receiving side.
func TestContextWindowModelVariantsRequireHostWindow(t *testing.T) {
	setHostModelEndpointContractWithModelContext(t, "claude-code", "anthropic", map[string]int64{
		"claude-opus-4-6": 1_000_000,
		"glm-5.3":         400_000, // known, but not a 1M window
		"default":         1_000_000,
	})
	options := ComposerOptions{
		Provider: "claude-code",
		ModelConfig: ComposerConfigOption{
			Configurable: true,
			Options: []ComposerConfigOptionValue{
				{ID: "claude-opus-4-6", Label: "Opus", Value: "claude-opus-4-6"},
				{ID: "deepseek-flash", Label: "DeepSeek Flash", Value: "deepseek-flash"},
				{ID: "glm-5.3", Label: "GLM-5.3", Value: "glm-5.3"},
				{ID: "default", Label: "Default", Value: "default"},
			},
		},
	}

	got := withContextWindowModelVariants("claude-code", options)

	want := []ComposerConfigOptionValue{
		{ID: "claude-opus-4-6", Label: "Opus", Value: "claude-opus-4-6"},
		{ID: "deepseek-flash", Label: "DeepSeek Flash", Value: "deepseek-flash"},
		{ID: "glm-5.3", Label: "GLM-5.3", Value: "glm-5.3"},
		{ID: "default", Label: "Default", Value: "default"},
		{ID: "claude-opus-4-6[1m]", Label: "claude-opus-4-6[1m]", Value: "claude-opus-4-6[1m]"},
	}
	if !reflect.DeepEqual(got.ModelConfig.Options, want) {
		t.Fatalf("model options = %#v, want %#v", got.ModelConfig.Options, want)
	}
}

// A catalog with no published windows at all gets no marked rows, and that
// includes the case of no host contract whatsoever: guessing means promising a
// window nobody vouched for, which is the failure that costs the most (the
// receiving runtime keeps the id and the session runs at 200k, or rejects the
// value outright).
func TestContextWindowModelVariantsWithoutHostTableSpellNothing(t *testing.T) {
	// Pin the absence explicitly: this suite reads the contract from the
	// environment, and an injected document (a developer shell inside a host
	// deployment) would otherwise decide the outcome of a test about its absence.
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, "")
	t.Setenv(runtimeprep.HostModelEndpointsEnv, "")
	options := ComposerOptions{
		Provider: "claude-code",
		ModelConfig: ComposerConfigOption{
			Options: []ComposerConfigOptionValue{
				{ID: "claude-opus-4-6", Label: "Opus", Value: "claude-opus-4-6"},
			},
		},
	}

	got := withContextWindowModelVariants("claude-code", options)

	if !reflect.DeepEqual(got.ModelConfig.Options, options.ModelConfig.Options) {
		t.Fatalf("model options = %#v, want the catalog untouched", got.ModelConfig.Options)
	}

	// An empty table inside an otherwise valid document behaves the same way.
	setHostModelEndpointContract(t, "claude-code", "anthropic")
	got = withContextWindowModelVariants("claude-code", options)
	if !reflect.DeepEqual(got.ModelConfig.Options, options.ModelConfig.Options) {
		t.Fatalf("model options = %#v, want the catalog untouched", got.ModelConfig.Options)
	}
}

// The runtime context projection carries the same rows, so a gated row must be
// gated in both places: the activity adapter reads the runtime snapshot back.
func TestContextWindowModelVariantsMirrorOnlyHostDeclaredRows(t *testing.T) {
	setHostModelEndpointContractWithModelContext(t, "claude-code", "anthropic", map[string]int64{
		"claude-opus-4-6": 1_000_000,
	})
	options := ComposerOptions{
		Provider: "claude-code",
		ModelConfig: ComposerConfigOption{
			Options: []ComposerConfigOptionValue{
				{ID: "claude-opus-4-6", Label: "Opus", Value: "claude-opus-4-6"},
				{ID: "deepseek-flash", Label: "DeepSeek Flash", Value: "deepseek-flash"},
			},
		},
		RuntimeContext: map[string]any{
			"configOptions": []map[string]any{
				{
					"id": "model",
					"options": []map[string]any{
						{"value": "claude-opus-4-6", "label": "Opus"},
						{"value": "deepseek-flash", "label": "DeepSeek Flash"},
					},
				},
			},
		},
	}

	got := withContextWindowModelVariants("claude-code", options)

	configOptions, ok := got.RuntimeContext["configOptions"].([]map[string]any)
	if !ok || len(configOptions) != 1 {
		t.Fatalf("configOptions = %#v", got.RuntimeContext["configOptions"])
	}
	runtimeOptions, ok := configOptions[0]["options"].([]map[string]any)
	if !ok || len(runtimeOptions) != 3 {
		t.Fatalf("runtime options = %#v, want exactly one added row", configOptions[0]["options"])
	}
	if runtimeOptions[2]["value"] != "claude-opus-4-6[1m]" {
		t.Fatalf("runtime variant = %#v, want claude-opus-4-6[1m]", runtimeOptions[2])
	}
}
