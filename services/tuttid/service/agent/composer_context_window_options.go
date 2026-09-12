package agent

import (
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/contextwindow"
)

// withContextWindowModelVariants appends the 1M spelling of every advertised
// model, so the composer can offer the 1M context window as its own row.
//
// The marker rides the model value instead of a separate setting: a runtime
// that knows the convention consumes it (Claude Code strips the suffix outbound
// and asks for the context-1m beta, the Codex app-server takes the window as
// thread config), and one that does not gets the bare id back from
// contextwindow.Bare. Nothing here has to know which provider is which.
//
// Only bare ids are spelled. Some providers address models with their own
// bracketed suffix (Cursor's "composer-2.5[fast=true]", its synthetic
// "default[]"), and stapling a second marker onto those would offer rows like
// "default[][1m]" that name nothing. A value that already carries a bracket is
// left alone.
//
// Ran last, after every overlay: the model plan overlay replaces ModelConfig
// wholesale, so injecting earlier would drop the variants for plan-backed
// providers.
func withContextWindowModelVariants(provider string, options ComposerOptions) ComposerOptions {
	base := options.ModelConfig.Options
	if len(base) == 0 {
		return options
	}
	variants := make([]ComposerConfigOptionValue, 0, len(base))
	for _, option := range base {
		value := strings.TrimSpace(option.Value)
		if value == "" || contextwindow.RequestsOneMillion(value) || strings.ContainsAny(value, "[]") {
			continue
		}
		spelled := contextwindow.WithMarker(value)
		if spelled == "" {
			continue
		}
		variant := option
		variant.ID = spelled
		variant.Label = spelled
		variant.Value = spelled
		variants = append(variants, variant)
	}
	if len(variants) == 0 {
		return options
	}
	options.ModelConfig.Options = append(append([]ComposerConfigOptionValue{}, base...), variants...)
	options.RuntimeContext = appendRuntimeModelOptionVariants(
		provider,
		options.RuntimeContext,
		composerConfigOptionValuesToRuntimeModelOptions(variants),
	)
	return options
}

// appendRuntimeModelOptionVariants mirrors the variants into the runtime
// snapshot's model config option, which is the projection the activity adapter
// and the session snapshot read back.
func appendRuntimeModelOptionVariants(
	provider string,
	runtimeContext map[string]any,
	variants []map[string]any,
) map[string]any {
	if len(runtimeContext) == 0 || len(variants) == 0 {
		return runtimeContext
	}
	entries, ok := runtimeContext["configOptions"].([]map[string]any)
	if !ok {
		return runtimeContext
	}
	optionID := composerModelConfigOptionID(provider)
	for _, entry := range entries {
		if strings.TrimSpace(stringFromAny(entry["id"])) != optionID {
			continue
		}
		existing, _ := entry["options"].([]map[string]any)
		merged := make([]map[string]any, 0, len(existing)+len(variants))
		merged = append(merged, existing...)
		merged = append(merged, variants...)
		entry["options"] = merged
	}
	return runtimeContext
}

func composerModelConfigOptionID(provider string) string {
	if id := strings.TrimSpace(composerProfileFor(provider).ModelConfigOptionID); id != "" {
		return id
	}
	return "model"
}
