package api

import (
	"reflect"
	"testing"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

// Completeness guards keep the API-owned projection in lockstep with generated
// OpenAPI transport structs without coupling the service/event layer to them.
func TestGeneratedWorkspaceAgentTurnCoversAllFields(t *testing.T) {
	t.Parallel()

	projected := generatedWorkspaceAgentTurn(agentactivitybiz.Turn{
		WorkspaceID:                  "ws-1",
		AgentSessionID:               "session-1",
		TurnID:                       "turn-1",
		RootProviderTurnID:           "provider-turn-1",
		ProviderForkBindingAvailable: true,
		CapabilityRefs:               []agentactivitybiz.CapabilityReference{{Capability: "tutti", Source: "slash_command"}},
		Origin:                       agentactivitybiz.TurnOriginGoalContinuation,
		SourceGoalOperationID:        "goal-operation-1",
		SourceGoalRevision:           2,
		SourceGoalRepairEpoch:        1,
		Phase:                        agentactivitybiz.TurnPhaseSettled,
		Outcome:                      agentactivitybiz.TurnOutcomeFailed,
		ErrorMessage:                 "provider exploded",
		ErrorCode:                    "provider_error",
		FileChanges:                  map[string]any{"added": 1},
		CompletedCommandKind:         "review",
		CompletedCommandStatus:       "completed",
		StartedAtUnixMS:              1717200000000,
		SettledAtUnixMS:              1717200001000,
		CreatedAtUnixMS:              1717200000000,
		UpdatedAtUnixMS:              1717200001000,
	})
	assertGeneratedAgentProjectionFieldsPopulated(t, projected)
}

func TestGeneratedWorkspaceAgentTurnOmitsErrorForCanceledOutcome(t *testing.T) {
	t.Parallel()

	projected := generatedWorkspaceAgentTurn(agentactivitybiz.Turn{
		AgentSessionID: "session-1",
		TurnID:         "turn-1",
		Phase:          agentactivitybiz.TurnPhaseSettled,
		Outcome:        agentactivitybiz.TurnOutcomeCanceled,
		ErrorMessage:   "context canceled",
	})
	if projected.Error != nil {
		t.Fatalf("canceled turn error = %#v, want omitted transport-only error", projected.Error)
	}
}

// A Turn that settled before the runtime could classify a tool-protocol
// rejection keeps a coarse stored code with the raw upstream text. The
// read-side projection must refine the code and expose the raw detail so the
// conversation card can explain it without a database migration.
func TestGeneratedWorkspaceAgentTurnRefinesStoredProtocolIncompatibility(t *testing.T) {
	t.Parallel()

	projected := generatedWorkspaceAgentTurn(agentactivitybiz.Turn{
		AgentSessionID: "session-1",
		TurnID:         "turn-1",
		Phase:          agentactivitybiz.TurnPhaseSettled,
		Outcome:        agentactivitybiz.TurnOutcomeFailed,
		ErrorCode:      "provider_error",
		ErrorMessage:   storedProtocolIncompatibilityDetail,
	})
	if projected.Error == nil {
		t.Fatal("projected turn error = nil, want the stored failure")
	}
	if got := stringValue(projected.Error.Code); got != agentruntime.FailureCodeProviderProtocolIncompatible {
		t.Fatalf("projected code = %q, want %q", got, agentruntime.FailureCodeProviderProtocolIncompatible)
	}
	if projected.Error.Message != storedProtocolIncompatibilityDetail {
		t.Fatalf("projected message = %q, want the stored text", projected.Error.Message)
	}
	if got := stringValue(projected.Error.Detail); got != storedProtocolIncompatibilityDetail {
		t.Fatalf("projected detail = %q, want the raw upstream text", got)
	}
}

// The HTTP projection read by host integrations (RnDMaster task errors) must
// carry the readable upstream sentence for an unclassified provider_error.
func TestGeneratedWorkspaceAgentTurnExposesReadableProviderErrorDetail(t *testing.T) {
	t.Parallel()

	const body = `{"error":{"code":"local_pool_unavailable","message":"本地账号池里的账号都已停用，请到 设置 → 本地凭证 启用至少一个。","type":"invalid_request_error"}}`
	projected := generatedWorkspaceAgentTurn(agentactivitybiz.Turn{
		AgentSessionID: "session-1",
		TurnID:         "turn-1",
		Phase:          agentactivitybiz.TurnPhaseSettled,
		Outcome:        agentactivitybiz.TurnOutcomeFailed,
		ErrorCode:      "provider_error",
		ErrorMessage:   body,
	})
	if projected.Error == nil {
		t.Fatal("projected turn error = nil, want the stored failure")
	}
	if projected.Error.Message != body {
		t.Fatalf("projected message = %q, want the stored raw text", projected.Error.Message)
	}
	want := "本地账号池里的账号都已停用，请到 设置 → 本地凭证 启用至少一个。 (local_pool_unavailable)"
	if got := stringValue(projected.Error.Detail); got != want {
		t.Fatalf("projected detail = %q, want %q", got, want)
	}
}

func TestGeneratedWorkspaceAgentTurnLeavesOtherStoredErrorsAlone(t *testing.T) {
	t.Parallel()

	projected := generatedWorkspaceAgentTurn(agentactivitybiz.Turn{
		AgentSessionID: "session-1",
		TurnID:         "turn-1",
		Phase:          agentactivitybiz.TurnPhaseSettled,
		Outcome:        agentactivitybiz.TurnOutcomeFailed,
		ErrorCode:      "auth_required",
		ErrorMessage:   "401 Unauthorized: invalid authentication credentials",
	})
	if projected.Error == nil {
		t.Fatal("projected turn error = nil, want the stored failure")
	}
	if got := stringValue(projected.Error.Code); got != "auth_required" {
		t.Fatalf("projected code = %q, want auth_required", got)
	}
	if projected.Error.Detail != nil {
		t.Fatalf("projected detail = %#v, want omitted for a non-protocol failure", projected.Error.Detail)
	}
}

const storedProtocolIncompatibilityDetail = "Failed to deserialize the JSON body into the target type: " +
	"tools[7].type: unknown variant `custom`, expected `function`"

func TestGeneratedWorkspaceAgentTurnProjectsProviderForkBindingState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		turn          agentactivitybiz.Turn
		want          string
		wantAvailable bool
	}{
		{
			name: "bound",
			turn: agentactivitybiz.Turn{
				TurnID:                       "turn-1",
				Phase:                        agentactivitybiz.TurnPhaseSettled,
				RootProviderTurnID:           "provider-turn-1",
				ProviderForkBindingAvailable: true,
			},
			want:          "bound",
			wantAvailable: true,
		},
		{
			name: "settled recovery required",
			turn: agentactivitybiz.Turn{
				TurnID: "turn-1",
				Phase:  agentactivitybiz.TurnPhaseSettled,
			},
			want: "recovery_required",
		},
		{
			name: "settled canonical identity echo requires recovery",
			turn: agentactivitybiz.Turn{
				TurnID:             "turn-1",
				Phase:              agentactivitybiz.TurnPhaseSettled,
				RootProviderTurnID: "turn-1",
			},
			want: "recovery_required",
		},
		{
			name: "settled unverified historical binding requires recovery",
			turn: agentactivitybiz.Turn{
				TurnID:             "turn-1",
				Phase:              agentactivitybiz.TurnPhaseSettled,
				RootProviderTurnID: "provider-turn-legacy",
			},
			want: "recovery_required",
		},
		{
			name: "running unavailable",
			turn: agentactivitybiz.Turn{
				TurnID: "turn-1",
				Phase:  agentactivitybiz.TurnPhaseRunning,
			},
			want: "unavailable",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := generatedWorkspaceAgentTurn(test.turn)
			if string(got.ProviderForkBindingState) != test.want {
				t.Fatalf("provider fork binding state = %q, want %q", got.ProviderForkBindingState, test.want)
			}
			if got.ProviderForkBindingAvailable != test.wantAvailable {
				t.Fatalf(
					"provider fork binding available = %v, want %v",
					got.ProviderForkBindingAvailable,
					test.wantAvailable,
				)
			}
		})
	}
}

func TestGeneratedWorkspaceAgentInteractionCoversAllFields(t *testing.T) {
	t.Parallel()

	projected := generatedWorkspaceAgentInteraction(agentactivitybiz.Interaction{
		WorkspaceID:     "ws-1",
		AgentSessionID:  "session-1",
		RequestID:       "request-1",
		TurnID:          "turn-1",
		Kind:            agentactivitybiz.InteractionKindApproval,
		Status:          agentactivitybiz.InteractionStatusPending,
		ToolName:        "shell",
		Input:           map[string]any{"command": "ls"},
		Output:          map[string]any{"optionId": "allow"},
		Metadata:        map[string]any{"source": "acp"},
		CreatedAtUnixMS: 1717200000000,
		UpdatedAtUnixMS: 1717200001000,
	})
	assertGeneratedAgentProjectionFieldsPopulated(t, projected)
}

func assertGeneratedAgentProjectionFieldsPopulated(t *testing.T, value any) {
	t.Helper()
	reflected := reflect.ValueOf(value)
	structType := reflected.Type()
	if structType.Kind() != reflect.Struct {
		t.Fatalf("expected struct, got %s", structType.Kind())
	}
	for i := range structType.NumField() {
		if reflected.Field(i).IsZero() {
			t.Errorf(
				"generated field %s.%s is zero: the API projection must assign every generated field explicitly",
				structType.Name(),
				structType.Field(i).Name,
			)
		}
	}
}
