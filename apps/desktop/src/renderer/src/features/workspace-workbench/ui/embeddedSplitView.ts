// 分栏配对（补丁 0108）的宿主端编排层：两栏 = 两个完整的 agent-gui 工作台窗口。
//
// 为什么是两个窗口而不是「一个窗口里放两个 DetailPane」：
// agentGuiNode 里 `activeConversationId` 有 ~200 处读点（transport / detail / composer /
// 滚动记忆 / 侧栏），`AgentGUIDetailPane` 收的是按单一 id 组装好的整套 view model，
// 没有 conversationId 入参 —— 逐点参数化不可行。而「窗口」本来就是 Tutti 的多实例单位：
// 每个窗口各有自己的 controller、订阅、草稿、滚动记忆，天生互不干扰。所以嵌入层把
// `reconcileEmbeddedDintalDock` 从「只留一个」放宽到「最多两个」，本文件负责
// 左右/焦点/宽度/放下/配对的编排，右栏窗口用 CSS 隐掉会话栏与 provider 栏（只留详情）。
import {
  conversationRailPeerPairIndex,
  conversationRailPeerPairLinks,
  conversationRailPeerPairingHost,
  createEmptySplitLayout,
  isSplitLayoutSplit,
  loadSplitLayout,
  reduceSplitLayout,
  registerConversationRailSplitHost,
  saveSplitLayout,
  splitLayoutStorageKey,
  type ConversationRailPeerPair,
  type ConversationRailPeerPairingHost,
  type ConversationRailSplitDragSession,
  type ConversationRailSplitPoint,
  type SplitLayoutEvent,
  type SplitLayoutState,
  type SplitSide
} from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  agentGuiWorkbenchOpenSessionActivationType,
  workspaceAgentGuiNodeID
} from "../services/workspaceAgentGuiLaunch.ts";
import {
  pairOnDrop,
  type EmbeddedSplitPairingLabels,
  type EmbeddedSplitPairingSide,
  type EmbeddedSplitToast,
  type PairOnDropResult
} from "./embeddedSplitPairing.ts";

// 宽度算术（PRD 故事 31/33）：每栏最小 320px，所以「够不够两栏」的门槛就是 2 × 320。
// 比例的下界 = 320 / 主区宽度，上界 = 1 - 下界；主区宽度未知时退回状态机的默认 0.2。
export const EMBEDDED_SPLIT_MIN_PANE_WIDTH_PX = 320;
export const EMBEDDED_SPLIT_COLLAPSE_WIDTH_PX =
  EMBEDDED_SPLIT_MIN_PANE_WIDTH_PX * 2;
const EMBEDDED_SPLIT_FALLBACK_MIN_RATIO = 0.2;
const EMBEDDED_SPLIT_RELAUNCH_WAIT_MS = 20_000;

export type EmbeddedSplitPairingState =
  | "paired"
  | "unpaired"
  | "unsupported"
  | "pending";

/** 栏头要写的那点身份信息（票 04）：来自侧栏上报 / 拖动摘要，拿不到就是 null。 */
export interface EmbeddedSplitPaneSessionSnapshot {
  iconUrl: string | null;
  projectLabel: string | null;
  provider: string | null;
  title: string;
}

export interface EmbeddedSplitPaneSnapshot {
  nodeId: string | null;
  /** 这一栏那条会话的标题/图标/项目名；侧栏没报过（例如刚恢复布局）时为 null。 */
  session: EmbeddedSplitPaneSessionSnapshot | null;
  /** 窗口刚被「新建会话」清空、还没落地新会话号时为 null（见 freshNodeIds）。 */
  sessionId: string | null;
}

export interface EmbeddedSplitDragSnapshot {
  hoverSide: SplitSide | null;
  point: ConversationRailSplitPoint;
  session: ConversationRailSplitDragSession;
}

export interface EmbeddedSplitViewSnapshot {
  collapsed: boolean;
  dragging: EmbeddedSplitDragSnapshot | null;
  focus: SplitSide;
  pairing: EmbeddedSplitPairingState;
  panes: {
    left: EmbeddedSplitPaneSnapshot | null;
    right: EmbeddedSplitPaneSnapshot | null;
  };
  /**
   * 左栏会话栏展开时，两栏几何整体右移的**占比**（票 05）。
   * 覆盖层量出会话栏真实宽度后用 `setRailPushPx` 写进来，这里换算成占比给 CSS
   * 与窗口 body 的 fraction 一起用——两者必须同源，否则 agent-gui 按一个宽度做
   * 响应式、CSS 按另一个宽度画壳，会话栏会在阈值两侧来回抖。
   */
  railPushRatio: number;
  ratio: number;
}

interface EmbeddedSplitViewNode {
  data: { snapshotNodeState?: unknown; typeId: string };
  id: string;
}

/** 只用到 workbench host 的这几件事，测试给个假的就够。 */
export interface EmbeddedSplitViewHost {
  activateNode(
    target: { nodeId: string },
    activation: { payload?: unknown; type: string }
  ): void;
  closeNode(nodeId: string): void;
  controller?: {
    getSnapshot(): { surfaceSize: { height: number; width: number } };
    subscribe(listener: () => void): () => void;
  };
  focusNode(nodeId: string): void;
  getSnapshot(): {
    nodes: readonly EmbeddedSplitViewNode[];
    surfaceSize?: { height: number; width: number };
  };
  launchNode(input: {
    /**
     * 必须带 payload：不带 payload 的 agent-gui 启动会落到 `reuseDockEntryNode`
     * 分支——宿主把**同一个 dock entry 里已有的那个窗口**还给你，右栏于是永远
     * 拿不到自己的窗口（真机验收：右半边全白）。见 launchPane 的注释。
     */
    payload?: unknown;
    reason: "host";
    typeId: string;
  }): Promise<string | null>;
}

/** 「某个窗口正显示哪条会话」的读法；生产实现见 createAgentGuiNodeSessionSource。 */
export interface EmbeddedSplitSessionSource {
  read(node: EmbeddedSplitViewNode): string | null;
  subscribe?(listener: () => void): () => void;
}

export interface EmbeddedSplitRect {
  height: number;
  left: number;
  top: number;
  width: number;
}

/** 判放下区要量的两块几何，抽出来是为了单测不依赖 DOM。 */
export interface EmbeddedSplitGeometry {
  /** 左栏 provider 栏 + 会话栏的右边缘（视口 x）；量不到给 null。 */
  railRightEdge(): number | null;
  /** 嵌入 `<main>` 的视口矩形；量不到给 null。 */
  surfaceRect(): EmbeddedSplitRect | null;
}

export interface EmbeddedSplitViewInput {
  geometry?: EmbeddedSplitGeometry;
  host: EmbeddedSplitViewHost;
  /** 取函数不取值：界面语言可以在运行时切。 */
  labels?: () => EmbeddedSplitPairingLabels;
  pairingHost?: () => ConversationRailPeerPairingHost | null;
  /** 等「重开的对端」在侧栏露面的上限，默认 20s；只给单测缩短。 */
  relaunchWaitMs?: number;
  sessions?: EmbeddedSplitSessionSource;
  storage?: Pick<Storage, "getItem" | "setItem"> | null;
  targetId?: string | null;
  toast?: EmbeddedSplitToast;
  workspaceId: string;
}

export interface EmbeddedSplitViewController {
  /** reconcile 把留下的 agent 窗口（最多两个，frontmost 在前）交给分栏层。 */
  adopt(nodeIds: readonly string[]): Promise<void>;
  closePane(side: SplitSide): Promise<void>;
  dispose(): void;
  dropSession(
    session: ConversationRailSplitDragSession,
    side: SplitSide
  ): Promise<void>;
  /** 放下区矩形（相对主区左上角的 px）；量不到几何时给 null。 */
  dropZoneRect(side: SplitSide): { left: number; width: number } | null;
  getSnapshot(): EmbeddedSplitViewSnapshot;
  /** 链条图标的「配对」动作，也是放下之后自动跑的那一段。 */
  pairPanes(): Promise<PairOnDropResult>;
  resetRatio(): void;
  resize(ratio: number): void;
  /**
   * 左栏会话栏当前占多少像素（收起时给 0）。由覆盖层测量后写入：栏宽可拖，
   * 常量猜不准；夹逼在这里做，保证右栏不会被推到 320px 以下。
   */
  setRailPushPx(px: number): void;
  /** 点选 / 宿主 open-session / 新建落地，统一入口。 */
  select(agentSessionId: string): boolean;
  setFocus(side: SplitSide): void;
  sideForNodeId(nodeId: string): SplitSide | null;
  subscribe(listener: () => void): () => void;
  /** 栏头 ⋮ 的「交换左右」：两栏各自换成对方那条会话，焦点跟着人走。 */
  swapPanes(): void;
  unpairPanes(): Promise<void>;
}

const noopToast: EmbeddedSplitToast = {
  error() {},
  info() {},
  success() {}
};

const defaultLabels = (): EmbeddedSplitPairingLabels => ({
  paired: "Paired",
  relaunchTimeout: "The relaunched session did not show up in time",
  unmanaged: "Unmanaged sessions cannot be paired"
});

function otherSide(side: SplitSide): SplitSide {
  return side === "left" ? "right" : "left";
}

function readSnapshotStateSessionId(
  node: EmbeddedSplitViewNode
): string | null {
  const state = node.data.snapshotNodeState as
    | { lastActiveAgentSessionId?: unknown }
    | null
    | undefined;
  const value = state?.lastActiveAgentSessionId;
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

const snapshotStateSessionSource: EmbeddedSplitSessionSource = {
  read: readSnapshotStateSessionId
};

/**
 * 生产用的「窗口 → 当前会话」读法：agent-gui 把 `lastActiveAgentSessionId` 写在
 * 自己那份 externalStateSource 里（packages/agent/gui/workbench/state.ts），
 * 而 workbench host 的 `getSnapshot()` **不**把它投影进 `node.data`。所以要拿到
 * 「用户在窗口里自己点了另一条会话」这件事，只能直接问 contribution 的 state source。
 */
export function createAgentGuiNodeSessionSource(input: {
  externalStateSource:
    | {
        getNodeState(request: {
          instanceId: string;
          instanceKey?: string | null;
          nodeId: string;
          typeId: string;
          workspaceId: string;
        }): unknown;
        subscribeNodeState?(
          request: {
            instanceId: string;
            instanceKey?: string | null;
            nodeId: string;
            typeId: string;
            workspaceId: string;
          },
          listener: () => void
        ): () => void;
      }
    | null
    | undefined;
  typeId?: string;
  workspaceId: string;
}): EmbeddedSplitSessionSource {
  const source = input.externalStateSource;
  const typeId = input.typeId ?? workspaceAgentGuiNodeID;
  if (!source) {
    return snapshotStateSessionSource;
  }
  const lookupOf = (node: EmbeddedSplitViewNode) => ({
    instanceId: (node.data as { instanceId?: string }).instanceId ?? node.id,
    instanceKey:
      (node.data as { instanceKey?: string | null }).instanceKey ?? null,
    nodeId: node.id,
    typeId: node.data.typeId,
    workspaceId: input.workspaceId
  });
  return {
    read(node) {
      const state = source.getNodeState(lookupOf(node)) as
        | { lastActiveAgentSessionId?: unknown }
        | null
        | undefined;
      const value = state?.lastActiveAgentSessionId;
      if (typeof value === "string" && value.trim()) {
        return value.trim();
      }
      return readSnapshotStateSessionId(node);
    },
    subscribe(listener) {
      return (
        source.subscribeNodeState?.(
          {
            instanceId: "",
            instanceKey: null,
            nodeId: "",
            typeId,
            workspaceId: input.workspaceId
          },
          listener
        ) ?? (() => {})
      );
    }
  };
}

/** node --test 下没有 Web Storage，取用本身可能抛；拿不到就不持久化。 */
function defaultStorage(): Pick<Storage, "getItem" | "setItem"> | null {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}

function domGeometry(): EmbeddedSplitGeometry {
  const rectOf = (element: Element | null): EmbeddedSplitRect | null => {
    if (!element?.getBoundingClientRect) return null;
    const rect = element.getBoundingClientRect();
    if (!Number.isFinite(rect.width) || rect.width <= 0) return null;
    return {
      height: rect.height,
      left: rect.left,
      top: rect.top,
      width: rect.width
    };
  };
  return {
    railRightEdge() {
      if (typeof document === "undefined") return null;
      // 两栏都会渲染 id="agent-gui-conversation-rail"，getElementById 命中的是
      // DOM 里靠前的那一个（左栏）——正是我们要量的那个，重复 id 在此可接受。
      const rail = rectOf(
        document.getElementById("agent-gui-conversation-rail")
      );
      const providerRail = rectOf(
        document.querySelector(".agent-gui-node__provider-rail-panel")
      );
      const edges = [rail, providerRail]
        .filter((rect): rect is EmbeddedSplitRect => rect !== null)
        .map((rect) => rect.left + rect.width);
      return edges.length > 0 ? Math.max(...edges) : null;
    },
    surfaceRect() {
      if (typeof document === "undefined") return null;
      return rectOf(document.querySelector(".rndmaster-dintaldock-embedded"));
    }
  };
}

export function createEmbeddedSplitViewController(
  input: EmbeddedSplitViewInput
): EmbeddedSplitViewController {
  const host = input.host;
  const toast = input.toast ?? noopToast;
  const labels = input.labels ?? defaultLabels;
  const geometry = input.geometry ?? domGeometry();
  const sessions = input.sessions ?? snapshotStateSessionSource;
  const readPairingHost = input.pairingHost ?? conversationRailPeerPairingHost;
  const storage =
    input.storage === undefined ? defaultStorage() : input.storage;
  const storageKey = splitLayoutStorageKey({
    targetId: input.targetId ?? null,
    workspaceId: input.workspaceId
  });

  let layout: SplitLayoutState = createEmptySplitLayout();
  let nodeIdBySide: { left: string | null; right: string | null } = {
    left: null,
    right: null
  };
  const sessionIdByNodeId = new Map<string, string>();
  // 宿主把「另一栏那个窗口」当成新窗口还回来时记在这里，避免每次 applyLayout 重试。
  let refusedLaunchFor: { left: string | null; right: string | null } = {
    left: null,
    right: null
  };
  const launching: { left: boolean; right: boolean } = {
    left: false,
    right: false
  };
  const pendingLaunches = new Set<Promise<void>>();
  let dragging: EmbeddedSplitDragSnapshot | null = null;
  let collapsed = false;
  // 左栏会话栏的实测宽度（px）。0 = 收起/量不到，几何退回纯比例式。
  let railPushPx = 0;
  let pairs: readonly ConversationRailPeerPair[] = [];
  let pairingBusy = 0;
  let pairingUnsupported = readPairingHost() === null;
  let disposed = false;
  // 配对时后端「先重开已结束的对端」还回来的新会话号。后端一拿到号就返回，而那条
  // Tutti 会话要等派工器异步起来才真的存在（`ManagedTuttiSessionID(taskID)` 是算出来的，
  // 不是查出来的）。拿到号就 activate 会让那一栏打开一条还不存在的会话：空标题 +
  // 红条 `workspace agent session not found`，之后会话真出现了它也不会自己重试
  //（真机：点右栏链条后左栏整栏废掉）。所以这里只记「待换」，栏里继续显示旧的
  // 已结束会话，等侧栏 describeSession 报到这条新会话再换槽；超时就放弃并提示。
  let pendingRelaunch: {
    forSessionId: string;
    sessionId: string;
    side: SplitSide;
    timer: ReturnType<typeof setTimeout> | null;
  } | null = null;
  const relaunchWaitMs = input.relaunchWaitMs ?? EMBEDDED_SPLIT_RELAUNCH_WAIT_MS;

  const listeners = new Set<() => void>();
  // 拖起时 gui 侧把摘要给过来一次；配对判据（非托管 / 已结束）就靠它。
  const dragSessionById = new Map<string, ConversationRailSplitDragSession>();
  let snapshot: EmbeddedSplitViewSnapshot = buildSnapshot();
  let shownSessionIds: ReadonlySet<string> = new Set<string>();
  // 「这个窗口至少报过一次真会话号」——用来把「刚起来还没初始化」和「用户点了
  // 新建会话」区分开：两者读出来都是 null，但只有后者该让栏头变回未命名。
  const settledNodeIds = new Set<string>();
  // 点过「新建会话」、还没落地新会话号的窗口。agent-gui 的 handleCreateConversation
  // 会把 lastActiveAgentSessionId 抹成 null（AgentGUINode.tsx）。
  // 槽里那条旧会话**故意不清**：状态机有「单栏永远在 left」的不变式，左槽一空，
  // 下一次 reduce 就会把右栏顶到左边、分栏整个塌掉。对外（栏头、配对、侧栏的
  // 「正在显示」标记）一律按空处理，等窗口报出新会话号再换槽。
  const freshNodeIds = new Set<string>();

  function surfaceWidth(): number {
    const size =
      host.controller?.getSnapshot().surfaceSize ??
      host.getSnapshot().surfaceSize;
    const width = size?.width ?? 0;
    return Number.isFinite(width) && width > 0 ? width : 0;
  }

  function minRatio(): number {
    const width = surfaceWidth();
    if (width <= 0) return EMBEDDED_SPLIT_FALLBACK_MIN_RATIO;
    return Math.min(Math.max(EMBEDDED_SPLIT_MIN_PANE_WIDTH_PX / width, 0), 0.5);
  }

  function isFreshPane(side: SplitSide): boolean {
    const nodeId = nodeIdBySide[side];
    return nodeId !== null && freshNodeIds.has(nodeId);
  }

  function paneOf(side: SplitSide): EmbeddedSplitPaneSnapshot | null {
    const sessionId = layout.panes[side];
    if (sessionId === null) return null;
    if (isFreshPane(side)) {
      return { nodeId: nodeIdBySide[side], session: null, sessionId: null };
    }
    const known = dragSessionById.get(sessionId);
    return {
      nodeId: nodeIdBySide[side],
      session: known
        ? {
            iconUrl: known.iconUrl,
            projectLabel: known.projectLabel ?? null,
            provider: known.provider,
            title: known.title
          }
        : null,
      sessionId
    };
  }

  /** 摘要有没有变（只比栏头会用到的那几项），用来决定要不要为一次上报重画。 */
  function sameHeaderSummary(
    a: ConversationRailSplitDragSession | undefined,
    b: ConversationRailSplitDragSession
  ): boolean {
    return (
      a !== undefined &&
      a.title === b.title &&
      a.iconUrl === b.iconUrl &&
      a.provider === b.provider &&
      (a.projectLabel ?? null) === (b.projectLabel ?? null) &&
      a.status === b.status &&
      a.isImported === b.isImported
    );
  }

  /** 链条状态按「即将成为这一栏的那条」判：重开等待期里配对已经绑在新号上。 */
  function effectivePaneSession(side: SplitSide): string | null {
    if (pendingRelaunch?.side === side) return pendingRelaunch.sessionId;
    if (isFreshPane(side)) return null;
    return layout.panes[side];
  }

  function clearPendingRelaunch(): void {
    if (!pendingRelaunch) return;
    if (pendingRelaunch.timer !== null) clearTimeout(pendingRelaunch.timer);
    pendingRelaunch = null;
  }

  /** 侧栏报到了待换的那条：现在才把槽换过去（applyLayout 会 activate 那栏的窗口）。 */
  function settleRelaunch(sessionId: string): boolean {
    const pending = pendingRelaunch;
    if (!pending || pending.sessionId !== sessionId) return false;
    clearPendingRelaunch();
    if (layout.panes[pending.side] !== pending.forSessionId) {
      // 等待期间用户已经把那一栏换成别的了：不抢。
      return false;
    }
    applyLayout(
      { ...layout, panes: { ...layout.panes, [pending.side]: sessionId } },
      { persist: true }
    );
    return true;
  }

  function pairingState(): EmbeddedSplitPairingState {
    if (pairingUnsupported) return "unsupported";
    if (pairingBusy > 0) return "pending";
    const left = effectivePaneSession("left");
    const right = effectivePaneSession("right");
    if (left === null || right === null) return "unpaired";
    const index = conversationRailPeerPairIndex(pairs);
    const linked = conversationRailPeerPairLinks(index, left).some(
      (link) => link.peer.sessionId?.trim() === right
    );
    return linked ? "paired" : "unpaired";
  }

  // 会话栏展开要「挤出空间」而不是盖住左栏：左栏加宽、分隔线右移、右栏变窄。
  // 夹逼是必须的 —— 右栏被推到 0 宽就等于分栏被会话栏吃掉了，用户还以为窗口坏了。
  // 折叠态（主区太窄、只显示焦点栏）下没有「两栏」可言，一律不推。
  function railPushRatio(): number {
    if (!isSplitLayoutSplit(layout) || collapsed) return 0;
    const width = surfaceWidth();
    if (width <= 0 || railPushPx <= 0) return 0;
    const rightWidthPx = width * (1 - layout.ratio);
    const maxPushPx = rightWidthPx - EMBEDDED_SPLIT_MIN_PANE_WIDTH_PX;
    if (maxPushPx <= 0) return 0;
    return Math.min(railPushPx, maxPushPx) / width;
  }

  function buildSnapshot(): EmbeddedSplitViewSnapshot {
    return {
      collapsed,
      dragging,
      focus: layout.focus,
      pairing: pairingState(),
      panes: { left: paneOf("left"), right: paneOf("right") },
      railPushRatio: railPushRatio(),
      ratio: layout.ratio
    };
  }

  function refreshShownSessionIds(): void {
    const next = [
      isFreshPane("left") ? null : layout.panes.left,
      isFreshPane("right") ? null : layout.panes.right
    ].filter(
      (id): id is string => typeof id === "string" && id.length > 0
    );
    if (
      next.length === shownSessionIds.size &&
      next.every((id) => shownSessionIds.has(id))
    ) {
      // 引用必须稳定：useConversationRailSplitShownIds 靠引用相等判「没变」。
      return;
    }
    shownSessionIds = new Set(next);
  }

  function emit(): void {
    refreshShownSessionIds();
    snapshot = buildSnapshot();
    for (const listener of [...listeners]) {
      listener();
    }
  }

  function persist(): void {
    if (!storage) return;
    saveSplitLayout(storage, storageKey, layout);
  }

  function nodeById(nodeId: string): EmbeddedSplitViewNode | null {
    return host.getSnapshot().nodes.find((node) => node.id === nodeId) ?? null;
  }

  function activate(nodeId: string, agentSessionId: string): void {
    sessionIdByNodeId.set(nodeId, agentSessionId);
    host.activateNode(
      { nodeId },
      {
        payload: { agentSessionId },
        type: agentGuiWorkbenchOpenSessionActivationType
      }
    );
  }

  /**
   * 真正发那一两次启动请求。第一次带会话号，让新窗口直接落在目标会话上；
   * agent-gui 的 onLaunchRequest 在**认不出 provider** 时会直接返回 null
   * （会话号那条 payload 原样透传，provider 必须在里面），所以要有第二次：
   * 退回「产品自己的新开窗口」那条（只要 openInNewWindow/forceNewInstance），
   * 窗口起来之后由调用方 activate 到目标会话。两次都钉着 openInNewWindow，
   * 否则 reusePolicy 会落到 dock-entry、把左栏那个窗口还回来。
   */
  async function launchPaneNode(agentSessionId: string): Promise<string | null> {
    const known = dragSessionById.get(agentSessionId);
    const withSession = await host.launchNode({
      payload: {
        agentSessionId,
        forceNewInstance: true,
        openInNewWindow: true,
        ...(known?.provider ? { provider: known.provider } : {})
      },
      reason: "host",
      typeId: workspaceAgentGuiNodeID
    });
    if (withSession) return withSession;
    return host.launchNode({
      payload: { forceNewInstance: true, openInNewWindow: true },
      reason: "host",
      typeId: workspaceAgentGuiNodeID
    });
  }

  /**
   * 起一栏自己的窗口。**payload 不能省**：agent-gui 的 onLaunchRequest 在既没有
   * agentSessionId、也没有 openInNewWindow 的时候，reusePolicy 会落到 `dock-entry`，
   * 宿主于是把同一个 dock entry 里**已经开着的那个窗口**原样还回来（返回的是左栏
   * 的 nodeId）。那样右栏拿不到窗口、`sideForNodeId` 又先命中 left，壳上永远不会
   * 出现 `data-rndmaster-pane="right"` —— 表现就是「状态里有两栏、右半边全白」
   * （真机验收 03 第三轮实测）。所以这里显式给会话号 + openInNewWindow/forceNewInstance，
   * 把 reusePolicy 钉在 `none` 上。
   */
  function launchPane(side: SplitSide): void {
    if (launching[side]) return;
    const wantedAtLaunch = layout.panes[side];
    if (wantedAtLaunch === null) return;
    if (refusedLaunchFor[side] === wantedAtLaunch) return;
    launching[side] = true;
    const pending = launchPaneNode(wantedAtLaunch)
      .then((nodeId) => {
        launching[side] = false;
        if (!nodeId) {
          // 两条启动都被拒：**必须**记下来。不记的话每次 applyLayout 都会再要一遍，
          // 恢复一个「右栏起不来」的布局时就变成无限重试，整个 DinTalDock 卡死
          // （真机上表现为面板全白、页面定时器停摆）。换会话号时自然解除。
          refusedLaunchFor = { ...refusedLaunchFor, [side]: wantedAtLaunch };
          emit();
          return;
        }
        if (disposed) {
          host.closeNode(nodeId);
          return;
        }
        const wanted = layout.panes[side];
        if (wanted === null) {
          // 窗口起来之前这一栏又被关了：起了就关掉，不留孤儿。
          host.closeNode(nodeId);
          return;
        }
        if (nodeId === nodeIdBySide[otherSide(side)]) {
          // 宿主把另一栏那个窗口还给了我们（复用分支）。**绝不能**认领：认了就等于
          // 两栏共用一个窗口，右栏白屏而状态看着是对的。记下这次拒绝，别每次
          // applyLayout 都再要一遍；换会话号时自然解除。也不关它——那是别人的窗口。
          refusedLaunchFor = { ...refusedLaunchFor, [side]: wanted };
          emit();
          return;
        }
        refusedLaunchFor = { ...refusedLaunchFor, [side]: null };
        nodeIdBySide = { ...nodeIdBySide, [side]: nodeId };
        activate(nodeId, wanted);
        focusActivePane();
        emit();
      })
      .catch(() => {
        launching[side] = false;
      })
      .finally(() => {
        pendingLaunches.delete(pending);
      });
    pendingLaunches.add(pending);
  }

  function focusActivePane(): void {
    const nodeId =
      nodeIdBySide[layout.focus] ?? nodeIdBySide[otherSide(layout.focus)];
    if (nodeId) host.focusNode(nodeId);
  }

  /**
   * 每次 reduce 之后的副作用。左栏窗口是嵌入模式的锚：`reconcile` 保证它一直在，
   * 空槽只表示「还没选会话」（首页态），绝不能因此把它关掉；只有右栏窗口由分栏
   * 自己起、自己关。
   */
  function applyLayout(
    next: SplitLayoutState,
    options: { persist: boolean }
  ): void {
    layout = next;
    if (
      pendingRelaunch &&
      layout.panes[pendingRelaunch.side] !== pendingRelaunch.forSessionId
    ) {
      // 等待期间那一栏被换掉 / 关掉：待换作废。
      clearPendingRelaunch();
    }

    const rightSession = layout.panes.right;
    if (rightSession === null) {
      const rightNodeId = nodeIdBySide.right;
      if (rightNodeId) {
        host.closeNode(rightNodeId);
        sessionIdByNodeId.delete(rightNodeId);
        nodeIdBySide = { ...nodeIdBySide, right: null };
      }
    } else if (nodeIdBySide.right) {
      if (sessionIdByNodeId.get(nodeIdBySide.right) !== rightSession) {
        activate(nodeIdBySide.right, rightSession);
      }
    } else {
      launchPane("right");
    }

    const leftSession = layout.panes.left;
    if (leftSession !== null) {
      if (nodeIdBySide.left) {
        if (sessionIdByNodeId.get(nodeIdBySide.left) !== leftSession) {
          activate(nodeIdBySide.left, leftSession);
        }
      } else {
        launchPane("left");
      }
    }

    focusActivePane();
    if (options.persist) persist();
    emit();
  }

  async function settle(): Promise<void> {
    while (pendingLaunches.size > 0) {
      await Promise.all([...pendingLaunches]);
    }
  }

  function dispatch(
    event: SplitLayoutEvent,
    options: { persist: boolean } = { persist: true }
  ): void {
    const next = reduceSplitLayout(layout, event, { minRatio: minRatio() });
    if (next === layout) {
      // 状态没变也可能有窗口层的活要干（例如首次 adopt），交给调用方决定。
      if (options.persist) persist();
      emit();
      return;
    }
    applyLayout(next, options);
  }

  function pairingSideOf(side: SplitSide): EmbeddedSplitPairingSide | null {
    const sessionId = layout.panes[side];
    if (sessionId === null || isFreshPane(side)) return null;
    const known = dragSessionById.get(sessionId);
    return {
      isImported: known?.isImported === true,
      sessionId,
      status: known?.status ?? null
    };
  }

  async function refreshPairs(): Promise<void> {
    const pairingHost = readPairingHost();
    if (!pairingHost) {
      pairingUnsupported = true;
      emit();
      return;
    }
    pairingBusy += 1;
    emit();
    try {
      pairs = (await pairingHost.listPeerPairs()).pairs;
      pairingUnsupported = false;
    } catch (error) {
      pairingUnsupported =
        error instanceof Error && error.message.trim() === "unsupported";
    } finally {
      pairingBusy -= 1;
      emit();
    }
  }

  async function pairPanes(): Promise<PairOnDropResult> {
    const pairingHost = readPairingHost();
    pairingBusy += 1;
    emit();
    try {
      const result = await pairOnDrop({
        focus: layout.focus,
        host: pairingHost,
        labels: labels(),
        left: pairingSideOf("left"),
        pairs,
        right: pairingSideOf("right"),
        toast
      });
      if (result.pairs) pairs = result.pairs;
      if (result.outcome === "unsupported" && !pairingHost) {
        pairingUnsupported = true;
      }
      if (result.relaunchedSessionId) {
        // 后端先重开了对端（PRD 故事 14）：不立刻换槽，等它在侧栏露面（见 pendingRelaunch）。
        const target = otherSide(layout.focus);
        const relaunched = result.relaunchedSessionId;
        const current = layout.panes[target];
        clearPendingRelaunch();
        if (current !== null && dragSessionById.has(relaunched)) {
          // 侧栏已经认识它（极少见：返回前就刷到了）——直接换。
          applyLayout(
            { ...layout, panes: { ...layout.panes, [target]: relaunched } },
            { persist: true }
          );
        } else if (current !== null) {
          pendingRelaunch = {
            forSessionId: current,
            sessionId: relaunched,
            side: target,
            timer:
              relaunchWaitMs > 0
                ? setTimeout(() => {
                    if (pendingRelaunch?.sessionId !== relaunched) return;
                    pendingRelaunch = null;
                    toast.error(labels().relaunchTimeout);
                    emit();
                  }, relaunchWaitMs)
                : null
          };
        }
      }
      return result;
    } finally {
      pairingBusy -= 1;
      emit();
    }
  }

  async function unpairPanes(): Promise<void> {
    const pairingHost = readPairingHost();
    const left = layout.panes.left;
    const right = layout.panes.right;
    if (!pairingHost || left === null || right === null) return;
    const index = conversationRailPeerPairIndex(pairs);
    const link = conversationRailPeerPairLinks(index, left).find(
      (candidate) => candidate.peer.sessionId?.trim() === right
    );
    if (!link) return;
    pairingBusy += 1;
    emit();
    try {
      await pairingHost.deletePeerPair({
        pairId: link.pairId,
        taskId: link.ownTaskId
      });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      pairingBusy -= 1;
      emit();
    }
    await refreshPairs();
  }

  /**
   * 放下区判定（PRD 故事 3/12）：
   * 左栏窗口才有 provider 栏 + 会话栏，它们是拖动的起点，指针还在那里面就不算
   * 「拖进主区」；越过会话栏右边缘之后，单栏比「详情区中点」、两栏比「当前分隔线」。
   * 折叠态只显示焦点栏，所以整片主区都归焦点栏。
   */
  /** 主区里「详情区」的左边界（相对主区左上角）：会话栏/provider 栏右缘。 */
  function detailLeftOffset(surface: EmbeddedSplitRect): number {
    const railRight = geometry.railRightEdge();
    return railRight === null ? 0 : Math.max(railRight - surface.left, 0);
  }

  function dropBoundary(surface: EmbeddedSplitRect): number {
    const detailLeft = detailLeftOffset(surface);
    return isSplitLayoutSplit(layout)
      ? surface.width * layout.ratio
      : (detailLeft + surface.width) / 2;
  }

  function dropZoneRect(
    side: SplitSide
  ): { left: number; width: number } | null {
    const surface = geometry.surfaceRect();
    if (!surface) return null;
    const detailLeft = detailLeftOffset(surface);
    if (collapsed) {
      return {
        left: detailLeft,
        width: Math.max(surface.width - detailLeft, 0)
      };
    }
    const boundary = dropBoundary(surface);
    return side === "left"
      ? { left: detailLeft, width: Math.max(boundary - detailLeft, 0) }
      : { left: boundary, width: Math.max(surface.width - boundary, 0) };
  }

  function resolveHoverSide(
    point: ConversationRailSplitPoint
  ): SplitSide | null {
    const surface = geometry.surfaceRect();
    if (!surface) return null;
    if (
      point.x < surface.left ||
      point.x > surface.left + surface.width ||
      point.y < surface.top ||
      point.y > surface.top + surface.height
    ) {
      return null;
    }
    const railRight = geometry.railRightEdge();
    if (railRight !== null && point.x < railRight) return null;
    if (collapsed) return layout.focus;
    return point.x - surface.left < dropBoundary(surface) ? "left" : "right";
  }

  function syncFromNodes(): void {
    // 折叠是纯显示态：只影响 CSS（藏起非焦点栏），两个槽、焦点、比例一个不改，
    // 也绝不写本地记录 —— 面板拉宽回来必须原样恢复两栏（PRD 故事 33/36）。
    collapsed =
      surfaceWidth() > 0 && surfaceWidth() < EMBEDDED_SPLIT_COLLAPSE_WIDTH_PX;

    let panes = layout.panes;
    for (const side of ["left", "right"] as const) {
      const nodeId = nodeIdBySide[side];
      // 空槽也要读：开机时 agent-gui 自己恢复上次那条会话，没人调过 select，
      // 左槽在我们眼里一直是空的；空槽 + 拖到右半边会被 left-fill 不变式收成单栏
      // （真机：第一次拖进去仍是一栏）。右槽没会话时窗口早被 applyLayout 关掉了，
      // `!nodeId` 这道判据已经拦住，不会凭空补出第二栏。
      if (!nodeId) continue;
      const node = nodeById(nodeId);
      if (!node) continue;
      // 空/与本槽同 = 无变化；与另一槽同 = 新窗口在 activate 落地前回声了另一栏的会话号（externalStateSource 落后于我们刚下的 activate），模型禁同号占两槽，绝不当用户切换——否则会把刚放下的第二栏当成重复收掉（真机验收 03 复现）。
      const observed = sessions.read(node);
      if (!observed) {
        // 报过真号之后又读成空 = 用户在这个窗口点了「新建会话」。只是刚起来还没
        // 初始化的窗口不算（否则右栏刚 launch 就会被当成空栏，落下即配对那一步会被跳过）。
        if (settledNodeIds.has(nodeId)) freshNodeIds.add(nodeId);
        continue;
      }
      settledNodeIds.add(nodeId);
      freshNodeIds.delete(nodeId);
      if (observed === panes[side] || observed === panes[otherSide(side)]) continue;
      sessionIdByNodeId.set(nodeId, observed);
      panes = { ...panes, [side]: observed };
    }
    if (panes !== layout.panes) {
      // 两栏落到同一条：留焦点栏，另一栏清空（同一条不占两槽）。
      if (panes.left !== null && panes.left === panes.right) {
        panes = { ...panes, [otherSide(layout.focus)]: null };
      }
      // 状态机的「单栏永远在 left」不变式在这里手工补一次（这不是一个事件）。
      let focus = layout.focus;
      if (panes.left === null && panes.right !== null) {
        panes = { left: panes.right, right: null };
      }
      if (panes.right === null) focus = "left";
      applyLayout({ ...layout, focus, panes }, { persist: true });
      return;
    }
    // 无条件 emit：覆盖层要在每次工作台状态变化后把 data-rndmaster-pane 重新投影到
    // 新挂上来的窗口壳上。嵌入模式下窗口不能拖不能缩，这个订阅本来就很少响。
    emit();
  }

  const unsubscribeController =
    host.controller?.subscribe(syncFromNodes) ?? (() => {});
  const unsubscribeSessions = sessions.subscribe?.(syncFromNodes) ?? (() => {});

  const unregisterSplitHost = registerConversationRailSplitHost({
    // 侧栏每渲染一条就报一次摘要（票 04）。**只有**当这条正显示在某一栏、且
    // 栏头会用到的字段真的变了才 emit：无条件 emit 会和侧栏形成
    //「emit → 重渲染 → 再上报」的循环。
    describeSession(session) {
      const previous = dragSessionById.get(session.id);
      dragSessionById.set(session.id, session);
      if (settleRelaunch(session.id)) return;
      if (sameHeaderSummary(previous, session)) return;
      if (
        layout.panes.left !== session.id &&
        layout.panes.right !== session.id
      ) {
        return;
      }
      emit();
    },
    getShownSessionIds: () => shownSessionIds,
    onDragCancel() {
      if (!dragging) return;
      dragging = null;
      emit();
    },
    onDragEnd(point) {
      const active = dragging;
      dragging = null;
      emit();
      if (!active) return;
      const side = resolveHoverSide(point);
      if (!side) return;
      void dropSession(active.session, side).then(() => pairPanes());
    },
    onDragMove(point) {
      if (!dragging) return;
      dragging = {
        hoverSide: resolveHoverSide(point),
        point,
        session: dragging.session
      };
      emit();
    },
    onDragStart(session, point) {
      dragSessionById.set(session.id, session);
      dragging = { hoverSide: resolveHoverSide(point), point, session };
      emit();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  });

  async function dropSession(
    session: ConversationRailSplitDragSession,
    side: SplitSide
  ): Promise<void> {
    dragSessionById.set(session.id, session);
    dispatch({ id: session.id, side, type: "drop" });
    await settle();
  }

  const controller: EmbeddedSplitViewController = {
    async adopt(nodeIds) {
      nodeIdBySide = {
        left: nodeIds[0] ?? null,
        right: nodeIds[1] ?? null
      };
      for (const nodeId of nodeIds) {
        const node = nodeById(nodeId);
        const observed = node ? sessions.read(node) : null;
        if (observed) sessionIdByNodeId.set(nodeId, observed);
      }
      collapsed =
        surfaceWidth() > 0 && surfaceWidth() < EMBEDDED_SPLIT_COLLAPSE_WIDTH_PX;
      const stored = storage ? loadSplitLayout(storage, storageKey) : null;
      // 开机时 workbench 并不知道有哪些会话（会话列表是 gui 自己的数据，宿主快照里
      // 没有），所以「存活」判据无从取得：一律当存活，激活不到就由 gui 自己退回首页。
      const alive = [stored?.panes.left, stored?.panes.right].filter(
        (id): id is string => typeof id === "string" && id.length > 0
      );
      const restored = reduceSplitLayout(
        createEmptySplitLayout(),
        { alive, stored: stored ?? null, type: "restore" },
        { minRatio: minRatio() }
      );
      // 没存过布局时左槽是空的，可窗口里其实已经开着一条会话。不种进去，
      // 用户第一次拖第二条进来就会被 left-fill 挪回左槽 —— 看着像「分栏没生效」。
      const observedLeft = nodeIdBySide.left
        ? (sessionIdByNodeId.get(nodeIdBySide.left) ?? null)
        : null;
      applyLayout(
        restored.panes.left === null && observedLeft !== null
          ? { ...restored, panes: { ...restored.panes, left: observedLeft } }
          : restored,
        { persist: false }
      );
      await settle();
      await refreshPairs();
    },
    async closePane(side) {
      dispatch({ side, type: "close" });
      await settle();
    },
    dispose() {
      disposed = true;
      clearPendingRelaunch();
      unregisterSplitHost();
      unsubscribeController();
      unsubscribeSessions();
      listeners.clear();
    },
    dropSession,
    dropZoneRect,
    getSnapshot: () => snapshot,
    pairPanes,
    resetRatio() {
      dispatch({ ratio: 0.5, type: "resize" });
    },
    setRailPushPx(px) {
      const next = Number.isFinite(px) && px > 0 ? Math.round(px) : 0;
      if (next === railPushPx) return;
      railPushPx = next;
      emit();
    },
    resize(ratio) {
      dispatch({ ratio, type: "resize" });
    },
    select(agentSessionId) {
      const id = agentSessionId.trim();
      if (!id) return false;
      dispatch({ id, type: "select" });
      return true;
    },
    setFocus(side) {
      if (layout.focus === side) return;
      if (layout.panes[side] === null) return;
      layout = { ...layout, focus: side };
      focusActivePane();
      persist();
      emit();
    },
    sideForNodeId(nodeId) {
      if (nodeIdBySide.left === nodeId) return "left";
      if (nodeIdBySide.right === nodeId) return "right";
      return null;
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    /**
     * 交换左右（票 04 的栏头 ⋮）。换的是**会话**不是窗口：左栏那个窗口是嵌入模式的
     * 锚（会话栏/provider 栏都长在它上面，几何测量也按「DOM 里靠前的那个是左栏」
     * 取），把 nodeId 对调会把这些前提一起拆掉。所以只把两个槽里的会话号对调，
     * 由 applyLayout 给两个窗口各自 activate 一次；焦点跟着原来那条会话走到对面。
     */
    swapPanes() {
      const left = layout.panes.left;
      const right = layout.panes.right;
      if (left === null || right === null) return;
      applyLayout(
        {
          ...layout,
          focus: otherSide(layout.focus),
          panes: { left: right, right: left }
        },
        { persist: true }
      );
    },
    unpairPanes
  };

  emit();
  return controller;
}

// ---------------------------------------------------------------------------
// 模块级注册口（形状照 0099 的 embeddedHostCreatedSessionOpener / 0103 的配对端口）。
// 嵌入层的 body 包裹与 React 覆盖层都在 contributions memo 之外拿不到 controller 实例，
// 走注册口比把 ref 串过整棵树干净。
// ---------------------------------------------------------------------------

let registeredController: EmbeddedSplitViewController | null = null;
let unsubscribeRegistered: (() => void) | null = null;
const registryListeners = new Set<() => void>();

function notifyRegistry(): void {
  for (const listener of [...registryListeners]) {
    listener();
  }
}

export function registerEmbeddedSplitViewController(
  controller: EmbeddedSplitViewController
): () => void {
  unsubscribeRegistered?.();
  registeredController = controller;
  unsubscribeRegistered = controller.subscribe(notifyRegistry);
  notifyRegistry();
  return () => {
    if (registeredController !== controller) return;
    unsubscribeRegistered?.();
    unsubscribeRegistered = null;
    registeredController = null;
    notifyRegistry();
  };
}

export function embeddedSplitViewController(): EmbeddedSplitViewController | null {
  return registeredController;
}

/** 覆盖层读的快照：controller 每次 emit 才换引用，适合 useSyncExternalStore。 */
export function embeddedSplitViewSnapshot(): EmbeddedSplitViewSnapshot | null {
  return registeredController?.getSnapshot() ?? null;
}

export function subscribeEmbeddedSplitView(listener: () => void): () => void {
  registryListeners.add(listener);
  return () => {
    registryListeners.delete(listener);
  };
}

/**
 * 给窗口 body 用的一行式描述：`"<side>|<focus 0|1>|<宽度占比>"`，没有分栏时是空串。
 * 之所以拼成字符串：useSyncExternalStore 靠引用相等判「没变」，返回对象会每次重渲染。
 */
export function embeddedSplitViewPaneDescriptor(nodeId: string): string {
  const controller = registeredController;
  if (!controller) return "";
  const side = controller.sideForNodeId(nodeId);
  if (!side) return "";
  const snapshot = controller.getSnapshot();
  const split = snapshot.panes.right !== null;
  const focused = snapshot.focus === side;
  // 推出来的那块地方是**左栏的**：左栏的 fraction 要加上它，右栏减去它。
  // 这个 fraction 决定 agent-gui 拿到的容器宽度（见 embeddedDintalDock.ts 的
  // projectEmbeddedDintalDockNodeContext），必须和 CSS 画的壳宽严格一致。
  const fraction =
    !split || snapshot.collapsed
      ? 1
      : side === "left"
        ? snapshot.ratio + snapshot.railPushRatio
        : 1 - snapshot.ratio - snapshot.railPushRatio;
  return `${side}|${focused ? "1" : "0"}|${fraction.toFixed(4)}`;
}

export interface EmbeddedSplitPaneDescriptor {
  focused: boolean;
  fraction: number;
  side: SplitSide;
}

export function parseEmbeddedSplitViewPaneDescriptor(
  descriptor: string
): EmbeddedSplitPaneDescriptor | null {
  if (!descriptor) return null;
  const [side, focused, fraction] = descriptor.split("|");
  if (side !== "left" && side !== "right") return null;
  const parsed = Number.parseFloat(fraction ?? "1");
  return {
    focused: focused === "1",
    fraction: Number.isFinite(parsed) && parsed > 0 ? parsed : 1,
    side
  };
}
