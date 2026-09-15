package agent

import (
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/modelcatalog"
	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

var modelPlanReasoningLevelOrder = []string{
	"off",
	"minimal",
	"low",
	"medium",
	"high",
	"xhigh",
	"max",
}

func composerModelPlanReasoningProfiles(
	provider string,
	endpoint *runtimeprep.ModelEndpointConfig,
	locale string,
) map[string]ComposerReasoningProfile {
	if endpoint == nil ||
		composerProfileFor(provider).ReasoningEffortOptions != providerregistry.ReasoningEffortOptionsModelCatalog {
		return nil
	}
	profiles := make(map[string]ComposerReasoningProfile, len(endpoint.Models))
	for _, model := range endpoint.Models {
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			continue
		}
		options := composerModelPlanReasoningOptionValues(model.ReasoningEfforts, locale)
		if len(options) == 0 {
			continue
		}
		profiles[planModelComposerValue(provider, modelID)] = ComposerReasoningProfile{
			DefaultValue: composerModelPlanReasoningDefault(provider, options),
			Options:      options,
		}
	}
	if len(profiles) == 0 {
		return nil
	}
	return profiles
}

func composerModelPlanReasoningOptionValues(
	efforts map[string]*string,
	locale string,
) []ComposerConfigOptionValue {
	if len(efforts) == 0 {
		return nil
	}
	options := make([]ComposerConfigOptionValue, 0, len(efforts))
	seen := make(map[string]struct{}, len(efforts))
	for _, level := range modelPlanReasoningLevelOrder {
		wire, declared := efforts[level]
		if !declared || wire == nil || strings.TrimSpace(*wire) == "" {
			continue
		}
		value := strings.TrimSpace(*wire)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		label, description := reasoningEffortDisplay(value, locale, "")
		options = append(options, ComposerConfigOptionValue{
			Description: description,
			ID:          value,
			Label:       label,
			Value:       value,
		})
	}
	return options
}

func composerModelPlanReasoningDefault(
	provider string,
	options []ComposerConfigOptionValue,
) string {
	preferred := strings.TrimSpace(composerProfileFor(provider).DefaultReasoningEffort)
	for _, option := range options {
		if strings.TrimSpace(option.Value) == preferred {
			return preferred
		}
	}
	if len(options) == 0 {
		return ""
	}
	return strings.TrimSpace(options[0].Value)
}

func applyComposerModelPlanReasoningOptions(
	options ComposerOptions,
	endpoint *runtimeprep.ModelEndpointConfig,
	locale string,
) ComposerOptions {
	profiles := composerModelPlanReasoningProfiles(options.Provider, endpoint, locale)
	if len(profiles) == 0 {
		return options
	}
	options.ReasoningOptionsByModel = profiles
	profile, ok := profiles[strings.TrimSpace(options.EffectiveSettings.Model)]
	if !ok {
		return options
	}
	current := composerModelPlanReasoningDefault(options.Provider, profile.Options)
	for _, option := range profile.Options {
		if strings.TrimSpace(option.Value) == strings.TrimSpace(options.EffectiveSettings.ReasoningEffort) {
			current = strings.TrimSpace(option.Value)
			break
		}
	}
	options.EffectiveSettings.ReasoningEffort = current
	options.ReasoningConfig = ComposerConfigOption{
		Configurable: len(profile.Options) > 0,
		CurrentValue: current,
		DefaultValue: profile.DefaultValue,
		Options:      cloneComposerConfigOptionValues(profile.Options),
	}
	if options.RuntimeContext != nil {
		options.RuntimeContext["reasoningEffort"] = nullableString(current)
	}
	return options
}

func composerModelReasoningOptionsByModel(
	provider string,
	locale string,
	profiles map[string]modelcatalog.ReasoningProfile,
) map[string]ComposerReasoningProfile {
	result := make(map[string]ComposerReasoningProfile, len(profiles))
	for model, profile := range profiles {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		defaultValue := profile.DefaultValue
		options := composerAdvertisedReasoningOptionValues(
			provider,
			"",
			locale,
			profile.Options,
		)
		result[model] = ComposerReasoningProfile{
			DefaultValue: defaultValue,
			Options:      options,
		}
	}
	return result
}

func composerReasoningConfig(provider string, selected string, locale string) ComposerConfigOption {
	return composerReasoningConfigFromOptions(
		provider,
		selected,
		composerReasoningOptionValues(provider, selected, locale),
	)
}

func composerReasoningConfigFromOptions(
	provider string,
	selected string,
	options []ComposerConfigOptionValue,
) ComposerConfigOption {
	selected = strings.TrimSpace(selected)
	profile := composerProfileFor(provider)
	configurable := profile.ReasoningEffort
	if profile.ReasoningEffortOptions == providerregistry.ReasoningEffortOptionsStrictModelCatalog {
		configurable = len(options) > 0
	}
	return ComposerConfigOption{
		Configurable: configurable,
		CurrentValue: selected,
		DefaultValue: selected,
		Options:      cloneComposerConfigOptionValues(options),
	}
}

func reasoningEffortValuesForProvider(provider string) []string {
	if !composerProfileKnown(provider) {
		return nil
	}
	profile := composerProfileFor(provider)
	if profile.ReasoningEffortOptions != providerregistry.ReasoningEffortOptionsStatic {
		return nil
	}
	return append([]string(nil), profile.ReasoningEffortValues...)
}

// composerProviderUsesModelReasoningCatalog identifies providers whose model
// catalog is authoritative for reasoning values, including strict catalogs
// that intentionally expose no provider-level fallback options.
func composerProviderUsesModelReasoningCatalog(provider string) bool {
	kind := composerProfileFor(provider).ReasoningEffortOptions
	return kind == providerregistry.ReasoningEffortOptionsModelCatalog ||
		kind == providerregistry.ReasoningEffortOptionsStrictModelCatalog
}

func composerReasoningOptionValues(provider string, selected string, locale string) []ComposerConfigOptionValue {
	values := reasoningEffortValuesForProvider(provider)
	advertised := make([]AgentModelReasoningEffortOption, 0, len(values))
	for _, value := range values {
		advertised = append(advertised, AgentModelReasoningEffortOption{Value: value})
	}
	return composerAdvertisedReasoningOptionValues(provider, selected, locale, advertised)
}

func composerAdvertisedReasoningOptionValues(
	_ string,
	selected string,
	locale string,
	advertised []AgentModelReasoningEffortOption,
) []ComposerConfigOptionValue {
	selected = strings.TrimSpace(selected)
	options := make([]ComposerConfigOptionValue, 0, len(advertised)+1)
	containsSelected := false
	for _, advertisedOption := range advertised {
		value := strings.TrimSpace(advertisedOption.Value)
		if value == "" {
			continue
		}
		if value == selected {
			containsSelected = true
		}
		label, description := reasoningEffortDisplay(
			value,
			locale,
			advertisedOption.Description,
		)
		if advertisedLabel := strings.TrimSpace(advertisedOption.Label); advertisedLabel != "" {
			label = advertisedLabel
		}
		options = append(options, ComposerConfigOptionValue{
			Description: description,
			ID:          value,
			Label:       label,
			Value:       value,
		})
	}
	if selected != "" && !containsSelected {
		options = append(options, ComposerConfigOptionValue{
			ID:    selected,
			Label: reasoningEffortLabel(selected, locale),
			Value: selected,
		})
	}
	return options
}

func resolveAdvertisedReasoningEffort(
	_ string,
	selected string,
	advertisedDefault string,
	advertised []AgentModelReasoningEffortOption,
) string {
	selected = strings.TrimSpace(selected)
	advertisedDefault = strings.TrimSpace(advertisedDefault)
	firstValue := ""
	defaultSupported := false
	for _, option := range advertised {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		if firstValue == "" {
			firstValue = value
		}
		if value == selected {
			return selected
		}
		if value == advertisedDefault {
			defaultSupported = true
		}
	}
	if defaultSupported {
		return advertisedDefault
	}
	return firstValue
}

func composerConfigOptionValuesToRuntimeOptions(
	options []ComposerConfigOptionValue,
) []map[string]string {
	result := make([]map[string]string, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		runtimeOption := map[string]string{
			"name":  strings.TrimSpace(option.Label),
			"value": value,
		}
		if description := strings.TrimSpace(option.Description); description != "" {
			runtimeOption["description"] = description
		}
		result = append(result, runtimeOption)
	}
	return result
}

func normalizeReasoningEffortForProvider(provider string, value string) string {
	provider = agentprovider.Normalize(provider)
	profile := composerProfileFor(provider)
	if !profile.ReasoningEffort {
		return ""
	}
	normalized := strings.TrimSpace(value)
	// Model-catalog values are model-specific and authoritative. A generic
	// provider-level normalizer must not rewrite values such as "minimal" or
	// "none" that the selected model explicitly advertises.
	if profile.ReasoningEffortOptions == providerregistry.ReasoningEffortOptionsModelCatalog ||
		profile.ReasoningEffortOptions == providerregistry.ReasoningEffortOptionsStrictModelCatalog {
		return normalized
	}
	if (normalized == "minimal" || normalized == "none") &&
		!reasoningEffortValueDeclared(profile, normalized) {
		return profile.DefaultReasoningEffort
	}
	return normalized
}

func reasoningEffortValueDeclared(profile composerProfile, value string) bool {
	for _, candidate := range profile.ReasoningEffortValues {
		if strings.TrimSpace(candidate) == value {
			return true
		}
	}
	return false
}
