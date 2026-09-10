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

func claudeCodeACPRuntimeEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(claudeCodeRuntimeEnv)), claudeCodeRuntimeACP)
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
				mode = strings.TrimSpace(mode)
				if permissionModeIDAllowedForProvider(descriptor.Identity.ID, mode) {
					return mode
				}
				return ""
			},
			initializeParams:   func() map[string]any { return claudeACPInitializeParams(host) },
			failOnSetModeError: true,
			env: func(session Session) []string {
				return append(standardACPEnv(session, host), "IS_SANDBOX=1")
			},
			commandResolver: commandResolver,
		},
		transport:  transport,
		host:       host,
		sessions:   make(map[string]*standardACPSession),
		inputUnits: providerInputUnitTrackerForTransport(transport),
	}
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
