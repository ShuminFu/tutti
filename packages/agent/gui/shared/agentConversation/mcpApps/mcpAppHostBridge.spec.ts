import { afterEach, describe, expect, it, vi } from "vitest";
import {
  McpAppHostBridge,
  clampMcpAppHeight,
  type McpAppHostBridgeOptions
} from "./mcpAppHostBridge";
import type { JsonRpcMessage } from "./mcpAppProtocol";

function createBridge(overrides: Partial<McpAppHostBridgeOptions> = {}) {
  const sent: JsonRpcMessage[] = [];
  const onSizeChanged = vi.fn();
  const bridge = new McpAppHostBridge({
    postToView: (message) => sent.push(message),
    getHostContext: () => ({
      theme: "dark",
      styles: { variables: { "--color-text-primary": "rgb(255 255 255)" } },
      displayMode: "inline",
      availableDisplayModes: ["inline"],
      containerDimensions: { width: 640, maxHeight: 800 },
      locale: "zh-CN",
      toolInfo: {
        tool: { name: "show_widget", inputSchema: { type: "object" } }
      }
    }),
    appliedCsp: {
      resourceDomains: ["https://lib.baomitu.com"],
      connectDomains: []
    },
    toolArguments: { title: "Chart", widget_code: "<svg></svg>" },
    toolResult: { content: [{ type: "text", text: "shown" }] },
    onSizeChanged,
    ...overrides
  });
  return { bridge, sent, onSizeChanged };
}

function initialize(bridge: McpAppHostBridge): void {
  bridge.handleViewMessage({
    jsonrpc: "2.0",
    id: 1,
    method: "ui/initialize",
    params: {
      appInfo: { name: "shell", version: "1" },
      appCapabilities: {},
      protocolVersion: "2026-01-26"
    }
  });
  bridge.handleViewMessage({
    jsonrpc: "2.0",
    method: "ui/notifications/initialized",
    params: {}
  });
}

describe("McpAppHostBridge", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("answers ui/initialize, then sends tool-input before tool-result only after initialized", () => {
    const { bridge, sent } = createBridge();

    bridge.handleViewMessage({
      jsonrpc: "2.0",
      id: 1,
      method: "ui/initialize",
      params: {
        appInfo: { name: "shell", version: "1" },
        appCapabilities: {},
        protocolVersion: "2026-01-26"
      }
    });
    expect(sent).toHaveLength(1);
    expect(sent[0]).toEqual({
      jsonrpc: "2.0",
      id: 1,
      result: {
        protocolVersion: "2026-01-26",
        hostInfo: { name: "tutti-agent-gui", version: "1.0.0" },
        hostCapabilities: {
          sandbox: {
            csp: {
              resourceDomains: ["https://lib.baomitu.com"],
              connectDomains: []
            }
          }
        },
        hostContext: expect.objectContaining({
          theme: "dark",
          displayMode: "inline",
          availableDisplayModes: ["inline"],
          containerDimensions: { width: 640, maxHeight: 800 },
          locale: "zh-CN",
          toolInfo: {
            tool: { name: "show_widget", inputSchema: { type: "object" } }
          }
        })
      }
    });
    // Nothing unsolicited before `initialized` (spec: host MUST NOT send).
    bridge.notifyHostContextChanged({ theme: "light" });
    expect(sent).toHaveLength(1);

    bridge.handleViewMessage({
      jsonrpc: "2.0",
      method: "ui/notifications/initialized"
    });
    expect(sent.slice(1)).toEqual([
      {
        jsonrpc: "2.0",
        method: "ui/notifications/tool-input",
        params: { arguments: { title: "Chart", widget_code: "<svg></svg>" } }
      },
      {
        jsonrpc: "2.0",
        method: "ui/notifications/tool-result",
        params: { content: [{ type: "text", text: "shown" }] }
      }
    ]);

    // tool-input is sent at most once.
    bridge.handleViewMessage({
      jsonrpc: "2.0",
      method: "ui/notifications/initialized"
    });
    expect(sent).toHaveLength(3);
  });

  it("does not advertise interactive host capabilities", () => {
    const { bridge, sent } = createBridge();
    initialize(bridge);
    const result = (sent[0] as { result: Record<string, unknown> }).result;
    expect(Object.keys(result.hostCapabilities as object)).toEqual(["sandbox"]);
  });

  it.each([
    "tools/call",
    "ui/message",
    "ui/open-link",
    "ui/update-model-context",
    "ui/download-file",
    "resources/read",
    "sampling/createMessage"
  ])("answers %s with JSON-RPC -32601", (method) => {
    const { bridge, sent } = createBridge();
    initialize(bridge);
    sent.length = 0;

    bridge.handleViewMessage({
      jsonrpc: "2.0",
      id: "req-7",
      method,
      params: {}
    });

    expect(sent).toEqual([
      {
        jsonrpc: "2.0",
        id: "req-7",
        error: {
          code: -32601,
          message: `Method not supported by this host: ${method}`
        }
      }
    ]);
  });

  it("answers ping and request-display-mode without enabling other modes", () => {
    const { bridge, sent } = createBridge();
    initialize(bridge);
    sent.length = 0;

    bridge.handleViewMessage({ jsonrpc: "2.0", id: 2, method: "ping" });
    bridge.handleViewMessage({
      jsonrpc: "2.0",
      id: 3,
      method: "ui/request-display-mode",
      params: { mode: "fullscreen" }
    });

    expect(sent).toEqual([
      { jsonrpc: "2.0", id: 2, result: {} },
      { jsonrpc: "2.0", id: 3, result: { mode: "inline" } }
    ]);
  });

  it("never responds to notifications or malformed messages", () => {
    const { bridge, sent } = createBridge();
    initialize(bridge);
    sent.length = 0;

    bridge.handleViewMessage({
      jsonrpc: "2.0",
      method: "notifications/message",
      params: { level: "info", data: "hi" }
    });
    bridge.handleViewMessage({
      jsonrpc: "2.0",
      method: "ui/notifications/request-teardown"
    });
    bridge.handleViewMessage({ id: 9, method: "tools/call" });
    bridge.handleViewMessage("ui/initialize");
    bridge.handleViewMessage(null);

    expect(sent).toEqual([]);
  });

  it("clamps reported heights to 40..800 px", () => {
    const { bridge, onSizeChanged } = createBridge();
    initialize(bridge);

    for (const height of [0, 12.4, 320.6, 5000]) {
      bridge.handleViewMessage({
        jsonrpc: "2.0",
        method: "ui/notifications/size-changed",
        params: { width: 300, height }
      });
    }
    bridge.handleViewMessage({
      jsonrpc: "2.0",
      method: "ui/notifications/size-changed",
      params: { width: 300, height: "tall" }
    });

    expect(onSizeChanged.mock.calls.map(([size]) => size)).toEqual([
      { height: 40 },
      { height: 40 },
      { height: 321 },
      { height: 800 }
    ]);
    expect(clampMcpAppHeight(801)).toBe(800);
    expect(clampMcpAppHeight(39)).toBe(40);
  });

  it("sends host-context-changed after initialization", () => {
    const { bridge, sent } = createBridge();
    initialize(bridge);
    sent.length = 0;

    bridge.notifyHostContextChanged({
      theme: "light",
      styles: { variables: { "--color-text-primary": "rgb(60 60 60)" } }
    });

    expect(sent).toEqual([
      {
        jsonrpc: "2.0",
        method: "ui/notifications/host-context-changed",
        params: {
          theme: "light",
          styles: { variables: { "--color-text-primary": "rgb(60 60 60)" } }
        }
      }
    ]);
  });

  it("sends ui/resource-teardown as a request and settles on the View's answer", async () => {
    const { bridge, sent } = createBridge();
    initialize(bridge);
    sent.length = 0;

    const done = bridge.teardown();
    expect(sent).toEqual([
      {
        jsonrpc: "2.0",
        id: "host-1",
        method: "ui/resource-teardown",
        params: {}
      }
    ]);
    bridge.handleViewMessage({ jsonrpc: "2.0", id: "host-1", result: {} });
    await done;

    // Disposed: later messages are ignored.
    bridge.handleViewMessage({ jsonrpc: "2.0", id: 5, method: "ping" });
    expect(sent).toHaveLength(1);
  });

  it("stops waiting for a silent View after the teardown timeout", async () => {
    vi.useFakeTimers();
    const { bridge } = createBridge();
    initialize(bridge);
    const done = bridge.teardown();
    await vi.advanceTimersByTimeAsync(500);
    await expect(done).resolves.toBeUndefined();
  });
});
