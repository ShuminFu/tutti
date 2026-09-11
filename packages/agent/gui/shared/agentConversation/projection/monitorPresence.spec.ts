import { describe, expect, it } from "vitest";
import type { AgentTaskSubAgentVM } from "../contracts/agentTaskItemVM";
import {
  EMPTY_MONITOR_PRESENCE,
  selectMonitorPresence
} from "./monitorPresence";

describe("selectMonitorPresence", () => {
  it("counts only monitors, and reports the one armed longest", () => {
    const presence = selectMonitorPresence(
      lanes({
        "monitor-1": [
          lane({ childSessionId: "m1", startedAtUnixMs: 3_000 }),
          lane({ childSessionId: "m2", startedAtUnixMs: 1_000 })
        ],
        // A sub-agent is not a monitor: it dies with the turn, so it must not
        // land in a chip that says "still watching".
        "spawn-1": [
          lane({
            childSessionId: "s1",
            parentToolName: "Agent",
            startedAtUnixMs: 500
          })
        ]
      })
    );

    expect(presence).toEqual({
      runningCount: 2,
      interruptedCount: 0,
      longestRunningStartedAtUnixMs: 1_000,
      runningChildSessionIds: ["m1", "m2"]
    });
  });

  it("counts interrupted monitors apart from running ones", () => {
    const presence = selectMonitorPresence(
      lanes({
        "monitor-1": [
          lane({ childSessionId: "m1", status: "interrupted" }),
          lane({ childSessionId: "m2", startedAtUnixMs: 2_000 })
        ]
      })
    );

    expect(presence).toMatchObject({
      runningCount: 1,
      interruptedCount: 1,
      longestRunningStartedAtUnixMs: 2_000
    });
  });

  it("finds a monitor armed by a sub-agent", () => {
    const presence = selectMonitorPresence(
      lanes({
        "spawn-1": [
          lane({
            childSessionId: "s1",
            parentToolName: "Agent",
            childSessions: [
              lane({ childSessionId: "m1", startedAtUnixMs: 7_000 })
            ]
          })
        ]
      })
    );

    expect(presence).toMatchObject({
      runningCount: 1,
      longestRunningStartedAtUnixMs: 7_000
    });
  });

  it("reports nothing when no monitor is armed", () => {
    expect(
      selectMonitorPresence(
        lanes({
          "spawn-1": [lane({ childSessionId: "s1", parentToolName: "Agent" })],
          "monitor-1": [
            lane({ childSessionId: "m1", status: "completed" }),
            lane({ childSessionId: "m2", status: "failed" })
          ]
        })
      )
    ).toBe(EMPTY_MONITOR_PRESENCE);
  });
});

function lanes(
  source: Record<string, AgentTaskSubAgentVM[]>
): ReadonlyMap<string, readonly AgentTaskSubAgentVM[]> {
  return new Map(Object.entries(source));
}

function lane(
  overrides: Partial<AgentTaskSubAgentVM> & { childSessionId: string }
): AgentTaskSubAgentVM {
  return {
    parentToolCallId: "monitor-1",
    parentToolName: "Monitor",
    status: "running",
    name: null,
    task: null,
    laneIndex: 1,
    laneCount: 1,
    latestActivity: null,
    latestActivityKind: null,
    activityLog: [],
    activityOmittedCount: 0,
    failureDetail: null,
    startedAtUnixMs: 1_000,
    latestActivityAtUnixMs: null,
    terminalAtUnixMs: null,
    childSessions: [],
    ...overrides
  };
}
