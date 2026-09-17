import { describe, expect, it } from "vitest";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import {
  projectAgentMcpAppRow,
  resolveJsonPointer
} from "./agentMcpAppProjection";

const SHA = "a".repeat(64);
const WIDGET_ARGUMENTS = { title: "Sales", widget_code: "<svg></svg>" };

function toolCall(
  payload: Record<string, unknown> | null,
  overrides: Partial<AgentToolCallVM> = {}
): AgentToolCallVM {
  const objectOrNull = (value: unknown) =>
    value && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : null;
  return {
    kind: "tool-call",
    id: "call:widget-1",
    turnId: "turn-1",
    name: "workflow_report / show_widget",
    toolName: "mcp__workflow_report__show_widget",
    callType: "tool",
    status: "Completed",
    statusKind: "completed",
    summary: "show_widget",
    compactSummary: null,
    payload,
    input: objectOrNull(payload?.input),
    output: objectOrNull(payload?.output),
    error: null,
    metadata: null,
    locations: null,
    rendererKind: "mcp",
    approval: null,
    planMode: null,
    askUserQuestion: null,
    task: null,
    occurredAtUnixMs: 1_000,
    ...overrides
  };
}

function mcpApp(argumentsPointer: string, extra: Record<string, unknown> = {}) {
  return {
    serverName: "workflow_report",
    toolName: "show_widget",
    resourceUri: "ui://workflow_report/widget",
    resourceSha256: SHA,
    argumentsPointer,
    ...extra
  };
}

describe("projectAgentMcpAppRow", () => {
  it.each([
    ["claude-code", { input: WIDGET_ARGUMENTS, mcpApp: mcpApp("/input") }],
    [
      "acp:grok",
      {
        input: {
          tool_name: "workflow_report__show_widget",
          tool_input: WIDGET_ARGUMENTS,
          variant: "UseTool"
        },
        output: { type: "MCP" },
        mcpApp: mcpApp("/input/tool_input")
      }
    ],
    [
      "codex",
      {
        input: {
          server: "workflow_report",
          tool: "show_widget",
          arguments: WIDGET_ARGUMENTS
        },
        mcpApp: mcpApp("/input/arguments")
      }
    ]
  ])(
    "follows argumentsPointer for the %s payload shape",
    (_provider, payload) => {
      expect(
        projectAgentMcpAppRow(toolCall(payload), "turn-1", "workspace-1")
      ).toMatchObject({
        kind: "mcp-app",
        id: "mcp-app:call:widget-1",
        turnId: "turn-1",
        workspaceId: "workspace-1",
        sourceCallId: "call:widget-1",
        serverName: "workflow_report",
        toolName: "show_widget",
        resourceUri: "ui://workflow_report/widget",
        resourceSha256: SHA,
        toolArguments: WIDGET_ARGUMENTS,
        occurredAtUnixMs: 1_000
      });
    }
  );

  it("projects persisted output text as a CallToolResult", () => {
    const row = projectAgentMcpAppRow(
      toolCall({
        input: WIDGET_ARGUMENTS,
        output: { text: "Shown to the user" },
        mcpApp: mcpApp("/input")
      }),
      "turn-1",
      "workspace-1"
    );
    expect(row?.toolResult).toEqual({
      content: [{ type: "text", text: "Shown to the user" }]
    });
    expect(
      projectAgentMcpAppRow(
        toolCall({ input: WIDGET_ARGUMENTS, mcpApp: mcpApp("/input") }),
        "turn-1",
        "workspace-1"
      )?.toolResult
    ).toEqual({ content: [] });
  });

  it("does not project a call that is not completed", () => {
    for (const statusKind of ["working", "failed", null] as const) {
      expect(
        projectAgentMcpAppRow(
          toolCall(
            { input: WIDGET_ARGUMENTS, mcpApp: mcpApp("/input") },
            { statusKind: statusKind as AgentToolCallVM["statusKind"] }
          ),
          "turn-1",
          "workspace-1"
        )
      ).toBeNull();
    }
  });

  it.each([undefined, null, "", "  "])(
    "does not project without the owning workspace (%s)",
    (workspaceId) => {
      expect(
        projectAgentMcpAppRow(
          toolCall({ input: WIDGET_ARGUMENTS, mcpApp: mcpApp("/input") }),
          "turn-1",
          workspaceId
        )
      ).toBeNull();
    }
  );

  it.each([
    ["no mcpApp reference", { input: WIDGET_ARGUMENTS }],
    ["no payload", null],
    [
      "a malformed hash",
      {
        input: WIDGET_ARGUMENTS,
        mcpApp: mcpApp("/input", { resourceSha256: "abc" })
      }
    ],
    [
      "a non ui:// resource",
      {
        input: WIDGET_ARGUMENTS,
        mcpApp: mcpApp("/input", { resourceUri: "https://example.com/widget" })
      }
    ],
    [
      "a pointer to a missing value",
      { input: WIDGET_ARGUMENTS, mcpApp: mcpApp("/input/arguments") }
    ],
    [
      "a pointer to a non-object",
      { input: WIDGET_ARGUMENTS, mcpApp: mcpApp("/input/title") }
    ],
    ["a relative pointer", { input: WIDGET_ARGUMENTS, mcpApp: mcpApp("input") }]
  ])("does not project %s", (_case, payload) => {
    expect(
      projectAgentMcpAppRow(toolCall(payload), "turn-1", "workspace-1")
    ).toBeNull();
  });
});

describe("resolveJsonPointer", () => {
  it("implements RFC 6901 escaping and array indexes over own properties", () => {
    const document = { "a/b": { "m~n": [{ x: 1 }] } };
    expect(resolveJsonPointer(document, "")).toBe(document);
    expect(resolveJsonPointer(document, "/a~1b/m~0n/0/x")).toBe(1);
    expect(resolveJsonPointer(document, "/a~1b/m~0n/01")).toBeUndefined();
    expect(resolveJsonPointer({}, "/constructor")).toBeUndefined();
    expect(resolveJsonPointer({}, "/__proto__")).toBeUndefined();
  });
});
