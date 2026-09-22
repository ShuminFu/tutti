package preferences

import (
	"context"
	"errors"
	"testing"

	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

type preferencesStoreStub struct {
	getResult                    preferencesbiz.DesktopPreferences
	patchAgentTarget             string
	patchInput                   preferencesbiz.AgentComposerDefaultsPatch
	patchResult                  preferencesbiz.AgentComposerDefaults
	patchLaunchWorkspaceID       string
	patchLaunchProjectSectionKey string
	patchLaunchMode              string
	patchLaunchResult            preferencesbiz.DesktopPreferences
	putInput                     preferencesbiz.DesktopPreferences
}

type preferencesPublisherStub struct {
	published []preferencesbiz.DesktopPreferences
	err       error
}

func (s preferencesStoreStub) GetDesktopPreferences(context.Context) (preferencesbiz.DesktopPreferences, error) {
	return s.getResult, nil
}

func (s *preferencesStoreStub) PutDesktopPreferences(_ context.Context, preferences preferencesbiz.DesktopPreferences) (preferencesbiz.DesktopPreferences, error) {
	s.putInput = preferences
	return preferences, nil
}

func (s *preferencesStoreStub) PatchAgentComposerDefaultsForTarget(_ context.Context, agentTargetID string, patch preferencesbiz.AgentComposerDefaultsPatch) (preferencesbiz.AgentComposerDefaults, error) {
	s.patchAgentTarget = agentTargetID
	s.patchInput = patch
	return s.patchResult, nil
}

func (s *preferencesStoreStub) PatchAgentSessionLaunchMode(_ context.Context, workspaceID string, projectSectionKey string, mode string) (preferencesbiz.DesktopPreferences, error) {
	s.patchLaunchWorkspaceID = workspaceID
	s.patchLaunchProjectSectionKey = projectSectionKey
	s.patchLaunchMode = mode
	return s.patchLaunchResult, nil
}

type agentComposerDefaultsValidatorStub struct {
	agentTargetID string
	patch         preferencesbiz.AgentComposerDefaultsPatch
	rejected      []AgentComposerDefaultsPatchOutcome
	rejectAll     bool
	err           error
}

func (s *agentComposerDefaultsValidatorStub) ValidateAgentComposerDefaultsPatch(
	_ context.Context,
	agentTargetID string,
	patch preferencesbiz.AgentComposerDefaultsPatch,
) (AgentComposerDefaultsPatchValidation, error) {
	s.agentTargetID = agentTargetID
	s.patch = patch
	if s.err != nil {
		return AgentComposerDefaultsPatchValidation{}, s.err
	}
	// Mirror the real validator: rejected fields are withheld from Applied, and
	// everything else passes through.
	applied := preferencesbiz.AgentComposerDefaultsPatch{}
	for field, value := range patch {
		rejected := false
		for _, outcome := range s.rejected {
			if outcome.Field == field {
				rejected = true
				break
			}
		}
		if !rejected {
			applied[field] = value
		}
	}
	return AgentComposerDefaultsPatchValidation{Applied: applied, Rejected: s.rejected}, nil
}

type agentComposerDefaultsResolvedPublisherStub struct {
	inputs []AgentComposerDefaultsResolvedInput
	err    error
}

func (s *agentComposerDefaultsResolvedPublisherStub) PublishAgentComposerDefaultsResolved(
	_ context.Context,
	input AgentComposerDefaultsResolvedInput,
) error {
	s.inputs = append(s.inputs, input)
	return s.err
}

type agentComposerDefaultsPublisherStub struct {
	agentTargetIDs []string
}

func (s *agentComposerDefaultsPublisherStub) PublishAgentComposerDefaultsChanged(_ context.Context, agentTargetID string) error {
	s.agentTargetIDs = append(s.agentTargetIDs, agentTargetID)
	return nil
}

func (s *preferencesPublisherStub) PublishDesktopPreferencesUpdated(_ context.Context, preferences preferencesbiz.DesktopPreferences) error {
	s.published = append(s.published, preferences)
	return s.err
}

func TestServiceGetReturnsStoredDesktopPreferences(t *testing.T) {
	t.Parallel()

	service := Service{
		Store: &preferencesStoreStub{
			getResult: preferencesbiz.DesktopPreferences{
				DefaultAgentProvider: "claude-code",

				AgentDockLayout:          "unified",
				BrowserUseConnectionMode: "autoConnect",
				DockIconStyle:            "default",
				DockPlacement:            "left",
				Initialized:              true,
				Locale:                   "zh-CN",
				MinimizeAnimation:        "scale",
				SleepPreventionMode:      "whileAgentRunning",
				ThemeSource:              "dark",
				UpdateChannel:            "rc",
				UpdatePolicy:             "auto",
			},
		},
	}

	preferences, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !preferences.Initialized {
		t.Fatal("Get() initialized = false, want true")
	}
	if preferences.DockPlacement != "left" {
		t.Fatalf("Get() dockPlacement = %q, want left", preferences.DockPlacement)
	}
	if preferences.Locale != "zh-CN" {
		t.Fatalf("Get() locale = %q, want zh-CN", preferences.Locale)
	}
	if preferences.DefaultAgentProvider != "claude-code" {
		t.Fatalf("Get() defaultAgentProvider = %q, want claude-code", preferences.DefaultAgentProvider)
	}
	if preferences.AgentDockLayout != "unified" {
		t.Fatalf("Get() agentDockLayout = %q, want unified", preferences.AgentDockLayout)
	}
	if preferences.ThemeSource != "dark" {
		t.Fatalf("Get() themeSource = %q, want dark", preferences.ThemeSource)
	}
	if preferences.SleepPreventionMode != "whileAgentRunning" {
		t.Fatalf("Get() sleepPreventionMode = %q, want whileAgentRunning", preferences.SleepPreventionMode)
	}
	if preferences.BrowserUseConnectionMode != "autoConnect" {
		t.Fatalf("Get() browserUseConnectionMode = %q, want autoConnect", preferences.BrowserUseConnectionMode)
	}
	if preferences.UpdateChannel != "rc" {
		t.Fatalf("Get() updateChannel = %q, want rc", preferences.UpdateChannel)
	}
	if preferences.UpdatePolicy != "auto" {
		t.Fatalf("Get() updatePolicy = %q, want auto", preferences.UpdatePolicy)
	}
}

func TestServicePutNotifiesChangeObserversWithPreviousAndCurrentPreferences(t *testing.T) {
	store := &preferencesStoreStub{getResult: preferencesbiz.DesktopPreferences{
		FeatureFlags: map[string]bool{"agent.extension.gemini": false},
	}}
	var previous, current preferencesbiz.DesktopPreferences
	service := Service{
		Store: store,
	}
	observed := 0
	service.RegisterChangeObserver(func(_ context.Context, before, after preferencesbiz.DesktopPreferences) {
		previous = before
		current = after
	})
	service.RegisterChangeObserver(func(context.Context, preferencesbiz.DesktopPreferences, preferencesbiz.DesktopPreferences) {
		observed++
	})

	_, err := service.Put(context.Background(), PutInput{
		FeatureFlags: map[string]bool{"agent.extension.gemini": true},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if previous.FeatureFlags["agent.extension.gemini"] {
		t.Fatalf("previous feature flags = %#v", previous.FeatureFlags)
	}
	if !current.FeatureFlags["agent.extension.gemini"] {
		t.Fatalf("current feature flags = %#v", current.FeatureFlags)
	}
	if observed != 1 {
		t.Fatalf("second observer calls = %d, want 1", observed)
	}
}

func TestServicePutPreservesAgentSessionLaunchModesWhenFieldIsOmitted(t *testing.T) {
	t.Parallel()

	storedLaunchModes := map[string]map[string]string{
		"workspace-a": {"project:/alpha": "worktree"},
	}
	store := &preferencesStoreStub{getResult: preferencesbiz.DesktopPreferences{
		AgentSessionLaunchModesByWorkspace: storedLaunchModes,
	}}
	service := Service{Store: store}

	if _, err := service.Put(context.Background(), PutInput{}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := store.putInput.AgentSessionLaunchModesByWorkspace["workspace-a"]["project:/alpha"]; got != "worktree" {
		t.Fatalf("stored Agent Session launch mode = %q, want worktree", got)
	}
}

func TestServicePutPreservesAgentSessionLaunchModesWhenStaleFieldIsProvided(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{getResult: preferencesbiz.DesktopPreferences{
		AgentSessionLaunchModesByWorkspace: map[string]map[string]string{
			"workspace-a": {"project:/alpha": "worktree"},
		},
	}}
	service := Service{Store: store}
	stale := map[string]map[string]string{
		"workspace-b": {"project:/beta": "local"},
	}
	if _, err := service.Put(context.Background(), PutInput{AgentSessionLaunchModesByWorkspace: &stale}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := store.putInput.AgentSessionLaunchModesByWorkspace; len(got) != 1 || got["workspace-a"]["project:/alpha"] != "worktree" {
		t.Fatalf("launch modes = %#v, want authoritative stored map", got)
	}
}

func TestServicePatchAgentSessionLaunchModeUsesDedicatedStoreAndPublishes(t *testing.T) {
	t.Parallel()

	result := preferencesbiz.DesktopPreferences{
		AgentSessionLaunchModesByWorkspace: map[string]map[string]string{
			"workspace-a": {"project:/alpha": "worktree"},
		},
	}
	store := &preferencesStoreStub{patchLaunchResult: result}
	publisher := &preferencesPublisherStub{}
	service := Service{Store: store, Publisher: publisher}
	got, err := service.PatchAgentSessionLaunchMode(context.Background(), PatchAgentSessionLaunchModeInput{
		WorkspaceID:       " workspace-a ",
		ProjectSectionKey: " project:/alpha ",
		Mode:              " worktree ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.patchLaunchWorkspaceID != "workspace-a" || store.patchLaunchProjectSectionKey != "project:/alpha" || store.patchLaunchMode != "worktree" {
		t.Fatalf("patch input = %q/%q/%q", store.patchLaunchWorkspaceID, store.patchLaunchProjectSectionKey, store.patchLaunchMode)
	}
	if got.AgentSessionLaunchModesByWorkspace["workspace-a"]["project:/alpha"] != "worktree" {
		t.Fatalf("patch result = %#v", got.AgentSessionLaunchModesByWorkspace)
	}
	if len(publisher.published) != 1 || publisher.published[0].AgentSessionLaunchModesByWorkspace["workspace-a"]["project:/alpha"] != "worktree" {
		t.Fatalf("published preferences = %#v", publisher.published)
	}
}

func TestServicePutTrimsDesktopPreferences(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{}
	publisher := &preferencesPublisherStub{}
	service := Service{
		Store:     store,
		Publisher: publisher,
	}

	preferences, err := service.Put(context.Background(), PutInput{
		AgentCLIUpdateCheckEnabled: true,
		AgentComposerDefaultsByProvider: map[string]preferencesbiz.AgentComposerDefaults{
			" claude ": {
				Model:            " claude-3-5 ",
				PermissionModeID: " full-access ",
				ReasoningEffort:  " high ",
			},
			"codex": {},
		},
		AgentGUIConversationRailCollapsedByProvider: map[string]bool{
			" codex ": true,
			"claude":  false,
			"unknown": true,
		},
		AgentConversationDetailMode: " general ",
		AgentDockLayout:             " unified ",
		DefaultAgentProvider:        " claude ",

		BrowserUseConnectionMode: " autoConnect ",
		DockIconStyle:            "default",
		DockPlacement:            " left ",
		FileDefaultOpenersByExtension: map[string]string{
			".HTML":   " fileViewer ",
			"bad/ext": "defaultBrowser",
			"pdf":     "defaultBrowser",
			"txt":     "unknown",
			"_tmp":    "system",
		},
		Locale:              " zh-CN ",
		MinimizeAnimation:   "scale",
		SleepPreventionMode: "whileAgentRunning",
		ThemeSource:         " dark ",
		UpdateChannel:       " rc ",
		UpdatePolicy:        " auto ",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !preferences.Initialized {
		t.Fatal("Put() initialized = false, want true")
	}
	if !store.putInput.AgentCLIUpdateCheckEnabled {
		t.Fatal("stored agent CLI update check = false, want true")
	}
	if store.putInput.DockPlacement != "left" {
		t.Fatalf("stored dockPlacement = %q, want left", store.putInput.DockPlacement)
	}
	if store.putInput.Locale != "zh-CN" {
		t.Fatalf("stored locale = %q, want zh-CN", store.putInput.Locale)
	}
	if store.putInput.DefaultAgentProvider != "claude-code" {
		t.Fatalf("stored defaultAgentProvider = %q, want claude-code", store.putInput.DefaultAgentProvider)
	}
	if store.putInput.AgentConversationDetailMode != "general" {
		t.Fatalf("stored agentConversationDetailMode = %q, want general", store.putInput.AgentConversationDetailMode)
	}
	if store.putInput.AgentDockLayout != "unified" {
		t.Fatalf("stored agentDockLayout = %q, want unified", store.putInput.AgentDockLayout)
	}
	if store.putInput.ThemeSource != "dark" {
		t.Fatalf("stored themeSource = %q, want dark", store.putInput.ThemeSource)
	}
	if store.putInput.SleepPreventionMode != "whileAgentRunning" {
		t.Fatalf("stored sleepPreventionMode = %q, want whileAgentRunning", store.putInput.SleepPreventionMode)
	}
	if store.putInput.BrowserUseConnectionMode != "autoConnect" {
		t.Fatalf("stored browserUseConnectionMode = %q, want autoConnect", store.putInput.BrowserUseConnectionMode)
	}
	if store.putInput.UpdateChannel != "rc" {
		t.Fatalf("stored updateChannel = %q, want rc", store.putInput.UpdateChannel)
	}
	if store.putInput.UpdatePolicy != "auto" {
		t.Fatalf("stored updatePolicy = %q, want auto", store.putInput.UpdatePolicy)
	}
	if store.putInput.FileDefaultOpenersByExtension["html"] != "fileViewer" ||
		store.putInput.FileDefaultOpenersByExtension["pdf"] != "defaultBrowser" ||
		len(store.putInput.FileDefaultOpenersByExtension) != 2 {
		t.Fatalf("stored file openers = %#v, want normalized html/pdf", store.putInput.FileDefaultOpenersByExtension)
	}
	// The legacy provider-keyed defaults are frozen: client input is ignored
	// and the stored value (empty in this stub) is written back instead.
	if len(store.putInput.AgentComposerDefaultsByProvider) != 0 {
		t.Fatalf("stored provider defaults = %#v, want legacy input ignored", store.putInput.AgentComposerDefaultsByProvider)
	}
	if !store.putInput.AgentGUIConversationRailCollapsedByProvider["codex"] {
		t.Fatal("stored codex rail collapsed = false, want true")
	}
	if collapsed, ok := store.putInput.AgentGUIConversationRailCollapsedByProvider["claude-code"]; !ok || collapsed {
		t.Fatalf("stored claude rail collapsed = %v/%v, want present false", collapsed, ok)
	}
	if _, ok := store.putInput.AgentGUIConversationRailCollapsedByProvider["unknown"]; ok {
		t.Fatal("stored unknown rail collapsed provider")
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published len = %d, want 1", len(publisher.published))
	}
	if publisher.published[0].DockPlacement != "left" ||
		publisher.published[0].Locale != "zh-CN" ||
		publisher.published[0].DefaultAgentProvider != "claude-code" ||
		publisher.published[0].AgentConversationDetailMode != "general" ||
		publisher.published[0].AgentDockLayout != "unified" ||
		publisher.published[0].ThemeSource != "dark" ||
		publisher.published[0].SleepPreventionMode != "whileAgentRunning" ||
		publisher.published[0].BrowserUseConnectionMode != "autoConnect" ||
		publisher.published[0].UpdateChannel != "rc" ||
		publisher.published[0].UpdatePolicy != "auto" {
		t.Fatalf("published preferences = %#v, want left/zh-CN/dark/prevent-sleep/autoConnect/rc/auto", publisher.published[0])
	}
}

func TestServicePutNormalizesAgentDockLayout(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "unified"},
		{name: "invalid", input: "stacked", want: "unified"},
		{name: "legacy", input: "legacySplit", want: "legacySplit"},
		{name: "unified", input: "unified", want: "unified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &preferencesStoreStub{}
			service := Service{Store: store}
			preferences, err := service.Put(context.Background(), PutInput{
				AgentConversationDetailMode: "coding",
				AgentDockLayout:             tc.input,
				AppCatalogChannel:           "production",
				DefaultAgentProvider:        "codex",
				DockIconStyle:               "default",
				DockPlacement:               "bottom",
				Locale:                      "en",
				MinimizeAnimation:           "scale",
				SleepPreventionMode:         "never",
				ThemeSource:                 "dark",
				UpdateChannel:               "rc",
				UpdatePolicy:                "prompt",
			})
			if err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			if preferences.AgentDockLayout != tc.want {
				t.Fatalf("Put() agentDockLayout = %q, want %q", preferences.AgentDockLayout, tc.want)
			}
			if store.putInput.AgentDockLayout != tc.want {
				t.Fatalf("stored agentDockLayout = %q, want %q", store.putInput.AgentDockLayout, tc.want)
			}
		})
	}
}

func TestServicePutNormalizesAgentConversationDetailMode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "coding"},
		{name: "invalid", input: "daily", want: "coding"},
		{name: "coding", input: "coding", want: "coding"},
		{name: "general", input: "general", want: "general"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &preferencesStoreStub{}
			service := Service{Store: store}
			preferences, err := service.Put(context.Background(), PutInput{
				AgentConversationDetailMode: tc.input,
				AppCatalogChannel:           "production",
				DefaultAgentProvider:        "codex",
				DockIconStyle:               "default",
				DockPlacement:               "bottom",
				Locale:                      "en",
				MinimizeAnimation:           "scale",
				SleepPreventionMode:         "never",
				ThemeSource:                 "dark",
				UpdateChannel:               "rc",
				UpdatePolicy:                "prompt",
			})
			if err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			if preferences.AgentConversationDetailMode != tc.want {
				t.Fatalf("Put() agentConversationDetailMode = %q, want %q", preferences.AgentConversationDetailMode, tc.want)
			}
			if store.putInput.AgentConversationDetailMode != tc.want {
				t.Fatalf("stored agentConversationDetailMode = %q, want %q", store.putInput.AgentConversationDetailMode, tc.want)
			}
		})
	}
}

func TestServicePutPreservesWindowSnappingWhenOmitted(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{
		getResult: preferencesbiz.DesktopPreferences{
			WindowSnappingEnabled:        true,
			WindowSnappingShortcutPreset: "commandShiftArrows",
		},
	}
	service := Service{Store: store}

	preferences, err := service.Put(context.Background(), PutInput{
		DefaultAgentProvider: "codex",

		DockIconStyle:       "default",
		DockPlacement:       "left",
		Locale:              "zh-CN",
		MinimizeAnimation:   "scale",
		SleepPreventionMode: "whileAgentRunning",
		ThemeSource:         "dark",
		UpdateChannel:       "stable",
		UpdatePolicy:        "prompt",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !preferences.WindowSnappingEnabled {
		t.Fatal("Put() window snapping enabled = false, want true")
	}
	if preferences.WindowSnappingShortcutPreset != "commandShiftArrows" {
		t.Fatalf("Put() window snapping shortcut = %q, want commandShiftArrows", preferences.WindowSnappingShortcutPreset)
	}
	if !store.putInput.WindowSnappingEnabled {
		t.Fatal("stored window snapping enabled = false, want true")
	}
	if store.putInput.WindowSnappingShortcutPreset != "commandShiftArrows" {
		t.Fatalf("stored window snapping shortcut = %q, want commandShiftArrows", store.putInput.WindowSnappingShortcutPreset)
	}
}

func TestServicePutAppliesWindowSnappingWhenProvided(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{
		getResult: preferencesbiz.DesktopPreferences{
			WindowSnappingEnabled:        true,
			WindowSnappingShortcutPreset: "commandShiftArrows",
		},
	}
	service := Service{Store: store}

	preferences, err := service.Put(context.Background(), PutInput{
		DefaultAgentProvider: "codex",

		DockIconStyle:       "default",
		DockPlacement:       "left",
		Locale:              "zh-CN",
		MinimizeAnimation:   "scale",
		SleepPreventionMode: "whileAgentRunning",
		ThemeSource:         "dark",
		UpdateChannel:       "stable",
		UpdatePolicy:        "prompt",
		WindowSnapping: &DesktopWindowSnappingInput{
			Enabled:        false,
			ShortcutPreset: " commandArrows ",
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if preferences.WindowSnappingEnabled {
		t.Fatal("Put() window snapping enabled = true, want false")
	}
	if preferences.WindowSnappingShortcutPreset != "commandArrows" {
		t.Fatalf("Put() window snapping shortcut = %q, want commandArrows", preferences.WindowSnappingShortcutPreset)
	}
	if store.putInput.WindowSnappingEnabled {
		t.Fatal("stored window snapping enabled = true, want false")
	}
	if store.putInput.WindowSnappingShortcutPreset != "commandArrows" {
		t.Fatalf("stored window snapping shortcut = %q, want commandArrows", store.putInput.WindowSnappingShortcutPreset)
	}
}

func TestServicePutReturnsStoredPreferencesWhenPublishFails(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{}
	publisher := &preferencesPublisherStub{err: errors.New("publish failed")}
	service := Service{
		Store:     store,
		Publisher: publisher,
	}

	preferences, err := service.Put(context.Background(), PutInput{
		DockPlacement:        "left",
		DefaultAgentProvider: "codex",

		DockIconStyle:       "default",
		Locale:              "zh-CN",
		MinimizeAnimation:   "scale",
		SleepPreventionMode: "whileAgentRunning",
		ThemeSource:         "dark",
		UpdateChannel:       "stable",
		UpdatePolicy:        "prompt",
	})
	if err != nil {
		t.Fatalf("Put() error = %v, want nil", err)
	}
	if !preferences.Initialized {
		t.Fatal("Put() initialized = false, want true")
	}
	if store.putInput.DockPlacement != "left" ||
		store.putInput.Locale != "zh-CN" ||
		store.putInput.DefaultAgentProvider != "codex" ||
		store.putInput.ThemeSource != "dark" ||
		store.putInput.SleepPreventionMode != "whileAgentRunning" ||
		store.putInput.UpdateChannel != "stable" ||
		store.putInput.UpdatePolicy != "prompt" {
		t.Fatalf("stored preferences = %#v, want left/zh-CN/dark/prevent-sleep/stable/prompt", store.putInput)
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published len = %d, want 1", len(publisher.published))
	}
}

func TestServiceGetDoesNotResurrectLegacyComposerDefaults(t *testing.T) {
	t.Parallel()

	// Legacy provider-keyed defaults were copied to agent target keys by a
	// one-time sqlite data migration; Get must not overlay them again, or a
	// user could never clear a migrated default.
	service := Service{
		Store: &preferencesStoreStub{
			getResult: preferencesbiz.DesktopPreferences{
				AgentComposerDefaultsByProvider: map[string]preferencesbiz.AgentComposerDefaults{
					"codex": {Model: "gpt-5", PermissionModeID: "full-access"},
				},
				AgentComposerDefaultsByAgentTarget: map[string]preferencesbiz.AgentComposerDefaults{},
				Initialized:                        true,
			},
		},
	}

	preferences, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(preferences.AgentComposerDefaultsByAgentTarget) != 0 {
		t.Fatalf("agent target defaults = %#v, want stored value without legacy overlay", preferences.AgentComposerDefaultsByAgentTarget)
	}
}

func TestServicePutIgnoresComposerDefaultsByAgentTarget(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{getResult: preferencesbiz.DesktopPreferences{
		AgentComposerDefaultsByAgentTarget: map[string]preferencesbiz.AgentComposerDefaults{
			"local:codex": {Model: "gpt-5"},
		},
	}}
	service := Service{Store: store}

	_, err := service.Put(context.Background(), PutInput{
		AgentComposerDefaultsByAgentTarget: map[string]preferencesbiz.AgentComposerDefaults{
			" local:codex ": {
				Model:            " gpt-5 ",
				PermissionModeID: " full-access ",
				ReasoningEffort:  " high ",
				Speed:            " fast ",
			},
			"local:claude-code": {},
			"  ":                {Model: "dropped"},
		},
		AgentConversationDetailMode: "coding",
		AgentDockLayout:             "unified",
		DefaultAgentProvider:        "codex",
		DockIconStyle:               "default",
		DockPlacement:               "bottom",
		Locale:                      "en",
		MinimizeAnimation:           "scale",
		SleepPreventionMode:         "never",
		ThemeSource:                 "dark",
		UpdateChannel:               "rc",
		UpdatePolicy:                "auto",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	stored := store.putInput.AgentComposerDefaultsByAgentTarget
	if len(stored) != 1 || stored["local:codex"].Model != "gpt-5" {
		t.Fatalf("stored agent target defaults = %#v, want frozen stored value", stored)
	}
}

func TestServicePatchAgentComposerDefaultsForTargetValidatesStoresAndInvalidates(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{patchResult: preferencesbiz.AgentComposerDefaults{
		Model: "gpt-5", PermissionModeID: "full-access",
	}}
	validator := &agentComposerDefaultsValidatorStub{}
	publisher := &agentComposerDefaultsPublisherStub{}
	service := Service{
		Store:                          store,
		AgentComposerDefaultsValidator: validator,
		AgentComposerDefaultsPublisher: publisher,
	}
	permission := " full-access "
	result, err := service.PatchAgentComposerDefaultsForTarget(context.Background(), PatchAgentComposerDefaultsForTargetInput{
		AgentTargetID: " local:codex ",
		Patch: preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldPermissionModeID: &permission,
		},
	})
	if err != nil {
		t.Fatalf("PatchAgentComposerDefaultsForTarget() error = %v", err)
	}
	if result.PermissionModeID != "full-access" {
		t.Fatalf("result = %#v", result)
	}
	if store.patchAgentTarget != "local:codex" || validator.agentTargetID != "local:codex" {
		t.Fatalf("targets store=%q validator=%q", store.patchAgentTarget, validator.agentTargetID)
	}
	if got, _ := store.patchInput[preferencesbiz.AgentComposerDefaultsFieldPermissionModeID].(string); got != "full-access" {
		t.Fatalf("stored permission = %q", got)
	}
	if len(publisher.agentTargetIDs) != 1 || publisher.agentTargetIDs[0] != "local:codex" {
		t.Fatalf("invalidations = %#v", publisher.agentTargetIDs)
	}
}

func TestServicePutFreezesLegacyComposerDefaultsByProvider(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{
		getResult: preferencesbiz.DesktopPreferences{
			AgentComposerDefaultsByProvider: map[string]preferencesbiz.AgentComposerDefaults{
				"codex":             {Model: "gpt-5"},
				"legacy-unknown":    {Model: "legacy-model"},
				" spaced-provider ": {Model: " legacy-whitespace "},
			},
			Initialized: true,
		},
	}
	service := Service{Store: store}

	_, err := service.Put(context.Background(), PutInput{
		AgentComposerDefaultsByProvider: map[string]preferencesbiz.AgentComposerDefaults{
			"codex":       {Model: "client-overwrite"},
			"claude-code": {Model: "client-new"},
		},
		AgentConversationDetailMode: "coding",
		AgentDockLayout:             "unified",
		DefaultAgentProvider:        "codex",
		DockIconStyle:               "default",
		DockPlacement:               "bottom",
		Locale:                      "en",
		MinimizeAnimation:           "scale",
		SleepPreventionMode:         "never",
		ThemeSource:                 "dark",
		UpdateChannel:               "rc",
		UpdatePolicy:                "auto",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	stored := store.putInput.AgentComposerDefaultsByProvider
	if len(stored) != 3 ||
		stored["codex"].Model != "gpt-5" ||
		stored["legacy-unknown"].Model != "legacy-model" ||
		stored[" spaced-provider "].Model != " legacy-whitespace " {
		t.Fatalf("stored provider defaults = %#v, want frozen stored value passed through verbatim", stored)
	}
}

func TestServicePutKeepsAgentTargetDefaultsWhenFieldOmitted(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{
		getResult: preferencesbiz.DesktopPreferences{
			AgentComposerDefaultsByAgentTarget: map[string]preferencesbiz.AgentComposerDefaults{
				"local:codex": {Model: "gpt-5"},
			},
			Initialized: true,
		},
	}
	service := Service{Store: store}

	basePut := PutInput{
		AgentConversationDetailMode: "coding",
		AgentDockLayout:             "unified",
		DefaultAgentProvider:        "codex",
		DockIconStyle:               "default",
		DockPlacement:               "bottom",
		Locale:                      "en",
		MinimizeAnimation:           "scale",
		SleepPreventionMode:         "never",
		ThemeSource:                 "dark",
		UpdateChannel:               "rc",
		UpdatePolicy:                "auto",
	}

	// A nil map (field omitted by an older client) keeps the stored defaults.
	if _, err := service.Put(context.Background(), basePut); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if store.putInput.AgentComposerDefaultsByAgentTarget["local:codex"].Model != "gpt-5" {
		t.Fatalf("stored agent target defaults = %#v, want preserved on omitted field", store.putInput.AgentComposerDefaultsByAgentTarget)
	}

	// An explicitly sent empty map is also ignored. Only the dedicated patch
	// mutation may change target defaults.
	clearedPut := basePut
	clearedPut.AgentComposerDefaultsByAgentTarget = map[string]preferencesbiz.AgentComposerDefaults{}
	if _, err := service.Put(context.Background(), clearedPut); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if store.putInput.AgentComposerDefaultsByAgentTarget["local:codex"].Model != "gpt-5" {
		t.Fatalf("stored agent target defaults = %#v, want preserved on explicit empty map", store.putInput.AgentComposerDefaultsByAgentTarget)
	}
}

// A refused field must not take its legal siblings down with it: the client
// sends every desired default in one snapshot.
func TestServicePatchAgentComposerDefaultsPersistsOnlyAppliedFields(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{patchResult: preferencesbiz.AgentComposerDefaults{
		PermissionModeID: "full-access",
	}}
	validator := &agentComposerDefaultsValidatorStub{
		rejected: []AgentComposerDefaultsPatchOutcome{
			{Field: "reasoningEffort", ReasonCode: "not_configurable"},
		},
	}
	publisher := &agentComposerDefaultsPublisherStub{}
	resolved := &agentComposerDefaultsResolvedPublisherStub{}
	service := Service{
		Store:                                  store,
		AgentComposerDefaultsValidator:         validator,
		AgentComposerDefaultsPublisher:         publisher,
		AgentComposerDefaultsResolvedPublisher: resolved,
	}
	permission := "full-access"
	reasoning := "ultra"
	_, err := service.PatchAgentComposerDefaultsForTarget(context.Background(), PatchAgentComposerDefaultsForTargetInput{
		AgentTargetID:    "local:codex",
		ClientMutationID: "mutation-3",
		Patch: preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldPermissionModeID: &permission,
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort:  &reasoning,
		},
	})
	if err != nil {
		t.Fatalf("PatchAgentComposerDefaultsForTarget() error = %v", err)
	}
	if len(publisher.agentTargetIDs) != 1 {
		t.Fatalf("changed invalidations = %#v, want one", publisher.agentTargetIDs)
	}
	if len(resolved.inputs) != 1 {
		t.Fatalf("resolved inputs = %#v, want one", resolved.inputs)
	}
	got := resolved.inputs[0]
	if got.ClientMutationID != "mutation-3" {
		t.Fatalf("resolved clientMutationId = %q, want mutation-3", got.ClientMutationID)
	}
	if len(got.Applied) != 1 || got.Applied[0] != preferencesbiz.AgentComposerDefaultsFieldPermissionModeID {
		t.Fatalf("resolved applied = %#v, want only permissionModeId", got.Applied)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].ReasonCode != "not_configurable" {
		t.Fatalf("resolved rejected = %#v", got.Rejected)
	}
}

// When nothing can be applied the stored defaults did not move, so a changed
// event would be a lie and no store write may happen.
func TestServicePatchAgentComposerDefaultsAllRejectedSkipsStoreAndChangedEvent(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{}
	validator := &agentComposerDefaultsValidatorStub{
		rejected: []AgentComposerDefaultsPatchOutcome{
			{Field: "reasoningEffort", ReasonCode: "not_configurable"},
		},
		rejectAll: true,
	}
	publisher := &agentComposerDefaultsPublisherStub{}
	resolved := &agentComposerDefaultsResolvedPublisherStub{}
	service := Service{
		Store:                                  store,
		AgentComposerDefaultsValidator:         validator,
		AgentComposerDefaultsPublisher:         publisher,
		AgentComposerDefaultsResolvedPublisher: resolved,
	}
	reasoning := "ultra"
	if _, err := service.PatchAgentComposerDefaultsForTarget(context.Background(), PatchAgentComposerDefaultsForTargetInput{
		AgentTargetID: "local:codex",
		Patch: preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort: &reasoning,
		},
	}); err != nil {
		t.Fatalf("PatchAgentComposerDefaultsForTarget() error = %v", err)
	}
	if store.patchAgentTarget != "" {
		t.Fatalf("store write happened for %q, want none", store.patchAgentTarget)
	}
	if len(publisher.agentTargetIDs) != 0 {
		t.Fatalf("changed events = %#v, want none", publisher.agentTargetIDs)
	}
	if len(resolved.inputs) != 1 || len(resolved.inputs[0].Applied) != 0 {
		t.Fatalf("resolved inputs = %#v, want one all-rejected report", resolved.inputs)
	}
}

// DINTAL-5308：常驻开关与 TTL 用指针传，是为了把「没表态」和「表态成 false / 0」
// 分开。这条测的是「没表态」不能把用户已经关掉的常驻悄悄改回来 ——
// 整份偏好是全量写入的，缺字段当成清零就会两边互相改回去，永远合不上。
func TestServicePutKeepsAgentRuntimeRetentionWhenFieldsAreOmitted(t *testing.T) {
	t.Parallel()

	store := &preferencesStoreStub{getResult: preferencesbiz.DesktopPreferences{
		AgentRuntimeKeepAliveEnabled: false,
		AgentRuntimeIdleMinutes:      5,
		AgentRuntimeMaxResident:      3,
	}}
	service := Service{Store: store}

	if _, err := service.Put(context.Background(), PutInput{}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if store.putInput.AgentRuntimeKeepAliveEnabled {
		t.Fatal("keep-alive was turned back on by an update that never mentioned it")
	}
	if store.putInput.AgentRuntimeIdleMinutes != 5 {
		t.Fatalf("idle minutes = %d, want the stored 5 preserved", store.putInput.AgentRuntimeIdleMinutes)
	}
	if store.putInput.AgentRuntimeMaxResident != 3 {
		t.Fatalf("max resident = %d, want the stored 3 preserved", store.putInput.AgentRuntimeMaxResident)
	}
}

// 条数上限同一条口径：0 是合法的「不限」，越界收回默认而不是夹边界。
func TestServicePutAcceptsUnlimitedAndRejectsOutOfRangeMaxResident(t *testing.T) {
	t.Parallel()

	unlimited := 0
	store := &preferencesStoreStub{getResult: preferencesbiz.DesktopPreferences{
		AgentRuntimeMaxResident: preferencesbiz.DefaultDesktopAgentRuntimeMaxResident,
	}}
	service := Service{Store: store}
	if _, err := service.Put(context.Background(), PutInput{AgentRuntimeMaxResident: &unlimited}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if store.putInput.AgentRuntimeMaxResident != 0 {
		t.Fatalf("max resident = %d, want 0 (unlimited) to survive", store.putInput.AgentRuntimeMaxResident)
	}

	tooLarge := preferencesbiz.MaxDesktopAgentRuntimeMaxResident + 1
	if _, err := service.Put(context.Background(), PutInput{AgentRuntimeMaxResident: &tooLarge}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if store.putInput.AgentRuntimeMaxResident != preferencesbiz.DefaultDesktopAgentRuntimeMaxResident {
		t.Fatalf("max resident = %d, want the out-of-range value normalized back to the default",
			store.putInput.AgentRuntimeMaxResident)
	}
}

// 表态成 0 分钟是合法的「永不回收」，不能被当成「没填」而顶成默认 30。
func TestServicePutAcceptsNeverReleaseAndRejectsOutOfRangeIdleMinutes(t *testing.T) {
	t.Parallel()

	never := 0
	store := &preferencesStoreStub{getResult: preferencesbiz.DesktopPreferences{
		AgentRuntimeKeepAliveEnabled: true,
		AgentRuntimeIdleMinutes:      preferencesbiz.DefaultDesktopAgentRuntimeIdleMinutes,
	}}
	service := Service{Store: store}
	if _, err := service.Put(context.Background(), PutInput{AgentRuntimeIdleMinutes: &never}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if store.putInput.AgentRuntimeIdleMinutes != 0 {
		t.Fatalf("idle minutes = %d, want 0 (never release) to survive", store.putInput.AgentRuntimeIdleMinutes)
	}

	// 越界值收回默认，而不是夹到边界：夹边界会把明显写错的数变成一个
	// 看着合理的数，用户再也不知道自己填错过。
	tooLarge := preferencesbiz.MaxDesktopAgentRuntimeIdleMinutes + 1
	if _, err := service.Put(context.Background(), PutInput{AgentRuntimeIdleMinutes: &tooLarge}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if store.putInput.AgentRuntimeIdleMinutes != preferencesbiz.DefaultDesktopAgentRuntimeIdleMinutes {
		t.Fatalf("idle minutes = %d, want the out-of-range value normalized back to the default",
			store.putInput.AgentRuntimeIdleMinutes)
	}
}
