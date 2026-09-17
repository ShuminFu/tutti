import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AgentGUIRuntimeProvider,
  type AgentActivityRuntimeMcpAppResource,
  type AgentGUIRuntime
} from "../../../agentActivityRuntime";
import type { AgentMcpAppRowVM } from "../contracts/agentMcpAppRowVM";
import { AgentMcpAppRow } from "./AgentMcpAppRow";

const SHA = "c".repeat(64);

const row: AgentMcpAppRowVM = {
  kind: "mcp-app",
  id: "mcp-app:call:widget-1",
  turnId: "turn-1",
  sourceCallId: "call:widget-1",
  serverName: "workflow_report",
  toolName: "show_widget",
  resourceUri: "ui://workflow_report/widget",
  resourceSha256: SHA,
  toolArguments: { title: "Sales", widget_code: "<svg></svg>" },
  toolResult: { content: [] },
  occurredAtUnixMs: 1
};

const snapshot: AgentActivityRuntimeMcpAppResource = {
  uri: "ui://workflow_report/widget",
  mimeType: "text/html;profile=mcp-app",
  html: "<!doctype html><html><head></head><body></body></html>",
  meta: {
    csp: { resourceDomains: ["https://lib.baomitu.com"], connectDomains: [] },
    prefersBorder: false
  }
};

function renderRow(loadMcpAppResource: AgentGUIRuntime["loadMcpAppResource"]) {
  const runtime = { loadMcpAppResource } as unknown as AgentGUIRuntime;
  return render(
    <AgentGUIRuntimeProvider runtime={runtime}>
      <div data-testid="transcript">
        <AgentMcpAppRow row={row} />
      </div>
    </AgentGUIRuntimeProvider>
  );
}

describe("AgentMcpAppRow", () => {
  it("renders the sandboxed view once the snapshot loads", async () => {
    const load = vi.fn(async () => snapshot);
    renderRow(load);

    const iframe = await screen.findByTitle("Tool view: Sales");
    expect(load).toHaveBeenCalledWith(SHA);
    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts");
    expect(screen.getByTestId("agent-mcp-app-artifact")).toBeInTheDocument();
  });

  it.each([
    ["the snapshot is missing", async () => null],
    [
      "the snapshot read fails",
      async () => {
        throw new Error("tuttid 404");
      }
    ],
    [
      "the snapshot is not an MCP App resource",
      async () => ({ ...snapshot, mimeType: "text/html" })
    ],
    ["the snapshot has no HTML", async () => ({ ...snapshot, html: "  " })]
  ])("renders nothing when %s", async (_case, loader) => {
    const load = vi.fn(loader);
    renderRow(load as AgentGUIRuntime["loadMcpAppResource"]);

    await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.getByTestId("transcript")).toBeEmptyDOMElement();
  });

  it("renders nothing on hosts without a snapshot reader", async () => {
    renderRow(undefined);
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.getByTestId("transcript")).toBeEmptyDOMElement();
  });
});
