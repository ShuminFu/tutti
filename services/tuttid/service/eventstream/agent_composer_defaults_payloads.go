package eventstream

import (
	"encoding/json"
	"fmt"
	"strings"

	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

type agentComposerDefaultsPatchRequestedPayload struct {
	AgentTargetID    string                                    `json:"agentTargetId"`
	Patch            preferencesbiz.AgentComposerDefaultsPatch `json:"patch"`
	ClientMutationID string                                    `json:"clientMutationId,omitempty"`
}

type agentComposerDefaultsChangedPayload struct {
	AgentTargetID string `json:"agentTargetId"`
}

// agentComposerDefaultsRejectedPayload is one refused field. Message is
// diagnostic only and must never be rendered to the user; clients pick their own
// localized copy from ReasonCode.
type agentComposerDefaultsRejectedPayload struct {
	Field      string `json:"field"`
	ReasonCode string `json:"reasonCode"`
	Message    string `json:"message,omitempty"`
}

// agentComposerDefaultsResolvedPayload reports the per-field outcome of a
// defaults patch. One bad field no longer drops its legal siblings, so the
// client needs to learn which of the fields it sent actually landed.
type agentComposerDefaultsResolvedPayload struct {
	AgentTargetID    string                                 `json:"agentTargetId"`
	ClientMutationID string                                 `json:"clientMutationId,omitempty"`
	Applied          []string                               `json:"applied"`
	Rejected         []agentComposerDefaultsRejectedPayload `json:"rejected,omitempty"`
}

func validateAgentComposerDefaultsPatchRequestedPayload(payload []byte) error {
	var decoded agentComposerDefaultsPatchRequestedPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if strings.TrimSpace(decoded.AgentTargetID) == "" {
		return fmt.Errorf("agentTargetId is required")
	}
	if len(decoded.Patch) == 0 {
		return fmt.Errorf("patch is required")
	}
	for field := range decoded.Patch {
		switch field {
		case preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode,
			preferencesbiz.AgentComposerDefaultsFieldModel,
			preferencesbiz.AgentComposerDefaultsFieldPermissionModeID,
			preferencesbiz.AgentComposerDefaultsFieldReasoningEffort,
			preferencesbiz.AgentComposerDefaultsFieldSpeed:
		default:
			return fmt.Errorf("patch contains unsupported field %q", field)
		}
	}
	return nil
}

func validateAgentComposerDefaultsChangedPayload(payload []byte) error {
	var decoded agentComposerDefaultsChangedPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if strings.TrimSpace(decoded.AgentTargetID) == "" {
		return fmt.Errorf("agentTargetId is required")
	}
	return nil
}

func validateAgentComposerDefaultsResolvedPayload(payload []byte) error {
	var decoded agentComposerDefaultsResolvedPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if strings.TrimSpace(decoded.AgentTargetID) == "" {
		return fmt.Errorf("agentTargetId is required")
	}
	if decoded.Applied == nil {
		return fmt.Errorf("applied is required")
	}
	for _, field := range decoded.Applied {
		if !isAgentComposerDefaultsField(field) {
			return fmt.Errorf("applied contains unsupported field %q", field)
		}
	}
	for _, rejected := range decoded.Rejected {
		if !isAgentComposerDefaultsField(rejected.Field) {
			return fmt.Errorf("rejected contains unsupported field %q", rejected.Field)
		}
		if !isAgentComposerDefaultsReasonCode(rejected.ReasonCode) {
			return fmt.Errorf("rejected contains unsupported reasonCode %q", rejected.ReasonCode)
		}
	}
	return nil
}

func isAgentComposerDefaultsField(field string) bool {
	switch field {
	case preferencesbiz.AgentComposerDefaultsFieldCodexSaverMode,
		preferencesbiz.AgentComposerDefaultsFieldModel,
		preferencesbiz.AgentComposerDefaultsFieldPermissionModeID,
		preferencesbiz.AgentComposerDefaultsFieldReasoningEffort,
		preferencesbiz.AgentComposerDefaultsFieldSpeed:
		return true
	default:
		return false
	}
}

// The reason-code enum is duplicated here on purpose: eventstream owns the wire
// contract and must not depend on the agent service package just to validate it.
// The values mirror packages/events/protocol/definitions/preferences/
// agent.composer.defaults.resolved.event.json and
// agent.AgentComposerDefaultsReason* — all three must change together.
func isAgentComposerDefaultsReasonCode(reasonCode string) bool {
	switch reasonCode {
	case "invalid_value",
		"unsupported_value",
		"not_configurable",
		"unsupported_field",
		"internal_error":
		return true
	default:
		return false
	}
}
