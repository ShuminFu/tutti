package agent

import (
	"reflect"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/contextwindow"
)

// baseModelOptions drops the 1M context variants withContextWindowModelVariants
// appends, so a test about where a model list came from can assert on the list
// itself. The transform has its own tests below.
func baseModelOptions(options []ComposerConfigOptionValue) []ComposerConfigOptionValue {
	trimmed := make([]ComposerConfigOptionValue, 0, len(options))
	for _, option := range options {
		if contextwindow.RequestsOneMillion(option.Value) {
			continue
		}
		trimmed = append(trimmed, option)
	}
	return trimmed
}

// baseRuntimeModelOptions is baseModelOptions for the runtime-context
// projection, which carries the same options as []map[string]any.
func baseRuntimeModelOptions(options []map[string]any) []map[string]any {
	trimmed := make([]map[string]any, 0, len(options))
	for _, option := range options {
		if contextwindow.RequestsOneMillion(stringFromAny(option["value"])) {
			continue
		}
		trimmed = append(trimmed, option)
	}
	return trimmed
}

func TestContextWindowModelVariantsSpellBareIDs(t *testing.T) {
	t.Parallel()
	options := ComposerOptions{
		Provider: "codex",
		ModelConfig: ComposerConfigOption{
			Configurable: true,
			Options: []ComposerConfigOptionValue{
				{ID: "gpt-5.6-sol", Label: "GPT-5.6 Sol", Value: "gpt-5.6-sol"},
				{ID: "glm-5.3", Label: "GLM-5.3", Value: "glm-5.3"},
			},
		},
	}

	got := withContextWindowModelVariants("codex", options)

	want := []ComposerConfigOptionValue{
		{ID: "gpt-5.6-sol", Label: "GPT-5.6 Sol", Value: "gpt-5.6-sol"},
		{ID: "glm-5.3", Label: "GLM-5.3", Value: "glm-5.3"},
		{ID: "gpt-5.6-sol[1m]", Label: "gpt-5.6-sol[1m]", Value: "gpt-5.6-sol[1m]"},
		{ID: "glm-5.3[1m]", Label: "glm-5.3[1m]", Value: "glm-5.3[1m]"},
	}
	if !reflect.DeepEqual(got.ModelConfig.Options, want) {
		t.Fatalf("model options = %#v, want %#v", got.ModelConfig.Options, want)
	}
	// The originals must survive untouched: the marker is an extra row, not a
	// rewrite of the choice the user already had.
	if !reflect.DeepEqual(baseModelOptions(got.ModelConfig.Options), options.ModelConfig.Options) {
		t.Fatalf("base options = %#v, want %#v", baseModelOptions(got.ModelConfig.Options), options.ModelConfig.Options)
	}
}

func TestContextWindowModelVariantsSkipBracketedValues(t *testing.T) {
	t.Parallel()
	// Cursor addresses models with its own bracket suffix and synthesizes a
	// "default[]" row; appending a second marker would offer rows that name
	// nothing.
	options := ComposerOptions{
		Provider: "cursor",
		ModelConfig: ComposerConfigOption{
			Options: []ComposerConfigOptionValue{
				{ID: "default[]", Label: "Auto", Value: "default[]"},
				{ID: "composer-2.5[fast=true]", Label: "Composer 2.5", Value: "composer-2.5[fast=true]"},
			},
		},
	}

	got := withContextWindowModelVariants("cursor", options)

	if !reflect.DeepEqual(got.ModelConfig.Options, options.ModelConfig.Options) {
		t.Fatalf("model options = %#v, want the bracketed values untouched", got.ModelConfig.Options)
	}
}

func TestContextWindowModelVariantsAreIdempotent(t *testing.T) {
	t.Parallel()
	options := ComposerOptions{
		Provider: "codex",
		ModelConfig: ComposerConfigOption{
			Options: []ComposerConfigOptionValue{
				{ID: "glm-5.3[1m]", Label: "glm-5.3 · 1M", Value: "glm-5.3[1m]"},
			},
		},
	}

	got := withContextWindowModelVariants("codex", options)

	if len(got.ModelConfig.Options) != 1 || got.ModelConfig.Options[0].Value != "glm-5.3[1m]" {
		t.Fatalf("model options = %#v, want no double marker", got.ModelConfig.Options)
	}
}

func TestContextWindowModelVariantsMirrorIntoRuntimeContext(t *testing.T) {
	t.Parallel()
	options := ComposerOptions{
		Provider: "codex",
		ModelConfig: ComposerConfigOption{
			Options: []ComposerConfigOptionValue{
				{ID: "glm-5.3", Label: "GLM-5.3", Value: "glm-5.3"},
			},
		},
		RuntimeContext: map[string]any{
			"configOptions": []map[string]any{
				{
					"id":      "model",
					"options": []map[string]any{{"value": "glm-5.3", "label": "GLM-5.3"}},
				},
			},
		},
	}

	got := withContextWindowModelVariants("codex", options)

	configOptions, ok := got.RuntimeContext["configOptions"].([]map[string]any)
	if !ok || len(configOptions) != 1 {
		t.Fatalf("configOptions = %#v", got.RuntimeContext["configOptions"])
	}
	runtimeOptions, ok := configOptions[0]["options"].([]map[string]any)
	if !ok || len(runtimeOptions) != 2 {
		t.Fatalf("runtime options = %#v, want the variant mirrored", configOptions[0]["options"])
	}
	if runtimeOptions[1]["value"] != "glm-5.3[1m]" {
		t.Fatalf("runtime variant = %#v, want glm-5.3[1m]", runtimeOptions[1])
	}
	if len(baseRuntimeModelOptions(runtimeOptions)) != 1 {
		t.Fatalf("runtime base options = %#v, want the single original", runtimeOptions)
	}
}

func TestContextWindowModelVariantsLeaveEmptyCatalogAlone(t *testing.T) {
	t.Parallel()
	options := ComposerOptions{Provider: "codex"}

	got := withContextWindowModelVariants("codex", options)

	if len(got.ModelConfig.Options) != 0 {
		t.Fatalf("model options = %#v, want none", got.ModelConfig.Options)
	}
}
