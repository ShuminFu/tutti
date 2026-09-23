package agent

import (
	"context"
	"testing"

	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

// Regression: the defaults validator asks Composer Options about the very value
// it is judging. Without a live Claude catalog the static fallback echoed that
// value back as a catalog row, so a Codex model was accepted as the
// claude-code default and every later claude-code create failed with
// `invalid model "gpt-5.6-luna" for provider "claude-code"`.
func TestValidateAgentComposerDefaultsPatchRejectsForeignModelEchoedByStaticClaudeCatalog(t *testing.T) {
	service := newTestService(newFakeRuntime())
	foreign := "gpt-5.6-luna"
	result, err := service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		agenttargetbiz.IDLocalClaudeCode,
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldModel: &foreign,
		},
	)
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() error = %v", err)
	}
	if _, applied := result.Applied[preferencesbiz.AgentComposerDefaultsFieldModel]; applied {
		t.Fatalf("applied = %#v, want model refused", result.Applied)
	}
	if len(result.Rejected) != 1 ||
		result.Rejected[0].Field != preferencesbiz.AgentComposerDefaultsFieldModel ||
		result.Rejected[0].ReasonCode != AgentComposerDefaultsReasonInvalidValue {
		t.Fatalf("rejected = %#v, want one invalid_value model", result.Rejected)
	}

	// A real static Claude alias must still be accepted on the same cold cache.
	alias := "opus"
	result, err = service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		agenttargetbiz.IDLocalClaudeCode,
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldModel: &alias,
		},
	)
	if err != nil || len(result.Rejected) != 0 || result.Applied[preferencesbiz.AgentComposerDefaultsFieldModel] != alias {
		t.Fatalf("alias result = %#v err = %v, want applied", result, err)
	}
}

// The picker keeps showing a custom selection, but flagged as an echo.
func TestStaticClaudeComposerModelOptionsMarksSelectionEchoAsRequested(t *testing.T) {
	t.Parallel()
	options := staticClaudeComposerModelOptions("my-custom-model")
	last := options[len(options)-1]
	if last.Value != "my-custom-model" || !last.Requested {
		t.Fatalf("echo row = %#v, want Requested custom model", last)
	}
	for _, option := range options[:len(options)-1] {
		if option.Requested {
			t.Fatalf("static alias %q marked Requested", option.Value)
		}
	}
}
