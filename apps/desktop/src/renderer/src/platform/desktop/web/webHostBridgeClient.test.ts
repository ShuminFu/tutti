import assert from "node:assert/strict";
import test from "node:test";
import {
  HOST_AGENT_SESSION_READY_TYPE,
  HOST_FILE_DROP_TYPE,
  HOST_FOCUS_TYPE,
  HOST_OPEN_AGENT_SESSION_ACK_TYPE,
  HOST_OPEN_AGENT_SESSION_TYPE,
  HOST_THEME_TYPE,
  HOST_WORKBENCH_LAYOUT_TYPE,
  installHostAgentSessionBridge,
  installHostFileDropBridge,
  installHostFocusRecovery,
  installHostThemeBridge,
  installHostWorkbenchLayoutNotifications,
  requestHostCapability,
  requestHostCreateAgentSession
} from "./webHostBridgeClient.ts";

test("createAgentSession reuses the host capability channel and frozen args shape", async () => {
  const previousWindow = globalThis.window;
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const posted: Array<{ message: Record<string, unknown>; origin: string }> = [];
  const parent = {
    postMessage(message: Record<string, unknown>, origin: string) {
      posted.push({ message, origin });
      queueMicrotask(() => messageListener?.({
        data: {
          type: "tutti-host-response",
          id: message.id,
          nonce: "nonce-1",
          result: { taskId: "task-1", agentSessionId: "host-session-1" }
        },
        origin: "http://wails.localhost",
        source: parent
      } as MessageEvent));
    }
  } as unknown as WindowProxy;
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") messageListener = listener as (event: MessageEvent) => void;
    },
    clearTimeout,
    location: {
      search: "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener() {},
    setTimeout
  } as unknown as Window;
  Object.defineProperty(globalThis, "window", { configurable: true, value: windowRef });

  try {
    const result = await requestHostCreateAgentSession({
      provider: "codex",
      cwd: "/workspace/project",
      prompt: "Build the feature",
      model: "gpt-5",
      thinkingLevel: "high"
    });
    assert.deepEqual(result, {
      taskId: "task-1",
      agentSessionId: "host-session-1"
    });
    assert.equal(posted.length, 1);
    assert.equal(posted[0]?.origin, "http://wails.localhost");
    assert.equal(posted[0]?.message.type, "tutti-host-request");
    assert.equal(posted[0]?.message.capability, "createAgentSession");
    assert.equal(posted[0]?.message.nonce, "nonce-1");
    assert.deepEqual(posted[0]?.message.args, [
      {
        provider: "codex",
        cwd: "/workspace/project",
        prompt: "Build the feature",
        model: "gpt-5",
        thinkingLevel: "high"
      }
    ]);
    assert.equal(typeof posted[0]?.message.id, "string");
  } finally {
    Object.defineProperty(globalThis, "window", { configurable: true, value: previousWindow });
  }
});

test("host capability errors preserve stable error codes", async () => {
  const previousWindow = globalThis.window;
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const parent = {
    postMessage(message: { id: string }, _origin: string) {
      queueMicrotask(() => messageListener?.({
        data: {
          type: "tutti-host-response",
          id: message.id,
          nonce: "nonce-1",
          error: "Agent prompt file is too large.",
          code: "file_too_large"
        },
        origin: "http://wails.localhost",
        source: parent
      } as MessageEvent));
    }
  } as unknown as WindowProxy;
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") messageListener = listener as (event: MessageEvent) => void;
    },
    clearTimeout,
    location: {
      search: "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener() {},
    setTimeout
  } as unknown as Window;
  Object.defineProperty(globalThis, "window", { configurable: true, value: windowRef });

  try {
    await assert.rejects(
      requestHostCapability("archiveAgentPromptFile", [{}]),
      (error: Error & { code?: string }) =>
        error.message === "Agent prompt file is too large." &&
        error.code === "file_too_large"
    );
  } finally {
    Object.defineProperty(globalThis, "window", { configurable: true, value: previousWindow });
  }
});

test("embedded host file drops reuse the workspace-file composer path", () => {
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const parent = {} as WindowProxy;
  const dispatched: Array<{ type: string; data: Record<string, string> }> = [];
  const editor = {
    dispatchEvent(event: {
      type: string;
      dataTransfer: { data: Record<string, string> };
    }) {
      dispatched.push({ type: event.type, data: event.dataTransfer.data });
      return true;
    }
  };
  const detail = { querySelector: () => editor };
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") {
        messageListener = listener as (event: MessageEvent) => void;
      }
    },
    location: {
      search: "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener() {}
  } as unknown as Window;
  const documentRef = {
    elementFromPoint: () => ({ closest: () => detail })
  } as unknown as Document;
  class FakeDataTransfer {
    data: Record<string, string> = {};
    effectAllowed = "none";
    setData(type: string, value: string) {
      this.data[type] = value;
    }
  }
  class FakeDragEvent {
    type: string;
    dataTransfer: FakeDataTransfer;
    constructor(type: string, input: { dataTransfer: FakeDataTransfer }) {
      this.type = type;
      this.dataTransfer = input.dataTransfer;
    }
  }

  installHostFileDropBridge(
    windowRef,
    documentRef,
    FakeDataTransfer as unknown as typeof DataTransfer,
    FakeDragEvent as unknown as typeof DragEvent
  );
  const drop = (overrides: Record<string, unknown> = {}) => {
    const { event, ...data } = overrides;
    messageListener?.({
      data: {
        type: HOST_FILE_DROP_TYPE,
        nonce: "nonce-1",
        paths: ["/tmp/report.pdf", "/tmp/report.pdf"],
        x: 30,
        y: 40,
        ...data
      },
      origin: "http://wails.localhost",
      source: parent,
      ...((event as Record<string, unknown> | undefined) ?? {})
    } as MessageEvent);
  };
  drop({ nonce: "wrong" });
  drop({ event: { origin: "https://evil.example" } });
  drop({ event: { source: {} as WindowProxy } });
  assert.equal(dispatched.length, 0);
  drop();

  assert.equal(dispatched.length, 1);
  assert.equal(dispatched[0]?.type, "drop");
  assert.deepEqual(
    JSON.parse(
      dispatched[0]?.data["application/x-tsh-workspace-file-paths+json"] ?? ""
    ),
    {
      entries: [
        { path: "/tmp/report.pdf", name: "report.pdf", kind: "unknown" }
      ]
    }
  );
});

test("host focus recovery restores the last focused element", () => {
  const harness = createHarness();
  const remembered = focusTarget();
  const dispose = installHostFocusRecovery(
    harness.windowRef,
    harness.documentRef
  );

  harness.focusIn(remembered);
  harness.hostFocus();

  assert.equal(remembered.focusCalls.length, 1);
  assert.deepEqual(remembered.focusCalls[0], { preventScroll: true });
  assert.equal(harness.terminalFallback.focusCalls.length, 0);
  dispose();
});

test("host focus recovery falls back to the focused xterm textarea", () => {
  const harness = createHarness();
  const dispose = installHostFocusRecovery(
    harness.windowRef,
    harness.documentRef
  );

  harness.hostFocus();

  assert.equal(harness.terminalFallback.focusCalls.length, 1);
  assert.equal(
    harness.lastSelector,
    '.workbench-window-shell[data-focused="true"] [data-terminal-xterm] .xterm-helper-textarea'
  );
  dispose();
});

test("host focus recovery rejects the wrong source, origin, or nonce", () => {
  const harness = createHarness();
  const dispose = installHostFocusRecovery(
    harness.windowRef,
    harness.documentRef
  );

  harness.hostFocus({ source: {} as WindowProxy });
  harness.hostFocus({ origin: "https://evil.example" });
  harness.hostFocus({ nonce: "wrong" });

  assert.equal(harness.terminalFallback.focusCalls.length, 0);
  harness.hostFocus();
  assert.equal(harness.terminalFallback.focusCalls.length, 1);
  dispose();
});

test("host focus recovery stops after dispose", () => {
  const harness = createHarness();
  const dispose = installHostFocusRecovery(
    harness.windowRef,
    harness.documentRef
  );

  dispose();
  harness.hostFocus();

  assert.equal(harness.terminalFallback.focusCalls.length, 0);
});

test("embedded host opens only secured Agent sessions and acknowledges the result", () => {
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const parentPosts: Array<{ message: unknown; origin: string }> = [];
  const parent = {
    postMessage(message: unknown, origin: string) {
      parentPosts.push({ message, origin });
    }
  } as WindowProxy;
  const opened: string[] = [];
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") messageListener = listener as (event: MessageEvent) => void;
    },
    location: {
      search: "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener() {}
  } as unknown as Window;
  installHostAgentSessionBridge((id) => {
    opened.push(id);
    return id === "session-1";
  }, windowRef);
  assert.deepEqual(parentPosts, [{
    message: { type: HOST_AGENT_SESSION_READY_TYPE, nonce: "nonce-1" },
    origin: "http://wails.localhost"
  }]);
  const send = (overrides: Record<string, unknown> = {}) => messageListener?.({
    data: {
      type: HOST_OPEN_AGENT_SESSION_TYPE,
      nonce: "nonce-1",
      requestId: "request-1",
      agentSessionId: " session-1 ",
      ...(overrides.data as object | undefined)
    },
    origin: "http://wails.localhost",
    source: parent,
    ...overrides
  } as MessageEvent);

  send({ origin: "https://evil.example" });
  send({ source: {} as WindowProxy });
  send({ data: { nonce: "wrong" } });
  send();
  send({ data: { requestId: "request-2", agentSessionId: "session-missing" } });
  assert.deepEqual(opened, ["session-1", "session-missing"]);
  assert.deepEqual(parentPosts.slice(1), [
    {
      message: {
        type: HOST_OPEN_AGENT_SESSION_ACK_TYPE,
        nonce: "nonce-1",
        requestId: "request-1",
        opened: true
      },
      origin: "http://wails.localhost"
    },
    {
      message: {
        type: HOST_OPEN_AGENT_SESSION_ACK_TYPE,
        nonce: "nonce-1",
        requestId: "request-2",
        opened: false
      },
      origin: "http://wails.localhost"
    }
  ]);
});

test("embedded workbench reports fullscreen layout changes to its host", () => {
  const parentPosts: Array<{ message: unknown; origin: string }> = [];
  let fullscreen = false;
  const mutationListeners: Array<() => void> = [];
  let disconnected = false;
  const parent = {
    postMessage(message: unknown, origin: string) {
      parentPosts.push({ message, origin });
    }
  } as WindowProxy;
  const windowRef = {
    location: {
      search:
        "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent
  } as unknown as Window;
  const documentRef = {
    documentElement: {},
    querySelector() {
      return fullscreen ? {} : null;
    }
  } as unknown as Document;
  class FakeMutationObserver {
    constructor(listener: () => void) {
      mutationListeners.push(listener);
    }
    disconnect() {
      disconnected = true;
    }
    observe() {}
  }

  const dispose = installHostWorkbenchLayoutNotifications(
    windowRef,
    documentRef,
    FakeMutationObserver as unknown as typeof MutationObserver
  );
  fullscreen = true;
  mutationListeners[0]?.();
  mutationListeners[0]?.();

  assert.deepEqual(parentPosts, [
    {
      message: {
        fullscreen: false,
        nonce: "nonce-1",
        type: HOST_WORKBENCH_LAYOUT_TYPE
      },
      origin: "http://wails.localhost"
    },
    {
      message: {
        fullscreen: true,
        nonce: "nonce-1",
        type: HOST_WORKBENCH_LAYOUT_TYPE
      },
      origin: "http://wails.localhost"
    }
  ]);

  dispose();
  assert.equal(disconnected, true);
});

test("embedded theme follows only secured host appearance pushes", () => {
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const parent = {} as WindowProxy;
  const applied: string[] = [];
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") {
        messageListener = listener as (event: MessageEvent) => void;
      }
    },
    location: {
      search:
        "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener(type: string) {
      if (type === "message") {
        messageListener = null;
      }
    }
  } as unknown as Window;

  const dispose = installHostThemeBridge(
    (appearance) => applied.push(appearance),
    windowRef
  );
  const send = (overrides: Record<string, unknown> = {}) => {
    const { data, ...event } = overrides;
    messageListener?.({
      data: {
        type: HOST_THEME_TYPE,
        nonce: "nonce-1",
        appearance: "light",
        ...(data as object | undefined)
      },
      origin: "http://wails.localhost",
      source: parent,
      ...event
    } as MessageEvent);
  };

  send({ data: { nonce: "wrong" } });
  send({ origin: "https://evil.example" });
  send({ source: {} as WindowProxy });
  send({ data: { appearance: "sepia" } });
  assert.deepEqual(applied, []);

  send();
  send({ data: { appearance: "dark" } });
  assert.deepEqual(applied, ["light", "dark"]);

  dispose();
  send();
  assert.deepEqual(applied, ["light", "dark"]);
});

test("embedded theme bridge forwards host role tokens and drops bad payloads", () => {
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const parent = {} as WindowProxy;
  const applied: string[] = [];
  const tokenPushes: Array<{ tokens: unknown; themeId: unknown }> = [];
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") {
        messageListener = listener as (event: MessageEvent) => void;
      }
    },
    location: {
      search:
        "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener(type: string) {
      if (type === "message") {
        messageListener = null;
      }
    }
  } as unknown as Window;

  installHostThemeBridge(
    (appearance) => applied.push(appearance),
    windowRef,
    (tokens, themeId) => tokenPushes.push({ tokens, themeId })
  );
  const send = (data: Record<string, unknown>) => {
    messageListener?.({
      data: {
        type: HOST_THEME_TYPE,
        nonce: "nonce-1",
        appearance: "light",
        ...data
      },
      origin: "http://wails.localhost",
      source: parent
    } as MessageEvent);
  };

  send({ themeId: "rnd-ocean", tokens: { accent: "#74ADE8" } });
  assert.deepEqual(tokenPushes, [
    { tokens: { accent: "#74ADE8" }, themeId: "rnd-ocean" }
  ]);

  // An appearance-only message stays valid, and a tokens field that is not a
  // plain record is dropped without touching the palette.
  send({});
  send({ tokens: ["#74ADE8"] });
  send({ tokens: "accent:#74ADE8" });
  send({ tokens: null });
  assert.equal(tokenPushes.length, 1);
  assert.deepEqual(applied, ["light", "light", "light", "light", "light"]);
});

test("embedded theme bridge forwards host window insets and drops bad payloads", () => {
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const parent = {} as WindowProxy;
  const applied: string[] = [];
  const insetPushes: unknown[] = [];
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") {
        messageListener = listener as (event: MessageEvent) => void;
      }
    },
    location: {
      search:
        "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener(type: string) {
      if (type === "message") {
        messageListener = null;
      }
    }
  } as unknown as Window;

  installHostThemeBridge(
    (appearance) => applied.push(appearance),
    windowRef,
    undefined,
    (insets) => insetPushes.push(insets)
  );
  const send = (data: Record<string, unknown>) => {
    messageListener?.({
      data: {
        type: HOST_THEME_TYPE,
        nonce: "nonce-1",
        appearance: "light",
        ...data
      },
      origin: "http://wails.localhost",
      source: parent
    } as MessageEvent);
  };

  const insets = {
    platform: "mac",
    controlsTop: 62,
    controlsLeft: 90,
    controlsGap: 8,
    captionRight: 0
  };
  send({ insets });
  assert.deepEqual(insetPushes, [insets]);

  // An appearance-only message stays valid, and an insets field that is not a
  // plain record of a platform string plus non-negative finite numbers is
  // dropped without touching the layout.
  send({});
  send({ insets: [62, 90] });
  send({ insets: "controls-top:62" });
  send({ insets: null });
  send({ insets: { controlsTop: 62 } });
  send({ insets: { platform: "mac", controlsTop: -1 } });
  send({ insets: { platform: "mac", controlsTop: Number.NaN } });
  send({ insets: { platform: "mac", controlsTop: "62" } });
  send({ insets: { platform: 42, controlsTop: 62 } });
  assert.equal(insetPushes.length, 1);
  assert.equal(applied.length, 10);
});

test("host theme bridge stays inert outside the embedded host", () => {
  const windowRef = {
    addEventListener() {
      throw new Error("must not listen outside the embedded host");
    },
    location: { search: "" },
    parent: undefined,
    removeEventListener() {}
  } as unknown as Window;

  installHostThemeBridge(() => {
    throw new Error("must not apply a theme outside the embedded host");
  }, windowRef)();
});

interface FakeFocusTarget {
  focus(options?: FocusOptions): void;
  focusCalls: Array<FocusOptions | undefined>;
  isConnected: boolean;
}

function focusTarget(): FakeFocusTarget {
  const focusCalls: Array<FocusOptions | undefined> = [];
  return {
    focus(options) {
      focusCalls.push(options);
    },
    focusCalls,
    isConnected: true
  };
}

function createHarness() {
  const parent = {} as WindowProxy;
  let focusInListener: ((event: Event) => void) | null = null;
  let messageListener: ((event: MessageEvent) => void) | null = null;
  const terminalFallback = focusTarget();
  const body = {} as HTMLElement;
  let lastSelector = "";

  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") {
        messageListener = listener as (event: MessageEvent) => void;
      }
    },
    location: {
      search:
        "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    },
    parent,
    removeEventListener(type: string) {
      if (type === "message") {
        messageListener = null;
      }
    }
  } as unknown as Window;

  const documentRef = {
    activeElement: body,
    addEventListener(type: string, listener: EventListener) {
      if (type === "focusin") {
        focusInListener = listener;
      }
    },
    body,
    querySelector(selector: string) {
      lastSelector = selector;
      return terminalFallback;
    },
    removeEventListener(type: string) {
      if (type === "focusin") {
        focusInListener = null;
      }
    }
  } as unknown as Document;

  return {
    documentRef,
    focusIn(target: FakeFocusTarget) {
      focusInListener?.({ target } as unknown as Event);
    },
    get lastSelector() {
      return lastSelector;
    },
    hostFocus(
      overrides: {
        nonce?: string;
        origin?: string;
        source?: MessageEventSource | null;
      } = {}
    ) {
      messageListener?.({
        data: {
          nonce: overrides.nonce ?? "nonce-1",
          type: HOST_FOCUS_TYPE
        },
        origin: overrides.origin ?? "http://wails.localhost",
        source: overrides.source === undefined ? parent : overrides.source
      } as MessageEvent);
    },
    terminalFallback,
    windowRef
  };
}
