package agentstatus

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

type memoryCodexRuntimeSelectionStore struct {
	selection agentproviderbiz.RuntimeSelection
	found     bool
}

func (s *memoryCodexRuntimeSelectionStore) GetAgentProviderRuntimeSelection(_ context.Context, _ string) (agentproviderbiz.RuntimeSelection, bool, error) {
	return s.selection, s.found, nil
}

func (s *memoryCodexRuntimeSelectionStore) PutAgentProviderRuntimeSelection(_ context.Context, selection agentproviderbiz.RuntimeSelection) (agentproviderbiz.RuntimeSelection, error) {
	s.selection, s.found = selection, true
	return selection, nil
}

func TestCodexRuntimeCatalogSelectionMarksMissingExplicitRuntimeStale(t *testing.T) {
	selection := agentproviderbiz.RuntimeSelection{LauncherPath: "/missing/codex"}
	got := codexRuntimeCatalogSelection([]CodexRuntimeCatalogCandidate{{ID: "candidate", LauncherPath: "/bin/codex"}}, codexRuntimeResolvedSelection{Selection: selection, Explicit: true})
	if got.State != CodexRuntimeSelectionStale || got.CandidateID != "" {
		t.Fatalf("selection = %#v", got)
	}
}

func TestCodexRuntimeCatalogRevisionDependsOnCandidateOrder(t *testing.T) {
	first := codexRuntimeCatalogRevision([]CodexRuntimeCatalogCandidate{{ID: "one"}, {ID: "two"}})
	second := codexRuntimeCatalogRevision([]CodexRuntimeCatalogCandidate{{ID: "two"}, {ID: "one"}})
	if first == second {
		t.Fatal("revisions must change when candidate order changes")
	}
}

func TestSetCodexRuntimeSelectionInvalidatesDerivedAvailability(t *testing.T) {
	home := t.TempDir()
	launcher := filepath.Join(home, "codex")
	launcher = writeCodexVersionFixture(t, launcher, "0.146.0")
	service := probeTestService(home)
	service.CodexRuntimeSelectionStore = &memoryCodexRuntimeSelectionStore{}
	service.Environ = func() []string {
		return []string{"PATH=" + filepath.Dir(launcher)}
	}
	service.CodexProtocolProbe = func(_ context.Context, _, _ []string) CodexProbeEvidence {
		return CodexProbeEvidence{CommandStarted: true, ProtocolReady: true}
	}
	var invalidated []string
	service.OnProviderStatusInvalidated = func(provider string) {
		invalidated = append(invalidated, provider)
	}

	catalog, err := service.GetCodexRuntimeCatalog(context.Background(), agentproviderbiz.Codex)
	if err != nil {
		t.Fatalf("GetCodexRuntimeCatalog() error = %v", err)
	}
	if _, err := service.SetCodexRuntimeSelection(context.Background(), SetCodexRuntimeSelectionInput{
		Provider:    agentproviderbiz.Codex,
		CandidateID: catalog.Candidates[0].ID,
		Revision:    catalog.Revision,
	}); err != nil {
		t.Fatalf("SetCodexRuntimeSelection() error = %v", err)
	}
	if !reflect.DeepEqual(invalidated, []string{agentproviderbiz.Codex}) {
		t.Fatalf("invalidated providers = %#v, want codex", invalidated)
	}
}
