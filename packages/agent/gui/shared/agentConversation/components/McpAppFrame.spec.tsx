import { act, render, screen } from "@testing-library/react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock
} from "vitest";
import { McpAppFrame } from "./McpAppFrame";

const SHELL_HTML =
  "<!doctype html><html><head><title>shell</title></head><body></body></html>";

function renderFrame() {
  const result = render(
    <McpAppFrame
      html={SHELL_HTML}
      csp={{
        resourceDomains: ["https://lib.baomitu.com"],
        connectDomains: []
      }}
      prefersBorder={false}
      title="Tool view: Sales"
      toolName="show_widget"
      toolArguments={{ title: "Sales", widget_code: "<svg></svg>" }}
      toolResult={{ content: [{ type: "text", text: "Shown" }] }}
    />
  );
  const iframe = screen.getByTitle("Tool view: Sales") as HTMLIFrameElement;
  const viewWindow = iframe.contentWindow;
  if (!viewWindow) {
    throw new Error("iframe has no contentWindow");
  }
  const posted = vi.spyOn(viewWindow, "postMessage") as unknown as Mock<
    (message: unknown, targetOrigin: string) => void
  >;
  return { ...result, iframe, viewWindow, posted };
}

function sentMessages(
  posted: Mock<(message: unknown, targetOrigin: string) => void>
): unknown[] {
  return posted.mock.calls.map((call) => call[0]);
}

function fromView(source: Window | null, data: unknown): void {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { data, source }));
  });
}

function handshake(viewWindow: Window): void {
  fromView(viewWindow, {
    jsonrpc: "2.0",
    id: 1,
    method: "ui/initialize",
    params: {
      appInfo: { name: "shell", version: "1" },
      appCapabilities: {},
      protocolVersion: "2026-01-26"
    }
  });
  fromView(viewWindow, {
    jsonrpc: "2.0",
    method: "ui/notifications/initialized"
  });
}

describe("McpAppFrame", () => {
  beforeEach(() => {
    document.documentElement.dataset.theme = "light";
    document.documentElement.style.setProperty(
      "--text-primary",
      "rgb(60 60 60)"
    );
  });

  afterEach(() => {
    delete document.documentElement.dataset.theme;
    document.documentElement.style.removeProperty("--text-primary");
  });

  it("renders a scripts-only sandbox whose srcdoc starts with the CSP meta", () => {
    const { iframe } = renderFrame();

    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts");
    expect(iframe.getAttribute("referrerpolicy")).toBe("no-referrer");
    const srcdoc = iframe.getAttribute("srcdoc") ?? "";
    expect(srcdoc).toMatch(
      /^<!doctype html><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' https:\/\/lib\.baomitu\.com; /
    );
    expect(srcdoc).toContain("connect-src 'none'");
  });

  it("completes the handshake in protocol order with the host context", () => {
    const { viewWindow, posted } = renderFrame();

    handshake(viewWindow);

    const messages = sentMessages(posted) as Array<Record<string, unknown>>;
    expect(messages.map((message) => message.method ?? "result")).toEqual([
      "result",
      "ui/notifications/tool-input",
      "ui/notifications/tool-result"
    ]);
    expect(posted.mock.calls.every((call) => call[1] === "*")).toBe(true);
    const initializeResult = messages[0]?.result as
      | { hostContext: Record<string, unknown> }
      | undefined;
    const hostContext = initializeResult?.hostContext ?? {};
    expect(hostContext).toMatchObject({
      theme: "light",
      displayMode: "inline",
      availableDisplayModes: ["inline"],
      containerDimensions: { maxHeight: 800 },
      platform: "desktop",
      toolInfo: { tool: { name: "show_widget" } }
    });
    expect(
      (hostContext.styles as { variables: Record<string, string> }).variables[
        "--color-text-primary"
      ]
    ).toBe("rgb(60 60 60)");
    expect(messages[1]).toEqual({
      jsonrpc: "2.0",
      method: "ui/notifications/tool-input",
      params: { arguments: { title: "Sales", widget_code: "<svg></svg>" } }
    });
  });

  it("ignores messages that do not come from its own iframe window", () => {
    const { posted } = renderFrame();
    const otherFrame = document.createElement("iframe");
    document.body.appendChild(otherFrame);
    const initialize = {
      jsonrpc: "2.0",
      id: 1,
      method: "ui/initialize",
      params: {}
    };

    fromView(window, initialize);
    fromView(otherFrame.contentWindow, initialize);
    fromView(null, initialize);

    expect(posted).not.toHaveBeenCalled();
    otherFrame.remove();
  });

  it("answers unsupported requests with -32601", () => {
    const { viewWindow, posted } = renderFrame();
    handshake(viewWindow);
    posted.mockClear();

    fromView(viewWindow, {
      jsonrpc: "2.0",
      id: 42,
      method: "tools/call",
      params: { name: "show_widget", arguments: {} }
    });

    expect(sentMessages(posted)).toEqual([
      {
        jsonrpc: "2.0",
        id: 42,
        error: {
          code: -32601,
          message: "Method not supported by this host: tools/call"
        }
      }
    ]);
  });

  it("clamps the iframe height reported through size-changed", () => {
    const { viewWindow, iframe } = renderFrame();
    handshake(viewWindow);
    expect(iframe.style.height).toBe("40px");

    const report = (height: number) =>
      fromView(viewWindow, {
        jsonrpc: "2.0",
        method: "ui/notifications/size-changed",
        params: { width: 500, height }
      });

    report(260);
    expect(iframe.style.height).toBe("260px");
    report(4000);
    expect(iframe.style.height).toBe("800px");
    report(3);
    expect(iframe.style.height).toBe("40px");
  });

  it("sends host-context-changed when the host theme switches", async () => {
    const { viewWindow, posted } = renderFrame();
    handshake(viewWindow);
    posted.mockClear();

    await act(async () => {
      document.documentElement.style.setProperty(
        "--text-primary",
        "rgb(255 255 255)"
      );
      document.documentElement.dataset.theme = "dark";
      await Promise.resolve();
    });

    const messages = sentMessages(posted) as Array<Record<string, unknown>>;
    expect(messages).toHaveLength(1);
    expect(messages[0]).toMatchObject({
      jsonrpc: "2.0",
      method: "ui/notifications/host-context-changed",
      params: {
        theme: "dark",
        styles: {
          variables: expect.objectContaining({
            "--color-text-primary": "rgb(255 255 255)"
          })
        }
      }
    });
  });

  it("sends ui/resource-teardown when unmounted", () => {
    const { viewWindow, posted, unmount } = renderFrame();
    handshake(viewWindow);
    posted.mockClear();

    unmount();

    expect(sentMessages(posted)).toEqual([
      {
        jsonrpc: "2.0",
        id: "host-1",
        method: "ui/resource-teardown",
        params: {}
      }
    ]);
  });

  it("drops the frame and stops the bridge if the View navigates away", () => {
    const { viewWindow, iframe, posted } = renderFrame();

    act(() => {
      iframe.dispatchEvent(new Event("load"));
    });
    act(() => {
      iframe.dispatchEvent(new Event("load"));
    });
    fromView(viewWindow, { jsonrpc: "2.0", id: 1, method: "ping" });

    expect(screen.queryByTitle("Tool view: Sales")).toBeNull();
    expect(posted).not.toHaveBeenCalled();
  });
});
