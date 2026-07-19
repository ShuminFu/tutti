package claudesidecar

// MessageProjection mirrors messageProjection.ts: system messages, content
// blocks, fast-mode state, and goal status become sidecar events.
type MessageProjection struct {
	providerSessionID func() string
	turns             *TurnLifecycle
	assistant         *AssistantStreamProjector
	activities        *ToolActivityProjector
	compaction        *CompactionTracker
	emit              Emitter
}

// MessageProjectionOptions configures a MessageProjection.
type MessageProjectionOptions struct {
	ProviderSessionID func() string
	Turns             *TurnLifecycle
	Assistant         *AssistantStreamProjector
	Activities        *ToolActivityProjector
	Compaction        *CompactionTracker
	Emit              Emitter
}

func NewMessageProjection(options MessageProjectionOptions) *MessageProjection {
	return &MessageProjection{
		providerSessionID: options.ProviderSessionID,
		turns:             options.Turns,
		assistant:         options.Assistant,
		activities:        options.Activities,
		compaction:        options.Compaction,
		emit:              options.Emit,
	}
}

func (p *MessageProjection) HandleSystemMessage(message map[string]any) {
	if _, present := message["fast_mode_state"]; present {
		p.EmitFastModeState(message["fast_mode_state"])
	}
	subtype := stringValue(message["subtype"])
	switch subtype {
	case "task_started", "task_progress", "task_notification":
		p.activities.HandleTaskSystemMessage(subtype, message)
		return
	case "task_updated":
		p.handleTaskUpdated(message)
		return
	case "init", "commands_changed":
		commands := commandEntries(message["commands"])
		_, isList := message["commands"].([]any)
		if len(commands) > 0 || isList {
			if commands == nil {
				commands = []map[string]any{}
			}
			p.emit("commands_updated", "", map[string]any{
				"providerSessionId": p.providerSessionID(),
				"commands":          commands,
			})
		}
		return
	}
	p.compaction.HandleSystemMessage(subtype, message)
}

func (p *MessageProjection) EmitFastModeState(state any) {
	rawState := stringValue(state)
	if rawState == "" {
		return
	}
	payload := map[string]any{"state": rawState}
	if speed := speedFromFastModeState(rawState); speed != "" {
		payload["speed"] = speed
	}
	p.emit("speed_updated", "", payload)
}

func (p *MessageProjection) HandleContentBlockStart(index *int, contentBlock map[string]any, parentToolUseID string) {
	if contentBlock == nil {
		return
	}
	if parentToolUseID == "" && contentBlock["type"] == "text" {
		p.assistant.Start(index, "assistant")
		return
	}
	if parentToolUseID == "" && contentBlock["type"] == "thinking" {
		p.assistant.Start(index, "thinking")
		return
	}
	if isToolUseBlock(contentBlock) {
		p.activities.UpsertToolUse(contentBlock, index, "tool_started", parentToolUseID)
	}
}

func (p *MessageProjection) HandleContentBlockStop(index *int) {
	if index == nil {
		return
	}
	if !p.assistant.CompleteIndex(index) {
		p.activities.CompleteToolIndex(*index)
	}
}

func (p *MessageProjection) HandleAssistantContentBlock(block map[string]any, parentToolUseID string, messageID string, usedSegmentIDs map[string]struct{}) {
	if isThinkingBlock(block) {
		if parentToolUseID == "" {
			p.assistant.CompleteContent("thinking", messageID, stringValue(block["thinking"]), usedSegmentIDs)
		}
		return
	}
	if block["type"] == "text" {
		if parentToolUseID == "" {
			p.assistant.CompleteContent("assistant", messageID, stringValue(block["text"]), usedSegmentIDs)
		}
		return
	}
	if isToolUseBlock(block) {
		p.activities.UpsertToolUse(block, nil, "tool_updated", parentToolUseID)
	}
}

func (p *MessageProjection) EmitGoalStatusFromBlocks(blocks []map[string]any) {
	goal := goalStateFromContentBlocks(blocks)
	if goal == nil {
		return
	}
	p.emit("goal_updated", "", map[string]any{
		"turnId":     p.turns.ActiveID(),
		"updateType": "thread_goal_update",
		"goal":       goal,
	})
}

func (p *MessageProjection) handleTaskUpdated(message map[string]any) {
	patch := recordValue(message["patch"])
	status := stringValue(patch["status"])
	if status != "completed" && status != "failed" && status != "killed" {
		return
	}
	if status == "killed" {
		status = "stopped"
	}
	summary := firstNonEmptyText(
		stringValue(patch["description"]),
		stringValue(message["summary"]),
		stringValue(message["description"]),
	)
	p.activities.HandleTaskSystemMessage("task_notification", map[string]any{
		"task_id":     stringValue(message["task_id"]),
		"tool_use_id": stringValue(message["tool_use_id"]),
		"status":      status,
		"summary":     summary,
	})
}
