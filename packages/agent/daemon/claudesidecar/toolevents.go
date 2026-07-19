package claudesidecar

// toolTerminalListener observes terminal tool payloads (for delegated-task
// registration).
type toolTerminalListener func(tool *ToolState, payload map[string]any)

// ToolEventProjector mirrors toolEvents.ts: tool_use blocks, input deltas,
// results, hook enrichments, and progress become tool_* sidecar events.
type ToolEventProjector struct {
	toolByIndex        map[int]*ToolState
	toolByID           map[string]*ToolState
	toolIDOrder        []string
	toolHookResultByID map[string]map[string]any
	resolveTurnID      func(tool *ToolState) string
	onTerminal         toolTerminalListener
	emit               Emitter
}

func NewToolEventProjector(resolveTurnID func(tool *ToolState) string, onTerminal toolTerminalListener, emit Emitter) *ToolEventProjector {
	return &ToolEventProjector{
		toolByIndex:        map[int]*ToolState{},
		toolByID:           map[string]*ToolState{},
		toolHookResultByID: map[string]map[string]any{},
		resolveTurnID:      resolveTurnID,
		onTerminal:         onTerminal,
		emit:               emit,
	}
}

func (p *ToolEventProjector) Reset() {
	p.toolByIndex = map[int]*ToolState{}
	p.toolByID = map[string]*ToolState{}
	p.toolIDOrder = nil
	p.toolHookResultByID = map[string]map[string]any{}
}

func (p *ToolEventProjector) CompleteIndex(index int) bool {
	tool, ok := p.toolByIndex[index]
	if !ok {
		return false
	}
	p.emitToolEvent("tool_updated", tool, "streaming", nil)
	return true
}

func (p *ToolEventProjector) ParentToolUseID(toolUseID string) string {
	tool, ok := p.toolByID[toolUseID]
	if !ok {
		return ""
	}
	return normalizeTitle(tool.ParentToolUseID)
}

func (p *ToolEventProjector) HasPendingChildResults(parentToolUseID string) bool {
	for _, id := range p.toolIDOrder {
		tool, ok := p.toolByID[id]
		if ok && tool.ParentToolUseID == parentToolUseID {
			return true
		}
	}
	return false
}

func (p *ToolEventProjector) HandleInputDelta(index any, partialJSON any) {
	indexNumber, isNumber := index.(float64)
	partialText, isString := partialJSON.(string)
	if !isNumber && index != nil {
		if intIndex, isInt := index.(int); isInt {
			indexNumber, isNumber = float64(intIndex), true
		}
	}
	if !isNumber || !isString {
		return
	}
	tool, ok := p.toolByIndex[int(indexNumber)]
	if !ok {
		return
	}
	tool.PartialInputJSON += partialText
	if parsed := parseJSONObject(tool.PartialInputJSON); parsed != nil {
		tool.Input = parsed
	}
	p.emitToolEvent("tool_updated", tool, "streaming", nil)
}

func (p *ToolEventProjector) HandleUserContentBlock(block map[string]any, parentToolUseID string) {
	if block["type"] != "tool_result" {
		return
	}
	toolUseID := stringValue(block["tool_use_id"])
	if toolUseID == "" {
		return
	}
	tool, ok := p.toolByID[toolUseID]
	if !ok {
		tool = &ToolState{
			ID:              toolUseID,
			Input:           map[string]any{},
			Started:         true,
			ParentToolUseID: parentToolUseID,
		}
	}
	if parentToolUseID != "" && tool.ParentToolUseID == "" {
		tool.ParentToolUseID = parentToolUseID
	}
	failed := block["is_error"] == true
	var result map[string]any
	if failed {
		result = block
	} else {
		result = mergeToolResult(block, p.toolHookResultByID[toolUseID])
	}
	if failed {
		p.emitToolEvent("tool_failed", tool, "failed", result)
	} else {
		p.emitToolEvent("tool_completed", tool, "completed", result)
	}
	delete(p.toolHookResultByID, toolUseID)
	p.deleteTool(toolUseID)
	for index, indexedTool := range p.toolByIndex {
		if indexedTool.ID == toolUseID {
			delete(p.toolByIndex, index)
		}
	}
}

func (p *ToolEventProjector) HandlePostToolUseHook(input map[string]any, toolUseID string) map[string]any {
	continueResult := map[string]any{"continue": true}
	if input == nil || input["hook_event_name"] != "PostToolUse" {
		return continueResult
	}
	toolID := firstNonEmptyText(
		normalizeTitle(toolUseID),
		stringValue(input["tool_use_id"]),
		stringValue(input["toolUseID"]),
	)
	toolResponse := recordValue(input["tool_response"])
	if toolID == "" || !isDiffToolResponse(toolResponse) {
		return continueResult
	}
	toolName := stringValue(input["tool_name"])
	if toolName == "" {
		if existing, ok := p.toolByID[toolID]; ok {
			toolName = existing.Name
		}
	}
	if toolName == "" {
		toolName = "Claude Code tool"
	}
	if toolName != "Edit" && toolName != "Write" {
		return continueResult
	}
	hookResult := map[string]any{
		"_meta": map[string]any{
			"claudeCode": map[string]any{
				"toolName":     toolName,
				"toolResponse": toolResponse,
			},
		},
		"tool_response": toolResponse,
	}
	p.toolHookResultByID[toolID] = hookResult
	hookTool, ok := p.toolByID[toolID]
	if !ok {
		toolInput := recordValue(input["tool_input"])
		if toolInput == nil {
			toolInput = map[string]any{}
		}
		hookTool = &ToolState{
			ID:              toolID,
			Name:            toolName,
			Input:           toolInput,
			Started:         true,
			ParentToolUseID: stringValue(input["parent_tool_use_id"]),
		}
	}
	p.emitToolEvent("tool_completed", hookTool, "completed", hookResult)
	return continueResult
}

func (p *ToolEventProjector) HandleProgress(message map[string]any, parentToolUseID string) {
	toolUseID := stringValue(message["tool_use_id"])
	if toolUseID == "" {
		return
	}
	tool, ok := p.toolByID[toolUseID]
	if !ok {
		return
	}
	if parentToolUseID != "" && tool.ParentToolUseID == "" {
		tool.ParentToolUseID = parentToolUseID
	}
	result := map[string]any{"progress": message["progress"]}
	if elapsedMS := numberValue(message["elapsed_ms"]); elapsedMS > 0 {
		result["elapsedMs"] = elapsedMS
	}
	p.emitToolEvent("tool_updated", tool, "streaming", result)
}

func (p *ToolEventProjector) Upsert(block map[string]any, index *int, eventType string, parentToolUseID string) {
	id := stringValue(block["id"])
	if id == "" {
		return
	}
	input := recordValue(block["input"])
	if input == nil {
		input = map[string]any{}
	}
	tool, exists := p.toolByID[id]
	if !exists {
		tool = &ToolState{
			ID:              id,
			Name:            stringValue(block["name"]),
			Input:           input,
			ParentToolUseID: parentToolUseID,
		}
	}
	if name := stringValue(block["name"]); name != "" {
		tool.Name = name
	}
	if parentToolUseID != "" && tool.ParentToolUseID == "" {
		tool.ParentToolUseID = parentToolUseID
	}
	if len(input) > 0 {
		tool.Input = input
	}
	p.storeTool(id, tool)
	if index != nil {
		p.toolByIndex[*index] = tool
	}
	if !tool.Started || eventType == "tool_started" {
		tool.Started = true
		p.emitToolEvent("tool_started", tool, "streaming", nil)
		return
	}
	p.emitToolEvent("tool_updated", tool, "streaming", nil)
}

func (p *ToolEventProjector) storeTool(id string, tool *ToolState) {
	if _, exists := p.toolByID[id]; !exists {
		p.toolIDOrder = append(p.toolIDOrder, id)
	}
	p.toolByID[id] = tool
}

func (p *ToolEventProjector) deleteTool(id string) {
	if _, exists := p.toolByID[id]; !exists {
		return
	}
	delete(p.toolByID, id)
	for index, candidate := range p.toolIDOrder {
		if candidate == id {
			p.toolIDOrder = append(p.toolIDOrder[:index], p.toolIDOrder[index+1:]...)
			break
		}
	}
}

func (p *ToolEventProjector) emitToolEvent(eventType string, tool *ToolState, status string, result map[string]any) {
	payload := toolPayload(p.resolveTurnID(tool), tool, status, result)
	if eventType == "tool_completed" || eventType == "tool_failed" {
		p.appendParentTaskStep(tool, payload)
		p.onTerminal(tool, payload)
	}
	p.emit(eventType, "", payload)
}

func (p *ToolEventProjector) appendParentTaskStep(tool *ToolState, payload map[string]any) {
	parentToolUseID := normalizeTitle(tool.ParentToolUseID)
	if parentToolUseID == "" {
		return
	}
	parent, ok := p.toolByID[parentToolUseID]
	if !ok {
		return
	}
	step := taskStepFromToolPayload(payload)
	stepID := firstNonEmptyText(stringValue(step["toolUseId"]), stringValue(step["id"]))
	replaced := false
	for index, candidate := range parent.Steps {
		if candidate == nil || stepID == "" {
			continue
		}
		if stringValue(candidate["toolUseId"]) == stepID || stringValue(candidate["id"]) == stepID {
			parent.Steps[index] = step
			replaced = true
			break
		}
	}
	if !replaced {
		parent.Steps = append(parent.Steps, step)
	}
}
