import assert from "node:assert/strict";
import test from "node:test";
import {
  HOST_AGENT_SESSION_READY_TYPE,
  HOST_FILE_DROP_TYPE,
  HOST_FOCUS_TYPE,
  HOST_OPEN_AGENT_SESSION_ACK_TYPE,
  HOST_OPEN_AGENT_SESSION_TYPE,
  HOST_WORKBENCH_LAYOUT_TYPE,
  installHostAgentSessionBridge,
  installHostFileDropBridge,
  installHostFocusRecovery,
  installHostWorkbenchLayoutNotifications
} from "./webHostBridgeClient.ts";

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
