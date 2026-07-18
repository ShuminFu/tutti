import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { describe, expect, it } from "vitest";
import { createTestAgentSessionEngine } from "../../../shared/testing/createTestAgentSessionEngine";
import {
  EMPTY_CONVERSATION_SEARCH_QUERY_STATE,
  createConversationRailQuerySnapshotSelector
} from "./agentGuiConversationRailQuerySnapshot";

describe("createConversationRailQuerySnapshotSelector", () => {
  it("projects only rail-owned sessions and preserves unchanged identities", () => {
    const engine = createTestAgentSessionEngine();
    const sessions = Array.from({ length: 176 }, (_, index) =>
      createSession(`session-${index}`, `Session ${index}`, index + 1)
    );
    engine.dispatch({ sessions, type: "session/snapshotReceived" });

    const selectSnapshot = createConversationRailQuerySnapshotSelector();
    const input = {
      engineState: engine.getSnapshot(),
      queryState: {
        pending: false,
        reconcilingSessionIds: ["session-2"],
        resolvedScopeKey: "all",
        sectionPageStates: new Map(),
        sections: [
          {
            id: "conversations",
            kind: "conversations" as const,
            project: null,
            sessionIds: ["session-0", "session-1"]
          }
        ]
      },
      runtimeSectionsEnabled: true,
      searchEnabled: true,
      searchQuery: "session",
      searchRequestKey: "search:session",
      searchState: {
        ...EMPTY_CONVERSATION_SEARCH_QUERY_STATE,
        requestKey: "search:session",
        resolvedQuery: "session",
        sessionIds: ["session-3"]
      }
    };
    const first = selectSnapshot(input, undefined);

    expect(first.runtimeRailConversations.map(({ id }) => id)).toEqual([
      "session-0",
      "session-1",
      "session-2",
      "session-3"
    ]);

    engine.dispatch({
      session: createSession("session-175", "Unrelated update", 1_000),
      type: "session/upserted"
    });
    const afterUnrelatedUpdate = selectSnapshot(
      { ...input, engineState: engine.getSnapshot() },
      first
    );
    expect(afterUnrelatedUpdate).toBe(first);

    engine.dispatch({
      session: createSession("session-1", "Changed title", 2_000),
      type: "session/upserted"
    });
    const afterVisibleUpdate = selectSnapshot(
      { ...input, engineState: engine.getSnapshot() },
      afterUnrelatedUpdate
    );

    expect(afterVisibleUpdate).not.toBe(first);
    expect(afterVisibleUpdate.runtimeRailConversations[0]).toBe(
      first.runtimeRailConversations[0]
    );
    expect(afterVisibleUpdate.runtimeRailConversations[1]).not.toBe(
      first.runtimeRailConversations[1]
    );
    expect(afterVisibleUpdate.runtimeRailConversations[1]?.title).toBe(
      "Changed title"
    );
    expect(afterVisibleUpdate.runtimeRailConversations[2]).toBe(
      first.runtimeRailConversations[2]
    );
    expect(afterVisibleUpdate.runtimeRailConversations[3]).toBe(
      first.runtimeRailConversations[3]
    );

    engine.dispose();
  });
});

function createSession(
  agentSessionId: string,
  title: string,
  updatedAtUnixMs: number
) {
  return normalizeAgentActivitySession({
    activeTurnId: null,
    agentSessionId,
    agentTargetId: "local:codex",
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    railSectionKey: "conversations",
    title,
    updatedAtUnixMs,
    workspaceId: "test-workspace"
  });
}
