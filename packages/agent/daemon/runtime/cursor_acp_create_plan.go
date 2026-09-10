package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

type cursorCreatePlanRequest struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
	Overview   string `json:"overview"`
	Plan       string `json:"plan"`
}

func (a *standardACPAdapter) handleCursorCreatePlan(
	ctx context.Context,
	client *acpClient,
	session Session,
	turnID string,
	message acpMessage,
	normalizer *acpTurnNormalizer,
	emit EventSink,
) ([]activityshared.Event, error) {
	if client == nil {
		return nil, errors.New("cursor create_plan request has no ACP client")
	}
	if normalizer == nil || strings.TrimSpace(turnID) == "" {
		err := errors.New("cursor create_plan request arrived outside an active prompt turn")
		_ = client.Respond(ctx, message.ID, nil, &acpError{Code: -32000, Message: err.Error()})
		return nil, err
	}
	requestID := acpRequestID(message.ID)
	if requestID == "" {
		err := errors.New("cursor create_plan request id is required")
		_ = client.Respond(ctx, message.ID, nil, &acpError{Code: -32602, Message: err.Error()})
		return nil, err
	}
	parsed, input, options, err := parseCursorCreatePlanRequest(message.Params)
	if err != nil {
		_ = client.Respond(ctx, message.ID, nil, &acpError{Code: -32602, Message: err.Error()})
		return nil, err
	}
	input["requestId"] = requestID
	input["options"] = cloneOptionMaps(options)
	title := firstNonEmpty(strings.TrimSpace(parsed.Name), "CreatePlan")

	if strings.TrimSpace(session.PermissionModeID) == cursorPermissionFullAccess {
		interactionErr := &AppError{
			Code:    AppErrorInteractionRequired,
			Message: "interaction_required: Cursor requested plan confirmation during a full-access headless turn.",
		}
		events := []activityshared.Event{newTurnActivityEventWithID(
			session, parsed.ToolCallID, EventCallFailed, turnID, SessionStatusWorking, "", title,
			map[string]any{
				"callId": parsed.ToolCallID, "callType": "interactive", "toolName": "CreatePlan",
				"status": "failed", "code": AppErrorInteractionRequired, "input": clonePayload(input),
			},
		)}
		if emit != nil {
			emit(events)
		}
		_ = client.Respond(ctx, message.ID, nil, &acpError{Code: -32000, Message: interactionErr.Error()})
		return events, interactionErr
	}

	pending := &pendingACPApproval{
		agentSessionID:       strings.TrimSpace(session.AgentSessionID),
		requestID:            requestID,
		eventID:              newID(),
		callID:               parsed.ToolCallID,
		callType:             "interactive",
		turnID:               strings.TrimSpace(turnID),
		input:                clonePayload(input),
		kind:                 "exit-plan",
		name:                 title,
		toolName:             "CreatePlan",
		options:              cloneOptionMaps(options),
		response:             make(chan pendingInteractiveResponse, 1),
		interactionRequested: true,
	}
	pending.prompt = &SessionInteractivePrompt{
		Kind: "exit-plan", RequestID: requestID, ToolName: "CreatePlan", Status: "waiting_input",
		Input: clonePayload(input), Metadata: map[string]any{
			"callType": "interactive", "interactiveKind": "exit-plan",
			"toolName": "CreatePlan", "providerMethod": cursorACPMethodCreatePlan,
		},
	}
	a.storePendingApproval(pending)
	events := []activityshared.Event{
		newTurnActivityEvent(session, EventTurnUpdated, turnID, SessionStatusWaiting, "", "", map[string]any{
			"phase": string(activityshared.TurnPhaseWaitingApproval), "requestId": requestID,
		}),
		newTurnActivityEventWithID(session, pending.eventID, EventCallStarted, turnID, SessionStatusWaiting, "", title, map[string]any{
			"callId": parsed.ToolCallID, "callType": "interactive", "name": title,
			"toolName": "CreatePlan", "status": "waiting_input", "input": clonePayload(input),
		}),
		normalizedInteractionRequestedEvent(session, turnID, pending),
	}
	if emit != nil {
		emit(events)
	}
	go a.respondACPPermissionRequest(ctx, client, session, turnID, message.ID, pending, emit)
	return nil, nil
}

func parseCursorCreatePlanRequest(raw json.RawMessage) (cursorCreatePlanRequest, map[string]any, []map[string]any, error) {
	var request cursorCreatePlanRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return cursorCreatePlanRequest{}, nil, nil, fmt.Errorf("invalid cursor create_plan request: %w", err)
	}
	request.ToolCallID = strings.TrimSpace(request.ToolCallID)
	request.Plan = strings.TrimSpace(request.Plan)
	if request.ToolCallID == "" {
		return cursorCreatePlanRequest{}, nil, nil, errors.New("cursor create_plan toolCallId is required")
	}
	if request.Plan == "" {
		return cursorCreatePlanRequest{}, nil, nil, errors.New("cursor create_plan plan is required")
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return cursorCreatePlanRequest{}, nil, nil, fmt.Errorf("invalid cursor create_plan payload: %w", err)
	}
	input["toolCall"] = map[string]any{
		"toolCallId": request.ToolCallID,
		"title":      firstNonEmpty(strings.TrimSpace(request.Name), "Create plan"),
		"kind":       "switch_mode",
	}
	options := []map[string]any{
		{"optionId": "accept", "id": "accept", "name": "Implement plan", "label": "Implement plan", "kind": "allow_once"},
		{"optionId": "plan", "id": "plan", "name": "Keep planning", "label": "Keep planning", "kind": "reject_once"},
	}
	return request, input, options, nil
}

func cursorACPCreatePlanResult(action, optionID string, payload map[string]any) (map[string]any, string) {
	actionToken := normalizePermissionOptionToken(action)
	optionID = strings.TrimSpace(optionID)
	if actionToken == "cancel" || actionToken == "cancelled" || actionToken == "dismiss" || actionToken == "dismissed" {
		return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, optionID
	}
	if actionToken == "deny" || actionToken == "reject" || actionToken == "rejected" || optionID == "plan" {
		outcome := map[string]any{"outcome": "rejected"}
		if reason := strings.TrimSpace(asString(payload["denyMessage"])); reason != "" {
			outcome["reason"] = reason
		}
		return map[string]any{"outcome": outcome}, firstNonEmpty(optionID, "plan")
	}
	return map[string]any{"outcome": map[string]any{"outcome": "accepted"}}, firstNonEmpty(optionID, "accept")
}
