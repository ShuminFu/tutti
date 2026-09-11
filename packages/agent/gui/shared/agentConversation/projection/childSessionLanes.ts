import type {
  AgentActivityMessage,
  AgentActivitySession
} from "@tutti-os/agent-activity-core";
import type {
  AgentTaskSubAgentActivityVM,
  AgentTaskSubAgentStatus,
  AgentTaskSubAgentVM
} from "../contracts/agentTaskItemVM";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import type { AgentToolGroupRowVM } from "../contracts/agentToolGroupRowVM";
import { projectWorkspaceAgentMessagesToTimelineItems } from "./workspaceAgentMessageProjection";
import type { WorkspaceAgentActivityTimelineItem } from "../../workspaceAgentTimelineTypes";
import {
  isWorkspaceAgentToolCallItem,
  resolveWorkspaceAgentToolName
} from "../../workspaceAgentToolCallDisplay";

export interface BuildChildSessionLanesInput {
  rootSession: AgentActivitySession;
  // Whether the host still holds a live agent process for this conversation.
  // null/undefined means "unknown" - only an explicit false downgrades a
  // never-settled child to "interrupted".
  runtimeLive?: boolean | null;
  rootTimelineItems: readonly WorkspaceAgentActivityTimelineItem[];
  childSessions: readonly AgentActivitySession[];
  messagesBySessionId: Readonly<
    Record<string, readonly AgentActivityMessage[] | undefined>
  >;
}

export function buildChildSessionLanesByParentToolCallId(
  input: BuildChildSessionLanesInput
): ReadonlyMap<string, AgentTaskSubAgentVM[]> {
  const rootSessionId = input.rootSession.agentSessionId.trim();
  const childrenByParentSessionId = new Map<string, AgentActivitySession[]>();
  for (const childSession of input.childSessions) {
    if (
      childSession.kind !== "child" ||
      childSession.rootAgentSessionId !== rootSessionId ||
      !childSession.parentAgentSessionId?.trim() ||
      !childSession.parentToolCallId?.trim()
    ) {
      continue;
    }
    const parentSessionId = childSession.parentAgentSessionId.trim();
    const siblings = childrenByParentSessionId.get(parentSessionId) ?? [];
    siblings.push(childSession);
    childrenByParentSessionId.set(parentSessionId, siblings);
  }
  for (const siblings of childrenByParentSessionId.values()) {
    siblings.sort(compareChildSessions);
  }

  const lanesByParentToolCallId = new Map<string, AgentTaskSubAgentVM[]>();
  const directChildren = childrenByParentSessionId.get(rootSessionId) ?? [];
  const visited = new Set<string>();
  for (const childSession of directChildren) {
    const parentToolCallId = childSession.parentToolCallId?.trim();
    if (!parentToolCallId) continue;
    const lanes = lanesByParentToolCallId.get(parentToolCallId) ?? [];
    lanes.push(
      buildChildSessionLane({
        childSession,
        childrenByParentSessionId,
        messagesBySessionId: input.messagesBySessionId,
        parentTimelineItems: input.rootTimelineItems,
        runtimeLive: input.runtimeLive ?? null,
        visited
      })
    );
    lanesByParentToolCallId.set(parentToolCallId, lanes);
  }
  for (const lanes of lanesByParentToolCallId.values()) {
    assignLanePositions(lanes);
  }
  return lanesByParentToolCallId;
}

export function attachChildSessionLanesToConversationVM(
  conversation: AgentConversationVM | null,
  lanesByParentToolCallId: ReadonlyMap<string, AgentTaskSubAgentVM[]>
): AgentConversationVM | null {
  if (!conversation || lanesByParentToolCallId.size === 0) {
    return conversation;
  }
  let changed = false;
  const rows = conversation.rows.map((row) => {
    if (row.kind !== "tool-group") return row;
    const nextRow = toolGroupRowWithChildSessions(row, lanesByParentToolCallId);
    if (nextRow !== row) changed = true;
    return nextRow;
  });
  return changed ? { ...conversation, rows } : conversation;
}

export function deriveSubAgentNameFromTask(task: string | null): string | null {
  if (!task) return null;
  const firstSentence = task
    .trim()
    .split(/[。．.!?！？，,\n]/, 1)[0]
    ?.trim();
  if (!firstSentence) return null;
  const selfAddress = /^(你现在是|你現在是|你是|you are|act as)\s*/i;
  if (!selfAddress.test(firstSentence)) return null;
  const stripped = firstSentence
    .replace(selfAddress, "")
    .replace(/\s+/g, " ")
    .trim();
  return stripped.length >= 2 && stripped.length <= 36 ? stripped : null;
}

function buildChildSessionLane(input: {
  childSession: AgentActivitySession;
  childrenByParentSessionId: ReadonlyMap<string, AgentActivitySession[]>;
  messagesBySessionId: BuildChildSessionLanesInput["messagesBySessionId"];
  parentTimelineItems: readonly WorkspaceAgentActivityTimelineItem[];
  runtimeLive: boolean | null;
  visited: Set<string>;
}): AgentTaskSubAgentVM {
  const childSessionId = input.childSession.agentSessionId.trim();
  if (input.visited.has(childSessionId)) {
    return emptyCycleLane(input.childSession, input.runtimeLive);
  }
  input.visited.add(childSessionId);
  const childMessages = input.messagesBySessionId[childSessionId] ?? [];
  const childTimelineItems =
    projectWorkspaceAgentMessagesToTimelineItems(childMessages);
  const parentToolCallId = input.childSession.parentToolCallId?.trim() ?? "";
  const parentCall = parentToolCall(
    input.parentTimelineItems,
    parentToolCallId
  );
  const task = parentCall?.task ?? null;
  const childSessions = (
    input.childrenByParentSessionId.get(childSessionId) ?? []
  ).map((nestedChild) =>
    buildChildSessionLane({
      childSession: nestedChild,
      childrenByParentSessionId: input.childrenByParentSessionId,
      messagesBySessionId: input.messagesBySessionId,
      parentTimelineItems: childTimelineItems,
      runtimeLive: input.runtimeLive,
      visited: input.visited
    })
  );
  assignSiblingLanePositions(childSessions);
  input.visited.delete(childSessionId);

  const activity = childSessionActivityLog(childTimelineItems);
  const latestActivity = activity.entries.at(-1) ?? null;
  const latestMessageAtUnixMs = childMessages.reduce(
    (latest, message) => Math.max(latest, message.occurredAtUnixMs),
    0
  );
  const latestTurn = input.childSession.latestTurn;
  const status = childSessionStatus(input.childSession, input.runtimeLive);
  const terminalAtUnixMs =
    status === "running"
      ? null
      : (latestTurn?.settledAtUnixMs ?? input.childSession.endedAtUnixMs);
  return {
    childSessionId,
    parentToolCallId,
    parentToolName: parentCall?.toolName ?? null,
    status,
    name:
      normalizedString(input.childSession.title) ??
      deriveSubAgentNameFromTask(task),
    task,
    laneIndex: 1,
    laneCount: 1,
    latestActivity: latestActivity?.text ?? null,
    latestActivityKind: latestActivity?.kind ?? null,
    activityLog: activity.entries,
    activityOmittedCount: activity.omittedCount,
    queued: input.childSession.activeTurn?.phase === "submitted",
    failureDetail:
      status === "failed" ? (latestTurn?.error?.message?.trim() ?? null) : null,
    startedAtUnixMs:
      input.childSession.activeTurn?.startedAtUnixMs ??
      latestTurn?.startedAtUnixMs ??
      input.childSession.createdAtUnixMs,
    latestActivityAtUnixMs: Math.max(
      latestMessageAtUnixMs,
      latestTurn?.updatedAtUnixMs ?? 0,
      input.childSession.updatedAtUnixMs
    ),
    terminalAtUnixMs,
    childSessions
  };
}

function emptyCycleLane(
  session: AgentActivitySession,
  runtimeLive: boolean | null
): AgentTaskSubAgentVM {
  return {
    childSessionId: session.agentSessionId,
    parentToolCallId: session.parentToolCallId ?? "",
    parentToolName: null,
    status: childSessionStatus(session, runtimeLive),
    name: normalizedString(session.title),
    task: null,
    laneIndex: 1,
    laneCount: 1,
    latestActivity: null,
    latestActivityKind: null,
    activityLog: [],
    activityOmittedCount: 0,
    failureDetail: session.latestTurn?.error?.message?.trim() ?? null,
    startedAtUnixMs: session.createdAtUnixMs,
    latestActivityAtUnixMs: session.updatedAtUnixMs,
    terminalAtUnixMs:
      session.latestTurn?.settledAtUnixMs ?? session.endedAtUnixMs,
    childSessions: []
  };
}

function childSessionStatus(
  session: AgentActivitySession,
  runtimeLive: boolean | null = null
): AgentTaskSubAgentStatus {
  // A child only settles when a terminal turn event arrives. When the agent
  // process is killed - app restart, idle release - no such event ever comes,
  // so "no settled turn" would read as "still running" forever. The host's
  // liveness answer is the only thing that can close that gap.
  const notSettled: AgentTaskSubAgentStatus =
    runtimeLive === false ? "interrupted" : "running";
  const activeTurn = session.activeTurn;
  if (activeTurn && activeTurn.phase !== "settled") return notSettled;
  const latestTurn = session.latestTurn;
  if (!latestTurn || latestTurn.phase !== "settled") return notSettled;
  switch (latestTurn.outcome) {
    case "failed":
      return "failed";
    case "canceled":
    case "interrupted":
      return "canceled";
    case "completed":
    default:
      return "completed";
  }
}

function parentToolCall(
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[],
  parentToolCallId: string
): { task: string | null; toolName: string | null } | null {
  if (!parentToolCallId) return null;
  for (const item of timelineItems) {
    if (!isWorkspaceAgentToolCallItem(item)) continue;
    const callId = firstString(item.callId, stringValue(item.payload?.callId));
    if (callId !== parentToolCallId) continue;
    const input = recordValue(item.payload?.input);
    return {
      task: firstString(
        stringValue(input?.task),
        stringValue(input?.prompt),
        stringValue(input?.description)
      ),
      toolName: resolveWorkspaceAgentToolName(item)
    };
  }
  return null;
}

function toolGroupRowWithChildSessions(
  row: AgentToolGroupRowVM,
  lanesByParentToolCallId: ReadonlyMap<string, AgentTaskSubAgentVM[]>
): AgentToolGroupRowVM {
  let changed = false;
  const callsById = new Map<string, AgentToolCallVM>();
  const calls = row.calls.map((call) => {
    const lanes = call.task
      ? lanesByParentToolCallId.get(toolCallRawId(call.id))
      : null;
    if (!lanes?.length || !call.task) return call;
    changed = true;
    const nextCall: AgentToolCallVM = {
      ...call,
      task: { ...call.task, subAgents: lanes }
    };
    callsById.set(nextCall.id, nextCall);
    return nextCall;
  });
  if (!changed) return row;
  const entries = row.entries.map((entry) => {
    if (entry.kind !== "tool-call") return entry;
    const nextCall = callsById.get(entry.call.id);
    return nextCall ? { ...entry, call: nextCall } : entry;
  });
  return { ...row, calls, entries };
}

function toolCallRawId(id: string): string {
  return id.startsWith("call:") ? id.slice("call:".length) : id;
}

const CHILD_ACTIVITY_LOG_CAP = 20;

function childSessionActivityLog(
  timelineItems: readonly WorkspaceAgentActivityTimelineItem[]
): { entries: AgentTaskSubAgentActivityVM[]; omittedCount: number } {
  const entries = timelineItems
    .map(displayableActivityEntry)
    .filter((entry): entry is AgentTaskSubAgentActivityVM => entry !== null);
  const omittedCount = Math.max(0, entries.length - CHILD_ACTIVITY_LOG_CAP);
  return { entries: entries.slice(omittedCount), omittedCount };
}

function displayableActivityEntry(
  item: WorkspaceAgentActivityTimelineItem
): AgentTaskSubAgentActivityVM | null {
  const atUnixMs = timelineItemTime(item) || null;
  if (isWorkspaceAgentToolCallItem(item)) {
    const name = firstString(
      resolveWorkspaceAgentToolName(item),
      item.name,
      stringValue(item.payload?.name)
    );
    return name ? { kind: "tool", text: name, atUnixMs } : null;
  }
  const text = snippet(timelineItemText(item));
  if (!text) return null;
  const itemType = item.itemType?.trim().toLowerCase() ?? "";
  const role = item.role?.trim().toLowerCase() ?? "";
  return itemType === "message.assistant_thinking" ||
    role === "assistant_thinking"
    ? { kind: "reasoning", text, atUnixMs }
    : { kind: "message", text, atUnixMs };
}

function timelineItemText(item: WorkspaceAgentActivityTimelineItem): string {
  return (
    stringValue(item.payload?.text) ??
    stringValue(item.payload?.content) ??
    stringValue(item.content) ??
    ""
  )
    .replace(/\s+/g, " ")
    .trim();
}

function snippet(text: string): string {
  return text.length <= 140
    ? text
    : String.fromCodePoint(0x2026) + text.slice(-140);
}

function assignSiblingLanePositions(lanes: AgentTaskSubAgentVM[]): void {
  const lanesByCallId = new Map<string, AgentTaskSubAgentVM[]>();
  for (const lane of lanes) {
    const siblings = lanesByCallId.get(lane.parentToolCallId) ?? [];
    siblings.push(lane);
    lanesByCallId.set(lane.parentToolCallId, siblings);
  }
  for (const siblings of lanesByCallId.values()) assignLanePositions(siblings);
}

function assignLanePositions(lanes: AgentTaskSubAgentVM[]): void {
  for (let index = 0; index < lanes.length; index += 1) {
    const lane = lanes[index];
    if (!lane) continue;
    lane.laneIndex = index + 1;
    lane.laneCount = lanes.length;
  }
}

function compareChildSessions(
  left: AgentActivitySession,
  right: AgentActivitySession
): number {
  return (
    left.createdAtUnixMs - right.createdAtUnixMs ||
    left.agentSessionId.localeCompare(right.agentSessionId)
  );
}

function timelineItemTime(
  item: WorkspaceAgentActivityTimelineItem | undefined
): number {
  return item?.occurredAtUnixMs ?? item?.createdAtUnixMs ?? 0;
}

function normalizedString(value: string | null | undefined): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function firstString(
  ...values: Array<string | null | undefined>
): string | null {
  for (const value of values) {
    const normalized = normalizedString(value);
    if (normalized) return normalized;
  }
  return null;
}
