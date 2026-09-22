package agent

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	market "github.com/tutti-os/tutti/packages/connector/host"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

func TestServiceGetComposerOptionsResolvesProviderFromAgentTargetID(t *testing.T) {
	runtime := newFakeRuntime()
	service := newIsolatedAgentService(runtime)
	service.AgentTargetStore = fakeAgentTargetStore{targets: defaultTestAgentTargets()}
	service.CapabilityLister = &recordingComposerCapabilityLister{}

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		AgentTargetID: agenttargetbiz.IDLocalCodex,
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if options.Provider != "codex" {
		t.Fatalf("provider = %q, want codex", options.Provider)
	}
	if options.RuntimeContext["agentTargetId"] != agenttargetbiz.IDLocalCodex {
		t.Fatalf("runtimeContext agentTargetId = %#v, want %q", options.RuntimeContext["agentTargetId"], agenttargetbiz.IDLocalCodex)
	}
}

func TestServiceGetComposerOptionsPreservesGenericExtensionTargetAndProjectsSignedLiveComposerData(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.startHook = func(input RuntimeStartInput, session ProviderRuntimeSession) ProviderRuntimeSession {
		if input.Visible != nil && !*input.Visible {
			runtimeContext := clonePayload(session.RuntimeContext)
			for key, value := range map[string]any{
				"configOptions": []any{
					map[string]any{
						"currentValue": "example-pro",
						"id":           "model-choice",
						"options": []any{
							map[string]any{"value": "example-pro", "name": "Example Pro"},
						},
					},
					map[string]any{
						"currentValue": "default",
						"id":           "approval-mode",
						"options": []any{
							map[string]any{"value": "default", "name": "Default"},
							map[string]any{"value": "acceptEdits", "name": "Accept edits"},
							map[string]any{"value": "auto", "name": "Auto"},
							map[string]any{"value": "dontAsk", "name": "Don't ask"},
							map[string]any{"value": "bypassPermissions", "name": "Bypass permissions"},
							map[string]any{"value": "fullAccess", "name": "Full access"},
							map[string]any{"value": "plan", "name": "Plan"},
						},
					},
					map[string]any{
						"currentValue": "enabled",
						"id":           "reasoning_effort",
						"runtimeId":    "thought_level",
						"options": []any{
							map[string]any{"value": "disabled", "name": "Off"},
							map[string]any{"value": "enabled", "name": "On"},
							map[string]any{"value": "deep", "name": "Deep"},
						},
					},
					map[string]any{
						"currentValue": "false",
						"id":           "sandbox",
						"options": []any{
							map[string]any{"value": "false", "name": "Off"},
							map[string]any{"value": "true", "name": "On"},
						},
					},
				},
				"capabilities": []any{"compact", "compact", "planMode", "unknown"},
				"availableCommands": []any{
					map[string]any{"name": "compact", "description": "Compact history", "inputHint": "optional focus"},
					map[string]any{"name": "status", "description": "Show status"},
					map[string]any{"name": "goal", "description": "Set goal"},
					map[string]any{"name": "plan", "description": "Toggle plan"},
					map[string]any{"name": "review", "description": "Review code"},
					map[string]any{"name": "effort", "description": "Set thinking effort"},
				},
			} {
				runtimeContext[key] = value
			}
			session.RuntimeContext = runtimeContext
		}
		return session
	}
	service := newIsolatedAgentService(runtime)
	service.AgentTargetStore = fakeAgentTargetStore{targets: map[string]agenttargetbiz.Target{
		"extension:example": {
			ID:            "extension:example",
			Provider:      "acp:example",
			LaunchRefJSON: `{"type":"agent_extension","extensionInstallationId":"example@1.0.0"}`,
			Name:          "Example Agent",
			Enabled:       true,
			Source:        agenttargetbiz.SourceSystem,
		},
	}}
	service.CapabilityLister = &recordingComposerCapabilityLister{}
	service.ExtensionComposerProfiles = extensionComposerProfileResolverStub{
		profile: ExtensionComposerProfile{
			Capabilities:             []string{"compact", "compact", "planMode", "unknown"},
			ModelConfigOptionID:      "model-choice",
			PermissionConfigOptionID: "approval-mode",
			ReasoningConfigOptionID:  "thought_level",
			PermissionModes: []ExtensionComposerPermissionMode{
				{RuntimeID: "default", Semantic: PermissionModeSemanticAskBeforeWrite},
				{RuntimeID: "acceptEdits", Semantic: PermissionModeSemanticAcceptEdits},
				{RuntimeID: "auto", Semantic: PermissionModeSemanticAuto},
				{RuntimeID: "dontAsk", Semantic: PermissionModeSemanticLockedDown},
				{RuntimeID: "bypassPermissions", Semantic: PermissionModeSemanticFullAccess},
				{RuntimeID: "fullAccess", Semantic: PermissionModeSemanticFullAccess},
			},
			SlashCommandCatalogAuthoritative: true,
			SlashCommands: []ExtensionComposerSlashCommand{
				{Name: "compact", Effect: string(providerregistry.SlashCommandEffectSubmitImmediate)},
				{Name: "status", Effect: string(providerregistry.SlashCommandEffectShowStatus)},
				{Name: "goal", Effect: string(providerregistry.SlashCommandEffectActivateGoalMode)},
				{Name: "plan", Effect: string(providerregistry.SlashCommandEffectTogglePlanMode)},
			},
		},
	}

	options, err := service.OpenComposerModelDropdown(context.Background(), ComposerOptionsInput{
		AgentTargetID: "extension:example",
		Provider:      "acp:example",
		WorkspaceID:   "workspace-1",
		Cwd:           t.TempDir(),
		Settings: ComposerSettings{
			PermissionModeID: "default",
			ReasoningEffort:  "deep",
		},
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if options.Provider != "acp:example" {
		t.Fatalf("provider = %q, want acp:example", options.Provider)
	}
	if options.RuntimeContext["agentTargetId"] != "extension:example" {
		t.Fatalf("runtimeContext agentTargetId = %#v, want extension:example", options.RuntimeContext["agentTargetId"])
	}
	if bound := baseModelOptions(options.ModelConfig.Options); !options.ModelConfig.Configurable || len(bound) != 1 || bound[0].Value != "example-pro" {
		t.Fatalf("modelConfig = %#v, want live extension model options", options.ModelConfig)
	}
	if !options.PermissionConfig.Configurable ||
		options.PermissionConfig.DefaultValue != "default" ||
		len(options.PermissionConfig.Modes) != 6 ||
		options.PermissionConfig.Modes[1].Semantic != PermissionModeSemanticAcceptEdits ||
		options.PermissionConfig.Modes[4].Semantic != PermissionModeSemanticFullAccess ||
		options.PermissionConfig.Modes[5].Semantic != PermissionModeSemanticFullAccess {
		t.Fatalf("permissionConfig = %#v, want runtime extension permission modes", options.PermissionConfig)
	}
	permissionModeIDs := make([]string, 0, len(options.PermissionConfig.Modes))
	for _, mode := range options.PermissionConfig.Modes {
		permissionModeIDs = append(permissionModeIDs, mode.ID)
	}
	if !slices.Equal(permissionModeIDs, []string{"default", "acceptEdits", "auto", "dontAsk", "bypassPermissions", "fullAccess"}) {
		t.Fatalf("permission mode ids = %#v, want exact runtime ids", permissionModeIDs)
	}
	if !options.ReasoningConfig.Configurable ||
		options.ReasoningConfig.CurrentValue != "enabled" ||
		len(options.ReasoningConfig.Options) != 3 ||
		options.ReasoningConfig.Options[2].Value != "deep" {
		t.Fatalf("reasoningConfig = %#v, want runtime extension thought_level options", options.ReasoningConfig)
	}
	if !slices.Equal(options.Capabilities, []string{"compact", "planMode"}) {
		t.Fatalf("capabilities = %#v, want deduplicated signed/live known capabilities", options.Capabilities)
	}
	if options.SlashCommandPolicy == nil ||
		!options.SlashCommandPolicy.CommandCatalogAuthoritative ||
		!slices.Equal(options.SlashCommandPolicy.FallbackCommands, []string{"compact", "status", "goal", "plan"}) ||
		len(options.SlashCommandPolicy.CommandEffects) != 4 ||
		options.SlashCommandPolicy.CommandEffects[2].Effect != providerregistry.SlashCommandEffectActivateGoalMode {
		t.Fatalf("slashCommandPolicy = %#v, want extension slash command profile", options.SlashCommandPolicy)
	}
	commands := options.Commands
	if len(commands) != 4 || commands[0].Name != "compact" || commands[0].Description != "Compact history" || commands[0].InputHint != "optional focus" || commands[3].Name != "plan" {
		t.Fatalf("commands = %#v, want filtered runtime commands", commands)
	}
	configOptions, ok := options.RuntimeContext["configOptions"].([]map[string]any)
	if !ok || len(configOptions) != 5 ||
		configOptions[0]["id"] != "model" ||
		configOptions[1]["id"] != "model-choice" ||
		configOptions[2]["id"] != "approval-mode" ||
		configOptions[3]["id"] != "reasoning_effort" ||
		configOptions[3]["runtimeId"] != "thought_level" ||
		configOptions[4]["id"] != "sandbox" {
		t.Fatalf("configOptions = %#v, want model + runtime extension config options", options.RuntimeContext["configOptions"])
	}
	if len(runtime.startCalls) != 1 || runtime.startCalls[0].ProviderTargetRef["kind"] != "agent_extension" {
		t.Fatalf("runtime start calls = %#v, want one target-scoped extension discovery", runtime.startCalls)
	}
	if runtime.startCalls[0].PermissionModeID != "default" || runtime.startCalls[0].ReasoningEffort != "deep" {
		t.Fatalf("runtime start settings = %#v, want exact runtime permission and requested reasoning", runtime.startCalls[0])
	}
	if len(runtime.closeCalls) != 1 || len(runtime.sessions) != 0 {
		t.Fatalf("runtime cleanup = calls %#v sessions %#v, want hidden discovery closed immediately", runtime.closeCalls, runtime.sessions)
	}
}

func TestServiceGetComposerOptionsDoesNotCarryConversationDetailMode(t *testing.T) {
	runtime := newFakeRuntime()
	service := newIsolatedAgentService(runtime)

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "codex",
		Settings: ComposerSettings{
			ConversationDetailMode: "general",
		},
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if got := options.EffectiveSettings.ConversationDetailMode; got != "" {
		t.Fatalf("effectiveSettings.conversationDetailMode = %q, want empty", got)
	}
	payload := ComposerSettingsToMap(options.EffectiveSettings)
	if _, ok := payload["conversationDetailMode"]; ok {
		t.Fatalf("effectiveSettings payload includes conversationDetailMode: %#v", payload)
	}
}

type recordingComposerCapabilityLister struct {
	callCount int
}

func (l *recordingComposerCapabilityLister) ListComposerCapabilityOptions(
	_ context.Context,
	_ string,
	_ string,
	_ []ComposerSkillOption,
) ([]ComposerCapabilityOption, []string) {
	l.callCount++
	return []ComposerCapabilityOption{{
		ID:         "connector:github",
		Kind:       "connector",
		Name:       "github",
		Label:      "GitHub",
		Status:     "available",
		Invocation: "promptItem",
	}}, nil
}

func TestServiceGetComposerOptionsSkipsCapabilityCatalogWhenDisabled(t *testing.T) {
	runtime := newFakeRuntime()
	lister := &recordingComposerCapabilityLister{}
	service := newIsolatedAgentService(runtime)
	service.CapabilityLister = lister
	includeCapabilityCatalog := false

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider:                 "codex",
		IncludeCapabilityCatalog: &includeCapabilityCatalog,
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if lister.callCount != 0 {
		t.Fatalf("capability lister calls = %d, want 0", lister.callCount)
	}
	if len(options.CapabilityCatalog) != 0 {
		t.Fatalf("capability catalog = %#v, want empty when disabled", options.CapabilityCatalog)
	}
}

func TestServiceGetComposerOptionsIncludesConnectorCatalogWhenLabFlagEnabled(t *testing.T) {
	runtime := newFakeRuntime()
	lister := &recordingComposerCapabilityLister{}
	service := newIsolatedAgentService(runtime)
	service.CapabilityLister = lister
	service.DesktopPreferencesReader = connectorCatalogPreferencesReader(true)

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "codex",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if lister.callCount != 1 {
		t.Fatalf("capability lister calls = %d, want 1", lister.callCount)
	}
	if len(options.CapabilityCatalog) != 1 || options.CapabilityCatalog[0].ID != "connector:github" {
		t.Fatalf("capability catalog = %#v", options.CapabilityCatalog)
	}
}

func TestServiceGetComposerOptionsHidesConnectorCatalogByDefault(t *testing.T) {
	runtime := newFakeRuntime()
	lister := &recordingComposerCapabilityLister{}
	service := newIsolatedAgentService(runtime)
	service.CapabilityLister = lister

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "codex",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if lister.callCount != 1 {
		t.Fatalf("capability lister calls = %d, want 1", lister.callCount)
	}
	if len(options.CapabilityCatalog) != 0 {
		t.Fatalf("capability catalog = %#v, want connectors hidden", options.CapabilityCatalog)
	}
}

func TestServiceGetComposerOptionsHidesConnectorCatalogWhenPreferencesAreUnavailable(t *testing.T) {
	runtime := newFakeRuntime()
	lister := &recordingComposerCapabilityLister{}
	service := newIsolatedAgentService(runtime)
	service.CapabilityLister = lister
	service.DesktopPreferencesReader = fakeDesktopPreferencesReader{
		err: errors.New("preferences unavailable"),
	}

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "codex",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if len(options.CapabilityCatalog) != 0 {
		t.Fatalf("capability catalog = %#v, want connectors hidden", options.CapabilityCatalog)
	}
	errors, ok := options.RuntimeContext["capabilityCatalogErrors"].([]string)
	if !ok || len(errors) != 1 || errors[0] != "load connector visibility: preferences unavailable" {
		t.Fatalf("capability catalog errors = %#v", options.RuntimeContext["capabilityCatalogErrors"])
	}
}

func TestServiceGetComposerOptionsUsesLocalInstalledConnectorCatalog(t *testing.T) {
	runtime := newFakeRuntime()
	lister := &recordingComposerCapabilityLister{}
	service := newIsolatedAgentService(runtime)
	service.CapabilityLister = lister
	service.DesktopPreferencesReader = connectorCatalogPreferencesReader(true)
	service.ConnectorMarketSnapshots = connectorMarketSnapshotStub{
		snapshot: market.Snapshot{Connectors: []market.Connector{
			localConnectorFixture(
				"notion",
				market.InstallationStateInstalled,
				market.AuthorizationStateDisconnected,
				market.CompatibilityStateSupported,
			),
		}},
	}

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "codex",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if len(options.CapabilityCatalog) != 1 {
		t.Fatalf("capability catalog = %#v, want only local DB connector", options.CapabilityCatalog)
	}
	connector := options.CapabilityCatalog[0]
	if connector.ID != "connector:notion" || connector.Source != "local-db" || connector.Status != "authRequired" {
		t.Fatalf("connector = %#v", connector)
	}
}

func TestServiceGetComposerOptionsCachesCapabilityCatalog(t *testing.T) {
	runtime := newFakeRuntime()
	lister := &recordingComposerCapabilityLister{}
	service := newIsolatedAgentService(runtime)
	service.CapabilityLister = lister
	service.DesktopPreferencesReader = connectorCatalogPreferencesReader(true)

	first, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "codex",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions first returned error: %v", err)
	}
	first.CapabilityCatalog[0].ID = "mutated"
	second, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "codex",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions second returned error: %v", err)
	}
	if lister.callCount != 1 {
		t.Fatalf("capability lister calls = %d, want 1", lister.callCount)
	}
	if len(second.CapabilityCatalog) != 1 || second.CapabilityCatalog[0].ID != "connector:github" {
		t.Fatalf("cached capability catalog = %#v, want unmutated github connector", second.CapabilityCatalog)
	}
}

func connectorCatalogPreferencesReader(enabled bool) fakeDesktopPreferencesReader {
	return fakeDesktopPreferencesReader{
		preferences: preferencesbiz.DesktopPreferences{
			FeatureFlags: map[string]bool{
				preferencesbiz.LabFlagConnectors: enabled,
			},
		},
	}
}

func TestServiceGetsComposerOptionsLocalizesDisplayLabels(t *testing.T) {
	runtime := newFakeRuntime()
	service := newIsolatedAgentService(runtime)

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Locale:   "zh-CN",
		Provider: "claude-code",
		Settings: ComposerSettings{
			PermissionModeID: "acceptEdits",
			ReasoningEffort:  "xhigh",
		},
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	var xhigh ComposerConfigOptionValue
	for _, option := range options.ReasoningConfig.Options {
		if option.ID == "xhigh" || option.Value == "xhigh" {
			xhigh = option
		}
	}
	if xhigh.Label != "超高" {
		t.Fatalf("reasoningConfig = %#v, want zh-CN xhigh label", options.ReasoningConfig)
	}
	if options.EffectiveSettings.PermissionModeID != "acceptEdits" {
		t.Fatalf("permission current = %q, want acceptEdits", options.EffectiveSettings.PermissionModeID)
	}
	var acceptEdits PermissionModeOption
	for _, mode := range options.PermissionConfig.Modes {
		if mode.ID == "dontAsk" {
			t.Fatalf("claude-code still offers retired dontAsk: %#v", options.PermissionConfig.Modes)
		}
		if mode.ID == "acceptEdits" {
			acceptEdits = mode
		}
	}
	if acceptEdits.Label != "接受编辑" || acceptEdits.Description == "" {
		t.Fatalf("acceptEdits = %#v, want localized label and description", acceptEdits)
	}
	capabilities, ok := options.RuntimeContext["capabilities"].([]string)
	if !ok || !slices.Contains(capabilities, "imageInput") {
		t.Fatalf("capabilities = %#v, want imageInput", options.RuntimeContext["capabilities"])
	}
}

func TestServiceGetsComposerOptionsRemapsRetiredClaudeDontAsk(t *testing.T) {
	runtime := newFakeRuntime()
	service := newIsolatedAgentService(runtime)

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "claude-code",
		Settings: ComposerSettings{PermissionModeID: "dontAsk"},
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if options.EffectiveSettings.PermissionModeID != "default" {
		t.Fatalf("retired dontAsk = %q, want default", options.EffectiveSettings.PermissionModeID)
	}
	for _, mode := range options.PermissionConfig.Modes {
		if mode.ID == "dontAsk" {
			t.Fatalf("claude-code still offers retired dontAsk: %#v", options.PermissionConfig.Modes)
		}
	}
}

func TestServiceGetsComposerOptionsFromTuttiAgentModelCatalog(t *testing.T) {
	runtime := newFakeRuntime()
	service := newIsolatedAgentService(runtime)
	service.ModelCatalog = fakeModelCatalog{
		result: AgentModelCatalogResult{
			Provider: "tutti-agent",
			Source:   "tutti-agent-cli",
			Models: []AgentModelOption{
				{ID: "gpt-5.4", DisplayName: "GPT-5.4", IsDefault: true},
				{ID: "nova-micro", DisplayName: "Nova Micro"},
			},
		},
	}

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "tutti-agent",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if options.EffectiveSettings.Model != "gpt-5.4" {
		t.Fatalf("effectiveSettings.model = %q, want gpt-5.4", options.EffectiveSettings.Model)
	}
	if options.EffectiveSettings.ReasoningEffort != "" || options.EffectiveSettings.Speed != "" {
		t.Fatalf("provider-wide hidden controls leaked into effectiveSettings: %#v", options.EffectiveSettings)
	}
	if options.ModelConfig.CurrentValue != "gpt-5.4" || len(baseModelOptions(options.ModelConfig.Options)) != 2 {
		t.Fatalf("modelConfig = %#v, want catalog-backed tutti-agent models", options.ModelConfig)
	}
	configOptions, ok := options.RuntimeContext["configOptions"].([]map[string]any)
	if !ok || len(configOptions) != 1 {
		t.Fatalf("configOptions = %#v", options.RuntimeContext["configOptions"])
	}
	if configOptions[0]["id"] != "model" || configOptions[0]["currentValue"] != "gpt-5.4" {
		t.Fatalf("model option = %#v", configOptions[0])
	}
	if len(options.ReasoningOptionsByModel) != 0 ||
		options.ReasoningConfig.Configurable ||
		options.SpeedConfig.Configurable {
		t.Fatalf("provider-wide hidden controls leaked into composer options: %#v", options)
	}
	if options.RuntimeContext["modelCatalogSource"] != "tutti-agent-cli" {
		t.Fatalf("modelCatalogSource = %#v, want tutti-agent-cli", options.RuntimeContext["modelCatalogSource"])
	}
	if len(runtime.sessions) != 0 {
		t.Fatalf("runtime sessions = %d, want no started sessions", len(runtime.sessions))
	}
}

func TestServiceGetsComposerOptionsNormalizesClaudeMinimalReasoningEffort(t *testing.T) {
	runtime := newFakeRuntime()
	service := newIsolatedAgentService(runtime)

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "claude-code",
		Settings: ComposerSettings{
			ReasoningEffort: "minimal",
		},
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if options.EffectiveSettings.ReasoningEffort != "high" {
		t.Fatalf("effectiveSettings.reasoningEffort = %q, want high", options.EffectiveSettings.ReasoningEffort)
	}
	configOptions, ok := options.RuntimeContext["configOptions"].([]map[string]any)
	if !ok || len(configOptions) < 1 {
		t.Fatalf("configOptions = %#v", options.RuntimeContext["configOptions"])
	}
	var reasoningOption map[string]any
	for _, option := range configOptions {
		if option["id"] == "effort" {
			reasoningOption = option
			break
		}
	}
	if reasoningOption == nil {
		t.Fatalf("configOptions = %#v, want effort option", configOptions)
	}
	reasoningOptions, ok := reasoningOption["options"].([]map[string]string)
	if !ok {
		t.Fatalf("reasoning options = %#v", reasoningOption["options"])
	}
	for _, option := range reasoningOptions {
		if option["value"] == "minimal" {
			t.Fatalf("reasoning options = %#v, want claude minimal filtered out", reasoningOptions)
		}
	}
}
