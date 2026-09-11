import type { AgentTaskSubAgentVM } from "../contracts/agentTaskItemVM";

// Canonical tool name of the Claude Code background watcher. A Monitor keeps
// running after the turn that armed it settles, which is exactly why it needs
// a surface of its own: nothing else in the transcript stays visible once the
// turn is over.
export const MONITOR_TOOL_NAME = "Monitor";

export interface AgentMonitorPresenceVM {
  // Monitors still watching right now.
  runningCount: number;
  // Monitors that can never report again because the runtime that owned them
  // is gone. Counted separately: "2 watching" and "2 cut off" must not look
  // the same.
  interruptedCount: number;
  // Start time of the monitor that has been armed the longest, so the surface
  // can tick one elapsed clock instead of N.
  longestRunningStartedAtUnixMs: number | null;
  // Child session ids of the running monitors, in the order found. Stopping
  // one is a turn cancel against its own child session, so the surface needs
  // the ids, not just the count.
  runningChildSessionIds: readonly string[];
}

export const EMPTY_MONITOR_PRESENCE: AgentMonitorPresenceVM = {
  runningCount: 0,
  interruptedCount: 0,
  longestRunningStartedAtUnixMs: null,
  runningChildSessionIds: []
};

export function selectMonitorPresence(
  lanesByParentToolCallId: ReadonlyMap<string, readonly AgentTaskSubAgentVM[]>
): AgentMonitorPresenceVM {
  let runningCount = 0;
  let interruptedCount = 0;
  let longestRunningStartedAtUnixMs: number | null = null;
  const runningChildSessionIds: string[] = [];

  const visit = (lane: AgentTaskSubAgentVM): void => {
    if (lane.parentToolName === MONITOR_TOOL_NAME) {
      if (lane.status === "running") {
        runningCount += 1;
        if (lane.childSessionId) {
          runningChildSessionIds.push(lane.childSessionId);
        }
        const started = lane.startedAtUnixMs;
        if (
          typeof started === "number" &&
          (longestRunningStartedAtUnixMs === null ||
            started < longestRunningStartedAtUnixMs)
        ) {
          longestRunningStartedAtUnixMs = started;
        }
      } else if (lane.status === "interrupted") {
        interruptedCount += 1;
      }
    }
    // A monitor armed by a sub-agent is still a monitor on this conversation:
    // walk the whole lane tree, not just the direct children.
    for (const child of lane.childSessions) {
      visit(child);
    }
  };

  for (const lanes of lanesByParentToolCallId.values()) {
    for (const lane of lanes) {
      visit(lane);
    }
  }

  return runningCount === 0 && interruptedCount === 0
    ? EMPTY_MONITOR_PRESENCE
    : {
        runningCount,
        interruptedCount,
        longestRunningStartedAtUnixMs,
        runningChildSessionIds
      };
}
