package agentruntime

import (
	"os"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

const (
	claudeCodeRuntimeEnv = "TUTTI_CLAUDE_CODE_RUNTIME"
	claudeCodeRuntimeACP = "acp"
)

// Mirrors runtimeprep's claude env names. The daemon reads them back off the
// prepared session env, the same way the cursor adapter reads its own plugin
// dir (see acp_provider_cursor.go).
const (
	claudePluginDirEnv        = "TUTTI_CLAUDE_PLUGIN_DIR"
	claudeSystemPromptFileEnv = "TUTTI_CLAUDE_SYSTEM_PROMPT_FILE"
)

func claudeCodeACPRuntimeEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(claudeCodeRuntimeEnv)), claudeCodeRuntimeACP)
}

// claudeACPSessionMeta carries the runtimeprep-prepared DinTalDock surface across
// the ACP hop.
//
// claude-agent-acp builds its query options from the session/new `_meta` it
// receives: `_meta.claudeCode.options` is spread straight into the SDK options
// and `_meta.systemPrompt` replaces the default `{type:"preset",
// preset:"claude_code"}` while preserving every other preset key. The SDK
// sidecar already consumes both env vars (claude-sdk-sidecar/src/options.ts), so
// without this the ACP runtime silently drops the injected skill plugin and the
// routing system prompt that every other runtime path delivers.
//
// Missing files degrade to "not injected" rather than failing session/new: a
// session without the DinTalDock plugin is still usable, and the injected
// system prompt already tells the model to fall back to the materialized
// SKILL.md when the Skill tool is unavailable.
func claudeACPSessionMeta(session Session) map[string]any {
	meta := map[string]any{}

	if pluginDir := strings.TrimSpace(sessionEnvValue(session.Env, claudePluginDirEnv)); pluginDir != "" {
		meta["claudeCode"] = map[string]any{
			"options": map[string]any{
				"plugins": []map[string]any{{"type": "local", "path": pluginDir}},
			},
		}
	}

	path := strings.TrimSpace(sessionEnvValue(session.Env, claudeSystemPromptFileEnv))
	if path == "" {
		return meta
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return meta
	}
	if prompt := strings.TrimSpace(string(content)); prompt != "" {
		meta["systemPrompt"] = map[string]any{
			"type":   "preset",
			"preset": "claude_code",
			"append": prompt,
		}
	}
	return meta
}

func newClaudeCodeACPAdapterFromProviderDescriptor(
	descriptor providerregistry.ProviderDescriptor,
	transport ProcessTransport,
	host HostMetadata,
	commandResolver ProviderCommandResolver,
) *standardACPAdapter {
	host = normalizeHostMetadata(host)
	return &standardACPAdapter{
		config: standardACPConfig{
			provider:            descriptor.Identity.ID,
			adapterName:         "claude-agent-acp",
			command:             []string{"claude-agent-acp"},
			defaultTitle:        descriptor.Identity.DisplayName,
			defaultTitleAliases: append([]string{descriptor.Identity.DisplayName, descriptor.Identity.ID}, descriptor.Identity.Aliases...),
			authRequiredMessage: "Claude Agent ACP requires authentication in the runtime VM; sign in to the local Claude Code agent so its credentials can be synced, then retry this session.",
			permissionModeID: func(mode string) string {
				mode = retiredClaudeCodePermissionModeID(mode)
				if permissionModeIDAllowedForProvider(descriptor.Identity.ID, mode) {
					return mode
				}
				return ""
			},
			// Tiers the composer profile marks as "do not ask" are answered by the
			// client instead of becoming a user approval. Without this the Claude
			// Code ACP target has no permission tier at all, so every
			// session/request_permission — including the ones Claude Code raises
			// after the model puts itself into plan mode — waits on a human even
			// though the user picked 完全放行.
			automaticPermissionDecision: composerAutomaticPermissionDecision(descriptor),
			initializeParams:            func() map[string]any { return claudeACPInitializeParams(host) },
			failOnSetModeError:          true,
			env: func(session Session) []string {
				return append(standardACPEnv(session, host), "IS_SANDBOX=1")
			},
			applySessionMeta: func(params map[string]any, session Session, _ HostMetadata) {
				mergeACPParamsMeta(params, claudeACPSessionMeta(session))
			},
			commandResolver: commandResolver,
			// Claude Code is the one standard ACP runtime that reads the 1M
			// context-window request off the model value: claude-agent-acp strips
			// the `[1m]` marker before the request leaves for the API and adds the
			// context-1m beta instead. The marker therefore has to survive every
			// outbound model value — see modelValueForRuntime. Every other runtime
			// (dsh takes the window via ModelEndpointConfig.ContextWindow) keeps
			// the bare spelling.
			modelValueCarriesContextWindow: true,
		},
		transport:  transport,
		host:       host,
		sessions:   make(map[string]*standardACPSession),
		inputUnits: providerInputUnitTrackerForTransport(transport),
	}
}

// composerAutomaticPermissionDecision turns the composer profile's per-mode
// automatic decisions into the callback the ACP adapter consults for an
// incoming session/request_permission. The callback is handed the live
// permission mode id — for these targets the profile id the composer stores —
// so both the id and its semantic are registered. nil (nothing declared) keeps
// the previous behaviour: prompt the user, or fall back to the RnDMaster
// runtime contract when the target has no tier at all.
func composerAutomaticPermissionDecision(descriptor providerregistry.ProviderDescriptor) func(string) string {
	decisions := map[string]string{}
	for _, mode := range descriptor.ComposerProfile.PermissionModes {
		decision := strings.TrimSpace(mode.AutomaticDecision)
		if decision == "" {
			continue
		}
		for _, id := range []string{mode.ID, mode.Semantic} {
			if normalized := strings.ToLower(strings.TrimSpace(id)); normalized != "" {
				decisions[normalized] = decision
			}
		}
	}
	return automaticPermissionDecisionFromMap(decisions)
}

func claudeACPInitializeParams(host HostMetadata) map[string]any {
	return map[string]any{
		"protocolVersion": acpProtocolVersion,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{
				"readTextFile":  true,
				"writeTextFile": true,
			},
			"terminal": true,
			"auth": map[string]any{
				"terminal": true,
			},
			"_meta": map[string]any{
				"terminal_output": true,
				"terminal-auth":   true,
			},
		},
		"clientInfo": host.clientInfoParams(),
	}
}
