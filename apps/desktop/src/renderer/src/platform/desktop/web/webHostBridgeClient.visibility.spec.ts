import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  isHostPanelVisible,
  resetHostPanelVisibilityForTests
} from "@tutti-os/agent-gui/host-panel-visibility";
import { createWebDesktopApi } from "./createWebDesktopApi";

const HOST_ORIGIN = "http://wails.localhost";

describe("tutti-host-visibility from the embedded web startup", () => {
  const parent = {
    postMessage() {
      return undefined;
    }
  };
  let listeners: Array<(event: MessageEvent) => void> = [];

  beforeEach(() => {
    resetHostPanelVisibilityForTests();
    listeners = [];
    Object.defineProperty(window, "parent", {
      configurable: true,
      value: parent
    });
    window.history.replaceState(
      null,
      "",
      `/?tuttiBootstrap=nonce-1&tuttiHostOrigin=${encodeURIComponent(HOST_ORIGIN)}`
    );
    const originalAddEventListener = window.addEventListener.bind(window);
    vi.spyOn(window, "addEventListener").mockImplementation(
      (type, listener, options) => {
        if (type === "message" && typeof listener === "function") {
          listeners.push(listener as (event: MessageEvent) => void);
        }
        originalAddEventListener(type, listener, options);
      }
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
    resetHostPanelVisibilityForTests();
    Object.defineProperty(window, "parent", {
      configurable: true,
      value: window
    });
    window.history.replaceState(null, "", "/");
  });

  function deliver(
    data: Record<string, unknown>,
    overrides: Partial<MessageEvent> = {}
  ): void {
    const event = {
      data,
      origin: HOST_ORIGIN,
      source: parent,
      ...overrides
    } as MessageEvent;
    for (const listener of listeners) listener(event);
  }

  it("accepts only a secured boolean visible and rejects the rest", () => {
    expect(isHostPanelVisible()).toBe(true);

    // 监听器来自嵌入启动路径 createWebDesktopApi，不直接调 install 函数。
    const api = createWebDesktopApi();
    void api.runtime.getBackendConfig().catch(() => undefined);
    expect(listeners.length).toBeGreaterThan(0);

    deliver({
      type: "tutti-host-visibility",
      nonce: "wrong",
      visible: false
    });
    deliver(
      { type: "tutti-host-visibility", nonce: "nonce-1", visible: false },
      { source: {} as WindowProxy }
    );
    deliver(
      { type: "tutti-host-visibility", nonce: "nonce-1", visible: false },
      { origin: "https://evil.example" }
    );
    deliver({
      type: "tutti-host-visibility",
      nonce: "nonce-1",
      visible: "false"
    });
    deliver({ type: "tutti-host-visibility", nonce: "nonce-1", visible: 0 });
    deliver({ type: "tutti-host-visibility", nonce: "nonce-1" });
    expect(isHostPanelVisible()).toBe(true);

    deliver({
      type: "tutti-host-visibility",
      nonce: "nonce-1",
      visible: false
    });
    expect(isHostPanelVisible()).toBe(false);

    deliver({
      type: "tutti-host-visibility",
      nonce: "nonce-1",
      visible: true
    });
    expect(isHostPanelVisible()).toBe(true);
  });
});
