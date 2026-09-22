package agent

import (
	"context"
	"fmt"
	"strings"

	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

// Stable reason codes for a rejected agent-composer-defaults field. They are
// part of the wire contract of
// preferences.agent.composer.defaults.resolved and must not be renamed without a
// protocol version bump.
const (
	AgentComposerDefaultsReasonInvalidValue     = "invalid_value"
	AgentComposerDefaultsReasonUnsupportedValue = "unsupported_value"
	AgentComposerDefaultsReasonNotConfigurable  = "not_configurable"
	AgentComposerDefaultsReasonUnsupportedField = "unsupported_field"
	AgentComposerDefaultsReasonInternalError    = "internal_error"
)

// AgentComposerDefaultsRejectedField is one field the daemon refused to persist.
// Message is diagnostic only: clients must select their own localized copy from
// ReasonCode and must never render Message to the user.
type AgentComposerDefaultsRejectedField struct {
	Field      string
	ReasonCode string
	Message    string
}

// AgentComposerDefaultsPatchResult is the per-field outcome of a defaults patch.
//
// It exists because the desktop client publishes every desired default in ONE
// snapshot call. Failing the whole patch on the first bad field silently dropped
// legal siblings, so validation now evaluates each field independently and
// reports which ones survived.
type AgentComposerDefaultsPatchResult struct {
	// Applied holds the fields that were validated and may be persisted.
	Applied preferencesbiz.AgentComposerDefaultsPatch
	// Rejected holds the fields that were refused, with a reason code each.
	Rejected []AgentComposerDefaultsRejectedField
}

func (r AgentComposerDefaultsPatchResult) hasApplied() bool {
	return len(r.Applied) > 0
}

// ValidateAgentComposerDefaultsPatch validates every field of the patch
// independently and returns the subset that may be persisted.
//
// Only a patch that cannot be judged at all returns an error: an unknown agent
// target or an unresolvable launch means there is no field set to report on, so
// the intent ack fails and no resolved event is published. A failure to project
// the target's capabilities is not in that class — the fields are known, so the
// patch degrades to a per-field internal_error verdict instead of collapsing
// into one opaque error that leaves the client waiting.
func (s *Service) ValidateAgentComposerDefaultsPatch(
	ctx context.Context,
	agentTargetID string,
	patch preferencesbiz.AgentComposerDefaultsPatch,
) (AgentComposerDefaultsPatchResult, error) {
	launchInput := CreateSessionInput{
		AgentTargetID: agentTargetID,
	}
	launch, err := s.resolveCreateSessionLaunch(ctx, "", &launchInput)
	if err != nil {
		return AgentComposerDefaultsPatchResult{}, err
	}
	settings := ComposerSettings{}
	for field, value := range patch {
		if value == nil || field == preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode {
			continue
		}
		selected := agentComposerDefaultsPatchText(value)
		switch field {
		case preferencesbiz.AgentComposerDefaultsFieldModel:
			settings.Model = selected
		case preferencesbiz.AgentComposerDefaultsFieldPermissionModeID:
			settings.PermissionModeID = selected
		case preferencesbiz.AgentComposerDefaultsFieldReasoningEffort:
			settings.ReasoningEffort = selected
		case preferencesbiz.AgentComposerDefaultsFieldSpeed:
			settings.Speed = selected
		}
	}
	options, err := s.GetComposerOptions(ctx, ComposerOptionsInput{
		AgentTargetID:            agentTargetID,
		Provider:                 launch.Provider,
		Settings:                 settings,
		IncludeCapabilityCatalog: boolPointer(false),
		// The patch carries no workspace or cwd, so the only runtime evidence
		// available is what the daemon previously observed for this target.
		// Without it, extension targets fall back to the provider registry's
		// static profile, which declares nothing for extension providers, and a
		// legal value is rejected as "not configurable".
		IncludeTargetRuntimeEvidence: true,
		providerTargetRef:            clonePayload(launch.ProviderTargetRef),
	})
	if err != nil {
		// The target exists and its launch resolved, but projecting the target's
		// capabilities failed — an unreadable extension profile, a runtime
		// evidence read error, and so on. Returning the error would collapse
		// every field behind one opaque failure and publish no resolved event,
		// so the client would never learn that its defaults were not stored.
		//
		// Degrade to "nothing applied, everything rejected as internal_error":
		// each field the caller asked about still comes back with a reason and
		// the client can surface it. The error text stays on the rejection for
		// daemon logs and is never rendered (clients select copy from
		// ReasonCode); the wire boundary bounds its length.
		result := AgentComposerDefaultsPatchResult{
			Applied: preferencesbiz.AgentComposerDefaultsPatch{},
		}
		message := err.Error()
		for _, field := range agentComposerDefaultsPatchFieldOrder() {
			if _, present := patch[field]; !present {
				continue
			}
			result.Rejected = append(result.Rejected, AgentComposerDefaultsRejectedField{
				Field:      field,
				ReasonCode: AgentComposerDefaultsReasonInternalError,
				Message:    message,
			})
		}
		return result, nil
	}
	// Deterministic order so a rejected list never reshuffles between two
	// identical patches.
	fields := agentComposerDefaultsPatchFieldOrder()
	result := AgentComposerDefaultsPatchResult{
		Applied: preferencesbiz.AgentComposerDefaultsPatch{},
	}
	for _, field := range fields {
		value, present := patch[field]
		if !present {
			continue
		}
		if rejected, ok := s.validateAgentComposerDefaultsField(
			ctx,
			field,
			value,
			launch,
			agentTargetID,
			options,
		); !ok {
			result.Rejected = append(result.Rejected, rejected)
			continue
		}
		// Text fields clear on null; absent keys were skipped above.
		if value == nil || field == preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode {
			result.Applied[field] = value
			continue
		}
		result.Applied[field] = agentComposerDefaultsPatchText(value)
	}
	return result, nil
}

// validateAgentComposerDefaultsField validates one field and reports whether it
// may be persisted. A rejection never aborts the patch.
func (s *Service) validateAgentComposerDefaultsField(
	ctx context.Context,
	field string,
	value any,
	launch resolvedCreateSessionLaunch,
	agentTargetID string,
	options ComposerOptions,
) (AgentComposerDefaultsRejectedField, bool) {
	reject := func(reasonCode string) (AgentComposerDefaultsRejectedField, bool) {
		return AgentComposerDefaultsRejectedField{
			Field:      field,
			ReasonCode: reasonCode,
		}, false
	}
	// A null value clears the stored default. There is nothing to validate
	// against runtime capability, and refusing a clear would strand a stale
	// default the user is trying to remove.
	if value == nil {
		return AgentComposerDefaultsRejectedField{}, true
	}
	selected := agentComposerDefaultsPatchText(value)
	if selected == "" && field != preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode {
		// Blank text is "no default", which is the same state as a clear.
		return AgentComposerDefaultsRejectedField{}, true
	}
	switch field {
	case preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode:
		enabled, ok := value.(bool)
		if !ok {
			return reject(AgentComposerDefaultsReasonInvalidValue)
		}
		if enabled && !composerProviderSupportsSaverSubagentMode(launch.Provider) {
			return reject(AgentComposerDefaultsReasonUnsupportedValue)
		}
		return AgentComposerDefaultsRejectedField{}, true
	case preferencesbiz.AgentComposerDefaultsFieldModel:
		if providerTargetRefKind(launch.ProviderTargetRef) == "agent_extension" {
			observedModels, observed := s.liveComposerModelOptionsForTarget(
				launch.Provider,
				agentTargetID,
			)
			return composerDefaultsRejection(field, validateComposerDefaultOption(field, selected, observed, observedModels))
		}
		// Composer Options already applied the host overlay and 1M rows for
		// this target — the catalog the picker shows when no workspace-scoped
		// plan is in play. Validating the provider-native CLI list after that
		// overlay is how Codex refused deepseek-flash[1m] as invalid_value
		// while the menu offered it. Providers without a model picker keep the
		// CLI/create clamp path.
		if composerProfileFor(launch.Provider).ModelSelection {
			if advertised := advertisedComposerModelValues(options.ModelConfig.Options); len(advertised) > 0 {
				if composerModelIDInCatalog(selected, advertised) {
					return AgentComposerDefaultsRejectedField{}, true
				}
				return composerDefaultsRejection(field, &InvalidModelError{
					Provider:        launch.Provider,
					Model:           selected,
					AvailableModels: advertised,
				})
			}
		}
		return composerDefaultsRejection(field, s.validateComposerModelForCreate(ctx, launch.Provider, "", "", selected))
	case preferencesbiz.AgentComposerDefaultsFieldPermissionModeID:
		if !options.PermissionConfig.Configurable || !permissionModeConfigHasModeID(options.PermissionConfig, selected) {
			return reject(AgentComposerDefaultsReasonNotConfigurable)
		}
		return AgentComposerDefaultsRejectedField{}, true
	case preferencesbiz.AgentComposerDefaultsFieldReasoningEffort:
		reasoningConfig := composerReasoningConfigForSelectedModel(options)
		return composerDefaultsRejection(field, validateComposerDefaultOption(field, selected, reasoningConfig.Configurable, reasoningConfig.Options))
	case preferencesbiz.AgentComposerDefaultsFieldSpeed:
		return composerDefaultsRejection(field, validateComposerDefaultOption(field, selected, options.SpeedConfig.Configurable, options.SpeedConfig.Options))
	}
	return reject(AgentComposerDefaultsReasonUnsupportedField)
}

// composerDefaultsRejection maps an existing validation error onto a stable
// per-field reason code. The message is preserved for daemon logs only.
func composerDefaultsRejection(field string, err error) (AgentComposerDefaultsRejectedField, bool) {
	if err == nil {
		return AgentComposerDefaultsRejectedField{}, true
	}
	return AgentComposerDefaultsRejectedField{
		Field:      field,
		ReasonCode: composerDefaultsReasonCodeForError(err),
		Message:    err.Error(),
	}, false
}

// composerDefaultsReasonCodeForError reads the reason back out of the error
// strings the existing validators already produce. Those strings are the
// daemon's internal vocabulary; the code below is the wire vocabulary.
func composerDefaultsReasonCodeForError(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "is not configurable for agent target"):
		return AgentComposerDefaultsReasonNotConfigurable
	case strings.Contains(message, "value is not supported by agent target"):
		return AgentComposerDefaultsReasonUnsupportedValue
	case strings.Contains(message, "unsupported agent composer defaults field"):
		return AgentComposerDefaultsReasonUnsupportedField
	default:
		return AgentComposerDefaultsReasonInvalidValue
	}
}

// agentComposerDefaultsPatchFieldOrder is the stable evaluation and reporting
// order for patch fields.
func agentComposerDefaultsPatchFieldOrder() []string {
	return []string{
		preferencesbiz.AgentComposerDefaultsFieldModel,
		preferencesbiz.AgentComposerDefaultsFieldPermissionModeID,
		preferencesbiz.AgentComposerDefaultsFieldReasoningEffort,
		preferencesbiz.AgentComposerDefaultsFieldSpeed,
		preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode,
	}
}

func agentComposerDefaultsPatchText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case *string:
		if typed != nil {
			return strings.TrimSpace(*typed)
		}
	}
	return ""
}

func validateComposerDefaultOption(
	field string,
	selected string,
	configurable bool,
	options []ComposerConfigOptionValue,
) error {
	if !configurable {
		return fmt.Errorf("%w: %s is not configurable for agent target", ErrInvalidArgument, field)
	}
	if len(options) == 0 {
		return nil
	}
	for _, option := range options {
		if strings.TrimSpace(option.Value) == selected {
			return nil
		}
	}
	return fmt.Errorf("%w: %s value is not supported by agent target", ErrInvalidArgument, field)
}

func (s *Service) validateExtensionComposerSettingsForCreate(
	ctx context.Context,
	workspaceID string,
	cwd string,
	input *CreateSessionInput,
	modelExplicit bool,
	permissionModeExplicit bool,
	reasoningEffortExplicit bool,
) error {
	if input == nil || providerTargetRefKind(input.ProviderTargetRef) != "agent_extension" {
		return nil
	}
	settings := ComposerSettings{
		Model:            strings.TrimSpace(value(input.Model)),
		PermissionModeID: strings.TrimSpace(value(input.PermissionModeID)),
		ReasoningEffort:  strings.TrimSpace(value(input.ReasoningEffort)),
		Speed:            strings.TrimSpace(value(input.Speed)),
	}
	if !modelExplicit {
		// Persisted defaults may outlive an extension-owned model catalog. Let
		// Composer Options resolve the runtime's current model rather than
		// validating the retired preference as an explicit caller selection.
		settings.Model = ""
	}
	if !permissionModeExplicit {
		// Persisted defaults are fallback preferences, not caller selections.
		// Let Composer Options ignore a stale default and resolve runtime/profile
		// state instead of treating old data as an explicit invalid request.
		settings.PermissionModeID = ""
	}
	options, err := s.GetComposerOptions(ctx, ComposerOptionsInput{
		AgentTargetID:            input.AgentTargetID,
		Provider:                 input.Provider,
		WorkspaceID:              workspaceID,
		Cwd:                      cwd,
		Settings:                 settings,
		IncludeCapabilityCatalog: boolPointer(false),
		// Creating a session still has to learn an extension's live catalog the
		// first time. Composer mount must not do this; only a missing cache
		// reaches the hidden probe here.
		modelListProbe: composerModelListProbeIfStale,
	})
	if err != nil {
		return err
	}
	if !modelExplicit {
		resolved := strings.TrimSpace(options.EffectiveSettings.Model)
		if resolved == "" {
			input.Model = nil
		} else {
			input.Model = stringPointer(resolved)
		}
	}
	if !permissionModeExplicit {
		resolved := strings.TrimSpace(options.EffectiveSettings.PermissionModeID)
		if resolved == "" {
			input.PermissionModeID = nil
		} else {
			input.PermissionModeID = stringPointer(resolved)
		}
	}
	if !reasoningEffortExplicit {
		resolved := strings.TrimSpace(options.EffectiveSettings.ReasoningEffort)
		settings.ReasoningEffort = resolved
		if resolved == "" {
			input.ReasoningEffort = nil
		} else {
			input.ReasoningEffort = stringPointer(resolved)
		}
	}
	if err := validateExtensionComposerOption(
		preferencesbiz.AgentComposerDefaultsFieldModel,
		settings.Model,
		options.ModelConfig,
	); err != nil {
		return err
	}
	if permissionModeExplicit && settings.PermissionModeID != "" &&
		(!options.PermissionConfig.Configurable ||
			!permissionModeConfigHasModeID(options.PermissionConfig, settings.PermissionModeID)) {
		available := make([]string, 0, len(options.PermissionConfig.Modes))
		for _, mode := range options.PermissionConfig.Modes {
			available = append(available, strings.TrimSpace(mode.ID))
		}
		return &UnsupportedPermissionModeIDError{
			AgentTargetID:              strings.TrimSpace(input.AgentTargetID),
			PermissionModeID:           settings.PermissionModeID,
			AvailablePermissionModeIDs: available,
		}
	}
	reasoningConfig := composerReasoningConfigForSelectedModel(options)
	if err := validateExtensionComposerOption(
		preferencesbiz.AgentComposerDefaultsFieldReasoningEffort,
		settings.ReasoningEffort,
		reasoningConfig,
	); err != nil {
		return err
	}
	return validateExtensionComposerOption(
		preferencesbiz.AgentComposerDefaultsFieldSpeed,
		settings.Speed,
		options.SpeedConfig,
	)
}

func composerReasoningConfigForSelectedModel(options ComposerOptions) ComposerConfigOption {
	if profile, advertised := composerReasoningProfileForModel(
		options.ReasoningOptionsByModel,
		options.EffectiveSettings.Model,
	); advertised {
		return ComposerConfigOption{
			Configurable: len(profile.Options) > 0,
			CurrentValue: strings.TrimSpace(options.EffectiveSettings.ReasoningEffort),
			DefaultValue: strings.TrimSpace(profile.DefaultValue),
			Options:      cloneComposerConfigOptionValues(profile.Options),
		}
	}
	return options.ReasoningConfig
}

func validateExtensionComposerOption(
	field string,
	selected string,
	config ComposerConfigOption,
) error {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return nil
	}
	if !config.Configurable || len(config.Options) == 0 {
		return fmt.Errorf("%w: %s is not configurable for agent target", ErrInvalidArgument, field)
	}
	for _, option := range config.Options {
		if strings.TrimSpace(option.Value) == selected {
			return nil
		}
	}
	return fmt.Errorf("%w: %s value is not supported by agent target", ErrInvalidArgument, field)
}
