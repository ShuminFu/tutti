package agentruntime

import (
	"encoding/json"
	"testing"
)

func TestCursorCreatePlanContract(t *testing.T) {
	request, input, options, err := parseCursorCreatePlanRequest(json.RawMessage(`{"toolCallId":"call-1","name":"Plan","plan":"1. change\n2. verify"}`))
	if err != nil || request.ToolCallID != "call-1" || input["toolCall"] == nil || len(options) != 2 {
		t.Fatalf("request=%#v input=%#v options=%#v err=%v", request, input, options, err)
	}
	accepted, optionID := cursorACPCreatePlanResult("approve", "accept", nil)
	if optionID != "accept" || asString(payloadObject(accepted["outcome"])["outcome"]) != "accepted" {
		t.Fatalf("accepted=%#v option=%q", accepted, optionID)
	}
	rejected, optionID := cursorACPCreatePlanResult("submit", "plan", map[string]any{"denyMessage": "revise"})
	if optionID != "plan" || asString(payloadObject(rejected["outcome"])["outcome"]) != "rejected" {
		t.Fatalf("rejected=%#v option=%q", rejected, optionID)
	}
}
