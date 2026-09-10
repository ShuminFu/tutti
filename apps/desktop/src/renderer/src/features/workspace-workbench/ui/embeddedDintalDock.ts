import type {
  WorkbenchContribution,
  WorkbenchHostHandle
} from "@tutti-os/workbench-surface";
import {
  createElement,
  type ReactNode,
  useEffect,
  useSyncExternalStore
} from "react";
import { installHostAgentSessionBridge } from "../../../platform/desktop/web/webHostBridgeClient.ts";
import { registerEmbeddedHostCreatedSessionOpener } from "../../workspace-agent/services/internal/embeddedHostCreatedSessionOpener.ts";
import { installEmbeddedRailPeerPairingHost } from "./embeddedRailPeerPairingHost.ts";
import {
  embeddedSplitViewController,
  embeddedSplitViewPaneDescriptor,
  parseEmbeddedSplitViewPaneDescriptor,
  subscribeEmbeddedSplitView,
  type EmbeddedSplitViewController
} from "./embeddedSplitView.ts";
import {
  agentGuiWorkbenchOpenSessionActivationType,
  workspaceAgentGuiNodeID
} from "../services/workspaceAgentGuiLaunch.ts";

type EmbeddedDintalDockNode = NonNullable<
  WorkbenchContribution["nodes"]
>[number];
type EmbeddedDintalDockBodyContext = Parameters<
  EmbeddedDintalDockNode["renderBody"]
>[0];

interface EmbeddedDintalDockBodyProps {
  context: EmbeddedDintalDockBodyContext;
  renderBody: EmbeddedDintalDockNode["renderBody"];
  railStore: EmbeddedDintalDockRailStore;
}

type EmbeddedDintalDockHeaderContext = Parameters<
  NonNullable<EmbeddedDintalDockNode["renderHeader"]>
>[0];

interface EmbeddedDintalDockHeaderProps {
  context: EmbeddedDintalDockHeaderContext;
  renderHeader: NonNullable<EmbeddedDintalDockNode["renderHeader"]>;
  railStore: EmbeddedDintalDockRailStore;
}

interface EmbeddedDintalDockRailPresentation {
  conversationRailNarrowExpanded: boolean;
  onConversationRailNarrowExpandedChange(expanded: boolean): void;
}

interface EmbeddedDintalDockRailStore {
  get(nodeId: string): boolean;
  set(nodeId: string, expanded: boolean): void;
  subscribe(nodeId: string, listener: () => void): () => void;
}

function createEmbeddedDintalDockRailStore(): EmbeddedDintalDockRailStore {
  const expandedNodeIds = new Set<string>();
  const listenersByNodeId = new Map<string, Set<() => void>>();
  return {
    get: (nodeId) => expandedNodeIds.has(nodeId),
    set(nodeId, expanded) {
      if (expanded === expandedNodeIds.has(nodeId)) {
        return;
      }
      if (expanded) {
        expandedNodeIds.add(nodeId);
      } else {
        expandedNodeIds.delete(nodeId);
      }
      for (const listener of listenersByNodeId.get(nodeId) ?? []) {
        listener();
      }
    },
    subscribe(nodeId, listener) {
      const listeners = listenersByNodeId.get(nodeId) ?? new Set();
      listeners.add(listener);
      listenersByNodeId.set(nodeId, listeners);
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
          listenersByNodeId.delete(nodeId);
        }
      };
    }
  };
}

type EmbeddedDintalDockHostWithController =
  EmbeddedDintalDockBodyContext["host"] & {
    controller: {
      getSnapshot: EmbeddedDintalDockBodyContext["host"]["getSnapshot"];
      subscribe(listener: () => void): () => void;
    };
  };

type EmbeddedDintalDockHost = Pick<
  WorkbenchHostHandle,
  | "activateNode"
  | "closeNode"
  | "exitFullscreenNode"
  | "focusNode"
  | "getSnapshot"
  | "launchNode"
  | "load"
>;

export function installEmbeddedDintalDockSessionBridge(
  host: EmbeddedDintalDockHost,
  windowRef: Window = window
): () => void {
  const openSession = (agentSessionId: string): boolean =>
    activateEmbeddedDintalDockSession(host, agentSessionId);
  const unregisterOpener = registerEmbeddedHostCreatedSessionOpener(openSession);
  // 只在嵌入 DinTalDock 时接上配对能力；普通 web 构建里侧栏没有这组 UI。
  const unregisterPeerPairing = installEmbeddedRailPeerPairingHost();
  const uninstallBridge = installHostAgentSessionBridge(openSession, windowRef);
  return () => {
    uninstallBridge();
    unregisterPeerPairing();
    unregisterOpener();
  };
}

// 分栏（补丁 0108）之后这是唯一的「落地一条会话」入口：宿主 open-session 桥、
// 新建会话回落、侧栏点选三条路都走这里。注册了分栏 controller 时交给它按
// 「已在某栏 → 只移焦点，否则顶替焦点栏」处理；没注册就退回单窗口的老行为。
export function activateEmbeddedDintalDockSession(
  host: EmbeddedDintalDockHost,
  agentSessionId: string
): boolean {
  const split = embeddedSplitViewController();
  if (split) {
    return split.select(agentSessionId);
  }
  const snapshot = host.getSnapshot();
  const nodeId = [...snapshot.nodeStack]
    .reverse()
    .find((id) => snapshot.nodes.some(
      (node) => node.id === id && node.data.typeId === workspaceAgentGuiNodeID
    ));
  if (!nodeId) return false;
  host.activateNode(
    { nodeId },
    {
      payload: { agentSessionId },
      type: agentGuiWorkbenchOpenSessionActivationType
    }
  );
  host.focusNode(nodeId);
  return true;
}

/**
 * 嵌入态里「带会话号的 launch」（完成提示的「打开」、Launchpad、Issue 面板、App Center
 * 都走 `requestWorkspaceAgentGuiLaunch({agentSessionId})`）统一收进上面那个漏斗。
 * 不收的话工作台按 `current-session` 复用策略找不到已显示这条会话的窗口，就会**再开一个
 * 窗口**：分栏层只认领自己的两个，`reconcileEmbeddedDintalDock` 又只跑一次，第三个窗口
 * 既没人关、也没有 `data-rndmaster-pane` 标记，于是按工作台原始坐标整块画在两栏之上
 *（真机：栏头叠在侧栏上、正文横跨两栏）。
 * 返回 `undefined` = 不归这里管（新窗口 / 草稿 / 无会话号），照常 `launchNode`；
 * 返回 `null` = 已经落进某一栏。
 */
export function routeEmbeddedDintalDockSessionLaunch(
  host: EmbeddedDintalDockHost,
  request: {
    agentSessionId?: string | null;
    draftPrompt?: string | null;
    forceNewInstance?: boolean;
    openInNewWindow?: boolean;
  }
): null | undefined {
  const agentSessionId = request.agentSessionId?.trim() ?? "";
  if (
    !agentSessionId ||
    request.openInNewWindow ||
    request.forceNewInstance ||
    (request.draftPrompt?.trim() ?? "")
  ) {
    return undefined;
  }
  return activateEmbeddedDintalDockSession(host, agentSessionId)
    ? null
    : undefined;
}

export function isEmbeddedDintalDock(
  search = globalThis.location?.search ?? ""
): boolean {
  return new URLSearchParams(search).has("tuttiBootstrap");
}

function projectEmbeddedDintalDockNodeContext<
  TContext extends {
    node: { frame: { height: number; width: number; x: number; y: number } };
  }
>(
  context: TContext,
  surfaceSize: { height: number; width: number },
  // 分栏时每栏只占主区的一部分。窗口自己那套 630px 自动折叠算术
  // （model/agentGuiRailLayout.ts）读的就是 frame.width，所以这里要按比例给，
  // 否则左栏会以为自己还有整个主区宽、该收会话栏时不收。
  paneFraction = 1
): TContext {
  if (
    !Number.isFinite(surfaceSize.width) ||
    !Number.isFinite(surfaceSize.height) ||
    surfaceSize.width <= 0 ||
    surfaceSize.height <= 0
  ) {
    return context;
  }
  const fraction =
    Number.isFinite(paneFraction) && paneFraction > 0 && paneFraction <= 1
      ? paneFraction
      : 1;
  return {
    ...context,
    node: {
      ...context.node,
      frame: {
        height: surfaceSize.height,
        width: Math.round(surfaceSize.width * fraction),
        x: 0,
        y: 0
      }
    }
  } as TContext;
}

export function renderEmbeddedDintalDockBody(
  context: EmbeddedDintalDockBodyContext,
  renderBody: EmbeddedDintalDockNode["renderBody"]
): ReactNode {
  return renderBody(
    projectEmbeddedDintalDockNodeContext(
      context,
      context.host.getSnapshot().surfaceSize
    )
  );
}

function EmbeddedDintalDockBody({
  context,
  renderBody,
  railStore
}: EmbeddedDintalDockBodyProps): ReactNode {
  const host = context.host as EmbeddedDintalDockHostWithController;
  useSyncExternalStore(
    (listener) => host.controller.subscribe(listener),
    () => host.controller.getSnapshot().surfaceSize,
    () => host.controller.getSnapshot().surfaceSize
  );
  const nodeId = context.node.id;
  const narrowExpanded = useSyncExternalStore(
    (listener) => railStore.subscribe(nodeId, listener),
    () => railStore.get(nodeId),
    () => railStore.get(nodeId)
  );
  // 分栏描述符是个字符串（不是对象）：useSyncExternalStore 靠引用相等判「没变」。
  const paneDescriptor = useSyncExternalStore(
    subscribeEmbeddedSplitView,
    () => embeddedSplitViewPaneDescriptor(nodeId),
    () => embeddedSplitViewPaneDescriptor(nodeId)
  );
  const pane = parseEmbeddedSplitViewPaneDescriptor(paneDescriptor);
  const projectedContext = projectEmbeddedDintalDockNodeContext(
    context,
    context.host.getSnapshot().surfaceSize,
    pane?.fraction ?? 1
  );
  const body = renderBody({
    ...projectedContext,
    conversationRailNarrowExpanded: narrowExpanded,
    onConversationRailNarrowExpandedChange: (expanded: boolean) =>
      railStore.set(nodeId, expanded)
  } as typeof projectedContext & EmbeddedDintalDockRailPresentation);
  if (!pane) {
    return body;
  }
  // `display:contents` 的包裹层：只是把「这是左/右栏、是不是焦点栏」投影到 DOM 上，
  // 不参与布局。右栏的会话栏/provider 栏就是靠它在 CSS 里隐掉的。
  return createElement(
    "div",
    {
      "data-rndmaster-pane": pane.side,
      "data-rndmaster-pane-focus": pane.focused ? "true" : "false",
      style: { display: "contents" }
    },
    body
  );
}

function EmbeddedDintalDockHeader({
  context,
  renderHeader,
  railStore
}: EmbeddedDintalDockHeaderProps): ReactNode {
  const nodeId = context.node.id;
  const narrowExpanded = useSyncExternalStore(
    (listener) => railStore.subscribe(nodeId, listener),
    () => railStore.get(nodeId),
    () => railStore.get(nodeId)
  );
  useEffect(() => {
    if (!narrowExpanded) {
      return;
    }
    document
      .getElementById("agent-gui-conversation-rail")
      ?.querySelector<HTMLElement>(
        'button:not(:disabled), [href], input:not(:disabled), [tabindex]:not([tabindex="-1"])'
      )
      ?.focus();
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      railStore.set(nodeId, false);
      globalThis.requestAnimationFrame?.(() => {
        document
          .querySelector<HTMLElement>(
            '[data-testid="agent-gui-toggle-conversation-rail"]'
          )
          ?.focus();
      });
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [narrowExpanded, nodeId, railStore]);

  const projectedContext = projectEmbeddedDintalDockNodeContext(
    context,
    context.surfaceSize
  );
  // Issue 03: the master shell already paints the only top bar, so the Agent
  // header row renders nothing here. Its controls move into the conversation
  // rail's own top row; this component stays mounted because it still owns the
  // narrow-drawer focus and Escape handling above.
  return renderHeader({
    ...projectedContext,
    conversationRailNarrowExpanded: narrowExpanded,
    embedded: true,
    dragHandleProps: {
      ...projectedContext.dragHandleProps,
      onDoubleClick: undefined,
      onPointerDown: undefined
    },
    onConversationRailNarrowExpandedChange: (expanded: boolean) =>
      railStore.set(nodeId, expanded)
  } as typeof projectedContext & EmbeddedDintalDockRailPresentation);
}

export function applyEmbeddedDintalDockContributions(
  contributions: readonly WorkbenchContribution[] | undefined
): readonly WorkbenchContribution[] | undefined {
  const railStore = createEmbeddedDintalDockRailStore();
  return contributions?.map((contribution) => ({
    ...contribution,
    nodes: contribution.nodes?.map((node) => {
      if (node.typeId !== workspaceAgentGuiNodeID) {
        return node;
      }
      const getHeaderFrameRenderKey = node.getHeaderFrameRenderKey;
      const renderBody = node.renderBody;
      const renderHeader = node.renderHeader;
      return {
        ...node,
        getHeaderFrameRenderKey: getHeaderFrameRenderKey
          ? (context) =>
              getHeaderFrameRenderKey(
                projectEmbeddedDintalDockNodeContext(
                  context,
                  context.surfaceSize
                )
              )
          : undefined,
        renderBody: (context) =>
          createElement(EmbeddedDintalDockBody, {
            context,
            railStore,
            renderBody
          }),
        renderHeader: renderHeader
          ? (context) =>
              createElement(EmbeddedDintalDockHeader, {
                context,
                railStore,
                renderHeader
              })
          : undefined
      };
    })
  }));
}

// The embedded product is a single-purpose Agent surface. Reconcile the
// persisted desktop snapshot once so stale app windows cannot reappear.
//
// 补丁 0108 起「最多留两个」：两栏 = 两个完整的 agent-gui 窗口，第三个及以后照旧关掉。
// 留下的两个按 frontmost 在前交给分栏 controller，由它按本地记住的布局决定
// 「真的是两栏」还是「多出来的那个关掉」。
// 同一个 host 只 reconcile 一次。`useWorkbenchHostRuntime` 的 effect 依赖 `onHandleReady`
// 这个回调本身，而它是 `useCallback` 且依赖里有 `contributions` —— 工作台一有窗口变化，
// 回调就换新身份，effect 重跑，于是**同一个 host** 被再回调一次。补丁 0109 之前重入是
// 无害的：reconcile 只关掉多余窗口，已经有窗口时是幂等的。现在它还要 `split.adopt`，
// 而 adopt 会为空着的那一栏 `launchPane` —— 重入一次就多起一个窗口，新窗口又触发下一次
// 重入。真机上表现为：落下第二栏后窗口每秒新增一个多、`<style>` 只增不减，
// WebContent 涨到 33GB 后整个进程崩掉（票 03 第四轮实测）。
const reconciledHosts = new WeakSet<object>();

export async function reconcileEmbeddedDintalDock(
  host: EmbeddedDintalDockHost,
  split?: Pick<EmbeddedSplitViewController, "adopt"> | null
): Promise<string | null> {
  if (reconciledHosts.has(host)) return null;
  reconciledHosts.add(host);
  await host.load();
  const snapshot = host.getSnapshot();
  const agents = snapshot.nodes.filter(
    (node) => node.data.typeId === workspaceAgentGuiNodeID
  );
  const frontmostFirst = [...snapshot.nodeStack]
    .reverse()
    .filter((id) => agents.some((node) => node.id === id));
  const ordered = [
    ...frontmostFirst,
    ...agents.map((node) => node.id).filter((id) => !frontmostFirst.includes(id))
  ];
  const keepIds = ordered.slice(0, 2);
  const keepId = keepIds[0] ?? null;

  for (const node of snapshot.nodes) {
    if (!keepIds.includes(node.id)) {
      host.closeNode(node.id);
    }
  }

  if (!keepId) {
    const launchedId = await host.launchNode({
      reason: "host",
      typeId: workspaceAgentGuiNodeID
    });
    if (launchedId) {
      await split?.adopt([launchedId]);
    }
    return launchedId;
  }

  for (const id of keepIds) {
    if (agents.find((node) => node.id === id)?.displayMode === "fullscreen") {
      host.exitFullscreenNode(id);
    }
  }
  host.focusNode(keepId);
  await split?.adopt(keepIds);
  return keepId;
}
