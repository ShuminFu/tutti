package agentextension

import (
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestExtensionHostModelEndpointAvailable(t *testing.T) {
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, "")
	t.Setenv(runtimeprep.HostModelEndpointsEnv, `{
		"version":1,
		"providers":{"acp:test":{"protocol":"openai","wireAPI":"chat","baseURL":"http://127.0.0.1:18788/v1","apiKey":"secret","model":"chat"}}
	}`)
	prep := &runtimeprep.ExtensionRuntimePrep{ModelEndpoint: &runtimeprep.ExtensionModelEndpoint{
		Protocol: "openai", WireAPI: "chat",
	}}
	if !extensionHostModelEndpointAvailable("acp:test", prep) {
		t.Fatal("matching extension host endpoint must satisfy setup authentication")
	}
	if extensionHostModelEndpointAvailable("acp:other", prep) {
		t.Fatal("missing extension host endpoint must keep setup authentication required")
	}
}
