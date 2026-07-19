package claudesidecar

// DelegatedTaskState mirrors toolActivityTypes.ts.
type DelegatedTaskState struct {
	ParentToolUseID string
	TurnID          string
	Input           map[string]any
	AgentID         string
	OutputFile      string
	TaskID          string
	Subject         string
	Description     string
	Status          string // running, completed, failed, stopped
	// ParentTaskToolUseID is the tool use id of the delegated task that
	// launched this one, set when a nested agent launch is observed inside a
	// child stream.
	ParentTaskToolUseID string
}

// ToolActivityProjector mirrors toolActivity.ts: it owns tool events,
// delegated background tasks, and the plan tracker.
type ToolActivityProjector struct {
	tools                         *ToolEventProjector
	taskPlan                      *taskPlanTracker
	delegatedTasksByParentToolUse map[string]*DelegatedTaskState
	delegatedTaskOrder            []string
	delegatedParentByAgentID      map[string]string
	delegatedParentByTaskID       map[string]string
	activeTurnID                  func() string
	emit                          Emitter
	onFinalDelegatedTaskSettling  func()
	lastTurnID                    func() string
}

func NewToolActivityProjector(
	activeTurnID func() string,
	emit Emitter,
	onFinalDelegatedTaskSettling func(),
	lastTurnID func() string,
) *ToolActivityProjector {
	if onFinalDelegatedTaskSettling == nil {
		onFinalDelegatedTaskSettling = func() {}
	}
	if lastTurnID == nil {
		lastTurnID = activeTurnID
	}
	projector := &ToolActivityProjector{
		delegatedTasksByParentToolUse: map[string]*DelegatedTaskState{},
		delegatedParentByAgentID:      map[string]string{},
		delegatedParentByTaskID:       map[string]string{},
		activeTurnID:                  activeTurnID,
		emit:                          emit,
		onFinalDelegatedTaskSettling:  onFinalDelegatedTaskSettling,
		lastTurnID:                    lastTurnID,
	}
	projector.taskPlan = newTaskPlanTracker(activeTurnID, emit)
	projector.tools = NewToolEventProjector(
		projector.resolveToolEventTurnID,
		projector.rememberDelegatedTaskFromToolPayload,
		emit,
	)
	return projector
}

func (p *ToolActivityProjector) ResetTurnScratch() {
	p.tools.Reset()
	p.taskPlan.reset()
}

func (p *ToolActivityProjector) CompleteToolIndex(index int) bool {
	return p.tools.CompleteIndex(index)
}

func (p *ToolActivityProjector) ResolveInteractiveTurnID(toolUseID string) string {
	parentToolUseID := p.tools.ParentToolUseID(toolUseID)
	if parentToolUseID == "" {
		return ""
	}
	if task, ok := p.delegatedTasksByParentToolUse[parentToolUseID]; ok {
		return task.TurnID
	}
	return ""
}

func (p *ToolActivityProjector) RunningDelegatedTurnID() string {
	for _, key := range p.delegatedTaskOrder {
		task := p.delegatedTasksByParentToolUse[key]
		if task != nil && task.Status == "running" && task.TurnID != "" {
			return task.TurnID
		}
	}
	return ""
}

func (p *ToolActivityProjector) LatestDelegatedTurnID() string {
	latest := ""
	for _, key := range p.delegatedTaskOrder {
		task := p.delegatedTasksByParentToolUse[key]
		if task != nil && task.TurnID != "" {
			latest = task.TurnID
		}
	}
	return latest
}

func (p *ToolActivityProjector) ResolveDelegatedTaskIDForStop(taskID string, parentToolUseID string) string {
	// Callers may only hold one identifier, and the daemon's child key can be
	// the SDK task id, the agent id, or the launching tool use id; try all.
	parentByAlias := ""
	if taskID != "" {
		parentByAlias = p.delegatedParentByAlias(taskID, taskID)
	}
	var task *DelegatedTaskState
	if parentByAlias != "" {
		task = p.delegatedTasksByParentToolUse[parentByAlias]
	} else {
		key := parentToolUseID
		if key == "" {
			key = taskID
		}
		task = p.delegatedTasksByParentToolUse[key]
	}
	if task != nil && task.Status == "running" {
		return task.TaskID
	}
	return ""
}

func (p *ToolActivityProjector) HandleTaskNotificationFromText(text string) {
	parsed := parseTaskNotification(text)
	if parsed == nil {
		return
	}
	p.HandleTaskSystemMessage("task_notification", taskNotificationToSystemMessage(parsed))
}

func (p *ToolActivityProjector) HandleTaskSystemMessage(subtype string, message map[string]any) {
	// task_notification is included so a terminal notice for a task launched
	// before a sidecar restart (reported e.g. as stopped on the next prompt)
	// still settles a card instead of being dropped by the fresh instance.
	task := p.resolveDelegatedTaskFromMessage(message, true)
	if task == nil && (subtype == "task_started" || subtype == "task_notification") {
		task = p.rememberBackgroundTask(message)
	}
	if task == nil {
		return
	}
	taskID := firstNonEmptyText(stringValue(message["task_id"]), stringValue(message["taskId"]))
	if taskID != "" && task.TaskID == "" {
		task.TaskID = taskID
		p.delegatedParentByTaskID[taskID] = task.ParentToolUseID
	}
	description := firstNonEmptyText(stringValue(message["description"]), stringValue(message["summary"]))
	if description != "" && task.Description == "" {
		task.Description = description
	}
	if subtype == "task_notification" {
		if task.Status != "running" {
			return
		}
		status := delegatedTaskStatus(message["status"])
		// A user-initiated stop needs no model follow-up, so it must not open a
		// synthetic continuation turn (which would time out and interrupt).
		if status != "stopped" {
			p.prepareDelegatedTaskTerminal(task)
		}
		task.Status = status
		p.emitDelegatedTaskLifecycleEvent("task_completed", task, message)
		p.emitDelegatedTaskParentUpdate(task, message)
		return
	}
	if subtype == "task_progress" && task.Status != "running" {
		// A trailing progress event delivered after the task's own completion
		// must not resurrect the task and bump the running count; only an
		// explicit task_started may restart a settled task.
		return
	}
	task.Status = "running"
	eventType := "task_progress"
	if subtype == "task_started" {
		eventType = "task_started"
	}
	p.emitDelegatedTaskLifecycleEvent(eventType, task, message)
}

func (p *ToolActivityProjector) HandleToolInputDelta(index any, partialJSON any) {
	p.tools.HandleInputDelta(index, partialJSON)
}

func (p *ToolActivityProjector) HandleUserContentBlock(block map[string]any, parentToolUseID string) {
	p.tools.HandleUserContentBlock(block, parentToolUseID)
}

func (p *ToolActivityProjector) HandlePostToolUseHook(input map[string]any, toolUseID string) map[string]any {
	return p.tools.HandlePostToolUseHook(input, toolUseID)
}

func (p *ToolActivityProjector) HandleTaskLifecycleHook(input map[string]any) map[string]any {
	continueResult := map[string]any{"continue": true}
	if input == nil {
		return continueResult
	}
	taskID := stringValue(input["task_id"])
	if taskID == "" {
		return continueResult
	}
	if input["hook_event_name"] == "TaskCreated" {
		subject := stringValue(input["task_subject"])
		if !p.taskPlan.create(taskID, subject, stringValue(input["task_description"])) {
			return continueResult
		}
		p.bindDelegatedTaskIDFromHook(taskID, input)
		return continueResult
	}
	if input["hook_event_name"] == "TaskCompleted" {
		p.bindDelegatedTaskIDFromHook(taskID, input)
		p.emitDelegatedTaskCompletedFromHook(input)
		p.taskPlan.complete(taskID)
	}
	return continueResult
}

func (p *ToolActivityProjector) bindDelegatedTaskIDFromHook(taskID string, hookInput map[string]any) {
	task := p.resolveDelegatedTaskFromMessage(hookInput, false)
	if task == nil || task.TaskID != "" {
		return
	}
	task.TaskID = taskID
	p.delegatedParentByTaskID[taskID] = task.ParentToolUseID
	task.Subject = firstNonEmptyText(stringValue(hookInput["task_subject"]), task.Subject)
	task.Description = firstNonEmptyText(stringValue(hookInput["task_description"]), task.Description)
}

func (p *ToolActivityProjector) emitDelegatedTaskCompletedFromHook(hookInput map[string]any) {
	task := p.resolveDelegatedTaskFromMessage(hookInput, true)
	if task == nil {
		return
	}
	status := delegatedTaskStatus(hookInput["status"])
	if status != "stopped" {
		p.prepareDelegatedTaskTerminal(task)
	}
	taskID := firstNonEmptyText(stringValue(hookInput["task_id"]), task.TaskID)
	if taskID != "" && task.TaskID == "" {
		task.TaskID = taskID
		p.delegatedParentByTaskID[taskID] = task.ParentToolUseID
	}
	task.Status = status
	task.Subject = firstNonEmptyText(stringValue(hookInput["task_subject"]), task.Subject)
	task.Description = firstNonEmptyText(stringValue(hookInput["task_description"]), task.Description)
	summary := firstNonEmptyText(
		stringValue(hookInput["summary"]),
		stringValue(hookInput["task_summary"]),
		stringValue(hookInput["task_result"]),
		task.Description,
		task.Subject,
	)
	message := cloneRecord(hookInput)
	message["task_id"] = task.TaskID
	message["taskId"] = task.TaskID
	message["status"] = task.Status
	if task.Description != "" {
		message["description"] = task.Description
	}
	if summary != "" {
		message["summary"] = summary
	}
	p.emitDelegatedTaskLifecycleEvent("task_completed", task, message)
	p.emitDelegatedTaskParentUpdate(task, message)
}

func (p *ToolActivityProjector) CompleteDelegatedTaskFromResultMessage(parentToolUseID string, message map[string]any) {
	summary := firstNonEmptyText(stringValue(message["summary"]), stringValue(message["result"]))
	completion := cloneRecord(message)
	if summary != "" {
		completion["summary"] = summary
	}
	statusSource := message["subtype"]
	if statusSource == nil {
		statusSource = message["status"]
	}
	completion["status"] = delegatedTaskStatus(statusSource)
	p.CompleteDelegatedTaskFromParentMessage(parentToolUseID, completion)
}

func (p *ToolActivityProjector) CompleteDelegatedTaskFromParentMessage(parentToolUseID string, message map[string]any) {
	resolved := cloneRecord(message)
	if resolved == nil {
		resolved = map[string]any{}
	}
	resolved["parentToolUseId"] = parentToolUseID
	task := p.resolveDelegatedTaskFromMessage(resolved, false)
	if task == nil || task.Status != "running" {
		return
	}
	status := delegatedTaskStatus(message["status"])
	if status != "stopped" {
		p.prepareDelegatedTaskTerminal(task)
	}
	task.Status = status
	p.emitDelegatedTaskLifecycleEvent("task_completed", task, message)
	p.emitDelegatedTaskParentUpdate(task, message)
}

func (*ToolActivityProjector) IsNestedDelegatedTaskTerminalAssistant(message map[string]any) bool {
	nested := recordValue(message["message"])
	stopReason := firstNonEmptyText(stringValue(nested["stop_reason"]), stringValue(message["stop_reason"]))
	if stopReason != "end_turn" {
		return false
	}
	for _, block := range contentBlocksFromMessage(message) {
		if block["type"] == "text" && stringValue(block["text"]) != "" {
			return true
		}
	}
	return false
}

func (*ToolActivityProjector) ExtractAssistantTextFromMessage(message map[string]any) string {
	var parts []string
	for _, block := range contentBlocksFromMessage(message) {
		if block["type"] != "text" {
			continue
		}
		if text := stringValue(block["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	result := ""
	for index, part := range parts {
		if index > 0 {
			result += "\n"
		}
		result += part
	}
	return normalizeTitleKeepingNewlines(result)
}

func normalizeTitleKeepingNewlines(value string) string {
	// TS uses String.prototype.trim() here; keep interior whitespace intact.
	start := 0
	end := len(value)
	for start < end && isJSWhitespace(value[start]) {
		start++
	}
	for end > start && isJSWhitespace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func isJSWhitespace(char byte) bool {
	switch char {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func (p *ToolActivityProjector) HandleToolProgress(message map[string]any, parentToolUseID string) {
	p.tools.HandleProgress(message, parentToolUseID)
}

func (p *ToolActivityProjector) UpsertToolUse(block map[string]any, index *int, eventType string, parentToolUseID string) {
	p.tools.Upsert(block, index, eventType, parentToolUseID)
}

func (p *ToolActivityProjector) HasUnsettledChildWork(parentToolUseID string) bool {
	return p.tools.HasPendingChildResults(parentToolUseID) || p.hasRunningChildDelegatedTasks(parentToolUseID)
}

func (p *ToolActivityProjector) resolveToolEventTurnID(tool *ToolState) string {
	if active := p.activeTurnID(); active != "" {
		return active
	}
	// Child-stream tool events can arrive after the launching turn settled;
	// attribute them to the turn of the delegated task they belong to.
	parentToolUseID := normalizeTitle(tool.ParentToolUseID)
	if parentToolUseID != "" {
		if task, ok := p.delegatedTasksByParentToolUse[parentToolUseID]; ok && task.TurnID != "" {
			return task.TurnID
		}
	}
	return ""
}

func (p *ToolActivityProjector) rememberDelegatedTaskFromToolPayload(tool *ToolState, payload map[string]any) {
	metadata := recordValue(payload["metadata"])
	// The launch result text sets subagentAsync; nested launches may stream
	// without a locally known tool name, so callType alone cannot gate here.
	if metadata == nil || metadata["subagentAsync"] != true {
		return
	}
	parentToolUseID := firstNonEmptyText(stringValue(payload["toolCallId"]), tool.ID)
	if parentToolUseID == "" {
		return
	}
	agentID := firstNonEmptyText(stringValue(metadata["subagentAgentId"]), stringValue(metadata["agentId"]))
	outputFile := firstNonEmptyText(stringValue(metadata["subagentOutputFile"]), stringValue(metadata["outputFile"]))
	launchingTask := p.delegatedTasksByParentToolUse[normalizeTitle(tool.ParentToolUseID)]
	turnID := firstNonEmptyText(stringValue(payload["turnId"]), p.activeTurnID())
	if turnID == "" && launchingTask != nil {
		turnID = launchingTask.TurnID
	}
	input := recordValue(payload["input"])
	if input == nil {
		input = cloneRecord(tool.Input)
	}
	task := &DelegatedTaskState{
		ParentToolUseID: parentToolUseID,
		TurnID:          turnID,
		Input:           input,
		AgentID:         agentID,
		OutputFile:      outputFile,
		Status:          "running",
	}
	if launchingTask != nil {
		task.ParentTaskToolUseID = launchingTask.ParentToolUseID
	}
	p.storeDelegatedTask(parentToolUseID, task)
	if agentID != "" {
		p.delegatedParentByAgentID[agentID] = parentToolUseID
	}
}

// rememberBackgroundTask puts background launches the launch-result path does
// not register (most importantly `run_in_background` Bash) on the same
// delegated-task rails as background subagents, so they gate the root turn,
// show up as child sessions, and can be stopped individually — instead of
// running invisibly after the turn settled.
func (p *ToolActivityProjector) rememberBackgroundTask(message map[string]any) *DelegatedTaskState {
	parentToolUseID := firstNonEmptyText(stringValue(message["tool_use_id"]), stringValue(message["toolCallId"]))
	taskID := firstNonEmptyText(stringValue(message["task_id"]), stringValue(message["taskId"]))
	if parentToolUseID == "" || taskID == "" {
		// Without a launching tool use id this could be a racing alias for a
		// not-yet-registered subagent launch; binding it here would poison the
		// alias maps, so leave it for the launch result to register.
		return nil
	}
	turnID := p.activeTurnID()
	if turnID == "" {
		// Background task_started frequently arrives after the launching
		// provider turn already settled; fall back to that turn so lifecycle
		// events are not dropped for lack of a turn id.
		turnID = p.lastTurnID()
	}
	task := &DelegatedTaskState{
		ParentToolUseID: parentToolUseID,
		TurnID:          turnID,
		Input:           map[string]any{},
		TaskID:          taskID,
		Status:          "running",
	}
	p.storeDelegatedTask(parentToolUseID, task)
	p.delegatedParentByTaskID[taskID] = parentToolUseID
	return task
}

func (p *ToolActivityProjector) storeDelegatedTask(parentToolUseID string, task *DelegatedTaskState) {
	if _, exists := p.delegatedTasksByParentToolUse[parentToolUseID]; !exists {
		p.delegatedTaskOrder = append(p.delegatedTaskOrder, parentToolUseID)
	}
	p.delegatedTasksByParentToolUse[parentToolUseID] = task
}

func (p *ToolActivityProjector) resolveDelegatedTaskFromMessage(message map[string]any, allowRunningFallback bool) *DelegatedTaskState {
	taskID := firstNonEmptyText(stringValue(message["task_id"]), stringValue(message["taskId"]))
	agentID := firstNonEmptyText(
		stringValue(message["agentId"]),
		stringValue(message["agent_id"]),
		stringValue(message["agentID"]),
	)
	explicitParentToolUseID := firstNonEmptyText(
		stringValue(message["parentToolUseId"]),
		stringValue(message["parent_tool_use_id"]),
		stringValue(message["tool_use_id"]),
		stringValue(message["toolCallId"]),
		stringValue(message["callId"]),
	)
	if explicitParentToolUseID != "" {
		return p.delegatedTasksByParentToolUse[explicitParentToolUseID]
	}
	if parentToolUseID := p.delegatedParentByAlias(taskID, agentID); parentToolUseID != "" {
		return p.delegatedTasksByParentToolUse[parentToolUseID]
	}
	if !allowRunningFallback {
		return nil
	}
	if (taskID != "" || agentID != "") && p.hasDelegatedTaskAliases() {
		// An unresolved task/agent id usually belongs to a delegated task whose
		// launch has not been observed yet. Binding it to "the only running"
		// task would poison the alias maps for concurrent launches, so drop the
		// event and let a later resolvable event settle that task.
		return nil
	}
	var activeTasks []*DelegatedTaskState
	for _, key := range p.delegatedTaskOrder {
		task := p.delegatedTasksByParentToolUse[key]
		if task != nil && task.TurnID == p.activeTurnID() && task.Status == "running" {
			activeTasks = append(activeTasks, task)
		}
	}
	if len(activeTasks) == 1 {
		return activeTasks[0]
	}
	var allRunning []*DelegatedTaskState
	for _, key := range p.delegatedTaskOrder {
		task := p.delegatedTasksByParentToolUse[key]
		if task != nil && task.Status == "running" {
			allRunning = append(allRunning, task)
		}
	}
	if len(allRunning) == 1 {
		return allRunning[0]
	}
	return nil
}

func (p *ToolActivityProjector) delegatedParentByAlias(taskID string, agentID string) string {
	// Claude Code hooks and task notifications frequently carry the agent id
	// in task_id, so each alias is matched against both maps.
	for _, alias := range []string{taskID, agentID} {
		if alias == "" {
			continue
		}
		if parent := p.delegatedParentByTaskID[alias]; parent != "" {
			return parent
		}
		if parent := p.delegatedParentByAgentID[alias]; parent != "" {
			return parent
		}
	}
	return ""
}

func (p *ToolActivityProjector) hasDelegatedTaskAliases() bool {
	for _, task := range p.delegatedTasksByParentToolUse {
		if task.AgentID != "" || task.TaskID != "" {
			return true
		}
	}
	return false
}

func (p *ToolActivityProjector) hasRunningChildDelegatedTasks(parentToolUseID string) bool {
	for _, task := range p.delegatedTasksByParentToolUse {
		if task.ParentTaskToolUseID == parentToolUseID && task.Status == "running" {
			return true
		}
	}
	return false
}

func (p *ToolActivityProjector) prepareDelegatedTaskTerminal(task *DelegatedTaskState) {
	if task.Status != "running" {
		return
	}
	var running []*DelegatedTaskState
	for _, key := range p.delegatedTaskOrder {
		candidate := p.delegatedTasksByParentToolUse[key]
		if candidate != nil && candidate.Status == "running" {
			running = append(running, candidate)
		}
	}
	if len(running) == 1 && running[0] == task {
		p.onFinalDelegatedTaskSettling()
	}
}

func (p *ToolActivityProjector) emitDelegatedTaskParentUpdate(task *DelegatedTaskState, message map[string]any) {
	turnID := firstNonEmptyText(task.TurnID, p.activeTurnID())
	if turnID == "" {
		return
	}
	task.TurnID = turnID
	summary := delegatedTaskSummaryFromMessage(message)
	if summary == "" {
		summary = "Subagent task completed."
	}
	usage := recordValue(message["usage"])
	metadata := map[string]any{
		"adapter":        "claude-agent-sdk",
		"toolName":       "Agent",
		"async":          true,
		"subagentAsync":  true,
		"taskStatus":     task.Status,
		"subagentStatus": task.Status,
	}
	if task.TaskID != "" {
		metadata["taskId"] = task.TaskID
	}
	if task.AgentID != "" {
		metadata["agentId"] = task.AgentID
		metadata["subagentAgentId"] = task.AgentID
	}
	if task.OutputFile != "" {
		metadata["outputFile"] = task.OutputFile
		metadata["subagentOutputFile"] = task.OutputFile
	}
	output := map[string]any{"text": summary}
	if usage != nil {
		output["usage"] = usage
	}
	status := "completed"
	eventType := "tool_completed"
	if task.Status == "failed" {
		status = "failed"
		eventType = "tool_failed"
	}
	p.emit(eventType, "", map[string]any{
		"turnId":     turnID,
		"toolCallId": task.ParentToolUseID,
		"callId":     task.ParentToolUseID,
		"toolName":   "Agent",
		"callType":   "subagent",
		"name":       "Agent",
		"status":     status,
		"input":      task.Input,
		"output":     output,
		"content":    []map[string]any{{"type": "tool_result", "text": summary}},
		"metadata":   metadata,
	})
}

func (p *ToolActivityProjector) emitDelegatedTaskLifecycleEvent(eventType string, task *DelegatedTaskState, message map[string]any) {
	turnID := firstNonEmptyText(task.TurnID, p.activeTurnID())
	if turnID == "" {
		return
	}
	task.TurnID = turnID
	taskID := firstNonEmptyText(stringValue(message["task_id"]), stringValue(message["taskId"]))
	if taskID != "" && task.TaskID == "" {
		task.TaskID = taskID
		p.delegatedParentByTaskID[taskID] = task.ParentToolUseID
	}
	description := firstNonEmptyText(
		stringValue(message["description"]),
		stringValue(message["summary"]),
		task.Description,
		task.Subject,
	)
	summary := delegatedTaskSummaryFromMessage(message)
	lastToolName := firstNonEmptyText(stringValue(message["last_tool_name"]), stringValue(message["lastToolName"]))
	usage := recordValue(message["usage"])
	metadata := map[string]any{
		"adapter":         "claude-agent-sdk",
		"parentToolUseId": task.ParentToolUseID,
		"async":           true,
		"subagentAsync":   true,
		"taskStatus":      task.Status,
		"subagentStatus":  task.Status,
	}
	if task.TaskID != "" {
		metadata["taskId"] = task.TaskID
	}
	if task.AgentID != "" {
		metadata["agentId"] = task.AgentID
		metadata["subagentAgentId"] = task.AgentID
	}
	if task.OutputFile != "" {
		metadata["outputFile"] = task.OutputFile
		metadata["subagentOutputFile"] = task.OutputFile
	}
	payload := map[string]any{
		"turnId":          turnID,
		"parentToolUseId": task.ParentToolUseID,
		"toolCallId":      task.ParentToolUseID,
		"callId":          task.ParentToolUseID,
		"status":          task.Status,
		"input":           task.Input,
		"metadata":        metadata,
	}
	if task.TaskID != "" {
		payload["taskId"] = task.TaskID
	}
	if task.AgentID != "" {
		payload["agentId"] = task.AgentID
	}
	if task.OutputFile != "" {
		payload["outputFile"] = task.OutputFile
	}
	if description != "" {
		payload["description"] = description
	}
	if summary != "" {
		payload["summary"] = summary
	}
	if lastToolName != "" {
		payload["lastToolName"] = lastToolName
	}
	if usage != nil {
		payload["usage"] = usage
	}
	p.emit(eventType, "", payload)
}

func delegatedTaskSummaryFromMessage(message map[string]any) string {
	return firstNonEmptyText(
		stringValue(message["result"]),
		stringValue(message["summary"]),
		stringValue(message["description"]),
	)
}

func delegatedTaskStatus(value any) string {
	switch stringValue(value) {
	case "failed", "error":
		return "failed"
	case "stopped", "canceled", "cancelled":
		return "stopped"
	default:
		return "completed"
	}
}
