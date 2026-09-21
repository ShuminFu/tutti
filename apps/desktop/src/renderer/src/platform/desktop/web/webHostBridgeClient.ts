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
//   clear text selection (host -> iframe): { type: "tutti-host-clear-text-selection", nonce }
//                              { type: "tutti-host-response", id, error: "unsupported" }
//                              { type: "tutti-host-response", id, error: "<message>", code? }
//
// createAgentSession args[0] = {
//   provider, cwd, prompt, model?, thinkingLevel?,       // 既有字段，形状不变
//   permissionModeId?, planMode?,                        // 用户显式选的权限/计划模式
//   isolation?, railPlacement?,                          // worktree 隔离 + 它要求的项目归属
//   initialContent?, initialDisplayPrompt?               // 结构化首轮内容 + 仅展示文本
// }
//                          result = { taskId, agentSessionId }
//
// 后四组字段是「首轮执行意图」，全部可选：不带它们时请求体与老版本逐字节相同。带上时
// 宿主原样透传到任务行，不准重算/降级（丢了权限 = 首轮跑在用户没选的高权限下；丢了
// isolation = 以为隔离、实际改原项目；丢了 initialContent = 图片静默消失）：
//   - planMode 的 false 也是有效选择，必须原样送到；
//   - worktree 隔离必须配 kind=project 的 railPlacement（tuttid 建 worktree 的前置条件）；
//   - 纯图片首轮的 prompt 可以是空串，展示文本另走 initialDisplayPrompt；
//   - 块的字段集与 tuttid 的 AgentPromptContentBlock 同形（它 additionalProperties:false，
//     多一个键整条请求会被拒收）；attachmentId 不在这条契约里——它按 workspace+session
//     定位，跨到宿主新建的另一条会话只会指向查无此物的附件。
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
// 分栏结对模式（peer-pair-mode 接口契约，三个能力一组注册、缺一不挂）：
// listPeerPairs 每个 pair 增 { pairMode: "solo"|"pair", developerTaskId, kickoffState: ""|"pending"|"sent" }
// setPeerPairMode    args[0] = { pairId, mode: "solo"|"pair", developerTaskId? }
//                    result  = { pair }
// previewPairKickoff args[0] = { pairId, senderTaskId, goal }
//                    result  = { block }                 —— 纯函数，不写库
// commitPairKickoff  args[0] = { pairId, senderTaskId, goal, expectedDeveloperTaskId? }
//                    result  = { pair, delivered, reason? } —— 给搭档投卡并记为 sent，幂等；
//                              delivered=false（卡被环路闸/限流丢了）时行仍是 pending
// 未注册时 iframe 收到 unsupported，分栏层整排单选不渲染。
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
// 模型收藏/最近使用（composer 菜单那两个星星与「最近」分组）的耐久副本：
// getComposerModelHistory    args[0] = { targetId, legacyFavorites, legacyRecents }
//                            result  = { favorites: string[], recents: string[] }
// updateComposerModelHistory args[0] = { targetId, kind: "favorite" | "recent",
//                                        modelId, favorite? }
//                            result  = 同上（写后的权威快照）
// 为什么必须落宿主：这两个列表原先只写在 iframe 的 localStorage 里，而嵌入
// DinTalDock 的 iframe origin 是**随机回环端口**，宿主每次重启/重装都会换一个，
// 于是 localStorage 跟着换 origin —— 用户看到的是「收藏自己没了」。数据按 target
// 落宿主，与会话/版本/端口无关：读只在**没有**耐久记录时用递上来的 legacy 列表播种
//（显式空记录也算有记录），已有记录一律以宿主为准；favorite 是「目标成员关系」而不是
// 取反，recent 最多留 5 条，写由宿主做 read-modify-write 串行 + 原子落盘。
// 当前 origin 里的老值会在第一次读时自动迁移；换过 origin 的旧值 iframe 读不到，
// 无法自动找回。
//
// 别名由宿主算，iframe 传空串；后端拒绝时回 { error: <中文文案>, code }。

import { writeWorkspaceFileDropData } from "@tutti-os/agent-gui/workspace-file-drop";

const REQUEST_TYPE = "tutti-host-request";
const RESPONSE_TYPE = "tutti-host-response";
export const HOST_FOCUS_TYPE = "tutti-host-focus";
export const HOST_FILE_DROP_TYPE = "tutti-host-file-drop";
export const HOST_OPEN_AGENT_SESSION_TYPE = "tutti-host-open-agent-session";
export const HOST_AGENT_SESSION_READY_TYPE = "tutti-host-agent-session-ready";
export const HOST_OPEN_AGENT_SESSION_ACK_TYPE =
  "tutti-host-open-agent-session-ack";
export const HOST_WORKBENCH_LAYOUT_TYPE = "tutti-host-workbench-layout";
export const HOST_THEME_TYPE = "tutti-host-theme";
export const HOST_LOCALE_TYPE = "tutti-host-locale";
export const HOST_CLEAR_TEXT_SELECTION_TYPE = "tutti-host-clear-text-selection";
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
    typeof window !== "undefined" && !!window.parent && window.parent !== window
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
  if (!coordinates || !windowRef.parent || windowRef.parent === windowRef) {
    return () => undefined;
  }

  let lastFocusedElement: HTMLElement | null = null;
  const parent = windowRef.parent;

  const onFocusIn = (event: Event): void => {
    if (isFocusTarget(event.target) && event.target !== documentRef.body) {
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

export function installHostClearTextSelectionBridge(
  windowRef: Window = window,
  documentRef: Document = document
): () => void {
  const coordinates = bridgeCoordinates(windowRef.location.search);
  if (!coordinates || !windowRef.parent || windowRef.parent === windowRef) {
    return () => undefined;
  }

  const parent = windowRef.parent;
  const onMessage = (event: MessageEvent): void => {
    const data = event.data as
      | { type?: unknown; nonce?: unknown }
      | null
      | undefined;
    if (
      !data ||
      typeof data !== "object" ||
      data.type !== HOST_CLEAR_TEXT_SELECTION_TYPE ||
      data.nonce !== coordinates.nonce ||
      event.source !== parent ||
      event.origin !== coordinates.hostOrigin
    ) {
      return;
    }
    // Host-originated: the user already left the iframe, so collapse even an
    // editable Range. In-document Esc/click skip contenteditable separately.
    documentRef.getSelection?.()?.removeAllRanges?.();
  };

  windowRef.addEventListener("message", onMessage);
  return () => windowRef.removeEventListener("message", onMessage);
}

// The embedding host owns interface language for the embedded build. The
// initial locale already arrives on the iframe URL (see i18n/runtime.ts); this
// bridge keeps it in sync when the host switches language afterwards, using the
// same source/origin/nonce triple as every other host-to-iframe notification.
export function installHostLocaleBridge(
  applyHostLocale: (locale: "en" | "zh-CN") => void,
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
          locale?: unknown;
        }
      | null
      | undefined;
    if (
      !data ||
      typeof data !== "object" ||
      data.type !== HOST_LOCALE_TYPE ||
      data.nonce !== coordinates.nonce ||
      event.source !== parent ||
      event.origin !== coordinates.hostOrigin ||
      (data.locale !== "en" && data.locale !== "zh-CN")
    ) {
      return;
    }
    applyHostLocale(data.locale);
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
  const interactive =
    capability === "selectUploadFiles" ||
    capability === "selectDirectory" ||
    capability === "saveWorkspaceFile";
  const failure = (message: string, code: string): Error =>
    Object.assign(new HostBridgeUnavailableError(message), { code });
  const coordinates = isHostBridgeAvailable() ? bridgeCoordinates() : null;
  if (!coordinates) {
    return Promise.reject(
      failure(
        interactive
          ? "宿主连接不可用，请重新打开工作台后重试"
          : "tutti host bridge: not embedded",
        "host_bridge_unavailable"
      )
    );
  }
  const id = nextRequestId();
  const startedAt = Date.now();
  const log = (stage: string, code?: string): void => {
    if (interactive)
      console.debug("[DinTalDock picker]", {
        requestId: id,
        capability,
        stage,
        elapsedMs: Date.now() - startedAt,
        code
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
      log(
        error.code === "host_request_cancelled" ? "cancelled" : "failed",
        error.code
      );
      reject(error);
    };
    const onPageHide = (): void =>
      finishError(
        failure(
          "文件选择页面已关闭，请重新打开后重试",
          "host_request_cancelled"
        )
      );
    const onMessage = (event: MessageEvent): void => {
      const data = event.data as
        | {
            type?: unknown;
            id?: unknown;
            nonce?: unknown;
            result?: T;
            error?: unknown;
            code?: unknown;
          }
        | null
        | undefined;
      if (
        !data ||
        typeof data !== "object" ||
        (data.type !== RESPONSE_TYPE &&
          data.type !== "tutti-host-request-accepted") ||
        data.id !== id ||
        data.nonce !== coordinates.nonce ||
        event.source !== window.parent ||
        event.origin !== coordinates.hostOrigin ||
        settled
      )
        return;
      if (data.type === "tutti-host-request-accepted") {
        if (interactive && !accepted) {
          accepted = true;
          window.clearTimeout(timer);
          log("accepted");
        }
        return;
      }
      if (typeof data.error === "string") {
        const error =
          data.error === "unsupported"
            ? failure(
                interactive
                  ? "宿主不支持此文件选择操作"
                  : `tutti host bridge: ${capability} unsupported`,
                "host_capability_unsupported"
              )
            : new Error(data.error);
        if (typeof data.code === "string" && data.code.trim())
          Object.assign(error, { code: data.code.trim() });
        finishError(error);
        return;
      }
      settled = true;
      cleanup();
      log("completed");
      resolve(data.result as T);
    };
    timer = window.setTimeout(
      () =>
        finishError(
          failure(
            interactive
              ? "宿主文件选择器未响应，请重试"
              : `tutti host bridge: ${capability} timed out`,
            "host_request_timeout"
          )
        ),
      timeoutMs
    );
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

/**
 * 首轮内容块：tuttid 的 AgentPromptContentBlock 里这条桥允许过桥的子集（它的 schema 是
 * additionalProperties:false，只列它承认的字段）。刻意不带 attachmentId：附件 id 按
 * workspace + agentSession 定位，宿主新建的是另一条会话号，搬过去只会指向查无此物。
 * 可搬运的是块的耐久载体——path（已归档进 tuttid 唯一接受的图片来源根）、url、data。
 */
export interface HostAgentPromptContentBlock {
  connectorKey?: string;
  data?: string;
  mimeType?: string;
  name?: string;
  path?: string;
  sizeBytes?: number;
  text?: string;
  type: "text" | "image" | "file" | "skill" | "mention" | "connector";
  url?: string;
}

/**
 * 会话栏归属。worktree 隔离的前置条件：tuttid 要求 kind=project 且 projectPath 非空，
 * 否则在建 worktree 之前直接拒收。
 */
export interface HostAgentRailPlacement {
  kind: "conversations" | "project";
  projectPath?: string;
  sectionKey?: string;
  version?: number;
}

export interface HostCreateAgentSessionArgs {
  /** Stable idempotency key shared with AgentGUI's optimistic session identity. */
  clientSubmitId?: string;
  provider: string;
  cwd: string;
  /** 首轮文本；纯图片首轮可以为空串（内容由 initialContent 承担）。 */
  prompt: string;
  model?: string;
  thinkingLevel?: string;
  /** 用户显式选的权限模式；缺省 = 调用方没表态，宿主沿用既有默认策略。 */
  permissionModeId?: string;
  /** 用户显式选的计划模式；false 也是有效选择，必须原样送到宿主。 */
  planMode?: boolean;
  /** create-only 启动隔离模式。目前只有 worktree，且必须配 project 归属。 */
  isolation?: "worktree";
  /** 会话栏归属；worktree 隔离的必填前置。 */
  railPlacement?: HostAgentRailPlacement;
  /** 结构化首轮内容（文字 + 图片）。空数组/缺省 = 首轮内容仍由 prompt 承担。 */
  initialContent?: HostAgentPromptContentBlock[];
  /** 仅展示用的首轮文本（回显与标题），与喂给模型的真实内容分开。 */
  initialDisplayPrompt?: string;
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
  // 结对模式（peer-pair-mode 契约）。老宿主不带这三项：分栏层靠「行上有没有
  // pairMode」判宿主支不支持结对模式。
  pairMode?: "solo" | "pair";
  developerTaskId?: string;
  kickoffState?: "" | "pending" | "sent";
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
    const pairId =
      typeof result?.pairId === "string" ? result.pairId.trim() : "";
    if (!pairId) {
      throw new Error(
        "tutti host bridge: createPeerPair result missing pairId"
      );
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

// 结对模式三件套：同样走 requestHostCapability（5s / nonce / 同源校验）。
// 结果形状在这里校验一次，免得缺字段的回包一路漏到分栏层才炸。
export function requestHostSetPeerPairMode(args: {
  developerTaskId?: string;
  mode: "solo" | "pair";
  pairId: string;
}): Promise<{ pair: HostPeerPair }> {
  return requestHostCapability<{ pair?: HostPeerPair }>("setPeerPairMode", [
    args
  ]).then((result) => {
    if (!result?.pair || typeof result.pair.pairId !== "string") {
      throw new Error("tutti host bridge: setPeerPairMode result missing pair");
    }
    return { pair: result.pair };
  });
}

export function requestHostPreviewPairKickoff(args: {
  goal: string;
  pairId: string;
  senderTaskId: string;
}): Promise<{ block: string }> {
  return requestHostCapability<{ block?: string }>("previewPairKickoff", [
    args
  ]).then((result) => {
    const block = typeof result?.block === "string" ? result.block : "";
    if (!block.trim()) {
      throw new Error(
        "tutti host bridge: previewPairKickoff result missing block"
      );
    }
    return { block };
  });
}

export function requestHostCommitPairKickoff(args: {
  /**
   * preview 时拍下的开发者（契约补充 2026-09-16 评审 A）：与行里当前 developer 不一致时
   * 后端回 409 kickoff_roles_changed，不投递、保持 pending。
   */
  expectedDeveloperTaskId?: string;
  goal: string;
  pairId: string;
  senderTaskId: string;
}): Promise<{ delivered: boolean; pair: HostPeerPair; reason?: string }> {
  return requestHostCapability<{
    delivered?: boolean;
    pair?: HostPeerPair;
    reason?: string;
  }>("commitPairKickoff", [args]).then((result) => {
    if (!result?.pair || typeof result.pair.pairId !== "string") {
      throw new Error(
        "tutti host bridge: commitPairKickoff result missing pair"
      );
    }
    return {
      delivered: result.delivered === true,
      pair: result.pair,
      ...(typeof result.reason === "string" && result.reason.trim()
        ? { reason: result.reason.trim() }
        : {})
    };
  });
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
      throw new Error(
        "tutti host bridge: decidePeerRequest result missing request"
      );
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
              nextFireAtUnixMs:
                typeof raw === "number" && Number.isFinite(raw) ? raw : null
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
 * 一个 agent target 的模型收藏/最近使用（composer 菜单那两个星星与「最近」分组）
 * 在 iframe 这边的形状。权威副本在宿主那边，与 iframe 端口、会话、版本都无关。
 */
export interface HostComposerModelHistorySnapshot {
  favorites: string[];
  recents: string[];
}

export type HostComposerModelHistoryKind = "favorite" | "recent";

/**
 * 宿主回的快照只做形状兜底（数组里不是字符串的条目丢掉），但**缺数组不算空快照**：
 * `{}` 这类回包要当失败抛出，否则会被下游当成「这个人把收藏全删了」的显式空记录，
 * 把界面上还在的收藏抹掉。老宿主没实现这个能力时桥抛 HostBridgeUnavailableError。
 */
function hostComposerModelHistorySnapshot(
  result: { favorites?: unknown; recents?: unknown } | null | undefined,
  capability: string
): HostComposerModelHistorySnapshot {
  if (!Array.isArray(result?.favorites) || !Array.isArray(result?.recents)) {
    throw new Error(
      `tutti host bridge: ${capability} result missing favorites or recents`
    );
  }
  return {
    favorites: result.favorites.flatMap((entry) =>
      typeof entry === "string" ? [entry] : []
    ),
    recents: result.recents.flatMap((entry) =>
      typeof entry === "string" ? [entry] : []
    )
  };
}

/**
 * 问宿主拿某个 target 的权威收藏/最近使用。`legacy*` 是**当前 origin** 里 localStorage
 * 的老值：宿主只在没有耐久记录时用它播种，已有记录以宿主为准（显式空记录也算有记录）。
 *
 * 读不到旧 origin 的值是必然的：iframe 读不了别的 origin 的 localStorage，换过 origin
 * 的那份旧数据无法自动找回。
 */
export function requestHostComposerModelHistory(args: {
  targetId: string;
  legacyFavorites: string[];
  legacyRecents: string[];
}): Promise<HostComposerModelHistorySnapshot> {
  return requestHostCapability<{ favorites?: unknown; recents?: unknown }>(
    "getComposerModelHistory",
    [args]
  ).then((result) =>
    hostComposerModelHistorySnapshot(result, "getComposerModelHistory")
  );
}

/**
 * 让宿主改一条收藏或最近使用，回写后的权威快照。
 *
 * `favorite` 是**目标成员关系**（true = 应该在收藏里），不是取反：宿主那边是
 * read-modify-write + 原子落盘，这里递过去的是「用户看到并想要的结果」，重复调不会
 * 把状态翻回去。`recent` 不带 favorite 字段，宿主自己按最多 5 条、最新在前收敛。
 */
export function requestHostUpdateComposerModelHistory(args: {
  targetId: string;
  kind: HostComposerModelHistoryKind;
  modelId: string;
  favorite?: boolean;
}): Promise<HostComposerModelHistorySnapshot> {
  return requestHostCapability<{ favorites?: unknown; recents?: unknown }>(
    "updateComposerModelHistory",
    [args]
  ).then((result) =>
    hostComposerModelHistorySnapshot(result, "updateComposerModelHistory")
  );
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
