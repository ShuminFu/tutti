// Host-bridge client for the web (non-Electron) build.
//
// When the static web build is embedded as an iframe inside a native host
// shell, a few desktop-only capabilities (directory / file selection) can be
// delegated to the host over `postMessage`. The host answers with the native
// dialog result. Interactive pickers have a bounded acknowledgement wait;
// after acceptance, the native dialog owns completion or user cancellation.
//
// Wire protocol (mirrors the host side):
//   request  (iframe -> host): { type: "tutti-host-request", capability, id, args?, nonce }
//   response (host -> iframe): { type: "tutti-host-response", id, result }
//                              { type: "tutti-host-response", id, error: "unsupported" }
//                              { type: "tutti-host-response", id, error: "<message>", code? }
//
// createAgentSession args[0] = { provider, cwd, prompt, model?, thinkingLevel? }
// createAgentSession result  = { taskId, agentSessionId }
//
// 会话配对（补丁 0103，宿主票 03 实现）：
// listPeerPairs   args[] = []
//                 result = { pairs: [{ pairId, a: Endpoint, b: Endpoint }] }
//                 Endpoint = { taskId, sessionId, provider, alias, title, cwd, status }
// createPeerPair  args[0] = { from, to, aliasForFrom, aliasForTo, relaunchClosed }
//                 result  = { pairId, relaunchedTaskId?, relaunchedSessionId? }
// deletePeerPair  args[0] = { taskId, pairId }
//                 result  = { ok: true }
//
// 配对请求就地审批（补丁 0116）：
// listPeerRequests  args[0] = { agentSessionId, status: "pending" }
//                   result  = { requests: [{ id, status, reason, createdAt, from: Endpoint, to: Endpoint }] }
// decidePeerRequest args[0] = { requestId, decision: "approve" | "reject" }
//                   result  = { request: {...} }
//
// 会话栏圆点按宿主任务行终态隐藏（补丁 0122）：
// sessionLiveness args[0] = { agentSessionIds: string[] }（≤200）
//                 result  = { sessions: { [agentSessionId]: { state, taskId, status } } }
//                 state ∈ "live" | "closed" | "unknown"；每个问到的 id 都在结果里。
// 别名由宿主算，iframe 传空串；后端拒绝时回 { error: <中文文案>, code }。

import { writeWorkspaceFileDropData } from "@tutti-os/agent-gui/workspace-file-drop";

const REQUEST_TYPE = "tutti-host-request";
const RESPONSE_TYPE = "tutti-host-response";
export const HOST_FOCUS_TYPE = "tutti-host-focus";
export const HOST_FILE_DROP_TYPE = "tutti-host-file-drop";
export const HOST_OPEN_AGENT_SESSION_TYPE = "tutti-host-open-agent-session";
export const HOST_AGENT_SESSION_READY_TYPE = "tutti-host-agent-session-ready";
export const HOST_OPEN_AGENT_SESSION_ACK_TYPE = "tutti-host-open-agent-session-ack";
export const HOST_WORKBENCH_LAYOUT_TYPE = "tutti-host-workbench-layout";
export const HOST_THEME_TYPE = "tutti-host-theme";
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

export function installHostAgentSessionBridge(
  onOpen: (agentSessionId: string) => boolean,
  windowRef: Window = window
): () => void {
  const coordinates = bridgeCoordinates(windowRef.location.search);
  if (!coordinates || !windowRef.parent || windowRef.parent === windowRef) {
    return () => undefined;
  }
  const parent = windowRef.parent;
  const onMessage = (event: MessageEvent): void => {
    const data = event.data as
      | {
          type?: unknown;
          nonce?: unknown;
          requestId?: unknown;
          agentSessionId?: unknown;
        }
      | null
      | undefined;
    if (
      !data ||
      typeof data !== "object" ||
      data.type !== HOST_OPEN_AGENT_SESSION_TYPE ||
      data.nonce !== coordinates.nonce ||
      event.source !== parent ||
      event.origin !== coordinates.hostOrigin ||
      typeof data.requestId !== "string" ||
      !data.requestId ||
      typeof data.agentSessionId !== "string" ||
      !data.agentSessionId.trim()
    ) {
      return;
    }
    let opened = false;
    try {
      opened = onOpen(data.agentSessionId.trim());
    } catch {
      // Activation failures are reported to the host so it can retry.
    }
    parent.postMessage(
      {
        type: HOST_OPEN_AGENT_SESSION_ACK_TYPE,
        nonce: coordinates.nonce,
        requestId: data.requestId,
        opened
      },
      coordinates.hostOrigin
    );
  };
  windowRef.addEventListener("message", onMessage);
  parent.postMessage(
    { type: HOST_AGENT_SESSION_READY_TYPE, nonce: coordinates.nonce },
    coordinates.hostOrigin
  );
  return () => windowRef.removeEventListener("message", onMessage);
}

// The embedding host owns dark/light for the embedded build. The initial
// appearance already arrives on the iframe URL (see theme/runtime.ts); this
// bridge keeps it in sync when the host switches theme afterwards, using the
// same source/origin/nonce triple as every other host-to-iframe notification.
// `applyHostThemeTokens` is optional so an appearance-only message — the shape
// this bridge shipped with — keeps working unchanged. Tokens that are not a
// plain record of strings are dropped silently; the appearance still applies.
// `applyHostWindowInsets` (issue 04) rides the same message: the host pushes
// the window safe area whenever it changes (page zoom included). A malformed
// `insets` field is dropped on its own — the appearance and the palette in the
// same message still apply.
export function installHostThemeBridge(
  applyHostThemeAppearance: (appearance: "light" | "dark") => void,
  windowRef: Window = window,
  applyHostThemeTokens?: (tokens: unknown, themeId: unknown) => void,
  applyHostWindowInsets?: (insets: unknown) => void
): () => void {
  const coordinates = bridgeCoordinates(windowRef.location.search);
  if (!coordinates || !windowRef.parent || windowRef.parent === windowRef) {
    return () => undefined;
  }

  const parent = windowRef.parent;
  const onMessage = (event: MessageEvent): void => {
    const data = event.data as
      | {
          type?: unknown;
          nonce?: unknown;
          appearance?: unknown;
          themeId?: unknown;
          tokens?: unknown;
          insets?: unknown;
        }
      | null
      | undefined;
    if (
      !data ||
      typeof data !== "object" ||
      data.type !== HOST_THEME_TYPE ||
      data.nonce !== coordinates.nonce ||
      event.source !== parent ||
      event.origin !== coordinates.hostOrigin ||
      (data.appearance !== "light" && data.appearance !== "dark")
    ) {
      return;
    }
    applyHostThemeAppearance(data.appearance);
    if (applyHostThemeTokens && "tokens" in data) {
      const tokens = data.tokens;
      if (tokens && typeof tokens === "object" && !Array.isArray(tokens)) {
        applyHostThemeTokens(tokens, data.themeId);
      }
    }
    if (applyHostWindowInsets && "insets" in data) {
      const insets = data.insets;
      // Shape gate here, value gate in theme/hostWindowInsets.ts: only a plain
      // record whose platform is a string and whose insets are finite,
      // non-negative numbers survives both.
      if (
        insets &&
        typeof insets === "object" &&
        !Array.isArray(insets) &&
        isHostWindowInsetsPayload(insets as Record<string, unknown>)
      ) {
        applyHostWindowInsets(insets);
      }
    }
  };

  windowRef.addEventListener("message", onMessage);
  return () => windowRef.removeEventListener("message", onMessage);
}

function isHostWindowInsetsPayload(insets: Record<string, unknown>): boolean {
  if (typeof insets.platform !== "string") {
    return false;
  }
  for (const [key, value] of Object.entries(insets)) {
    if (key === "platform") {
      continue;
    }
    if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
      return false;
    }
  }
  return true;
}

export function installHostFileDropBridge(
  windowRef: Window = window,
  documentRef: Document = document,
  DataTransferRef: typeof DataTransfer = globalThis.DataTransfer,
  DragEventRef: typeof DragEvent = globalThis.DragEvent
): () => void {
  const coordinates = bridgeCoordinates(windowRef.location.search);
  if (
    !coordinates ||
    !windowRef.parent ||
    windowRef.parent === windowRef ||
    !DataTransferRef ||
    !DragEventRef
  ) {
    return () => undefined;
  }

  const parent = windowRef.parent;
  const onMessage = (event: MessageEvent): void => {
    const data = event.data as
      | {
          type?: unknown;
          nonce?: unknown;
          paths?: unknown;
          x?: unknown;
          y?: unknown;
        }
      | null
      | undefined;
    if (
      !data ||
      typeof data !== "object" ||
      data.type !== HOST_FILE_DROP_TYPE ||
      data.nonce !== coordinates.nonce ||
      event.source !== parent ||
      event.origin !== coordinates.hostOrigin
    ) {
      return;
    }
    const x = Number(data.x);
    const y = Number(data.y);
    const paths = Array.isArray(data.paths)
      ? data.paths.filter(
          (path): path is string =>
            typeof path === "string" && path.trim() !== ""
        )
      : [];
    if (!Number.isFinite(x) || !Number.isFinite(y) || paths.length === 0) {
      return;
    }
    const detail = documentRef
      .elementFromPoint(x, y)
      ?.closest?.("#agent-gui-detail");
    const editor = detail?.querySelector<HTMLElement>(
      '.agent-gui-node__rich-text-editor-content [contenteditable="true"]'
    );
    if (!editor) {
      return;
    }
    const transfer = new DataTransferRef();
    writeWorkspaceFileDropData(
      transfer,
      paths.map((path) => ({ path, name: "", kind: "unknown" as const }))
    );
    editor.dispatchEvent(
      new DragEventRef("drop", {
        bubbles: true,
        cancelable: true,
        clientX: x,
        clientY: y,
        dataTransfer: transfer
      })
    );
  };

  windowRef.addEventListener("message", onMessage);
  return () => windowRef.removeEventListener("message", onMessage);
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
// with a plain Error when the host reports an execution failure. Picker ACKs
// clear only the acceptance timer, never the final response listener.
export function requestHostCapability<T>(
  capability: string,
  args: unknown[] = [],
  timeoutMs: number = DEFAULT_TIMEOUT_MS
): Promise<T> {
  const interactive = capability === "selectUploadFiles" || capability === "selectDirectory";
  const failure = (message: string, code: string): Error =>
    Object.assign(new HostBridgeUnavailableError(message), { code });
  const coordinates = isHostBridgeAvailable() ? bridgeCoordinates() : null;
  if (!coordinates) {
    return Promise.reject(failure(interactive ? "宿主连接不可用，请重新打开工作台后重试" : "tutti host bridge: not embedded", "host_bridge_unavailable"));
  }
  const id = nextRequestId();
  const startedAt = Date.now();
  const log = (stage: string, code?: string): void => {
    if (interactive) console.debug("[DinTalDock picker]", {
      requestId: id, capability, stage, elapsedMs: Date.now() - startedAt, code
    });
  };
  return new Promise<T>((resolve, reject) => {
    let settled = false;
    let accepted = false;
    let timer: ReturnType<typeof window.setTimeout> | undefined;
    const cleanup = (): void => {
      window.removeEventListener("message", onMessage);
      window.removeEventListener("pagehide", onPageHide);
      window.clearTimeout(timer);
    };
    const finishError = (error: Error & { code?: string }): void => {
      if (settled) return;
      settled = true;
      cleanup();
      log(error.code === "host_request_cancelled" ? "cancelled" : "failed", error.code);
      reject(error);
    };
    const onPageHide = (): void => finishError(failure(
      "文件选择页面已关闭，请重新打开后重试", "host_request_cancelled"
    ));
    const onMessage = (event: MessageEvent): void => {
      const data = event.data as
        | { type?: unknown; id?: unknown; nonce?: unknown; result?: T; error?: unknown; code?: unknown }
        | null | undefined;
      if (!data || typeof data !== "object" ||
          (data.type !== RESPONSE_TYPE && data.type !== "tutti-host-request-accepted") ||
          data.id !== id || data.nonce !== coordinates.nonce ||
          event.source !== window.parent || event.origin !== coordinates.hostOrigin || settled) return;
      if (data.type === "tutti-host-request-accepted") {
        if (interactive && !accepted) {
          accepted = true;
          window.clearTimeout(timer);
          log("accepted");
        }
        return;
      }
      if (typeof data.error === "string") {
        const error = data.error === "unsupported"
          ? failure(interactive ? "宿主不支持此文件选择操作" : `tutti host bridge: ${capability} unsupported`, "host_capability_unsupported")
          : new Error(data.error);
        if (typeof data.code === "string" && data.code.trim()) Object.assign(error, { code: data.code.trim() });
        finishError(error);
        return;
      }
      settled = true;
      cleanup();
      log("completed");
      resolve(data.result as T);
    };
    timer = window.setTimeout(() => finishError(failure(
      interactive ? "宿主文件选择器未响应，请重试" : `tutti host bridge: ${capability} timed out`, "host_request_timeout"
    )), timeoutMs);
    window.addEventListener("message", onMessage);
    if (interactive) window.addEventListener("pagehide", onPageHide);
    log("sent");
    try {
      window.parent.postMessage(
        { type: REQUEST_TYPE, capability, id, args, nonce: coordinates.nonce },
        coordinates.hostOrigin === "null" ? "*" : coordinates.hostOrigin
      );
    } catch (error) {
      finishError(failure(String(error), "host_bridge_unavailable"));
    }
  });
}

export interface HostCreateAgentSessionArgs {
  provider: string;
  cwd: string;
  prompt: string;
  model?: string;
  thinkingLevel?: string;
}

export interface HostCreateAgentSessionResult {
  taskId: string;
  agentSessionId: string;
}

// 创建会话需等待 runtime 就绪；超时预算覆盖宿主的 120s 等待和 HTTP 开销。
export function requestHostCreateAgentSession(
  args: HostCreateAgentSessionArgs
): Promise<HostCreateAgentSessionResult> {
  return requestHostCapability<HostCreateAgentSessionResult>(
    "createAgentSession",
    [args],
    150_000
  ).then((result) => {
    const taskId =
      typeof result?.taskId === "string" ? result.taskId.trim() : "";
    const agentSessionId =
      typeof result?.agentSessionId === "string"
        ? result.agentSessionId.trim()
        : "";
    if (!taskId || !agentSessionId) {
      throw new Error(
        "tutti host bridge: createAgentSession result missing taskId or agentSessionId"
      );
    }
    return { taskId, agentSessionId };
  });
}

export interface HostPeerPairEndpoint {
  alias: string;
  cwd: string;
  provider: string;
  sessionId: string;
  status: string;
  taskId: string;
  title: string;
}

export interface HostPeerPair {
  a: HostPeerPairEndpoint;
  b: HostPeerPairEndpoint;
  pairId: string;
}

export interface HostCreatePeerPairArgs {
  aliasForFrom: string;
  aliasForTo: string;
  from: string;
  relaunchClosed: boolean;
  to: string;
}

export interface HostCreatePeerPairResult {
  pairId: string;
  relaunchedSessionId?: string;
  relaunchedTaskId?: string;
}

// 三个配对能力都复用 requestHostCapability（5s / nonce / 同源校验）。
export function requestHostListPeerPairs(): Promise<{ pairs: HostPeerPair[] }> {
  return requestHostCapability<{ pairs?: HostPeerPair[] }>(
    "listPeerPairs",
    []
  ).then((result) => ({
    pairs: Array.isArray(result?.pairs) ? result.pairs : []
  }));
}

export function requestHostCreatePeerPair(
  args: HostCreatePeerPairArgs
): Promise<HostCreatePeerPairResult> {
  return requestHostCapability<HostCreatePeerPairResult>("createPeerPair", [
    args
  ]).then((result) => {
    const pairId = typeof result?.pairId === "string" ? result.pairId.trim() : "";
    if (!pairId) {
      throw new Error("tutti host bridge: createPeerPair result missing pairId");
    }
    return {
      pairId,
      ...(typeof result?.relaunchedSessionId === "string"
        ? { relaunchedSessionId: result.relaunchedSessionId }
        : {}),
      ...(typeof result?.relaunchedTaskId === "string"
        ? { relaunchedTaskId: result.relaunchedTaskId }
        : {})
    };
  });
}

export function requestHostDeletePeerPair(args: {
  pairId: string;
  taskId: string;
}): Promise<unknown> {
  return requestHostCapability<unknown>("deletePeerPair", [args]);
}

export interface HostPeerRequest {
  createdAt: string;
  from: HostPeerPairEndpoint;
  id: string;
  reason: string;
  status: string;
  to: HostPeerPairEndpoint;
}

export function requestHostListPeerRequests(args: {
  agentSessionId: string;
}): Promise<{ requests: HostPeerRequest[] }> {
  return requestHostCapability<{ requests?: HostPeerRequest[] }>(
    "listPeerRequests",
    [{ agentSessionId: args.agentSessionId, status: "pending" }]
  ).then((result) => ({
    requests: Array.isArray(result?.requests) ? result.requests : []
  }));
}

export function requestHostDecidePeerRequest(args: {
  decision: "approve" | "reject";
  requestId: string;
}): Promise<{ request: HostPeerRequest }> {
  return requestHostCapability<{ request?: HostPeerRequest }>(
    "decidePeerRequest",
    [args]
  ).then((result) => {
    if (!result?.request || typeof result.request.id !== "string") {
      throw new Error("tutti host bridge: decidePeerRequest result missing request");
    }
    return { request: result.request };
  });
}

export type HostSessionLivenessState = "live" | "closed" | "unknown";

export interface HostSessionLivenessEntry {
  state: HostSessionLivenessState;
  status: string;
  taskId: string;
}

/**
 * 批量问宿主：这些会话在 `cli_agent_tasks` 里还活着吗。宿主保证每个问到的 id
 * 都在结果里；这里只做形状兜底（宿主回了不认识的东西时当没这条，而不是崩）。
 */
export function requestHostSessionLiveness(args: {
  agentSessionIds: string[];
}): Promise<{ sessions: Record<string, HostSessionLivenessEntry> }> {
  return requestHostCapability<{
    sessions?: Record<string, HostSessionLivenessEntry>;
  }>("sessionLiveness", [{ agentSessionIds: args.agentSessionIds }]).then(
    (result) => ({
      sessions:
        result?.sessions && typeof result.sessions === "object"
          ? result.sessions
          : {}
    })
  );
}
