package preferences

import (
	"context"
	"errors"
	"strings"

	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
)

type DesktopPreferencesPublisher interface {
	PublishDesktopPreferencesUpdated(context.Context, preferencesbiz.DesktopPreferences) error
}

type AgentComposerDefaultsPublisher interface {
	PublishAgentComposerDefaultsChanged(context.Context, string) error
}

// AgentComposerDefaultsPatchOutcome is one field the daemon refused to persist,
// with a stable reason code. Message is diagnostic only and must never be shown
// to the user.
type AgentComposerDefaultsPatchOutcome struct {
	Field      string
	ReasonCode string
	Message    string
}

// AgentComposerDefaultsPatchValidation is the per-field result of a defaults
// patch: the fields that survived validation, and those that did not.
type AgentComposerDefaultsPatchValidation struct {
	Applied  preferencesbiz.AgentComposerDefaultsPatch
	Rejected []AgentComposerDefaultsPatchOutcome
}

type AgentComposerDefaultsPatchValidator interface {
	ValidateAgentComposerDefaultsPatch(context.Context, string, preferencesbiz.AgentComposerDefaultsPatch) (AgentComposerDefaultsPatchValidation, error)
}

// AgentComposerDefaultsResolvedPublisher reports the per-field outcome of a
// defaults patch back to the client that requested it.
type AgentComposerDefaultsResolvedPublisher interface {
	PublishAgentComposerDefaultsResolved(context.Context, AgentComposerDefaultsResolvedInput) error
}

// AgentComposerDefaultsResolvedInput is the daemon-side shape of
// preferences.agent.composer.defaults.resolved. ClientMutationID is echoed back
// verbatim so the client can match the answer to its optimistic update; it is
// never persisted.
type AgentComposerDefaultsResolvedInput struct {
	AgentTargetID    string
	ClientMutationID string
	Applied          []string
	Rejected         []AgentComposerDefaultsPatchOutcome
}

type ChangeObserver func(
	context.Context,
	preferencesbiz.DesktopPreferences,
	preferencesbiz.DesktopPreferences,
)

type Service struct {
	Store                          workspacedata.PreferencesStore
	Publisher                      DesktopPreferencesPublisher
	AgentComposerDefaultsPublisher AgentComposerDefaultsPublisher
	AgentComposerDefaultsValidator AgentComposerDefaultsPatchValidator
	// AgentComposerDefaultsResolvedPublisher is optional: when unset the daemon
	// still applies and publishes the legal fields, the client just does not get
	// the per-field breakdown back.
	AgentComposerDefaultsResolvedPublisher AgentComposerDefaultsResolvedPublisher
	changeObservers                        []ChangeObserver
}

// RegisterChangeObserver adds a startup-wired observer for successful preference changes.
func (s *Service) RegisterChangeObserver(observer ChangeObserver) {
	if s == nil || observer == nil {
		return
	}
	s.changeObservers = append(s.changeObservers, observer)
}

type PatchAgentComposerDefaultsForTargetInput struct {
	AgentTargetID string
	Patch         preferencesbiz.AgentComposerDefaultsPatch
	// ClientMutationID is echoed back on the resolved event so the client can
	// correlate the answer with its optimistic update. It is never persisted.
	ClientMutationID string
}

type PatchAgentSessionLaunchModeInput struct {
	WorkspaceID       string
	ProjectSectionKey string
	Mode              string
}

type PutInput struct {
	AgentCLIUpdateCheckEnabled bool
	// AgentRuntimeKeepAliveEnabled / AgentRuntimeIdleMinutes 是 DINTAL-5308 的两个旋钮。
	// 用指针是为了把「没表态」和「表态成 false / 0」分开：0 分钟在这里是**永不回收**
	// 的合法取值，false 是「别常驻」的合法取值，两个都不能被「字段没填」冒充。
	// nil 一律沿用库里已有的值。
	AgentRuntimeKeepAliveEnabled *bool
	AgentRuntimeIdleMinutes      *int
	// AgentComposerDefaultsByProvider is accepted for wire compatibility but
	// ignored on write: the legacy provider-keyed defaults are frozen after
	// the one-time migration onto AgentComposerDefaultsByAgentTarget.
	AgentComposerDefaultsByProvider             map[string]preferencesbiz.AgentComposerDefaults
	AgentComposerDefaultsByAgentTarget          map[string]preferencesbiz.AgentComposerDefaults
	AgentGUIConversationRailCollapsedByProvider map[string]bool
	// AgentSessionLaunchModesByWorkspace is accepted for wire compatibility but
	// ignored on write. Launch modes mutate through PatchAgentSessionLaunchMode
	// so concurrent workspace windows cannot replace the global map.
	AgentSessionLaunchModesByWorkspace    *map[string]map[string]string
	AgentConversationDetailMode           string
	AgentDockLayout                       string
	AppCatalogChannel                     string
	BrowserUseConnectionMode              string
	DefaultAgentProvider                  string
	DockIconStyle                         string
	DockPlacement                         string
	DeletedAgentConversationRetentionDays int
	FileDefaultOpenersByExtension         map[string]string
	FeatureFlags                          map[string]bool
	WorkbenchShortcuts                    preferencesbiz.DesktopWorkbenchShortcuts
	Locale                                string
	MinimizeAnimation                     string
	SleepPreventionMode                   string
	ShowAppDeveloperSources               bool
	ThemeSource                           string
	UpdateChannel                         string
	UpdatePolicy                          string
	WindowSnapping                        *DesktopWindowSnappingInput
}

type DesktopWindowSnappingInput struct {
	Enabled        bool
	ShortcutPreset string
}

func (s Service) Get(ctx context.Context) (preferencesbiz.DesktopPreferences, error) {
	if s.Store == nil {
		return preferencesbiz.DesktopPreferences{}, errors.New("desktop preferences store is not configured")
	}

	return s.Store.GetDesktopPreferences(ctx)
}

func (s Service) GetAgentComposerDefaultsForTarget(
	ctx context.Context,
	agentTargetID string,
) (preferencesbiz.AgentComposerDefaults, error) {
	stored, err := s.Get(ctx)
	if err != nil {
		return preferencesbiz.AgentComposerDefaults{}, err
	}
	return stored.AgentComposerDefaultsByAgentTarget[strings.TrimSpace(agentTargetID)], nil
}

func (s Service) PatchAgentComposerDefaultsForTarget(
	ctx context.Context,
	input PatchAgentComposerDefaultsForTargetInput,
) (preferencesbiz.AgentComposerDefaults, error) {
	if s.Store == nil {
		return preferencesbiz.AgentComposerDefaults{}, errors.New("desktop preferences store is not configured")
	}
	agentTargetID := strings.TrimSpace(input.AgentTargetID)
	if agentTargetID == "" {
		return preferencesbiz.AgentComposerDefaults{}, errors.New("agent target id is required")
	}
	patch, err := normalizeAgentComposerDefaultsPatch(input.Patch)
	if err != nil {
		return preferencesbiz.AgentComposerDefaults{}, err
	}
	if s.AgentComposerDefaultsValidator == nil {
		return preferencesbiz.AgentComposerDefaults{}, errors.New("agent composer defaults validator is not configured")
	}
	validation, err := s.AgentComposerDefaultsValidator.ValidateAgentComposerDefaultsPatch(ctx, agentTargetID, patch)
	if err != nil {
		return preferencesbiz.AgentComposerDefaults{}, err
	}
	applied := validation.Applied
	if len(applied) == 0 {
		// Every field was refused. Publishing a changed event would tell the
		// client the stored defaults moved when they did not, so only the
		// resolved event goes out.
		return preferencesbiz.AgentComposerDefaults{}, s.publishAgentComposerDefaultsResolved(
			ctx,
			agentTargetID,
			input.ClientMutationID,
			nil,
			validation.Rejected,
		)
	}
	patchStore, ok := s.Store.(workspacedata.AgentComposerDefaultsPatchStore)
	if !ok {
		return preferencesbiz.AgentComposerDefaults{}, errors.New("agent composer defaults patch store is not configured")
	}
	defaults, err := patchStore.PatchAgentComposerDefaultsForTarget(ctx, agentTargetID, applied)
	if err != nil {
		return preferencesbiz.AgentComposerDefaults{}, err
	}
	if s.AgentComposerDefaultsPublisher != nil {
		if err := s.AgentComposerDefaultsPublisher.PublishAgentComposerDefaultsChanged(ctx, agentTargetID); err != nil {
			return preferencesbiz.AgentComposerDefaults{}, err
		}
	}
	if err := s.publishAgentComposerDefaultsResolved(
		ctx,
		agentTargetID,
		input.ClientMutationID,
		agentComposerDefaultsAppliedFieldNames(applied),
		validation.Rejected,
	); err != nil {
		return preferencesbiz.AgentComposerDefaults{}, err
	}
	return defaults, nil
}

// publishAgentComposerDefaultsResolved emits the per-field outcome. It is
// deliberately best-effort-neutral: a missing publisher is not an error, but a
// publisher that fails is, because the client is otherwise left waiting.
func (s Service) publishAgentComposerDefaultsResolved(
	ctx context.Context,
	agentTargetID string,
	clientMutationID string,
	applied []string,
	rejected []AgentComposerDefaultsPatchOutcome,
) error {
	if s.AgentComposerDefaultsResolvedPublisher == nil {
		return nil
	}
	return s.AgentComposerDefaultsResolvedPublisher.PublishAgentComposerDefaultsResolved(
		ctx,
		AgentComposerDefaultsResolvedInput{
			AgentTargetID:    agentTargetID,
			ClientMutationID: strings.TrimSpace(clientMutationID),
			Applied:          applied,
			Rejected:         rejected,
		},
	)
}

// agentComposerDefaultsAppliedFieldNames lists applied fields in the daemon's
// stable field order so two identical patches produce identical payloads.
func agentComposerDefaultsAppliedFieldNames(
	applied preferencesbiz.AgentComposerDefaultsPatch,
) []string {
	order := []string{
		preferencesbiz.AgentComposerDefaultsFieldModel,
		preferencesbiz.AgentComposerDefaultsFieldPermissionModeID,
		preferencesbiz.AgentComposerDefaultsFieldReasoningEffort,
		preferencesbiz.AgentComposerDefaultsFieldSpeed,
		preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode,
	}
	result := make([]string, 0, len(applied))
	for _, field := range order {
		if _, ok := applied[field]; ok {
			result = append(result, field)
		}
	}
	return result
}

func (s Service) PatchAgentSessionLaunchMode(
	ctx context.Context,
	input PatchAgentSessionLaunchModeInput,
) (preferencesbiz.DesktopPreferences, error) {
	if s.Store == nil {
		return preferencesbiz.DesktopPreferences{}, errors.New("desktop preferences store is not configured")
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	projectSectionKey := strings.TrimSpace(input.ProjectSectionKey)
	mode := strings.TrimSpace(input.Mode)
	if workspaceID == "" {
		return preferencesbiz.DesktopPreferences{}, errors.New("workspace id is required")
	}
	if projectSectionKey == "" {
		return preferencesbiz.DesktopPreferences{}, errors.New("project section key is required")
	}
	if mode != "local" && mode != "worktree" {
		return preferencesbiz.DesktopPreferences{}, errors.New("agent session launch mode is unsupported")
	}
	patchStore, ok := s.Store.(workspacedata.AgentSessionLaunchModePatchStore)
	if !ok {
		return preferencesbiz.DesktopPreferences{}, errors.New("agent session launch mode patch store is not configured")
	}
	previous, err := s.Store.GetDesktopPreferences(ctx)
	if err != nil {
		return preferencesbiz.DesktopPreferences{}, err
	}
	preferences, err := patchStore.PatchAgentSessionLaunchMode(ctx, workspaceID, projectSectionKey, mode)
	if err != nil {
		return preferencesbiz.DesktopPreferences{}, err
	}
	for _, observer := range s.changeObservers {
		observer(ctx, previous, preferences)
	}
	if s.Publisher != nil {
		_ = s.Publisher.PublishDesktopPreferencesUpdated(ctx, preferences)
	}
	return preferences, nil
}

func (s Service) Put(ctx context.Context, input PutInput) (preferencesbiz.DesktopPreferences, error) {
	if s.Store == nil {
		return preferencesbiz.DesktopPreferences{}, errors.New("desktop preferences store is not configured")
	}

	stored, err := s.Store.GetDesktopPreferences(ctx)
	if err != nil {
		return preferencesbiz.DesktopPreferences{}, err
	}

	windowSnapping := resolveWindowSnapping(stored, input.WindowSnapping)
	preferences, err := s.Store.PutDesktopPreferences(ctx, preferencesbiz.DesktopPreferences{
		AgentCLIUpdateCheckEnabled:   input.AgentCLIUpdateCheckEnabled,
		AgentRuntimeKeepAliveEnabled: resolveAgentRuntimeKeepAlive(stored, input.AgentRuntimeKeepAliveEnabled),
		AgentRuntimeIdleMinutes:      resolveAgentRuntimeIdleMinutes(stored, input.AgentRuntimeIdleMinutes),
		// The legacy provider-keyed defaults are frozen: client input is
		// ignored so nothing writes the old field anymore; the stored value
		// is only kept for downgrade compatibility and should pass through
		// unchanged.
		AgentComposerDefaultsByProvider: stored.AgentComposerDefaultsByProvider,
		// Target defaults are frozen on the full preferences mutation. Only the
		// dedicated daemon-side field patch may change this map.
		AgentComposerDefaultsByAgentTarget:          stored.AgentComposerDefaultsByAgentTarget,
		AgentGUIConversationRailCollapsedByProvider: normalizeAgentGUIConversationRailCollapsedByProvider(input.AgentGUIConversationRailCollapsedByProvider),
		// Launch modes are frozen on the full preferences mutation. Only the
		// dedicated daemon-side single-key patch may change this map.
		AgentSessionLaunchModesByWorkspace:    stored.AgentSessionLaunchModesByWorkspace,
		AgentConversationDetailMode:           preferencesbiz.NormalizeDesktopAgentConversationDetailMode(input.AgentConversationDetailMode),
		AgentDockLayout:                       normalizeAgentDockLayout(input.AgentDockLayout),
		AppCatalogChannel:                     normalizeAppCatalogChannel(input.AppCatalogChannel),
		BrowserUseConnectionMode:              normalizeBrowserUseConnectionMode(input.BrowserUseConnectionMode),
		DefaultAgentProvider:                  normalizeDefaultAgentProvider(input.DefaultAgentProvider),
		DockIconStyle:                         strings.TrimSpace(input.DockIconStyle),
		DockPlacement:                         strings.TrimSpace(input.DockPlacement),
		DeletedAgentConversationRetentionDays: preferencesbiz.NormalizeDeletedAgentConversationRetentionDays(input.DeletedAgentConversationRetentionDays),
		FileDefaultOpenersByExtension:         normalizeFileDefaultOpenersByExtension(input.FileDefaultOpenersByExtension),
		Initialized:                           true,
		FeatureFlags:                          preferencesbiz.NormalizeDesktopFeatureFlags(input.FeatureFlags),
		WorkbenchShortcuts:                    preferencesbiz.NormalizeDesktopWorkbenchShortcuts(input.WorkbenchShortcuts),
		Locale:                                strings.TrimSpace(input.Locale),
		MinimizeAnimation:                     normalizeMinimizeAnimation(input.MinimizeAnimation),
		SleepPreventionMode:                   strings.TrimSpace(input.SleepPreventionMode),
		ShowAppDeveloperSources:               input.ShowAppDeveloperSources,
		ThemeSource:                           strings.TrimSpace(input.ThemeSource),
		UpdateChannel:                         strings.TrimSpace(input.UpdateChannel),
		UpdatePolicy:                          strings.TrimSpace(input.UpdatePolicy),
		WindowSnappingEnabled:                 windowSnapping.Enabled,
		WindowSnappingShortcutPreset:          windowSnapping.ShortcutPreset,
	})
	if err != nil {
		return preferencesbiz.DesktopPreferences{}, err
	}
	for _, observer := range s.changeObservers {
		observer(ctx, stored, preferences)
	}
	if s.Publisher != nil {
		_ = s.Publisher.PublishDesktopPreferencesUpdated(ctx, preferences)
	}
	return preferences, nil
}

func normalizeAgentComposerDefaultsPatch(
	input preferencesbiz.AgentComposerDefaultsPatch,
) (preferencesbiz.AgentComposerDefaultsPatch, error) {
	if len(input) == 0 {
		return nil, errors.New("agent composer defaults patch is empty")
	}
	result := make(preferencesbiz.AgentComposerDefaultsPatch, len(input))
	for field, value := range input {
		switch field {
		case preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode:
			enabled, ok := value.(bool)
			if !ok {
				return nil, errors.New("agent composer defaults codexSaverMode must be boolean")
			}
			result[field] = enabled
			continue
		case preferencesbiz.AgentComposerDefaultsFieldModel,
			preferencesbiz.AgentComposerDefaultsFieldPermissionModeID,
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort,
			preferencesbiz.AgentComposerDefaultsFieldSpeed:
		default:
			return nil, errors.New("agent composer defaults patch contains an unsupported field")
		}
		if value == nil {
			result[field] = nil
			continue
		}
		normalized := ""
		switch typed := value.(type) {
		case string:
			normalized = strings.TrimSpace(typed)
		case *string:
			if typed != nil {
				normalized = strings.TrimSpace(*typed)
			}
		default:
			return nil, errors.New("agent composer defaults text patch values must be strings or null")
		}
		if normalized == "" {
			return nil, errors.New("agent composer defaults patch values must be non-empty or null")
		}
		result[field] = normalized
	}
	return result, nil
}

// resolveAgentRuntimeKeepAlive / resolveAgentRuntimeIdleMinutes：没表态就沿用库里的，
// 与 resolveWindowSnapping 同一条口径 —— 整份偏好是全量写入的，缺字段不能当成清零。
func resolveAgentRuntimeKeepAlive(stored preferencesbiz.DesktopPreferences, input *bool) bool {
	if input == nil {
		return stored.AgentRuntimeKeepAliveEnabled
	}
	return *input
}

func resolveAgentRuntimeIdleMinutes(stored preferencesbiz.DesktopPreferences, input *int) int {
	if input == nil {
		return preferencesbiz.NormalizeDesktopAgentRuntimeIdleMinutes(stored.AgentRuntimeIdleMinutes)
	}
	return preferencesbiz.NormalizeDesktopAgentRuntimeIdleMinutes(*input)
}

func resolveWindowSnapping(stored preferencesbiz.DesktopPreferences, input *DesktopWindowSnappingInput) DesktopWindowSnappingInput {
	if input != nil {
		return DesktopWindowSnappingInput{
			Enabled:        input.Enabled,
			ShortcutPreset: normalizeWindowSnappingShortcutPreset(input.ShortcutPreset),
		}
	}

	return DesktopWindowSnappingInput{
		Enabled:        stored.WindowSnappingEnabled,
		ShortcutPreset: normalizeWindowSnappingShortcutPreset(stored.WindowSnappingShortcutPreset),
	}
}

func normalizeDefaultAgentProvider(value string) string {
	normalized := agentproviderbiz.Normalize(value)
	if preferencesbiz.IsDesktopDefaultAgentProvider(normalized) {
		return normalized
	}
	return preferencesbiz.DefaultDesktopDefaultAgentProvider
}

func normalizeAgentDockLayout(value string) string {
	normalized := strings.TrimSpace(value)
	if preferencesbiz.IsDesktopAgentDockLayout(normalized) {
		return normalized
	}
	return preferencesbiz.DefaultDesktopAgentDockLayout
}

func normalizeAppCatalogChannel(value string) string {
	normalized := strings.TrimSpace(value)
	if preferencesbiz.IsDesktopAppCatalogChannel(normalized) {
		return normalized
	}
	return preferencesbiz.DefaultDesktopAppCatalogChannel
}

func normalizeFileDefaultOpenersByExtension(input map[string]string) map[string]string {
	if input == nil {
		return preferencesbiz.DefaultDesktopPreferences().FileDefaultOpenersByExtension
	}
	result := map[string]string{}
	for extension, opener := range input {
		normalizedExtension := preferencesbiz.NormalizeDesktopFileExtension(extension)
		if normalizedExtension == "" {
			continue
		}
		normalizedOpener := strings.TrimSpace(opener)
		if !preferencesbiz.IsDesktopFileDefaultOpener(normalizedOpener) {
			continue
		}
		result[normalizedExtension] = normalizedOpener
	}
	return result
}

func normalizeBrowserUseConnectionMode(value string) string {
	normalized := strings.TrimSpace(value)
	if preferencesbiz.IsDesktopBrowserUseConnectionMode(normalized) {
		return normalized
	}
	return preferencesbiz.DefaultDesktopBrowserUseConnectionMode
}

func normalizeMinimizeAnimation(value string) string {
	normalized := strings.TrimSpace(value)
	if preferencesbiz.IsDesktopMinimizeAnimation(normalized) {
		return normalized
	}
	return preferencesbiz.DefaultDesktopMinimizeAnimation
}

func normalizeWindowSnappingShortcutPreset(value string) string {
	normalized := strings.TrimSpace(value)
	if preferencesbiz.IsDesktopWindowSnappingShortcutPreset(normalized) {
		return normalized
	}
	return preferencesbiz.DefaultDesktopWindowSnappingShortcut
}

func normalizeAgentGUIConversationRailCollapsedByProvider(input map[string]bool) map[string]bool {
	result := map[string]bool{}
	for provider, collapsed := range input {
		normalizedProvider := agentproviderbiz.Normalize(provider)
		if normalizedProvider == "" {
			continue
		}
		result[normalizedProvider] = collapsed
	}
	return result
}
