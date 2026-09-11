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
//                 result  = { sessions: { [agentSessionId]: { state, taskId, status, attached } } }
//                 state ∈ "live" | "closed" | "unknown"；每个问到的 id 都在结果里。
//                 attached（补丁 0128）与 state 正交：state = tuttid 里有没有活的
//                 ACP 进程（圆点看它），attached = rndmaster 有没有非终态任务行盯着
//                 （补挂看它）。缺字段按 false 处理。
//
// 会话顶部「我起了 N 个监控器」胶囊（补丁 0135）：
// listMonitorAutomations       args[0] = { creatorSessionIds: string[] }（≤200）
//                              result  = { monitors: HostMonitorAutomation[] }
// setMonitorAutomationsEnabled args[0] = { ids: string[], enabled: boolean }
//                              result  = { updated: number }
// 宿主只回「启用中、且确实由问到的那些会话起的」监控自动化；停用时也在宿主侧
// 再核对一次归属，iframe 递来的 id 不能直接落到别的自动化项上。
//
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
  /**
   * 「rndmaster 那边还有非终态任务行盯着这条会话吗」（补丁 0128）。
   *
   * 与 `state` 正交：`state` 问的是 tuttid 里这条会话有没有活的 ACP 进程，
   * `attached` 问的是宿主自己有没有在记账。老宿主不回这个字段，缺就当 `false`
   *（= 没在盯），补挂那条路自己会去把账挂上，幂等。
   */
  attached: boolean;
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

/**
 * 一条监控自动化（宿主侧 preset=monitor 的自动化项）在 iframe 这边的形状。
 *
 * 为什么不是 Monitor 工具：DinTalDock 的 Claude 运行时把 `Monitor` 放进了
 * disallowedTools，这个产品里真正能用的「监控」只有自动化项。它住在
 * cliagent-backend，会话投影里没有它，只能经宿主桥问。
 */
export interface HostMonitorAutomation {
  /** 自动化项 id，停用时原样递回去。 */
  id: string;
  /** 人话名字，例如「盯 grok-8014」。 */
  name: string;
  /** 被盯的那条会话（task id 或配对别名），给 tooltip 用；宿主可能给空串。 */
  peerTarget: string;
  /**
   * 下一次心跳的时刻（毫秒）。监控自动化平时**没有进程在跑**，只有一个闹钟，
   * 所以胶囊显示的是「下次几点响」，不是「已经跑了多久」——后者对它不成立。
   * 宿主解析不出时刻时给 null，界面就只显示条数，不编一个时间出来。
   */
  nextFireAtUnixMs: number | null;
}

/**
 * 批量问宿主：这些会话各自起过哪些还启用着的监控自动化。
 *
 * 宿主已经按归属过滤过；这里只做形状兜底（回了不认识的东西当没这条，而不是崩）。
 * 宿主没实现这个能力时桥抛 HostBridgeUnavailableError，胶囊整个不显示。
 */
export function requestHostMonitorAutomations(args: {
  creatorSessionIds: string[];
}): Promise<{ monitors: HostMonitorAutomation[] }> {
  return requestHostCapability<{ monitors?: unknown }>(
    "listMonitorAutomations",
    [{ creatorSessionIds: args.creatorSessionIds }]
  ).then((result) => ({
    monitors: Array.isArray(result?.monitors)
      ? result.monitors.flatMap((entry) => {
          const id = String((entry as { id?: unknown })?.id ?? "").trim();
          // 没有 id 的条目没法停用，留着只会让「全部停用」静默漏掉一条。
          if (!id) return [];
          const raw = (entry as { nextFireAtUnixMs?: unknown })
            ?.nextFireAtUnixMs;
          return [
            {
              id,
              name: String((entry as { name?: unknown })?.name ?? ""),
              peerTarget: String(
                (entry as { peerTarget?: unknown })?.peerTarget ?? ""
              ),
              nextFireAtUnixMs: typeof raw === "number" && Number.isFinite(raw)
                ? raw
                : null
            }
          ];
        })
      : []
  }));
}

/**
 * 停用（或重新启用）一批监控自动化。宿主会再核对一次归属与 preset，
 * 回的 updated 是**真正改到的条数**，可能少于递过去的 id 数。
 */
export function requestHostSetMonitorAutomationsEnabled(args: {
  ids: string[];
  enabled: boolean;
}): Promise<{ updated: number }> {
  return requestHostCapability<{ updated?: unknown }>(
    "setMonitorAutomationsEnabled",
    [{ ids: args.ids, enabled: args.enabled }]
  ).then((result) => ({
    updated:
      typeof result?.updated === "number" && Number.isFinite(result.updated)
        ? result.updated
        : 0
  }));
}

/**
 * 告诉宿主「用户又在这条会话里说话了」，让它把被后端重启打断的任务行重新挂上
 *（补丁 0124）。宿主幂等：已经有活行就原样返回那一行，短时间内重复调也不会多挂。
 *
 * 回的形状与 sessionLiveness 的单条一致，调用方拿到就能直接更新圆点，
 * 不必等下一拍轮询。宿主没实现这个能力时桥抛 HostBridgeUnavailableError。
 */
export function requestHostSessionAttach(args: {
  agentSessionId: string;
}): Promise<HostSessionLivenessEntry> {
  return requestHostCapability<Partial<HostSessionLivenessEntry>>(
    "sessionAttach",
    [{ agentSessionId: args.agentSessionId }]
  ).then((result) => ({
    // 宿主回了不认识的 state 就当 unknown：这条路上宁可什么都不断言，
    // 也不要凭空报一个 closed 把还活着的会话的圆点抹掉。
    state:
      result?.state === "live" ||
      result?.state === "closed" ||
      result?.state === "unknown"
        ? result.state
        : "unknown",
    status: typeof result?.status === "string" ? result.status : "",
    taskId: typeof result?.taskId === "string" ? result.taskId : "",
    // 缺字段兜 false：老宿主不回 attached，当成「没在盯」比当成「盯着」安全 ——
    // 前者最多多补挂一次（宿主幂等），后者会让补挂整档失效（补丁 0128）。
    attached: result?.attached === true
  }));
}
