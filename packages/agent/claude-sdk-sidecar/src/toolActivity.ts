import type { SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import {
  BackgroundTaskLifecycle,
  type DelegatedTaskCounts
} from "./backgroundTaskLifecycle.ts";
import {
  contentBlocksFromMessage,
  recordValue,
  type ToolState
} from "./normalizer.ts";
import type { ClaudeSDKSidecarEventEmitter } from "./protocol.ts";
import { stringValue } from "./runtimeValues.ts";
import {
  parseTaskNotification,
  taskNotificationToSystemMessage
} from "./taskNotification.ts";
import type {
  BackgroundProcessState,
  DelegatedTaskState,
  DelegatedTaskStatus
} from "./toolActivityTypes.ts";
import { ToolEventProjector } from "./toolEvents.ts";
import { TaskPlanTracker } from "./taskPlan.ts";

export class ToolActivityProjector {
  private readonly tools: ToolEventProjector;
  private readonly taskPlan: TaskPlanTracker;
  private readonly backgroundTasks: BackgroundTaskLifecycle;
  private readonly delegatedTasksByParentToolUseID = new Map<
    string,
    DelegatedTaskState
  >();
  private readonly delegatedParentByAgentID = new Map<string, string>();
  private readonly delegatedParentByTaskID = new Map<string, string>();
  private readonly backgroundProcessesByParentToolUseID = new Map<
    string,
    BackgroundProcessState
  >();
  private readonly backgroundProcessParentByTaskID = new Map<string, string>();
  private readonly activeTurnId: () => string;
  private readonly emit: ClaudeSDKSidecarEventEmitter;
  private readonly lastTurnId: () => string;

  constructor(
    activeTurnId: () => string,
    emit: ClaudeSDKSidecarEventEmitter,
    onFinalDelegatedTaskSettling: () => void = () => {},
    lastTurnId: () => string = activeTurnId
  ) {
    this.activeTurnId = activeTurnId;
    this.emit = emit;
    this.lastTurnId = lastTurnId;
    this.taskPlan = new TaskPlanTracker(activeTurnId, emit);
    this.tools = new ToolEventProjector(
      (tool) => this.resolveToolEventTurnId(tool),
      (tool, payload) =>
        this.rememberDelegatedTaskFromToolPayload(tool, payload),
      emit
    );
    this.backgroundTasks = new BackgroundTaskLifecycle({
      activeTurnId,
      lastTurnId,
      delegatedTaskCounts: (turnId) => this.delegatedTaskCounts(turnId),
      emit,
      onContinuationPending: onFinalDelegatedTaskSettling
    });
  }

  resetTurnScratch(): void {
    this.tools.reset();
    this.taskPlan.reset();
  }

  resetBackgroundTaskLevel(): void {
    this.backgroundTasks.reset();
  }

  handleBackgroundTasksChanged(message: Record<string, unknown>): void {
    this.backgroundTasks.handleLevelChanged(message);
  }

  beginRootTurn(): void {
    this.backgroundTasks.beginRootTurn();
  }

  cancel(): void {
    this.backgroundTasks.cancel();
  }

  close(): void {
    this.backgroundTasks.close();
  }

  hasPendingBackgroundContinuation(): boolean {
    return this.backgroundTasks.hasPendingContinuation();
  }

  markTaskNotificationContinuation(): void {
    this.backgroundTasks.markTaskNotificationContinuation();
  }

  clearBackgroundContinuation(): boolean {
    return this.backgroundTasks.clearContinuation();
  }

  handleRootResultSettled(succeeded: boolean): void {
    this.backgroundTasks.handleRootResultSettled(succeeded);
  }

  completePendingRootBackgroundLaunches(): void {
    this.tools.completePendingRootBackgroundLaunches();
  }

  completeToolIndex(index: number): boolean {
    return this.tools.completeIndex(index);
  }

  resolveInteractiveTurnId(toolUseID: string): string {
    const parentToolUseID = this.tools.parentToolUseID(toolUseID);
    return parentToolUseID
      ? (this.delegatedTasksByParentToolUseID.get(parentToolUseID)?.turnId ??
          "")
      : "";
  }

  runningDelegatedTurnId(): string {
    for (const task of this.delegatedTasksByParentToolUseID.values()) {
      if (task.status === "running" && task.turnId) {
        return task.turnId;
      }
    }
    return "";
  }

  latestDelegatedTurnId(): string {
    let latest = "";
    for (const task of this.delegatedTasksByParentToolUseID.values()) {
      if (task.turnId) {
        latest = task.turnId;
      }
    }
    return latest;
  }

  resolveDelegatedTaskIdForStop(
    taskId: string,
    parentToolUseId: string
  ): string {
    // Callers may only hold one identifier, and the daemon's child key can be
    // the SDK task id, the agent id, or the launching tool use id; try all.
    const parentByAlias = taskId
      ? this.delegatedParentByAlias(taskId, taskId)
      : "";
    const task = parentByAlias
      ? this.delegatedTasksByParentToolUseID.get(parentByAlias)
      : this.delegatedTasksByParentToolUseID.get(parentToolUseId || taskId);
    if (task?.status === "running") {
      return task.taskId ?? "";
    }
    const backgroundParent =
      this.backgroundProcessParentByTaskID.get(taskId) ||
      parentToolUseId ||
      taskId;
    const background =
      this.backgroundProcessesByParentToolUseID.get(backgroundParent);
    return background?.status === "running" ? background.taskId : "";
  }

  handleTaskNotificationFromText(text: string): void {
    const parsed = parseTaskNotification(text);
    if (!parsed) {
      return;
    }
    this.handleTaskSystemMessage(
      "task_notification",
      taskNotificationToSystemMessage(parsed)
    );
  }

  handleTaskSystemMessage(
    subtype:
      | "task_started"
      | "task_progress"
      | "task_updated"
      | "task_notification",
    message: Record<string, unknown>
  ): void {
    const task =
      this.resolveDelegatedTaskFromMessage(message) ??
      ((subtype === "task_started" ||
        subtype === "task_updated" ||
        subtype === "task_notification") &&
      isExplicitSubagentTaskMessage(message)
        ? this.rememberUnclaimedDelegatedTask(message)
        : undefined);
    if (!task) {
      this.handleBackgroundProcessTaskMessage(subtype, message);
      return;
    }
    const taskId = stringValue(message.task_id) || stringValue(message.taskId);
    if (taskId && !task.taskId) {
      task.taskId = taskId;
      this.delegatedParentByTaskID.set(taskId, task.parentToolUseId);
    }
    const description =
      stringValue(message.description) || stringValue(message.summary);
    if (description && !task.description) {
      task.description = description;
    }
    if (subtype === "task_updated" || subtype === "task_notification") {
      const status = delegatedTaskStatus(message.status);
      if (task.status !== "running") {
        if (subtype !== "task_notification") {
          return;
        }
        if (status !== "stopped") {
          this.backgroundTasks.markTaskNotificationContinuation();
        }
        this.emitDelegatedTaskLifecycleEvent(
          "task_result_updated",
          task,
          message
        );
        return;
      }
      // A user-initiated stop needs no model follow-up, so it must not open a
      // synthetic continuation turn (which would time out and interrupt).
      // task_updated is only the SDK's lifecycle patch. The later
      // task_notification carries the model continuation and often a richer
      // result, so only that notification marks a pending follow-up.
      if (subtype === "task_notification" && status !== "stopped") {
        this.backgroundTasks.markTaskNotificationContinuation();
      }
      if (status !== "stopped") {
        this.prepareDelegatedTaskTerminal(task);
      }
      task.status = status;
      this.emitDelegatedTaskLifecycleEvent("task_completed", task, message);
      this.emitDelegatedTaskParentUpdate(task, message);
      return;
    }
    if (subtype === "task_progress" && task.status !== "running") {
      // A trailing progress event delivered after the task's own completion
      // must not resurrect the task and bump the running count; only an
      // explicit task_started may restart a settled task.
      return;
    }
    task.status = "running";
    this.emitDelegatedTaskLifecycleEvent(
      subtype === "task_started" ? "task_started" : "task_progress",
      task,
      message
    );
  }

  handleToolInputDelta(index: unknown, partialJSON: unknown): void {
    this.tools.handleInputDelta(index, partialJSON);
  }

  handleUserContentBlock(
    block: Record<string, unknown>,
    parentToolUseID = ""
  ): void {
    this.tools.handleUserContentBlock(block, parentToolUseID);
  }

  async handlePostToolUseHook(
    input: unknown,
    toolUseID?: string
  ): Promise<{ continue: boolean }> {
    return this.tools.handlePostToolUseHook(input, toolUseID);
  }

  async handleTaskLifecycleHook(
    input: unknown
  ): Promise<{ continue: boolean }> {
    const hookInput = recordValue(input);
    if (!hookInput) {
      return { continue: true };
    }
    const taskID = stringValue(hookInput.task_id);
    if (!taskID) {
      return { continue: true };
    }
    if (hookInput.hook_event_name === "TaskCreated") {
      const subject = stringValue(hookInput.task_subject);
      if (
        !this.taskPlan.create(
          taskID,
          subject,
          stringValue(hookInput.task_description)
        )
      ) {
        return { continue: true };
      }
      this.bindDelegatedTaskIDFromHook(taskID, hookInput);
      return { continue: true };
    }
    if (hookInput.hook_event_name === "TaskCompleted") {
      this.bindDelegatedTaskIDFromHook(taskID, hookInput);
      this.emitDelegatedTaskCompletedFromHook(hookInput);
      this.taskPlan.complete(taskID);
    }
    return { continue: true };
  }

  private bindDelegatedTaskIDFromHook(
    taskID: string,
    hookInput: Record<string, unknown>
  ): void {
    const task = this.resolveDelegatedTaskFromMessage(hookInput);
    if (!task || task.taskId) {
      return;
    }
    task.taskId = taskID;
    this.delegatedParentByTaskID.set(taskID, task.parentToolUseId);
    task.subject = stringValue(hookInput.task_subject) || task.subject;
    task.description =
      stringValue(hookInput.task_description) || task.description;
  }

  private emitDelegatedTaskCompletedFromHook(
    hookInput: Record<string, unknown>
  ): void {
    const task = this.resolveDelegatedTaskFromMessage(hookInput);
    if (!task) {
      return;
    }
    const status = delegatedTaskStatus(hookInput.status);
    if (status !== "stopped") {
      this.prepareDelegatedTaskTerminal(task);
    }
    const taskId = stringValue(hookInput.task_id) || task.taskId;
    if (taskId && !task.taskId) {
      task.taskId = taskId;
      this.delegatedParentByTaskID.set(taskId, task.parentToolUseId);
    }
    task.status = status;
    task.subject = stringValue(hookInput.task_subject) || task.subject;
    task.description =
      stringValue(hookInput.task_description) || task.description;
    const summary =
      stringValue(hookInput.summary) ||
      stringValue(hookInput.task_summary) ||
      stringValue(hookInput.task_result) ||
      task.description ||
      task.subject;
    const message: Record<string, unknown> = {
      ...hookInput,
      task_id: task.taskId,
      taskId: task.taskId,
      status: task.status,
      ...(task.description ? { description: task.description } : {}),
      ...(summary ? { summary } : {})
    };
    this.emitDelegatedTaskLifecycleEvent("task_completed", task, message);
    this.emitDelegatedTaskParentUpdate(task, message);
  }

  completeDelegatedTaskFromResultMessage(
    parentToolUseID: string,
    message: SDKMessage
  ): void {
    const result = message as Record<string, unknown>;
    const summary = stringValue(result.summary) || stringValue(result.result);
    this.completeDelegatedTaskFromParentMessage(parentToolUseID, {
      ...result,
      ...(summary ? { summary } : {}),
      status: delegatedTaskStatus(result.subtype ?? result.status)
    });
  }

  completeDelegatedTaskFromParentMessage(
    parentToolUseID: string,
    message: Record<string, unknown>
  ): void {
    const task = this.resolveDelegatedTaskFromMessage({
      ...message,
      parentToolUseId: parentToolUseID
    });
    if (!task || task.status !== "running") {
      return;
    }
    const status = delegatedTaskStatus(message.status);
    if (status !== "stopped") {
      this.prepareDelegatedTaskTerminal(task);
    }
    task.status = status;
    this.emitDelegatedTaskLifecycleEvent("task_completed", task, message);
    this.emitDelegatedTaskParentUpdate(task, message);
  }

  isNestedDelegatedTaskTerminalAssistant(message: SDKMessage): boolean {
    const nested = recordValue((message as { message?: unknown }).message);
    const stopReason =
      stringValue(nested?.stop_reason) ||
      stringValue((message as Record<string, unknown>).stop_reason);
    if (stopReason !== "end_turn") {
      return false;
    }
    return contentBlocksFromMessage(message).some(
      (block) =>
        block.type === "text" && Boolean(stringValue(block.text)?.trim())
    );
  }

  extractAssistantTextFromMessage(message: SDKMessage): string {
    return contentBlocksFromMessage(message)
      .flatMap((block) =>
        block.type === "text" && stringValue(block.text)
          ? [stringValue(block.text) as string]
          : []
      )
      .join("\n")
      .trim();
  }

  handleToolProgress(
    message: Record<string, unknown>,
    parentToolUseID = ""
  ): void {
    this.tools.handleProgress(message, parentToolUseID);
  }

  upsertToolUse(
    block: Record<string, unknown>,
    index: number | undefined,
    eventType: "tool_started" | "tool_updated",
    parentToolUseID = ""
  ): void {
    this.tools.upsert(block, index, eventType, parentToolUseID);
  }

  private resolveToolEventTurnId(tool: ToolState): string {
    if (this.activeTurnId()) {
      return this.activeTurnId();
    }
    // Child-stream tool events can arrive after the launching turn settled;
    // attribute them to the turn of the delegated task they belong to.
    const parentToolUseID = stringValue(tool.parentToolUseId);
    if (parentToolUseID) {
      const task = this.delegatedTasksByParentToolUseID.get(parentToolUseID);
      if (task?.turnId) {
        return task.turnId;
      }
    }
    return "";
  }

  private rememberDelegatedTaskFromToolPayload(
    tool: ToolState,
    payload: Record<string, unknown>
  ): void {
    const metadata = recordValue(payload.metadata);
    // The launch result text sets subagentAsync; nested launches may stream
    // without a locally known tool name, so callType alone cannot gate here.
    if (metadata?.subagentAsync !== true) {
      return;
    }
    const parentToolUseId = stringValue(payload.toolCallId) || tool.id;
    if (!parentToolUseId) {
      return;
    }
    const agentId =
      stringValue(metadata.subagentAgentId) || stringValue(metadata.agentId);
    const outputFile =
      stringValue(metadata.subagentOutputFile) ||
      stringValue(metadata.outputFile);
    const launchingTask = this.delegatedTasksByParentToolUseID.get(
      stringValue(tool.parentToolUseId)
    );
    const task: DelegatedTaskState = {
      parentToolUseId,
      turnId:
        stringValue(payload.turnId) ||
        this.activeTurnId() ||
        launchingTask?.turnId ||
        "",
      input: recordValue(payload.input) ?? { ...tool.input },
      ...(agentId ? { agentId } : {}),
      ...(outputFile ? { outputFile } : {}),
      status: "running",
      ...(launchingTask
        ? { parentTaskToolUseId: launchingTask.parentToolUseId }
        : {})
    };
    this.delegatedTasksByParentToolUseID.set(parentToolUseId, task);
    if (agentId) {
      this.delegatedParentByAgentID.set(agentId, parentToolUseId);
    }
  }

  // A sidecar restarted between a subagent launch and its terminal notice may
  // receive an explicit agent task before rebuilding its alias maps. Only
  // provider-declared agent work may recreate delegated state here; an
  // unclaimed task-system event can also represent a detached Bash process.
  private rememberUnclaimedDelegatedTask(
    message: Record<string, unknown>
  ): DelegatedTaskState | undefined {
    const parentToolUseId =
      stringValue(message.tool_use_id) || stringValue(message.toolCallId);
    const taskId = stringValue(message.task_id) || stringValue(message.taskId);
    if (!parentToolUseId || !taskId) {
      return undefined;
    }
    const task: DelegatedTaskState = {
      parentToolUseId,
      turnId: this.activeTurnId() || this.lastTurnId(),
      input: {},
      taskId,
      status: "running"
    };
    this.delegatedTasksByParentToolUseID.set(parentToolUseId, task);
    this.delegatedParentByTaskID.set(taskId, parentToolUseId);
    return task;
  }

  private handleBackgroundProcessTaskMessage(
    subtype:
      | "task_started"
      | "task_progress"
      | "task_updated"
      | "task_notification",
    message: Record<string, unknown>
  ): void {
    const parentToolUseId =
      stringValue(message.parentToolUseId) ||
      stringValue(message.parent_tool_use_id) ||
      stringValue(message.tool_use_id) ||
      stringValue(message.toolCallId) ||
      stringValue(message.callId);
    const taskId = stringValue(message.task_id) || stringValue(message.taskId);
    const knownParent =
      this.backgroundProcessParentByTaskID.get(taskId) || parentToolUseId;
    let process = knownParent
      ? this.backgroundProcessesByParentToolUseID.get(knownParent)
      : undefined;
    if (!process && subtype === "task_started" && parentToolUseId && taskId) {
      const turnId = this.activeTurnId() || this.lastTurnId();
      if (!turnId || message.skip_transcript === true) return;
      process = {
        parentToolUseId,
        taskId,
        turnId,
        description: stringValue(message.description),
        status: "running"
      };
      this.backgroundProcessesByParentToolUseID.set(parentToolUseId, process);
      this.backgroundProcessParentByTaskID.set(taskId, parentToolUseId);
      this.tools.completeBackgroundLaunch(parentToolUseId, taskId);
    }
    if (!process) return;
    if (isTerminalTaskStatus(message.status)) {
      process.status = delegatedTaskStatus(message.status);
    } else if (process.status !== "running") {
      return;
    }
    const summary = delegatedTaskSummaryFromMessage(message);
    const error = stringValue(message.error);
    this.emit({
      type: "background_process_updated",
      payload: {
        turnId: process.turnId,
        toolCallId: `background:${process.taskId}`,
        name: process.description || process.taskId,
        callType: "command",
        status: process.status,
        input: { description: process.description, taskId: process.taskId },
        output: {
          status: process.status,
          ...(summary ? { text: summary } : {}),
          ...(error ? { stderr: error } : {}),
          ...(stringValue(message.output_file)
            ? { outputFile: message.output_file }
            : {}),
          ...(typeof message.exit_code === "number"
            ? { exitCode: message.exit_code }
            : {})
        },
        ...(process.status === "failed"
          ? { error: { message: error || summary } }
          : {})
      }
    });
    // task_updated can precede the richer final notification.
    if (subtype === "task_notification") {
      this.backgroundProcessesByParentToolUseID.delete(process.parentToolUseId);
      this.backgroundProcessParentByTaskID.delete(process.taskId);
    }
  }

  private resolveDelegatedTaskFromMessage(
    message: Record<string, unknown>
  ): DelegatedTaskState | undefined {
    const taskId = stringValue(message.task_id) || stringValue(message.taskId);
    const agentId =
      stringValue(message.agentId) ||
      stringValue(message.agent_id) ||
      stringValue(message.agentID);
    const explicitParentToolUseId =
      stringValue(message.parentToolUseId) ||
      stringValue(message.parent_tool_use_id) ||
      stringValue(message.tool_use_id) ||
      stringValue(message.toolCallId) ||
      stringValue(message.callId);
    if (explicitParentToolUseId) {
      return this.delegatedTasksByParentToolUseID.get(explicitParentToolUseId);
    }
    const parentToolUseId = this.delegatedParentByAlias(taskId, agentId);
    if (parentToolUseId) {
      return this.delegatedTasksByParentToolUseID.get(parentToolUseId);
    }
    return undefined;
  }

  private delegatedParentByAlias(taskId: string, agentId: string): string {
    // Claude Code hooks and task notifications frequently carry the agent id
    // in task_id, so each alias is matched against both maps.
    for (const alias of [taskId, agentId]) {
      if (!alias) {
        continue;
      }
      const parent =
        this.delegatedParentByTaskID.get(alias) ||
        this.delegatedParentByAgentID.get(alias);
      if (parent) {
        return parent;
      }
    }
    return "";
  }

  private hasRunningChildDelegatedTasks(parentToolUseId: string): boolean {
    for (const task of this.delegatedTasksByParentToolUseID.values()) {
      if (
        task.parentTaskToolUseId === parentToolUseId &&
        task.status === "running"
      ) {
        return true;
      }
    }
    return false;
  }

  hasUnsettledChildWork(parentToolUseId: string): boolean {
    return (
      this.tools.hasPendingChildResults(parentToolUseId) ||
      this.hasRunningChildDelegatedTasks(parentToolUseId)
    );
  }

  private prepareDelegatedTaskTerminal(task: DelegatedTaskState): void {
    if (task.status !== "running") {
      return;
    }
    const running = [...this.delegatedTasksByParentToolUseID.values()].filter(
      (candidate) => candidate.status === "running"
    );
    if (running.length === 1 && running[0] === task) {
      this.backgroundTasks.reserveContinuation();
    }
  }

  private delegatedTaskCounts(turnId: string): DelegatedTaskCounts {
    const delegatedCounts: Record<DelegatedTaskStatus, number> = {
      running: 0,
      completed: 0,
      failed: 0,
      stopped: 0
    };
    let knownCount = 0;
    for (const task of this.delegatedTasksByParentToolUseID.values()) {
      if (turnId && task.turnId !== turnId) {
        continue;
      }
      knownCount += 1;
      delegatedCounts[task.status] += 1;
    }
    return { known: knownCount, ...delegatedCounts };
  }

  private emitDelegatedTaskParentUpdate(
    task: DelegatedTaskState,
    message: Record<string, unknown>
  ): void {
    const turnId = task.turnId || this.activeTurnId();
    if (!turnId) {
      return;
    }
    task.turnId = turnId;
    const summary =
      delegatedTaskSummaryFromMessage(message) || "Subagent task completed.";
    const usage = recordValue(message.usage);
    const metadata: Record<string, unknown> = {
      adapter: "claude-agent-sdk",
      toolName: "Agent",
      async: true,
      subagentAsync: true,
      taskStatus: task.status,
      subagentStatus: task.status,
      ...(task.taskId ? { taskId: task.taskId } : {}),
      ...(task.agentId
        ? { agentId: task.agentId, subagentAgentId: task.agentId }
        : {}),
      ...(task.outputFile
        ? { outputFile: task.outputFile, subagentOutputFile: task.outputFile }
        : {})
    };
    this.emit({
      type: task.status === "failed" ? "tool_failed" : "tool_completed",
      payload: {
        turnId,
        toolCallId: task.parentToolUseId,
        callId: task.parentToolUseId,
        toolName: "Agent",
        callType: "subagent",
        name: "Agent",
        status: task.status === "failed" ? "failed" : "completed",
        input: task.input,
        output: { text: summary, ...(usage ? { usage } : {}) },
        content: [{ type: "tool_result", text: summary }],
        metadata
      }
    });
  }

  private emitDelegatedTaskLifecycleEvent(
    type:
      | "task_started"
      | "task_progress"
      | "task_completed"
      | "task_result_updated",
    task: DelegatedTaskState,
    message: Record<string, unknown>
  ): void {
    const turnId = task.turnId || this.activeTurnId();
    if (!turnId) {
      return;
    }
    task.turnId = turnId;
    const taskId = stringValue(message.task_id) || stringValue(message.taskId);
    if (taskId && !task.taskId) {
      task.taskId = taskId;
      this.delegatedParentByTaskID.set(taskId, task.parentToolUseId);
    }
    const description =
      stringValue(message.description) ||
      stringValue(message.summary) ||
      task.description ||
      task.subject;
    const summary = delegatedTaskSummaryFromMessage(message);
    const lastToolName =
      stringValue(message.last_tool_name) || stringValue(message.lastToolName);
    const usage = recordValue(message.usage);
    this.emit({
      type,
      payload: {
        turnId,
        parentToolUseId: task.parentToolUseId,
        toolCallId: task.parentToolUseId,
        callId: task.parentToolUseId,
        ...(task.taskId ? { taskId: task.taskId } : {}),
        ...(task.agentId ? { agentId: task.agentId } : {}),
        ...(task.outputFile ? { outputFile: task.outputFile } : {}),
        status: task.status,
        ...(description ? { description } : {}),
        ...(summary ? { summary } : {}),
        ...(lastToolName ? { lastToolName } : {}),
        ...(usage ? { usage } : {}),
        input: task.input,
        metadata: {
          adapter: "claude-agent-sdk",
          parentToolUseId: task.parentToolUseId,
          async: true,
          subagentAsync: true,
          taskStatus: task.status,
          subagentStatus: task.status,
          ...(task.taskId ? { taskId: task.taskId } : {}),
          ...(task.agentId
            ? { agentId: task.agentId, subagentAgentId: task.agentId }
            : {}),
          ...(task.outputFile
            ? {
                outputFile: task.outputFile,
                subagentOutputFile: task.outputFile
              }
            : {})
        }
      }
    });
  }
}

function delegatedTaskSummaryFromMessage(
  message: Record<string, unknown>
): string {
  return (
    stringValue(message.result) ||
    stringValue(message.summary) ||
    stringValue(message.description)
  );
}

function isExplicitSubagentTaskMessage(
  message: Record<string, unknown>
): boolean {
  const taskType = (
    stringValue(message.task_type) || stringValue(message.taskType)
  ).toLowerCase();
  return Boolean(
    stringValue(message.agent_id) ||
    stringValue(message.agentId) ||
    stringValue(message.agentID) ||
    taskType === "agent" ||
    taskType === "subagent"
  );
}

function isTerminalTaskStatus(value: unknown): boolean {
  switch (stringValue(value).toLowerCase()) {
    case "completed":
    case "failed":
    case "error":
    case "stopped":
    case "canceled":
    case "cancelled":
      return true;
    default:
      return false;
  }
}

function delegatedTaskStatus(value: unknown): DelegatedTaskStatus {
  switch (stringValue(value)) {
    case "failed":
    case "error":
      return "failed";
    case "stopped":
    case "canceled":
    case "cancelled":
      return "stopped";
    default:
      return "completed";
  }
}
