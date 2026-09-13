package agentruntime

import (
	"context"
	"testing"
)

type installationAdapterResolver func(context.Context, AdapterResolveInput) (Adapter, error)

func (f installationAdapterResolver) ResolveAdapter(ctx context.Context, input AdapterResolveInput) (Adapter, error) {
	return f(ctx, input)
}

func TestExtensionRefreshKeepsExistingSessionsOnTheirInstallation(t *testing.T) {
	ctx := context.Background()
	calls := 0
	resolver := installationAdapterResolver(func(_ context.Context, input AdapterResolveInput) (Adapter, error) {
		calls++
		id := providerTargetRefString(input.ProviderTargetRef, "extensionInstallationId")
		return NewStandardACPAdapter(StandardACPAdapterConfig{
			Provider: input.Provider, Name: "example-acp", Command: []string{"example", "--acp"},
			AgentTargetID: input.AgentTargetID, InstallationID: id,
		}, newStandardACPTransport("Example", id), LegacyHostMetadata())
	})
	controller := NewControllerWithAdapterResolver(nil, nil, resolver)
	start := func(id, installation string) Session {
		t.Helper()
		result, err := controller.Start(ctx, StartInput{
			RoomID: "room", AgentSessionID: id, Provider: "acp:example", AgentTargetID: "extension:example",
			ProviderTargetRef: map[string]any{"kind": "agent_extension", "provider": "acp:example", "targetId": "extension:example", "extensionInstallationId": installation},
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.Session
	}
	old := start("old", "example@1.0.0")
	current := start("new", "example@2.0.0")
	if !controller.HasLiveSession(old.RoomID, old.AgentSessionID) || !controller.HasLiveSession(current.RoomID, current.AgentSessionID) {
		t.Fatal("refresh lost a live session")
	}
	if controller.adapterForSession(old) == controller.adapterForSession(current) {
		t.Fatal("installations shared an adapter")
	}
	resolved, err := controller.resolveAdapter(ctx, AdapterResolveInput{Provider: current.Provider, AgentTargetID: current.AgentTargetID, ProviderTargetRef: current.ProviderTargetRef})
	if err != nil || resolved != controller.adapterForSession(current) {
		t.Fatalf("current installation was not reused: %v", err)
	}
	if calls != 2 {
		t.Fatalf("resolved %d times, want once per installation", calls)
	}
	if _, err := controller.Close(ctx, CloseInput{RoomID: old.RoomID, AgentSessionID: old.AgentSessionID}); err != nil {
		t.Fatal(err)
	}
	if !controller.HasLiveSession(current.RoomID, current.AgentSessionID) {
		t.Fatal("closing old installation disconnected the new session")
	}
	result := controller.CloseAllLiveSessions(ctx)
	if result.Failed != 0 {
		t.Fatalf("shutdown failed: %#v", result)
	}
	if controller.HasLiveSession(current.RoomID, current.AgentSessionID) {
		t.Fatal("shutdown missed new installation")
	}
}
