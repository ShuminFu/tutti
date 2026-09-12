package agent

import "strings"

func composerSpeedOptionValues(provider string, locale string) []ComposerConfigOptionValue {
	values := speedTierValuesForProvider(provider)
	options := make([]ComposerConfigOptionValue, 0, len(values))
	for _, value := range values {
		label, description := speedDisplay(value, locale)
		options = append(options, ComposerConfigOptionValue{
			ID:          value,
			Label:       label,
			Value:       value,
			Description: description,
		})
	}
	return options
}

func composerSpeedConfigFromOptions(provider string, selected string, options []ComposerConfigOptionValue) ComposerConfigOption {
	selected = strings.TrimSpace(selected)
	return ComposerConfigOption{
		Configurable: speedProviderSupportsSpeed(provider) && len(options) > 0,
		CurrentValue: selected,
		DefaultValue: selected,
		Options:      cloneComposerConfigOptionValues(options),
	}
}

func composerAdvertisedSpeedOptionValues(locale string, advertised []AgentModelSpeedOption) []ComposerConfigOptionValue {
	options := make([]ComposerConfigOptionValue, 0, len(advertised))
	for _, advertisedOption := range advertised {
		value := strings.TrimSpace(advertisedOption.Value)
		if value == "" {
			continue
		}
		label, description := speedDisplay(value, locale)
		if advertisedLabel := strings.TrimSpace(advertisedOption.Label); advertisedLabel != "" {
			label = advertisedLabel
		}
		if advertisedDescription := strings.TrimSpace(advertisedOption.Description); advertisedDescription != "" {
			description = advertisedDescription
		}
		options = append(options, ComposerConfigOptionValue{
			ID: value, Label: label, Value: value, Description: description,
		})
	}
	return options
}

func resolveAdvertisedSpeed(selected string, advertisedDefault string, advertised []AgentModelSpeedOption) string {
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
