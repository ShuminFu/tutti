package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	agentextensiondata "github.com/tutti-os/tutti/services/tuttid/data/agentextension"
	agentextensionservice "github.com/tutti-os/tutti/services/tuttid/service/agentextension"
	tuttitypes "github.com/tutti-os/tutti/services/tuttid/types"
)

func TestEmbeddedStartupDefersMissingRemoteAgentExtension(t *testing.T) {
	t.Setenv("RNDMASTER_TUTTI_EMBEDDED", "1")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	manager := &agentextensionservice.Manager{
		Sources: []tuttitypes.AgentExtensionSource{{
			Key: "kimi", Enabled: true, ReleaseIndexURL: server.URL,
		}},
		Installations: agentextensiondata.NewFileInstallationStore(t.TempDir()),
		Client:        server.Client(),
	}
	if !restoreAgentExtensionsForStartup(context.Background(), manager) {
		t.Fatal("embedded startup did not schedule the missing extension for background refresh")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("embedded startup made %d remote requests before readiness", got)
	}
}
