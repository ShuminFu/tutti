package providerregistry

import canonical "github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"

const (
	ClaudeCodeProviderID = canonical.ClaudeCodeProviderID
	ClaudeCodeTargetID   = "local:claude-code"
)

func claudeCodeDescriptor() ProviderDescriptor {
	return ProviderDescriptor{
		Identity: canonicalProviderIdentity(ClaudeCodeProviderID),
		Runtime: RuntimeDescriptor{
			Kind:              RuntimeKindClaudeSDK,
			Name:              "claude-agent-sdk",
			NativeSessionFork: true,
			Endpoint: RuntimeEndpointDescriptor{
				BaseURLEnvVars: []string{
					"ANTHROPIC_BASE_URL",
					"ANTHROPIC_API_BASE_URL",
				},
				ConfigKind:         EndpointConfigKindClaudeSettings,
				ModelPlanProtocol:  ModelPlanProtocolAnthropic,
				NativeSubscription: true,
			},
		},
		Status: StatusDescriptor{
			Kind:                            StatusKindClaudeCLI,
			AuthOutputParserKind:            AuthOutputParserKindClaude,
			AuthMarkerParserKind:            AuthMarkerParserKindClaude,
			AuthCommandRunnerKind:           AuthCommandRunnerKindClaudeGate,
			StaticSpecResolverKind:          StaticSpecResolverKindGeneric,
			BinaryNames:                     []string{"claude"},
			AuthStatusCommand:               []string{"auth", "status"},
			AuthStatusCommandTimeoutSeconds: 600,
			AuthMarkerPaths:                 []string{"~/.claude.json", "~/.claude/auth.json"},
			APIEndpoints:                    []string{"https://api.anthropic.com/v1/messages"},
			CustomConfigEnvVars: []string{
				"ANTHROPIC_API_KEY",
				"ANTHROPIC_AUTH_TOKEN",
				"ANTHROPIC_BASE_URL",
				"ANTHROPIC_API_BASE_URL",
			},
			CredentialEnvVars: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
			Install: InstallerDescriptor{
				Kind:                     InstallerKindOfficialScript,
				DisplayCommand:           "curl -fsSL https://claude.ai/install.sh | bash",
				ScriptURL:                "https://claude.ai/install.sh",
				ScriptShell:              "bash",
				WindowsFallback:          InstallerWindowsFallbackPowerShell,
				WindowsPowerShellCommand: `try { $installer = irm https://claude.ai/install.ps1 -TimeoutSec 30 -ErrorAction Stop } catch { [Console]::Error.WriteLine('TUTTI_INSTALL_SOURCE_UNREACHABLE: ' + $_.Exception.Message); exit 1 }; & ([scriptblock]::Create($installer)) stable`,
				PackageName:              "@anthropic-ai/claude-code",
				BinaryName:               "claude",
				IncludeOptional:          true,
				HomebrewFormula:          "claude-code",
				FailureReasonMarkers: map[string][]string{
					"install_source_unreachable":    {"tutti_install_source_unreachable"},
					"install_unavailable_in_region": {"app-unavailable-in-region", "app unavailable in region", "claude isn't available here", "claude isn&#x27;t available here", "claude isn&apos;t available here"},
				},
			},
			Update: UpdateDescriptor{
				Capability:        UpdateCapabilityUnsupported,
				UnsupportedReason: UpdateUnsupportedReasonOfficialScript,
			},
			LoginArgs: []string{"auth", "login"},
			AuthWatch: AuthWatchDescriptor{
				Sources: []AuthWatchSourceDescriptor{
					{
						RootCandidates: []AuthWatchRootCandidateDescriptor{{EnvVar: "CLAUDE_CONFIG_DIR"}},
						DefaultRoot:    "~/.claude",
						Paths:          []string{"settings.json", "auth.json", ".credentials.json"},
					},
					{DefaultRoot: "~", Paths: []string{".claude.json"}},
				},
				ContentFingerprint: AuthWatchContentFingerprintClaudeState,
			},
		},
		ComposerProfile: ComposerProfileDescriptor{
			ModelSelection: true,
			LiveModelDiscovery: LiveModelDiscoveryDescriptor{
				Kind:          LiveModelDiscoveryKindClaudeSDK,
				HiddenProbe:   true,
				AccountScoped: true,
			},
			ReasoningEffort:        true,
			ReasoningEffortValues:  []string{"low", "medium", "high", "xhigh"},
			ReasoningEffortOptions: ReasoningEffortOptionsStatic,
			DefaultReasoningEffort: "high",
			Speed:                  true,
			SpeedValues:            []string{"standard", "fast"},
			DefaultSpeed:           "standard",
			Capabilities: []string{
				"imageInput",
				"skills",
				"compact",
				"tokenUsage",
				"rateLimits",
				"planMode",
				CapabilityInterrupt,
				CapabilityActiveTurnGuidance,
				CapabilityModelSwitch,
				CapabilityModelPlanBinding,
				"permissionModeChangeDuringTurn",
			},
			PermissionConfigurable:  true,
			DefaultPermissionModeID: "default",
			PermissionModes: []PermissionModeDescriptor{
				{ID: "default", Semantic: "ask-before-write"},
				{ID: "acceptEdits", Semantic: "accept-edits"},
				{ID: "dontAsk", Semantic: "locked-down"},
				// 完全放行 = 不再逐条问用户。Claude Code 的 ACP 目标只能从
				// 这里拿到"自动裁决"：它的适配器不注册自动档位，于是每一条
				// session/request_permission 都会落成用户审批——包括模型自己
				// 进入 plan mode 之后要求放行的那些（plan 模式优先于 bypass，
				// 所以「完全放行」的会话此时仍会问人）。声明成 approved 让
				// 客户端直接放行，与扩展 provider 的 permissionModes
				// automaticDecision 同一套语义。
				// 唯一的例外是「退出 plan mode」那一问：它同时是方案评审和
				// CLI 侧的模式切换，见 standard_acp_stream.go 里的 plan-exit 规则。
				{ID: "bypassPermissions", Semantic: "full-access", AutomaticDecision: "approved"},
			},
			ConfigOptionIDs: ComposerConfigOptionIDs{
				Model:      "model",
				Reasoning:  "effort",
				Speed:      "fast",
				Permission: "permission_mode",
			},
			Skills: SkillDescriptor{
				Kind:       SkillKindClaudeCode,
				Invocation: SkillInvocationTextTrigger,
			},
			SlashCommandPolicy: SlashCommandPolicyDescriptor{
				FallbackCommands:            []string{"compact", "status", "fast", "goal", "review"},
				CommandCatalogAuthoritative: true,
				CommandEffects: []SlashCommandEffectDescriptor{
					{Command: "compact", Effect: SlashCommandEffectSubmitImmediate},
					{Command: "context", Effect: SlashCommandEffectSubmitImmediate},
					{Command: "usage", Effect: SlashCommandEffectSubmitImmediate},
					{Command: "review", Effect: SlashCommandEffectShowReviewPicker},
					{Command: "goal", Effect: SlashCommandEffectActivateGoalMode},
					{Command: "plan", Effect: SlashCommandEffectTogglePlanMode},
					{Command: "status", Effect: SlashCommandEffectShowStatus},
					{Command: "fast", Effect: SlashCommandEffectToggleSpeed},
				},
			},
			Behavior: ComposerBehaviorDescriptor{
				ModelOptionsAuthoritative:           true,
				RefreshModelOptionsAfterSettings:    true,
				PrewarmDraftSession:                 true,
				PlanModeExclusiveWithPermissionMode: true,
			},
		},
		Target: TargetDescriptor{
			ID:            ClaudeCodeTargetID,
			LaunchRefType: TargetLaunchRefTypeLocalCLI,
			Enabled:       true,
			SortOrder:     20,
		},
		Events: EventsDescriptor{
			Enabled:                 true,
			Aliases:                 []string{"claude", "claude_code"},
			TurnLifecycleProjection: TurnLifecycleProjectionExplicit,
		},
		Sidecar: SidecarDescriptor{MentionRouting: SidecarMentionRoutingClaudeNamespaced, ExecutionEnvironment: SidecarExecutionEnvironmentClaudeIPC},
		Desktop: DesktopIntegrationDescriptor{Managed: true, ManagedOrder: 1, StatusProbePriority: 2, UsageProbeKind: DesktopUsageProbeClaudeCode, DeveloperLogs: true, DefaultProviderEligible: true, DefaultProviderPriority: 3},
		ExternalImport: ExternalImportDescriptor{
			Enabled:               true,
			RootEnvVar:            "CLAUDE_CONFIG_DIR",
			DefaultRoot:           "~/.claude",
			ExtraRootsEnvVar:      "TUTTI_CLAUDE_EXTRA_IMPORT_ROOTS",
			ScanDirectories:       []string{"projects"},
			SkipDirectoryPrefixes: []string{"agent-"},
			ParserKind:            ExternalImportParserKindClaudeJSONL,
			UserTextCleanerKind:   ExternalImportUserTextCleanerKindClaude,
		},
	}
}
