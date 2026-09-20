package agentruntime

import (
	"encoding/json"
	"testing"
)

func TestACPModeValueReadsCurrentModeID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		update map[string]any
		want   string
	}{
		{name: "acp canonical currentModeId", update: map[string]any{"currentModeId": "acceptEdits"}, want: "acceptEdits"},
		{name: "snake current_mode_id", update: map[string]any{"current_mode_id": "plan"}, want: "plan"},
		{name: "legacy modeId fallback", update: map[string]any{"modeId": "default"}, want: "default"},
		{name: "empty", update: map[string]any{}, want: ""},
	}
	for _, tc := range cases {
		if got := acpModeValue(tc.update); got != tc.want {
			t.Fatalf("%s: acpModeValue = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestApplyACPUpdateToLiveStateCapturesCurrentModeID(t *testing.T) {
	t.Parallel()

	state := newACPLiveState()
	raw, err := json.Marshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "current_mode_update",
			"currentModeId": "auto",
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	applyACPUpdateToLiveState(&state, "agent-session-1", "turn-1", raw)
	if state.currentMode != "auto" {
		t.Fatalf("state.currentMode = %q, want auto", state.currentMode)
	}
}

func TestACPUsageUpdateKeepsLastTurnModels(t *testing.T) {
	t.Parallel()

	state := newACPLiveState()
	raw, err := json.Marshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "usage_update",
			"used":          110,
			"size":          128000,
			"lastTurn": map[string]any{
				"models": map[string]any{
					"deepseek-flash": map[string]any{
						"inputTokens":      70,
						"outputTokens":     6,
						"cacheReadTokens":  30,
						"cacheWriteTokens": 4,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	applyACPUpdateToLiveState(&state, "agent-session-1", "turn-a", raw)
	models := payloadObject(payloadObject(state.usage.lastTurn)["models"])
	last := payloadObject(models["deepseek-flash"])
	if got, _ := firstInt64Value(last, "cacheReadTokens"); got != 30 {
		t.Fatalf("lastTurn = %#v", state.usage.lastTurn)
	}
	if state.usage.lastTurnID != "turn-a" {
		t.Fatalf("lastTurnID = %q, want turn-a", state.usage.lastTurnID)
	}
	binding := acpTurnUsageBindingJSON(state.usage, "turn-a")
	if string(binding) == "" || !json.Valid(binding) {
		t.Fatalf("binding = %s", binding)
	}
	if acpTurnUsageBindingJSON(state.usage, "turn-b") != nil {
		t.Fatal("later turn inherited previous lastTurn")
	}
}

func TestACPUsageMergeDoesNotReviveClearedLastTurn(t *testing.T) {
	t.Parallel()

	previous := acpUsageState{
		contextKnown:        true,
		contextUsedTokens:   110,
		contextWindowTokens: 128000,
	}
	next, ok := acpUsageValue(map[string]any{"used": 120, "size": 128000})
	if !ok {
		t.Fatal("used/size update was ignored")
	}
	merged := mergeACPUsageState(previous, next)
	if len(merged.lastTurn) != 0 || merged.lastTurnID != "" {
		t.Fatalf("cleared lastTurn came back: %#v", merged)
	}
}
