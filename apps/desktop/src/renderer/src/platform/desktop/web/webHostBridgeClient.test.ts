import assert from "node:assert/strict";
import test from "node:test";
import {
  HOST_FOCUS_TYPE,
  installHostFocusRecovery
} from "./webHostBridgeClient.ts";

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
