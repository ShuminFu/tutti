package agent

import (
	"context"
	"testing"
)

func TestFrozenAgentModelCatalogPreservesCassetteOrderAndDeduplicates(t *testing.T) {
	catalog := NewFrozenAgentModelCatalog(map[string][]string{
		"codex": {" gpt-5.3-codex-spark ", "gpt-5.3-codex-spark", "gpt-5-codex"},
	})
	result, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{
		Provider: "codex",
	})
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if result.Source != replayFrozenModelCatalogSource {
		t.Fatalf("Source = %q, want %q", result.Source, replayFrozenModelCatalogSource)
	}
	if len(result.Models) != 2 ||
		result.Models[0].ID != "gpt-5.3-codex-spark" ||
		result.Models[1].ID != "gpt-5-codex" {
		t.Fatalf("Models = %#v, want cassette order without duplicates", result.Models)
	}
	if !result.Models[0].IsDefault || result.Models[1].IsDefault {
		t.Fatalf("default flags = %#v, want only first model default", result.Models)
	}
}

func TestReplayModelValidationUsesFrozenCatalog(t *testing.T) {
	service := &Service{
		ReplayMode: true,
		ModelCatalog: NewFrozenAgentModelCatalog(map[string][]string{
			"codex": {"gpt-5.3-codex-spark"},
		}),
	}
	if err := service.validateComposerModelForCreate(
		context.Background(),
		"codex",
		"workspace-1",
		"",
		"gpt-5.3-codex-spark",
	); err != nil {
		t.Fatalf("frozen model validation error = %v", err)
	}
	if err := service.validateComposerModelForCreate(
		context.Background(),
		"codex",
		"workspace-1",
		"",
		"current-live-only-model",
	); err == nil {
		t.Fatal("frozen model validation accepted a model absent from the cassette")
	}
}
