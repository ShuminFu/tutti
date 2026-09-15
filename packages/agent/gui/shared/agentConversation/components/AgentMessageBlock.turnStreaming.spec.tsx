import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentSessionDetailViewModel } from "../../workspaceAgentSessionDetailViewModel";
import { AgentConversationFlow } from "./AgentConversationFlow";
import { projectAgentConversationVM } from "../projection/agentConversationProjection";

const mermaidMocks = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn()
}));

vi.mock("mermaid", () => ({
  default: mermaidMocks
}));

const MERMAID_BODY = [
  "Here is the flow:",
  "",
  "```mermaid",
  'flowchart TD\n    A["start"] --> B["done"]',
  "```"
].join("\n");

describe("Agent transcript streaming affordance", () => {
  beforeEach(() => {
    mermaidMocks.initialize.mockReset();
    mermaidMocks.render.mockReset();
    mermaidMocks.render.mockResolvedValue({
      svg: '<svg viewBox="0 0 320 180"><text>Rendered diagram</text></svg>'
    });
  });

  const renderConversation = (
    detail: WorkspaceAgentSessionDetailViewModel
  ): void => {
    render(
      <AgentConversationFlow
        conversation={projectAgentConversationVM(detail)}
        empty={null}
        isLoading={false}
        loadingLabel="Loading"
        labels={{
          thinkingLabel: "Thought process",
          toolCallsLabel: (count) => `Tool calls (${count})`,
          processing: "Planning next moves",
          turnSummary: "Changed files"
        }}
      />
    );
  };

  it("renders a settled Turn's stale streaming reply instead of a permanent placeholder", async () => {
    // The provider died mid-stream, so the Turn settled while its final reply
    // message never left `streaming` (statusKind "working").
    renderConversation(detailViewModel({ outcome: "settled", live: false }));

    await waitFor(() => {
      expect(screen.getByText("Rendered diagram")).toBeInTheDocument();
    });
    expect(
      screen.queryByTestId("agent-mermaid-placeholder")
    ).not.toBeInTheDocument();
  });

  it("keeps the placeholder while the Turn is still live", () => {
    renderConversation(detailViewModel({ outcome: "running", live: true }));

    expect(screen.getByTestId("agent-mermaid-placeholder")).toBeInTheDocument();
    expect(mermaidMocks.render).not.toHaveBeenCalled();
  });
});

function detailViewModel(input: {
  outcome: "settled" | "running";
  live: boolean;
}): WorkspaceAgentSessionDetailViewModel {
  const settled = input.outcome === "settled";
  return {
    activity: {
      id: "activity-1",
      sessionId: "session-1",
      agentName: "Claude Code",
      agentProvider: "claude-code",
      title: "Claude Code",
      latestActivitySummary: "Working",
      status: settled ? "idle" : "working",
      sortTimeUnixMs: 10,
      changedFiles: [],
      userId: "user-1",
      userName: "Taylor",
      userAvatarUrl: ""
    },
    session: normalizeAgentActivitySession({
      activeTurnId: input.live ? "turn-1" : null,
      latestTurnInteractions: [],
      pendingInteractions: [],
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      userId: "user-1",
      provider: "claude-code",
      providerSessionId: "provider-session-1",
      cwd: "/workspace/demo",
      title: "Claude Code",
      createdAtUnixMs: 1,
      updatedAtUnixMs: 10,
      ...(input.live
        ? {
            activeTurn: {
              agentSessionId: "session-1",
              outcome: null,
              origin: "user_prompt",
              phase: "running" as const,
              settledAtUnixMs: null,
              startedAtUnixMs: 1,
              turnId: "turn-1",
              updatedAtUnixMs: 10
            }
          }
        : {})
    }),
    cwd: "/workspace/demo",
    workspaceRoot: "/workspace/demo",
    sessionTurns: [
      {
        agentSessionId: "session-1",
        turnId: "turn-1",
        phase: settled ? "settled" : "running",
        origin: "user_prompt",
        outcome: settled ? ("completed" as const) : null,
        startedAtUnixMs: 1,
        settledAtUnixMs: settled ? 10 : null,
        updatedAtUnixMs: 10
      }
    ],
    turns: [
      {
        id: "turn-1",
        userMessage: { id: "user-1", body: "Draw the flow" },
        userMessages: [{ id: "user-1", body: "Draw the flow" }],
        agentMessages: [
          {
            id: "assistant-reply",
            body: MERMAID_BODY,
            statusKind: "working" as const
          }
        ],
        toolCalls: [],
        toolCallCount: 0,
        hasFailedToolCall: false,
        agentItems: [
          {
            kind: "message",
            message: {
              id: "assistant-reply",
              body: MERMAID_BODY,
              // Stale per-message stream state: the Turn is what decides.
              statusKind: "working" as const
            }
          }
        ]
      }
    ],
    showProcessingIndicator: !settled
  };
}
