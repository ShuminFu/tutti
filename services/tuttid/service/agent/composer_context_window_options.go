package agent

import (
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/contextwindow"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// withContextWindowModelVariants appends the 1M spelling of every advertised
// model that the host says really has a 1M window, so the composer can offer the
// 1M context window as its own row.
//
// The marker rides the model value instead of a separate setting: a runtime that
// knows the convention consumes it (Claude Code strips the suffix outbound and
// asks for the context-1m beta, the Codex app-server takes the window as thread
// config), and one that does not gets the bare id back from
// contextwindow.Bare. Nothing here has to know which provider is which.
//
// The gate is the host's window table (runtimeprep.HostModelContextWindows, the
// `modelContext` numbers in the host endpoint contract): a model earns a marked
// row only by being declared with a >= 1M window there. A marked row is not a
// wish the receiving runtime may ignore: for the runtimes that read the window
// off the model VALUE it is a routing key that must resolve against the ids that
// runtime was told about, and fabricating one the host never declared is how
// `X[1m]` came back as plain 200k (or, for an id the runtime cannot fuzz back,
// as a session/new rejection). The host owns this knowledge because only it can
// see the real per-channel windows, and its allowlist expansion
// (expandManagedModelAllowlist) answers to the same table -- so "row exists"
// and "the receiving runtime was told about the marked id" stay one condition.
//
// Two conservative readings matter here, and both are on purpose:
//   - A missing entry means "window unknown" and never earns a row: over-
//     reporting a window is what makes the CLI skip auto-compact and die at the
//     upstream 400.
//   - No table at all (a standalone DinTalDock, an older host, a document
//     without `modelContext`) means no marked rows. Guessing there would put the
//     model picker back in the business of promising windows nobody vouched for.
//
// Only bare ids are spelled. Some providers address models with their own
// bracketed suffix (Cursor's "composer-2.5[fast=true]", its synthetic
// "default[]"), and stapling a second marker onto those would offer rows like
// "default[][1m]" that name nothing. A value that already carries a bracket is
// left alone, as is a row that names no model at all (the composer's synthetic
// "default": no runtime has a marked spelling of it, so offering "default[1m]"
// only offers a value that fails to resolve).
//
// Ran last, after every overlay: the model plan overlay replaces ModelConfig
// wholesale, so injecting earlier would drop the variants for plan-backed
// providers.
func withContextWindowModelVariants(provider string, options ComposerOptions) ComposerOptions {
	base := options.ModelConfig.Options
	if len(base) == 0 {
		return options
	}
	windows := runtimeprep.HostModelContextWindows()
	if len(windows) == 0 {
		return options
	}
	variants := make([]ComposerConfigOptionValue, 0, len(base))
	for _, option := range base {
		value := strings.TrimSpace(option.Value)
		if value == "" || contextwindow.RequestsOneMillion(value) || strings.ContainsAny(value, "[]") {
			continue
		}
		if composerModelRowNamesNoModel(value) {
			continue
		}
		if window, ok := windows[value]; !ok || window < contextwindow.OneMillionTokens {
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
	for _, variant := range variants {
		baseValue, _ := contextwindow.Split(variant.Value)
		profile, ok := options.ReasoningOptionsByModel[baseValue]
		if !ok {
			continue
		}
		options.ReasoningOptionsByModel[variant.Value] = ComposerReasoningProfile{
			DefaultValue: profile.DefaultValue,
			Options:      cloneComposerConfigOptionValues(profile.Options),
		}
	}
	options.RuntimeContext = appendRuntimeModelOptionVariants(
		provider,
		options.RuntimeContext,
		composerConfigOptionValuesToRuntimeModelOptions(variants),
	)
	return options
}

// composerModelRowNamesNoModel reports a composer row that is a model *choice*
// without naming a model: the synthetic "default" that providers such as Cursor
// expose alongside their catalog. It has no marked spelling in any runtime, so
// "default[1m]" can only ever be an unroutable value.
func composerModelRowNamesNoModel(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "default")
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
	entries := runtimeConfigOptionsAsMapSlice(runtimeContext["configOptions"])
	if len(entries) == 0 {
		return runtimeContext
	}
	optionID := composerModelConfigOptionID(provider)
	for _, entry := range entries {
		if strings.TrimSpace(stringFromAny(entry["id"])) != optionID {
			continue
		}
		existing := runtimeConfigOptionsAsMapSlice(entry["options"])
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
