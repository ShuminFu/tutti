package agentextension

import (
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestRuntimeAdapterConfigCopiesRuntimePrepIsolationEnvNames(t *testing.T) {
	t.Parallel()

	binding := RuntimeBinding{
		Installation: Installation{
			ID: "inst-1", Provider: "acp:test-agent", AgentKey: "test-agent", DisplayName: "Test Agent",
		},
		Command: []string{"test-agent", "acp"},
		RuntimePrep: &runtimeprep.ExtensionRuntimePrep{
			Home:          &runtimeprep.ExtensionRuntimeHome{EnvVar: "TEST_AGENT_HOME"},
			ModelEndpoint: &runtimeprep.ExtensionModelEndpoint{APIKeyEnv: "OPENAI_API_KEY"},
		},
	}
	config := runtimeAdapterConfig(binding, "extension:test-agent")
	if config.AgentTargetID != "extension:test-agent" {
		t.Fatalf("AgentTargetID = %q", config.AgentTargetID)
	}
	if got := strings.Join(config.IsolatedRuntimeEnvNames, ","); got != "TEST_AGENT_HOME,OPENAI_API_KEY" {
		t.Fatalf("IsolatedRuntimeEnvNames = %#v", config.IsolatedRuntimeEnvNames)
	}

	empty := runtimeAdapterConfig(RuntimeBinding{
		Installation: binding.Installation,
		Command:      binding.Command,
	}, "extension:test-agent")
	if len(empty.IsolatedRuntimeEnvNames) != 0 {
		t.Fatalf("IsolatedRuntimeEnvNames without runtimePrep = %#v", empty.IsolatedRuntimeEnvNames)
	}
}
