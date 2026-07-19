package claudesidecar

// SDKMessageRouter mirrors messageRouter.ts: it routes raw SDK messages into
// turn lifecycle, assistant projection, tool activity, compaction, and usage.
type SDKMessageRouter struct {
	getProviderSessionID   func() string
	setProviderSessionID   func(value string)
	onAssistantUUID        func(value string)
	onSessionState         func()
	onMaybeTitle           func(shouldEmit func() bool)
	turns                  *TurnLifecycle
	assistant              *AssistantStreamProjector
	activities             *ToolActivityProjector
	projection             *MessageProjection
	compaction             *CompactionTracker
	emit                   Emitter
	contextUsageGeneration int
}

// SDKMessageRouterOptions configures an SDKMessageRouter.
type SDKMessageRouterOptions struct {
	GetProviderSessionID func() string
	SetProviderSessionID func(value string)
	OnAssistantUUID      func(value string)
	OnSessionState       func()
	OnMaybeTitle         func(shouldEmit func() bool)
	Turns                *TurnLifecycle
	Assistant            *AssistantStreamProjector
	Activities           *ToolActivityProjector
	Projection           *MessageProjection
	Compaction           *CompactionTracker
	Emit                 Emitter
}

func NewSDKMessageRouter(options SDKMessageRouterOptions) *SDKMessageRouter {
	return &SDKMessageRouter{
		getProviderSessionID: options.GetProviderSessionID,
		setProviderSessionID: options.SetProviderSessionID,
		onAssistantUUID:      options.OnAssistantUUID,
		onSessionState:       options.OnSessionState,
		onMaybeTitle:         options.OnMaybeTitle,
		turns:                options.Turns,
		assistant:            options.Assistant,
		activities:           options.Activities,
		projection:           options.Projection,
		compaction:           options.Compaction,
		emit:                 options.Emit,
	}
}

func (r *SDKMessageRouter) Handle(message map[string]any) {
	parentToolUseID := readSDKParentToolUseID(message)
	r.emitLifecycleObservation(message, parentToolUseID)
	sessionID := readSDKSessionID(message)
	if sessionID != "" && sessionID != r.getProviderSessionID() {
		r.setProviderSessionID(sessionID)
		r.onSessionState()
	}
	assistantUUID := readSDKAssistantUUID(message)
	if assistantUUID != "" && parentToolUseID == "" {
		r.onAssistantUUID(assistantUUID)
		r.onSessionState()
	}

	switch message["type"] {
	case "attachment":
		if prompt := readQueuedTaskNotificationPrompt(message); prompt != "" {
			r.activities.HandleTaskNotificationFromText(prompt)
		}
	case "system":
		r.projection.HandleSystemMessage(message)
	case "stream_event":
		r.handleStreamEvent(message, parentToolUseID)
	case "assistant":
		r.handleAssistant(message, parentToolUseID)
	case "user":
		r.handleUser(message, parentToolUseID)
	case "tool_progress":
		if r.turns.EnsureActive("tool_progress") == nil {
			return
		}
		r.activities.HandleToolProgress(message, parentToolUseID)
	case "result":
		r.handleResult(message, parentToolUseID)
	}
}

func (r *SDKMessageRouter) emitLifecycleObservation(message map[string]any, parentToolUseID string) {
	messageType := stringValue(message["type"])
	messageSubtype := stringValue(message["subtype"])
	notificationText := ""
	if messageType == "user" {
		notificationText = readUserMessageNotificationText(message)
	}
	taskNotification := containsTaskNotification(notificationText)
	systemTaskLifecycle := messageType == "system" &&
		(messageSubtype == "task_started" ||
			messageSubtype == "task_progress" ||
			messageSubtype == "task_notification" ||
			messageSubtype == "task_updated")
	rootContinuationCandidate := messageType == "assistant" &&
		parentToolUseID == "" &&
		(r.turns.ActiveID() == "" || r.turns.AwaitingContinuation())
	result := messageType == "result"
	if !taskNotification && !systemTaskLifecycle && !rootContinuationCandidate && !result {
		return
	}

	payload := map[string]any{
		"sdkMessageType":     messageType,
		"activeTurnIdBefore": r.turns.ActiveID(),
	}
	if messageSubtype != "" {
		payload["sdkMessageSubtype"] = messageSubtype
	}
	if taskNotification {
		payload["taskNotification"] = true
	}
	if rootContinuationCandidate {
		payload["rootContinuationCandidate"] = true
	}
	if parentToolUseID != "" {
		payload["parentToolUseId"] = parentToolUseID
	}
	if taskID := stringValue(message["task_id"]); taskID != "" {
		payload["taskId"] = taskID
	}
	if agentID := stringValue(message["agent_id"]); agentID != "" {
		payload["agentId"] = agentID
	}
	if toolUseID := stringValue(message["tool_use_id"]); toolUseID != "" {
		payload["toolUseId"] = toolUseID
	}
	if status := stringValue(message["status"]); status != "" {
		payload["status"] = status
	}
	r.emit("sdk_lifecycle_observed", "", payload)
}

func (r *SDKMessageRouter) handleStreamEvent(message map[string]any, parentToolUseID string) {
	if r.turns.EnsureActive("stream_event") == nil {
		return
	}
	event := recordValue(message["event"])
	if event == nil {
		return
	}
	index := indexPointer(event["index"])
	switch event["type"] {
	case "message_start":
		if parentToolUseID == "" {
			r.assistant.SetMessageBase(stringValue(recordValue(event["message"])["id"]))
		}
	case "content_block_start":
		r.projection.HandleContentBlockStart(index, recordValue(event["content_block"]), parentToolUseID)
	case "content_block_stop":
		r.projection.HandleContentBlockStop(index)
	case "message_delta":
		if parentToolUseID != "" {
			return
		}
		if usage := recordValue(event["usage"]); usage != nil {
			r.emit("usage_updated", "", map[string]any{
				"turnId": r.turns.ActiveID(),
				"usage":  usage,
			})
		}
	case "content_block_delta":
		delta := recordValue(event["delta"])
		if delta == nil {
			return
		}
		if delta["type"] == "input_json_delta" {
			r.activities.HandleToolInputDelta(event["index"], delta["partial_json"])
			return
		}
		if parentToolUseID != "" {
			return
		}
		if delta["type"] == "text_delta" {
			if text, _ := delta["text"].(string); text != "" {
				r.assistant.AppendDelta(index, "assistant", text)
			}
		}
		if delta["type"] == "thinking_delta" {
			if thinking, _ := delta["thinking"].(string); thinking != "" {
				r.assistant.AppendDelta(index, "thinking", thinking)
			}
		}
	}
}

func indexPointer(value any) *int {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	index := int(number)
	return &index
}

func (r *SDKMessageRouter) handleAssistant(message map[string]any, parentToolUseID string) {
	if parentToolUseID != "" {
		r.handleNestedAssistant(message, parentToolUseID)
		return
	}
	if r.turns.EnsureActive("assistant") == nil {
		return
	}
	messageID := readSDKAssistantMessageID(message)
	blocks := contentBlocksFromMessage(message)
	usedAssistantSegmentIDs := map[string]struct{}{}
	for _, block := range blocks {
		r.projection.HandleAssistantContentBlock(block, parentToolUseID, messageID, usedAssistantSegmentIDs)
	}
	r.projection.EmitGoalStatusFromBlocks(blocks)
}

func (r *SDKMessageRouter) handleNestedAssistant(message map[string]any, parentToolUseID string) {
	for _, block := range contentBlocksFromMessage(message) {
		if isToolUseBlock(block) {
			r.activities.UpsertToolUse(block, nil, "tool_updated", parentToolUseID)
		}
	}
	if r.activities.IsNestedDelegatedTaskTerminalAssistant(message) &&
		!r.activities.HasUnsettledChildWork(parentToolUseID) {
		summary := r.activities.ExtractAssistantTextFromMessage(message)
		if summary == "" {
			summary = "Subagent task completed."
		}
		r.activities.CompleteDelegatedTaskFromParentMessage(parentToolUseID, map[string]any{
			"status":  "completed",
			"summary": summary,
		})
	}
}

func (r *SDKMessageRouter) handleUser(message map[string]any, parentToolUseID string) {
	notificationText := readUserMessageNotificationText(message)
	if containsTaskNotification(notificationText) {
		r.activities.HandleTaskNotificationFromText(notificationText)
	}
	activeTurnIDBefore := r.turns.ActiveID()
	r.turns.ActivateForUserMessage(readSDKMessageUUID(message))
	if parentToolUseID == "" && r.turns.ActiveID() != "" && r.turns.ActiveID() != activeTurnIDBefore {
		r.contextUsageGeneration++
	}
	blocks := contentBlocksFromMessage(message)
	if r.turns.PendingOrphans() > 0 {
		for _, block := range blocks {
			if block["type"] == "text" {
				r.turns.ClearPendingOrphans()
				break
			}
		}
	}
	for _, block := range blocks {
		r.activities.HandleUserContentBlock(block, parentToolUseID)
	}
	r.projection.EmitGoalStatusFromBlocks(blocks)
}

func (r *SDKMessageRouter) handleResult(message map[string]any, parentToolUseID string) {
	if parentToolUseID != "" {
		r.activities.CompleteDelegatedTaskFromResultMessage(parentToolUseID, message)
		return
	}
	r.projection.EmitFastModeState(message["fast_mode_state"])
	if r.turns.ConsumeTimedOutContinuationResult() ||
		r.turns.ConsumePendingOrphan() ||
		r.turns.EnsureActive("result") == nil {
		return
	}
	turnID := r.turns.ActiveID()
	contextUsageGeneration := r.contextUsageGeneration
	if r.turns.Cancelled() {
		r.turns.SettleActive("turn_canceled", nil)
		r.turns.ClearCancelled()
	} else if message["subtype"] == "success" {
		r.turns.SettleActive("turn_completed", map[string]any{"stopReason": "end_turn"})
	} else {
		errorText := "Claude SDK turn failed"
		if rawErrors, ok := message["errors"].([]any); ok && len(rawErrors) > 0 {
			if text, isString := rawErrors[0].(string); isString {
				errorText = text
			}
		}
		r.turns.SettleActive("turn_failed", map[string]any{"error": errorText})
	}
	r.emitResultUsage(turnID, contextUsageGeneration, message)
	if r.onMaybeTitle != nil {
		r.onMaybeTitle(func() bool {
			return r.contextUsageGeneration == contextUsageGeneration
		})
	}
}

func (r *SDKMessageRouter) emitResultUsage(turnID string, contextUsageGeneration int, result map[string]any) {
	shouldEmit := func() bool {
		return r.contextUsageGeneration == contextUsageGeneration
	}
	r.compaction.EmitContextUsageSnapshot(turnID, result["modelUsage"], shouldEmit, func(outcome string) {
		if outcome != "unavailable" || !shouldEmit() {
			return
		}
		emitUsageUpdated(r.emit, turnID, map[string]any{
			"usage":        result["usage"],
			"modelUsage":   result["modelUsage"],
			"totalCostUsd": result["total_cost_usd"],
		})
	})
}
