package agent

import "strings"

// extensionStaticReasoningConfig projects an extension's declared reasoning
// levels into the same shape runtime ACP config options produce.
//
// Runtime evidence from a live session stays authoritative and is merged
// before this runs. The declaration exists because the defaults-persistence
// path has no session and no workspace scope, so without it a reachable
// reasoning picker would look non-configurable the moment a user tried to
// remember a default.
func extensionStaticReasoningConfig(
	profile ExtensionComposerProfile,
	selected string,
	locale string,
) (ComposerConfigOption, bool) {
	options := extensionStaticReasoningOptionValues(profile, locale)
	if len(options) == 0 {
		return ComposerConfigOption{}, false
	}
	defaultValue := strings.TrimSpace(profile.DefaultReasoningEffort)
	if defaultValue == "" {
		defaultValue = strings.TrimSpace(options[0].Value)
	}
	return ComposerConfigOption{
		Configurable: true,
		CurrentValue: strings.TrimSpace(selected),
		DefaultValue: defaultValue,
		Options:      options,
	}, true
}

func extensionStaticReasoningOptionValues(
	profile ExtensionComposerProfile,
	locale string,
) []ComposerConfigOptionValue {
	if len(profile.ReasoningEffortOptions) == 0 {
		return nil
	}
	advertised := make([]AgentModelReasoningEffortOption, 0, len(profile.ReasoningEffortOptions))
	for _, value := range profile.ReasoningEffortOptions {
		advertised = append(advertised, AgentModelReasoningEffortOption{Value: value})
	}
	return composerAdvertisedReasoningOptionValues("", "", locale, advertised)
}

// applyExtensionStaticReasoningConfig fills the declared reasoning contract
// only when observed runtime evidence produced no usable options. A live
// session therefore keeps its own picker: the declaration never narrows or
// widens an observed runtime option list.
func applyExtensionStaticReasoningConfig(
	options ComposerOptions,
	profile ExtensionComposerProfile,
	locale string,
) ComposerOptions {
	if options.ReasoningConfig.Configurable && len(options.ReasoningConfig.Options) > 0 {
		return options
	}
	static, ok := extensionStaticReasoningConfig(
		profile,
		options.EffectiveSettings.ReasoningEffort,
		locale,
	)
	if !ok {
		return options
	}
	options.ReasoningConfig = static
	return options
}
