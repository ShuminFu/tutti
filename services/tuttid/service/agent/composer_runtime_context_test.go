package agent

import (
	"slices"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

func TestComposerRuntimeContextProjectsExactPersistedCapabilities(t *testing.T) {
	project := t.TempDir()
	settings := ComposerSettings{ReasoningEffort: "high"}
	ref := map[string]any{"kind": "agent_extension", "extensionInstallationId": "example@1.0.0"}
	scope := newComposerLiveModelScopeForInput(ComposerOptionsInput{
		Provider:          "acp:example",
		WorkspaceID:       "workspace-1",
		Cwd:               project,
		AgentTargetID:     "extension:example",
		providerTargetRef: ref,
	}, settings)
	exact := stampAgentExtensionComposerScope(map[string]any{}, ref, project, settings)
	wrongInstallation := stampAgentExtensionComposerScope(
		map[string]any{},
		map[string]any{"kind": "agent_extension", "extensionInstallationId": "example@2.0.0"},
		project,
		settings,
	)
	wrongProject := stampAgentExtensionComposerScope(map[string]any{}, ref, t.TempDir(), settings)
	wrongSettings := stampAgentExtensionComposerScope(
		map[string]any{},
		ref,
		project,
		ComposerSettings{ReasoningEffort: "low"},
	)
	service := newIsolatedAgentService(newFakeRuntime())
	service.SessionReader = fakeSessionReader{sessions: map[string]PersistedSession{
		"workspace-1:exact": {
			ID: "exact", WorkspaceID: "workspace-1", Provider: "acp:example", AgentTargetID: "extension:example",
			Capabilities:           canonical.NewCapabilitySnapshot([]string{"imageInput", "interrupt"}),
			InternalRuntimeContext: exact, UpdatedAtUnixMS: 100,
		},
		"workspace-1:wrong-installation": {
			ID: "wrong-installation", WorkspaceID: "workspace-1", Provider: "acp:example", AgentTargetID: "extension:example",
			Capabilities:           canonical.NewCapabilitySnapshot([]string{"planMode"}),
			InternalRuntimeContext: wrongInstallation, UpdatedAtUnixMS: 500,
		},
		"workspace-1:wrong-project": {
			ID: "wrong-project", WorkspaceID: "workspace-1", Provider: "acp:example", AgentTargetID: "extension:example",
			Capabilities:           canonical.NewCapabilitySnapshot([]string{"planMode"}),
			InternalRuntimeContext: wrongProject, UpdatedAtUnixMS: 600,
		},
		"workspace-1:wrong-settings": {
			ID: "wrong-settings", WorkspaceID: "workspace-1", Provider: "acp:example", AgentTargetID: "extension:example",
			Capabilities:           canonical.NewCapabilitySnapshot([]string{"planMode"}),
			InternalRuntimeContext: wrongSettings, UpdatedAtUnixMS: 700,
		},
		"workspace-1:wrong-target": {
			ID: "wrong-target", WorkspaceID: "workspace-1", Provider: "acp:example", AgentTargetID: "extension:other",
			Capabilities:           canonical.NewCapabilitySnapshot([]string{"planMode"}),
			InternalRuntimeContext: exact, UpdatedAtUnixMS: 800,
		},
	}}

	context := service.composerRuntimeContextFromSession(scope)
	if got := stringSliceFromAny(context["capabilities"]); !slices.Equal(got, []string{"imageInput", "interrupt"}) {
		t.Fatalf("persisted capabilities = %#v, want typed capabilities projected", got)
	}
}

func TestMergeRuntimeComposerContextFailsClosedWithoutExactCapabilityEvidence(t *testing.T) {
	sessionProject := t.TempDir()
	sessionSettings := ComposerSettings{PermissionModeID: "ask-before-write"}
	sessionRef := map[string]any{"kind": "agent_extension", "extensionInstallationId": "example@1.0.0"}
	tests := []struct {
		name     string
		project  string
		target   string
		ref      map[string]any
		settings ComposerSettings
	}{
		{
			name:     "installation",
			project:  sessionProject,
			target:   "extension:example",
			ref:      map[string]any{"kind": "agent_extension", "extensionInstallationId": "example@2.0.0"},
			settings: sessionSettings,
		},
		{
			name:     "project",
			project:  t.TempDir(),
			target:   "extension:example",
			ref:      sessionRef,
			settings: sessionSettings,
		},
		{
			name:     "settings",
			project:  sessionProject,
			target:   "extension:example",
			ref:      sessionRef,
			settings: ComposerSettings{PermissionModeID: "full-access"},
		},
		{
			name:     "target",
			project:  sessionProject,
			target:   "extension:other",
			ref:      sessionRef,
			settings: sessionSettings,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := newFakeRuntime()
			runtime.sessions["workspace-1:live"] = ProviderRuntimeSession{
				ID: "live", WorkspaceID: "workspace-1", Provider: "acp:example", AgentTargetID: "extension:example",
				RuntimeContext: stampAgentExtensionComposerScope(map[string]any{
					"capabilities": []any{"imageInput", "interrupt"},
				}, sessionRef, sessionProject, sessionSettings),
				CreatedAtUnixMS: 100,
			}
			service := newIsolatedAgentService(runtime)
			profile := ExtensionComposerProfile{Capabilities: []string{"imageInput", "interrupt"}}
			options, err := service.mergeRuntimeComposerContextForComposerOptions(
				ComposerOptionsInput{
					Provider:          "acp:example",
					WorkspaceID:       "workspace-1",
					Cwd:               tt.project,
					AgentTargetID:     tt.target,
					providerTargetRef: tt.ref,
				},
				tt.settings,
				"en",
				profile,
				"",
				ComposerOptions{RuntimeContext: map[string]any{}},
			)
			if err != nil {
				t.Fatalf("mergeRuntimeComposerContextForComposerOptions error = %v", err)
			}
			options = applyExtensionComposerCapabilities(options, profile, false, false)
			if got := options.Capabilities; len(got) != 0 {
				t.Fatalf("capabilities = %#v, want fail-closed result for mismatched %s identity", got, tt.name)
			}
		})
	}
}

func TestMergeRuntimeComposerContextRoutesUnlistedCommandsToSkills(t *testing.T) {
	project := t.TempDir()
	ref := map[string]any{
		"kind":                    "agent_extension",
		"extensionInstallationId": "example@1.0.0",
	}
	runtime := newFakeRuntime()
	runtime.sessions["workspace-1:live"] = ProviderRuntimeSession{
		ID:            "live",
		WorkspaceID:   "workspace-1",
		Provider:      "acp:example",
		AgentTargetID: "extension:example",
		RuntimeContext: stampAgentExtensionComposerScope(map[string]any{
			"availableCommands": []any{
				map[string]any{"name": "status", "description": "Show status"},
				map[string]any{"name": "custom-theme", "description": "Edit a theme"},
				map[string]any{"name": "skill:browser-use", "description": "Use browser"},
			},
		}, ref, project, ComposerSettings{}),
		CreatedAtUnixMS: 100,
	}
	service := newIsolatedAgentService(runtime)
	profile := ExtensionComposerProfile{
		Skills: &ExtensionComposerSkillProfile{
			Invocation:               "textTrigger",
			RuntimeCommandProjection: "unlisted-as-skills",
		},
		SlashCommands: []ExtensionComposerSlashCommand{{
			Name:   "status",
			Effect: string(providerregistry.SlashCommandEffectShowStatus),
		}},
		SlashCommandCatalogAuthoritative: true,
	}
	policy := composerSlashCommandPolicyFromExtensionProfile(profile)
	options, err := service.mergeRuntimeComposerContextForComposerOptions(
		ComposerOptionsInput{
			Provider:          "acp:example",
			WorkspaceID:       "workspace-1",
			Cwd:               project,
			AgentTargetID:     "extension:example",
			providerTargetRef: ref,
		},
		ComposerSettings{},
		"en",
		profile,
		"",
		ComposerOptions{
			RuntimeContext:     map[string]any{},
			SlashCommandPolicy: policy,
		},
	)
	if err != nil {
		t.Fatalf("mergeRuntimeComposerContextForComposerOptions error = %v", err)
	}
	if len(options.Commands) != 1 || options.Commands[0].Name != "status" {
		t.Fatalf("commands = %#v, want only signed core command", options.Commands)
	}
	if len(options.Skills) != 1 ||
		options.Skills[0].Name != "custom-theme" ||
		options.Skills[0].Trigger != "/custom-theme" ||
		options.Skills[0].SourceKind != composerSkillSourceBundled ||
		options.Skills[0].Description != "Edit a theme" {
		t.Fatalf("skills = %#v, want unlisted runtime command projected as a Skill", options.Skills)
	}
	if got := runtimeConfigOptionsAsMapSlice(options.RuntimeContext["skills"]); len(got) != 1 {
		t.Fatalf("runtime skills = %#v, want typed projection mirrored for diagnostics", options.RuntimeContext["skills"])
	}
}

// 运行时给的档位显示名只有在它**确实是文案**时才优先。DeepSeek Harness 的 ACP 把
// name 直接填成取值本身（扩展的 acpServer.ts 写的是 `name: value`），于是 off/high
// 这种裸 token 会一路压掉本地化标签，中文界面里显示成小写英文。
func TestComposerReasoningConfigLocalizesRuntimeTokenEcho(t *testing.T) {
	option := map[string]any{
		"id":           "reasoning_effort",
		"category":     "thought_level",
		"currentValue": "high",
		"options": []any{
			// DH 的回显：把取值本身当显示名。
			map[string]any{"value": "off", "name": "off"},
			map[string]any{"value": "high", "name": "High"},
			// 运行时给了**不同**的文字：那是显示名，必须保留（它也允许别的 provider
			// 用自己的措辞覆盖 locale）。注意 X-High 不是 xhigh 的忽略大小写回显，
			// 所以按「有文案」处理。
			map[string]any{"value": "xhigh", "name": "X-High"},
			map[string]any{"value": "max", "name": "Maximum depth"},
		},
	}
	config, current := composerReasoningConfigFromRuntimeOption(option, map[string]any{}, "zh-CN")
	if !config.Configurable {
		t.Fatal("运行时报了 4 个档位，选择器必须是可配的")
	}
	if current != "high" {
		t.Fatalf("current = %q, want the runtime's currentValue", current)
	}
	labels := map[string]string{}
	for _, value := range config.Options {
		labels[value.Value] = value.Label
	}
	// 这两个取值在 zh-CN 目录里本来没有条目，会显示成裸英文；补上后应显示中文。
	if labels["off"] != "关闭" {
		t.Fatalf("off 的标签 = %q, want 关闭（token 回显不该压掉本地化）", labels["off"])
	}
	if labels["high"] != "高" {
		t.Fatalf("high 的标签 = %q, want 高（忽略大小写同样算回显）", labels["high"])
	}
	// 不是回显的两条保持运行时给的名字。
	if labels["xhigh"] != "X-High" {
		t.Fatalf("xhigh 的标签 = %q, want 运行时给的 X-High", labels["xhigh"])
	}
	if labels["max"] != "Maximum depth" {
		t.Fatalf("max 的标签 = %q, want 运行时给的 Maximum depth（运行时的文案优先）", labels["max"])
	}
}
