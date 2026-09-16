package agent

import (
	"context"
	"testing"
	"time"

	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

// The profile declares the ACP config-option ids the extension manifest maps to
// composer concepts; the runtime snapshot must use the same ids.
const extensionValidationReasoningConfigOptionID = "reasoning_effort"

// An ACP config-option snapshot for the extension target: it advertises the
// model plus a reasoning effort, which is exactly what the defaults-persistence
// path could not see before target evidence existed.
func extensionValidationRuntimeConfigOptions() []any {
	return []any{
		map[string]any{
			"id": "model",
			"options": []any{
				map[string]any{"value": "gemini-pro", "name": "Gemini Pro"},
				map[string]any{"value": "gemini-fast", "name": "Gemini Fast"},
			},
		},
		map[string]any{
			"id":      extensionValidationReasoningConfigOptionID,
			"current": "medium",
			"options": []any{
				map[string]any{"value": "low", "name": "Low"},
				map[string]any{"value": "medium", "name": "Medium"},
				map[string]any{"value": "high", "name": "High"},
			},
		},
	}
}

// The core T1 regression: a defaults patch carries no workspace or cwd, so it
// cannot open a session to read ACP capabilities. Without the target-scoped
// evidence store the reasoning value would be refused as not_configurable even
// though the runtime advertises it.
func TestValidateAgentComposerDefaultsPatchUsesTargetRuntimeEvidenceWithoutScope(t *testing.T) {
	_, service := newExtensionComposerValidationService(t)
	service.ExtensionComposerProfiles = extensionComposerValidationProfileResolver()

	// No workspace, no cwd: the same shape the sparse defaults patch has.
	scope := newComposerLiveModelScope("acp:gemini", "", "", extensionComposerValidationTargetID)
	service.setComposerRuntimeContextForScope(scope, time.Now().UTC(), time.Now().UTC(), map[string]any{
		"configOptions": extensionValidationRuntimeConfigOptions(),
	})

	reasoning := "high"
	result, err := service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		extensionComposerValidationTargetID,
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort: &reasoning,
		},
	)
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() error = %v", err)
	}
	if len(result.Rejected) != 0 {
		t.Fatalf("rejected = %#v, want reasoning accepted from runtime evidence", result.Rejected)
	}
	if got := result.Applied[preferencesbiz.AgentComposerDefaultsFieldReasoningEffort]; got != "high" {
		t.Fatalf("applied reasoningEffort = %#v, want high", got)
	}
}

// Evidence is per target. A sibling extension target must not inherit it, or a
// value legal on one runtime would be silently accepted on another.
func TestValidateAgentComposerDefaultsPatchDoesNotLeakTargetRuntimeEvidence(t *testing.T) {
	_, service := newExtensionComposerValidationService(t)
	service.ExtensionComposerProfiles = extensionComposerValidationProfileResolver()
	scope := newComposerLiveModelScope("acp:gemini", "", "", extensionComposerValidationTargetID)
	service.setComposerRuntimeContextForScope(scope, time.Now().UTC(), time.Now().UTC(), map[string]any{
		"configOptions": extensionValidationRuntimeConfigOptions(),
	})
	service.AgentTargetStore = fakeAgentTargetStore{targets: map[string]agenttargetbiz.Target{
		extensionComposerValidationTargetID: {
			ID:            extensionComposerValidationTargetID,
			Provider:      "acp:gemini",
			LaunchRefJSON: `{"type":"agent_extension","extensionInstallationId":"gemini@1.0.0"}`,
			Name:          "Gemini CLI",
			Enabled:       true,
			Source:        agenttargetbiz.SourceSystem,
		},
		"extension:other-validation": {
			ID:            "extension:other-validation",
			Provider:      "acp:gemini",
			LaunchRefJSON: `{"type":"agent_extension","extensionInstallationId":"other@1.0.0"}`,
			Name:          "Other CLI",
			Enabled:       true,
			Source:        agenttargetbiz.SourceSystem,
		},
	}}

	reasoning := "high"
	result, err := service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		"extension:other-validation",
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort: &reasoning,
		},
	)
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() error = %v", err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("applied = %#v, want sibling target to inherit nothing", result.Applied)
	}
}

// T2 regression: one illegal field must not drop its legal siblings. Before
// this, the permission mode was lost purely because the reasoning value in the
// same snapshot was bad.
func TestValidateAgentComposerDefaultsPatchKeepsLegalFieldsBesideRejectedOnes(t *testing.T) {
	_, service := newExtensionComposerValidationService(t)
	service.ExtensionComposerProfiles = extensionComposerValidationProfileResolver()
	scope := newComposerLiveModelScope("acp:gemini", "", "", extensionComposerValidationTargetID)
	service.setComposerRuntimeContextForScope(scope, time.Now().UTC(), time.Now().UTC(), map[string]any{
		"configOptions": extensionValidationRuntimeConfigOptions(),
	})

	permissionMode := "yolo"
	reasoning := "ultra"
	result, err := service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		extensionComposerValidationTargetID,
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldPermissionModeID: &permissionMode,
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort:  &reasoning,
		},
	)
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() error = %v", err)
	}
	if got := result.Applied[preferencesbiz.AgentComposerDefaultsFieldPermissionModeID]; got != "yolo" {
		t.Fatalf("applied permissionModeId = %#v, want yolo to survive a bad sibling", result.Applied)
	}
	if _, ok := result.Applied[preferencesbiz.AgentComposerDefaultsFieldReasoningEffort]; ok {
		t.Fatalf("applied = %#v, want reasoningEffort withheld", result.Applied)
	}
	if len(result.Rejected) != 1 ||
		result.Rejected[0].Field != preferencesbiz.AgentComposerDefaultsFieldReasoningEffort ||
		result.Rejected[0].ReasonCode != AgentComposerDefaultsReasonUnsupportedValue {
		t.Fatalf("rejected = %#v, want one unsupported_value reasoningEffort", result.Rejected)
	}
}

// A rejected field still reports a reason even when nothing else in the patch
// can be judged, and clearing a value is never blocked by capability checks.
func TestValidateAgentComposerDefaultsPatchClearsDoNotRequireCapability(t *testing.T) {
	_, service := newExtensionComposerValidationService(t)
	permissionMode := "yolo"
	result, err := service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		extensionComposerValidationTargetID,
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldPermissionModeID: &permissionMode,
		},
	)
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch() error = %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("applied = %#v, want the profile-declared permission mode accepted", result.Applied)
	}

	// A null clears the stored default without any runtime evidence at all.
	result, err = service.ValidateAgentComposerDefaultsPatch(
		context.Background(),
		extensionComposerValidationTargetID,
		preferencesbiz.AgentComposerDefaultsPatch{
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort: nil,
		},
	)
	if err != nil {
		t.Fatalf("ValidateAgentComposerDefaultsPatch(clear) error = %v", err)
	}
	if _, ok := result.Applied[preferencesbiz.AgentComposerDefaultsFieldReasoningEffort]; !ok {
		t.Fatalf("applied = %#v, want a null clear accepted", result.Applied)
	}
	if len(result.Rejected) != 0 {
		t.Fatalf("rejected = %#v, want a clear never rejected", result.Rejected)
	}
}

// extensionComposerValidationProfileResolver mirrors the shared validation
// fixture but names the reasoning and permission ACP config options the runtime
// snapshot above advertises.
func extensionComposerValidationProfileResolver() extensionComposerProfileResolverStub {
	return extensionComposerProfileResolverStub{
		profile: ExtensionComposerProfile{
			PermissionConfigOptionID: "mode",
			ReasoningConfigOptionID:  extensionValidationReasoningConfigOptionID,
			PermissionModes: []ExtensionComposerPermissionMode{
				{RuntimeID: "default", Semantic: PermissionModeSemanticAskBeforeWrite},
				{RuntimeID: "yolo", Semantic: PermissionModeSemanticFullAccess},
			},
		},
	}
}
