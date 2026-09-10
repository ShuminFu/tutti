// Host-bridge client for the web (non-Electron) build.
//
// When the static web build is embedded as an iframe inside a native host
// shell, a few desktop-only capabilities (directory / file selection) can be
// delegated to the host over `postMessage`. The host answers with the native
// dialog result. Outside an iframe (or when the host does not implement the
// capability, or does not answer in time) callers must fall back to their
// original web behaviour.
//
// Wire protocol (mirrors the host side):
//   request  (iframe -> host): { type: "tutti-host-request", capability, id, args? }
//   response (host -> iframe): { type: "tutti-host-response", id, result }
//                              { type: "tutti-host-response", id, error: "unsupported" }
//                              { type: "tutti-host-response", id, error: "<message>" }

const REQUEST_TYPE = "tutti-host-request";
const RESPONSE_TYPE = "tutti-host-response";
export const HOST_FOCUS_TYPE = "tutti-host-focus";
export const HOST_WORKBENCH_LAYOUT_TYPE = "tutti-host-workbench-layout";
const FULLSCREEN_WORKBENCH_WINDOW_SELECTOR =
  '.workbench-window-shell[data-display-mode="fullscreen"]';
const TERMINAL_FOCUS_SELECTOR =
  '.workbench-window-shell[data-focused="true"] [data-terminal-xterm] .xterm-helper-textarea';
const DEFAULT_TIMEOUT_MS = 5_000;

// Signals that the host cannot serve the request (not embedded, capability
// unsupported, or timed out). Callers use it to trigger their web fallback.
export class HostBridgeUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "HostBridgeUnavailableError";
  }
}

// True when running inside an iframe whose parent can receive host requests.
export function isHostBridgeAvailable(): boolean {
  return (
    typeof window !== "undefined" &&
    !!window.parent &&
    window.parent !== window
  );
}

function bridgeCoordinates(
  search: string = window.location.search
): { nonce: string; hostOrigin: string } | null {
  const params = new URLSearchParams(search);
  const nonce = params.get("tuttiBootstrap")?.trim();
  const hostOrigin = params.get("tuttiHostOrigin")?.trim();
  return nonce && hostOrigin ? { nonce, hostOrigin } : null;
}

// WKWebView can reactivate the iframe browsing context without restoring the
// element that actually owns keyboard input. Keep the last inner focus target
// and accept a secured one-way notification from the embedding host. The
// focused terminal textarea is a fallback for reloads that happen before the
// first focusin event is observed.
export function installHostFocusRecovery(
  windowRef: Window = window,
  documentRef: Document = document
): () => void {
  const coordinates = bridgeCoordinates(windowRef.location.search);
  if (
    !coordinates ||
    !windowRef.parent ||
    windowRef.parent === windowRef
  ) {
    return () => undefined;
  }

  let lastFocusedElement: HTMLElement | null = null;
  const parent = windowRef.parent;

  const onFocusIn = (event: Event): void => {
    if (
      isFocusTarget(event.target) &&
      event.target !== documentRef.body
    ) {
      lastFocusedElement = event.target;
    }
  };
  const onMessage = (event: MessageEvent): void => {
    const data = event.data as
      | { type?: unknown; nonce?: unknown }
      | null
      | undefined;
    if (
      !data ||
      typeof data !== "object" ||
      data.type !== HOST_FOCUS_TYPE ||
      data.nonce !== coordinates.nonce ||
      event.source !== parent ||
      event.origin !== coordinates.hostOrigin
    ) {
      return;
    }

    const target =
      (isConnectedFocusTarget(lastFocusedElement)
        ? lastFocusedElement
        : null) ??
      documentRef.querySelector<HTMLElement>(TERMINAL_FOCUS_SELECTOR);
    if (!target) {
      return;
    }
    try {
      target.focus({ preventScroll: true });
    } catch {
      // Older embedded WebKit versions do not accept FocusOptions.
      target.focus();
    }
  };

  documentRef.addEventListener("focusin", onFocusIn);
  windowRef.addEventListener("message", onMessage);
  return () => {
    documentRef.removeEventListener("focusin", onFocusIn);
    windowRef.removeEventListener("message", onMessage);
  };
}

export function installHostWorkbenchLayoutNotifications(
  windowRef: Window = window,
  documentRef: Document = document,
  MutationObserverRef: typeof MutationObserver = globalThis.MutationObserver
): () => void {
  const coordinates = bridgeCoordinates(windowRef.location.search);
  if (
    !coordinates ||
    !windowRef.parent ||
    windowRef.parent === windowRef ||
    !MutationObserverRef
  ) {
    return () => undefined;
  }

  const parent = windowRef.parent;
  let lastFullscreen: boolean | null = null;
  const publish = (): void => {
    const fullscreen = Boolean(
      documentRef.querySelector(FULLSCREEN_WORKBENCH_WINDOW_SELECTOR)
    );
    if (fullscreen === lastFullscreen) {
      return;
    }
    lastFullscreen = fullscreen;
    parent.postMessage(
      {
        fullscreen,
        nonce: coordinates.nonce,
        type: HOST_WORKBENCH_LAYOUT_TYPE
      },
      coordinates.hostOrigin === "null" ? "*" : coordinates.hostOrigin
    );
  };
  const observer = new MutationObserverRef(publish);
  observer.observe(documentRef.documentElement, {
    attributeFilter: ["data-display-mode"],
    attributes: true,
    childList: true,
    subtree: true
  });
  publish();

  return () => observer.disconnect();
}

function isFocusTarget(value: unknown): value is HTMLElement {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { focus?: unknown }).focus === "function"
  );
}

function isConnectedFocusTarget(
  value: HTMLElement | null
): value is HTMLElement {
  return Boolean(value?.isConnected);
}

let requestCounter = 0;

function nextRequestId(): string {
  requestCounter += 1;
  return `tutti-host-${Date.now()}-${requestCounter}`;
}

// Sends a capability request to the host and resolves with its result. Rejects
// with HostBridgeUnavailableError when not embedded, unsupported, or timed out;
// with a plain Error when the host reports an execution failure.
export function requestHostCapability<T>(
  capability: string,
  args: unknown[] = [],
  timeoutMs: number = DEFAULT_TIMEOUT_MS
): Promise<T> {
  const coordinates = isHostBridgeAvailable() ? bridgeCoordinates() : null;
  if (!coordinates) {
    return Promise.reject(
      new HostBridgeUnavailableError("tutti host bridge: not embedded")
    );
  }

  const id = nextRequestId();

  return new Promise<T>((resolve, reject) => {
    let settled = false;

    const cleanup = (): void => {
      window.removeEventListener("message", onMessage);
      window.clearTimeout(timer);
    };

    const onMessage = (event: MessageEvent): void => {
      const data = event.data as
        | { type?: unknown; id?: unknown; nonce?: unknown; result?: T; error?: unknown }
        | null
        | undefined;
      if (
        !data ||
        typeof data !== "object" ||
        data.type !== RESPONSE_TYPE ||
        data.id !== id ||
        data.nonce !== coordinates.nonce ||
        event.source !== window.parent ||
        event.origin !== coordinates.hostOrigin
      ) {
        return;
      }
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      if (typeof data.error === "string") {
        reject(
          data.error === "unsupported"
            ? new HostBridgeUnavailableError(
                `tutti host bridge: ${capability} unsupported`
              )
            : new Error(data.error)
        );
        return;
      }
      resolve(data.result as T);
    };

    const timer = window.setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      reject(
        new HostBridgeUnavailableError(
          `tutti host bridge: ${capability} timed out`
        )
      );
    }, timeoutMs);

    window.addEventListener("message", onMessage);
    window.parent.postMessage(
      { type: REQUEST_TYPE, capability, id, args, nonce: coordinates.nonce },
      coordinates.hostOrigin === "null" ? "*" : coordinates.hostOrigin
    );
  });
}
