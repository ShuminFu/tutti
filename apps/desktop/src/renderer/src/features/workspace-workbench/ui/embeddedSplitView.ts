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
  notifyConversationRailPeerPairsChanged,
  reduceSplitLayout,
  registerConversationRailSplitHost,
  resolveSplitPairSelection,
  splitLayoutStorageKey,
  subscribeConversationRailPeerPairsChanged,
  type AgentPromptSubmitPreparation,
  type ConversationRailPeerPair,
  type ConversationRailPeerPairEndpoint,
  type ConversationRailPeerPairingHost,
  type ConversationRailSplitDragSession,
  type ConversationRailSplitPoint,
  type SplitLayoutEvent,
  type SplitLayoutState,
  type SplitPairSelectMemory,
  type SplitSide
} from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  createEmbeddedSplitLayoutStore,
  splitPairOrderKey,
  type EmbeddedSplitLayoutStore,
  type SplitPairOrderRecord
} from "./embeddedSplitLayoutStore.ts";
import {
  agentGuiWorkbenchGoHomeActivationType,
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

/** 结对模式里的角色（后端词 developer / reviewer）。 */
export type EmbeddedSplitPairRole = "developer" | "reviewer";

/** composer 上方单选的三个取值：独立模式 / 结对·开发者 / 结对·审查者。 */
export type EmbeddedSplitPairModeChoice = "solo" | EmbeddedSplitPairRole;

/**
 * 分栏结对模式（peer-pair-mode 票 04）的投影。**不是**前端自己存的一份状态：
 * 每一项都由配对表里「连着左右两栏的那一行」现算（PRD「一个事实一个所有者」），
 * 写只走 `setPairMode` → 宿主 → 后端，写完重拉配对表。
 * 为 null 时整排单选与栏头角色胶囊都不渲染（PRD D6）。
 */
export interface EmbeddedSplitPairModeSnapshot {
  /** 正在 setPeerPairMode：单选暂时不可点，免得连点发出互相覆盖的两次写。 */
  busy: boolean;
  kickoffState: "" | "pending" | "sent";
  mode: "solo" | "pair";
  pairId: string;
  /**
   * 每栏的角色：按「这一栏那条会话的 task 是不是 developerTaskId」算，
   * 所以交换左右栏之后角色跟着会话走，而不是跟着左右走。
   */
  roles: {
    left: EmbeddedSplitPairRole | null;
    right: EmbeddedSplitPairRole | null;
  };
}

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
  /** 结对模式单选 / 角色胶囊的投影；不满足显示条件时为 null（票 04）。 */
  pairMode: EmbeddedSplitPairModeSnapshot | null;
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
  /**
   * 布局与「这一对上次怎么摆」的耐久存储（票 03/04）。默认宿主优先、
   * localStorage 兜底；单测传桩。
   */
  layoutStore?: EmbeddedSplitLayoutStore | null;
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
  /**
   * 结对第一句的开工卡准备（票 05）。只在「这条会话所在的一对是 pair 模式且
   * kickoffState=pending」时给出前缀；其余一律 null（不拦截）。
   */
  preparePairKickoff(input: {
    agentSessionId: string;
    text: string;
  }): Promise<AgentPromptSubmitPreparation | null>;
  /** 同步预判版：给提交链路决定要不要走异步准备（不要时零延迟）。 */
  wantsPairKickoff(agentSessionId: string): boolean;
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
  /** composer 上方单选：从 `side` 这一栏的视角选模式 / 角色（票 04）。 */
  setPairMode(
    side: SplitSide,
    choice: EmbeddedSplitPairModeChoice
  ): Promise<void>;
  /** 这条会话现在在哪一栏（给 composer 上方那排单选认栏用）。 */
  sideForSessionId(agentSessionId: string): SplitSide | null;
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
  kickoffCommitFailed: "Your partner did not receive the pairing kickoff card",
  kickoffPreviewFailed:
    "Could not prepare the pairing kickoff card; sent as a normal message",
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
      // Explicit Home selection is authoritative, not a missing snapshot.
      if (value === null) return null;
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
  const layoutStore =
    input.layoutStore === undefined
      ? createEmbeddedSplitLayoutStore({
          scopeKey: storageKey,
          storage,
          targetId: input.targetId?.trim() || "default"
        })
      : input.layoutStore;

  let layout: SplitLayoutState = createEmptySplitLayout();
  /** 「这一对上次怎么摆 / 上次配的是谁」：key 见 splitPairOrderKey，值里带 usedAt。 */
  let pairOrder: SplitPairOrderRecord = {};
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
  // 结对模式写操作在途计数（票 04）。
  let pairModeBusy = 0;
  // 宿主桥对结对模式三件套回过 unsupported：粘住，整排单选不再渲染（PRD D6）。
  // 与 pairingUnsupported 分开记：老宿主可能支持配对、不支持结对模式。
  let pairModeUnsupported = false;
  // 开工卡「已 preview、还没 commit 完」的 pairId（票 05）。这不是第二份
  // kickoffState：它只挡住同一对在这段在途时间里的第二句又拼一张卡，
  // commit 结束（成败都算）就摘掉，真相仍以配对表里的 kickoffState 为准。
  const kickoffInFlight = new Set<string>();
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
  const relaunchWaitMs =
    input.relaunchWaitMs ?? EMBEDDED_SPLIT_RELAUNCH_WAIT_MS;

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
  // 已经发过「回首页」、还没看到窗口把会话号清掉的那些窗口：nodeId → 请它关掉的
  // 那条会话号。窗口自己的 `lastActiveAgentSessionId` 要等宿主状态一来一回才会变空，
  // 这中间 `syncFromNodes` 读到的仍是旧号，不挡就会把刚关掉的会话又认回槽里
  // （表现：✕ 点了没反应，还停在这条会话）。
  const goingHomeByNodeId = new Map<string, string>();
  // 刚 activate 过、还没换过来的窗口：值是「我们请它显示的那条」和「它这会儿还在
  // 显示的那条」。activateNode 是异步的，中间这几拍它照旧报旧会话号；不记这一笔，
  // 整组切换之后右栏那一拍的旧号会被当成「用户在右栏切了会话」，把刚换掉的那条
  // 塞回来（真机：分支③整组切换退化成只换左栏）。
  const pendingActivateByNodeId = new Map<
    string,
    { expected: string; stale: string }
  >();

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

  /** 连着左右两栏那两条会话的配对行（无序比较）；任一栏空 / 没配对时为 null。 */
  function splitPeerPair(): ConversationRailPeerPair | null {
    // 先判空槽再问 isFreshPane：buildSnapshot 在控制器初始化时就会跑一次，
    // 那时 freshNodeIds 还在暂时性死区里，空布局必须在碰它之前就返回。
    if (layout.panes.left === null || layout.panes.right === null) return null;
    const left = isFreshPane("left") ? null : layout.panes.left;
    const right = isFreshPane("right") ? null : layout.panes.right;
    if (left === null || right === null) return null;
    return (
      pairs.find((pair) => {
        const a = pair?.a?.sessionId?.trim() ?? "";
        const b = pair?.b?.sessionId?.trim() ?? "";
        return (a === left && b === right) || (a === right && b === left);
      }) ?? null
    );
  }

  function endpointOf(
    pair: ConversationRailPeerPair,
    sessionId: string | null
  ): ConversationRailPeerPairEndpoint | null {
    if (!sessionId) return null;
    if (pair.a?.sessionId?.trim() === sessionId) return pair.a;
    if (pair.b?.sessionId?.trim() === sessionId) return pair.b;
    return null;
  }

  /**
   * 一条会话在这一对里的角色。判据**只看 task**：本端 task id 等于
   * developerTaskId 就是开发者，否则是审查者 —— 不看它在左栏还是右栏，
   * 交换左右之后角色才会跟着会话走（票 04 回退看红钉的就是这一条）。
   */
  function pairRoleOf(
    pair: ConversationRailPeerPair,
    sessionId: string | null
  ): EmbeddedSplitPairRole | null {
    if (pair.pairMode !== "pair") return null;
    const developerTaskId = pair.developerTaskId?.trim() ?? "";
    if (!developerTaskId) return null;
    const own = endpointOf(pair, sessionId);
    if (!own) return null;
    return own.taskId?.trim() === developerTaskId ? "developer" : "reviewer";
  }

  /**
   * 票 04 的显示条件，四条缺一不画：
   *   ① 分栏（两栏都有会话）；② 两栏之间已配对（配对表里有这一行）；
   *   ③ 两栏都是托管会话（不是导入的历史会话）；④ 宿主支持结对模式
   *      —— 配对行上带 pairMode 字段，且三件套没回过 unsupported。
   * 刻意不看 pairingState()：它在 refreshPairs 在途时是 "pending"，
   * 拿它判会让单选每次刷新都闪一下。
   */
  function pairModeSnapshot(): EmbeddedSplitPairModeSnapshot | null {
    if (pairingUnsupported || pairModeUnsupported) return null;
    const pair = splitPeerPair();
    if (!pair || pair.pairMode === undefined) return null;
    const host = readPairingHost();
    if (
      !host?.setPeerPairMode ||
      !host.previewPairKickoff ||
      !host.commitPairKickoff
    ) {
      return null;
    }
    const left = pairingSideOf("left");
    const right = pairingSideOf("right");
    if (!left || !right || left.isImported || right.isImported) return null;
    const mode = pair.pairMode === "pair" ? "pair" : "solo";
    return {
      busy: pairModeBusy > 0,
      kickoffState:
        pair.kickoffState === "pending" || pair.kickoffState === "sent"
          ? pair.kickoffState
          : "",
      mode,
      pairId: pair.pairId,
      roles: {
        left: pairRoleOf(pair, left.sessionId),
        right: pairRoleOf(pair, right.sessionId)
      }
    };
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
      pairMode: pairModeSnapshot(),
      panes: { left: paneOf("left"), right: paneOf("right") },
      railPushRatio: railPushRatio(),
      ratio: layout.ratio
    };
  }

  function refreshShownSessionIds(): void {
    const next = [
      isFreshPane("left") ? null : layout.panes.left,
      isFreshPane("right") ? null : layout.panes.right
    ].filter((id): id is string => typeof id === "string" && id.length > 0);
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
    if (!layoutStore) return;
    layoutStore.write({ layout, pairOrder });
  }

  /**
   * 这条会话**正在结对编程**的对端（配对行 pairMode === "pair"）。
   * 空心徽标（配对在、已切独立）与老宿主报不出 pairMode 的都不算，
   * 与徽标两态口径一致（DINTAL-5331）——点它们还是单列。
   */
  function activePartnersOf(sessionId: string): readonly string[] {
    const index = conversationRailPeerPairIndex(pairs);
    return conversationRailPeerPairLinks(index, sessionId)
      .filter((link) => link.pairMode === "pair")
      .map((link) => link.peer.sessionId?.trim() ?? "")
      .filter((id) => id && id !== sessionId);
  }

  /**
   * 记忆读侧：`lastPartnerOf` 不单独存一份，从 pairOrder 里「含这条会话、usedAt
   * 最大」的那条反推 —— 两份缓存迟早互相打架（CLAUDE 教训：一个事实一个所有者）。
   */
  const pairSelectMemory: SplitPairSelectMemory = {
    lastPartnerOf(sessionId) {
      const id = sessionId.trim();
      if (!id) return null;
      let best: { partner: string; usedAt: number } | null = null;
      for (const entry of Object.values(pairOrder)) {
        const partner =
          entry.left === id
            ? entry.right
            : entry.right === id
              ? entry.left
              : "";
        if (!partner) continue;
        if (!best || entry.usedAt > best.usedAt) {
          best = { partner, usedAt: entry.usedAt };
        }
      }
      return best?.partner ?? null;
    },
    orderOf(a, b) {
      const entry = pairOrder[splitPairOrderKey(a, b)];
      return entry ? ([entry.left, entry.right] as const) : null;
    }
  };

  /**
   * 把「这一对此刻的左右」记下来。usedAt 强制单调递增：同一毫秒内连着换两对时
   * `Date.now()` 会打平，`lastPartnerOf` 就会挑回更早的那一对（真机上表现为
   * 「明明刚开过 C，点回去又变成 B」）。
   */
  function rememberPairOrder(left: string, right: string): void {
    const a = left.trim();
    const b = right.trim();
    if (!a || !b || a === b) return;
    const highest = Object.values(pairOrder).reduce(
      (max, entry) => Math.max(max, entry.usedAt ?? 0),
      0
    );
    pairOrder = {
      ...pairOrder,
      [splitPairOrderKey(a, b)]: {
        left: a,
        right: b,
        usedAt: Math.max(Date.now(), highest + 1)
      }
    };
  }

  function nodeById(nodeId: string): EmbeddedSplitViewNode | null {
    return host.getSnapshot().nodes.find((node) => node.id === nodeId) ?? null;
  }

  function activate(nodeId: string, agentSessionId: string): void {
    goingHomeByNodeId.delete(nodeId);
    const stale = sessionIdByNodeId.get(nodeId) ?? "";
    if (stale && stale !== agentSessionId) {
      pendingActivateByNodeId.set(nodeId, { expected: agentSessionId, stale });
    } else {
      pendingActivateByNodeId.delete(nodeId);
    }
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
  async function launchPaneNode(
    agentSessionId: string
  ): Promise<string | null> {
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
  /**
   * 请这个窗口退回首页态（新建会话落地页）。单栏下栏头的 ✕ 关的是**会话**，可左栏
   * 那个窗口是嵌入模式的锚、不能关，所以只能反过来告诉它「别再显示这条了」。
   * 宿主这边先把自己记的会话号抹掉，免得下一次 applyLayout 以为窗口还停在旧会话上。
   */
  function goHome(nodeId: string): void {
    const shown = sessionIdByNodeId.get(nodeId) ?? null;
    if (shown === null) return;
    goingHomeByNodeId.set(nodeId, shown);
    sessionIdByNodeId.delete(nodeId);
    pendingActivateByNodeId.delete(nodeId);
    host.activateNode(
      { nodeId },
      { type: agentGuiWorkbenchGoHomeActivationType }
    );
  }

  function applyLayout(
    next: SplitLayoutState,
    options: { persist: boolean }
  ): void {
    layout = next;
    // 两栏都有人就把此刻的左右记下来：交换左右、拖出分栏、整组切换走的都是这里，
    // 下次再点开这一对就照这个顺序摆（票 03）。
    if (layout.panes.left !== null && layout.panes.right !== null) {
      rememberPairOrder(layout.panes.left, layout.panes.right);
    }
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
        pendingActivateByNodeId.delete(rightNodeId);
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
    } else if (nodeIdBySide.left) {
      // 左槽空 = 首页态。左栏窗口不能关（它是嵌入模式的锚），所以要显式请它回首页；
      // 不请的话它照旧显示着刚关掉的那条会话，`syncFromNodes` 再把它认回槽里。
      goHome(nodeIdBySide.left);
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

  // 侧栏右键菜单的配对/解除写的是它自己那份缓存（补丁 0130），本控制器这份
  // 只在自己 pairPanes/unpairPanes 之后刷新 —— 不订阅的话，从侧栏解除后分栏
  // 栏头还会一直显示「解除」。refreshPairs 自己不广播，所以不会绕成环。
  // 自己广播时置位：本控制器也订阅着这条广播，不挡一下就会在刚 refreshPairs
  // 完又拉一次（多一次 listPeerPairs，测试里的调用次数也对不上）。
  let broadcastingOwnPairChange = false;
  const unsubscribePeerPairs = subscribeConversationRailPeerPairsChanged(() => {
    if (disposed || broadcastingOwnPairChange) return;
    void refreshPairs();
  });

  function broadcastPeerPairsChanged(): void {
    broadcastingOwnPairChange = true;
    try {
      notifyConversationRailPeerPairsChanged();
    } finally {
      broadcastingOwnPairChange = false;
    }
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
      // 配好了就告诉侧栏那份缓存，否则右键菜单一直是 `Unpair (0)`（补丁 0130）。
      if (result.outcome === "paired") broadcastPeerPairsChanged();
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

  function errorText(error: unknown): string {
    return error instanceof Error ? error.message : String(error);
  }

  function isUnsupported(error: unknown): boolean {
    return error instanceof Error && error.message.trim() === "unsupported";
  }

  /** 宿主写回来的那一行替换进缓存（形状不全就不替换，等 refreshPairs 兜底）。 */
  function replacePair(next: ConversationRailPeerPair | undefined): void {
    if (!next?.pairId || !next.a || !next.b) return;
    pairs = pairs.map((pair) => (pair.pairId === next.pairId ? next : pair));
  }

  /**
   * 票 04：从 `side` 这一栏的视角选。开发者 → developerTaskId 是本栏的 task；
   * 审查者 → 是另一栏的 task；独立模式 → 整对退回 solo（两栏一起退出）。
   * 不做乐观更新：单选只投影配对表，写失败时它自然还是行里的值。
   */
  async function setPairMode(
    side: SplitSide,
    choice: EmbeddedSplitPairModeChoice
  ): Promise<void> {
    // 评审补充 2：上一次写还没回来就不接新的选择（连点 / 键盘连按），免得两次写互相覆盖。
    if (pairModeBusy > 0) return;
    const pairingHost = readPairingHost();
    const current = pairModeSnapshot();
    const pair = splitPeerPair();
    if (!pairingHost?.setPeerPairMode || !current || !pair) return;
    const currentChoice: EmbeddedSplitPairModeChoice =
      current.mode === "pair" && current.roles[side]
        ? current.roles[side]
        : "solo";
    if (currentChoice === choice) return;
    const own = endpointOf(pair, layout.panes[side]);
    const other = endpointOf(pair, layout.panes[otherSide(side)]);
    if (!own || !other) return;
    const developer = choice === "developer" ? own : other;
    const input =
      choice === "solo"
        ? { mode: "solo" as const, pairId: pair.pairId }
        : {
            // 契约允许 task id 或 Tutti 会话号；task id 缺时退回会话号让后端归一。
            developerTaskId:
              developer.taskId?.trim() || developer.sessionId?.trim() || "",
            mode: "pair" as const,
            pairId: pair.pairId
          };
    pairModeBusy += 1;
    emit();
    let wrote = false;
    try {
      const result = await pairingHost.setPeerPairMode(input);
      replacePair(result?.pair);
      wrote = true;
    } catch (error) {
      if (isUnsupported(error)) {
        pairModeUnsupported = true;
      } else {
        // 后端那句人话原样端出来（400 invalid_pair_mode / 409 pair_revoked …）。
        toast.error(errorText(error));
      }
    } finally {
      pairModeBusy -= 1;
      emit();
    }
    // 写成功才广播（补丁 0130 的约定：刷新路径本身绝不 notify）。
    if (wrote) broadcastPeerPairsChanged();
    await refreshPairs();
  }

  /** 这条会话此刻显示在哪一栏（空槽 / 新建占位栏不算）。 */
  function sideOfSession(agentSessionId: string): SplitSide | null {
    const id = agentSessionId.trim();
    if (!id) return null;
    if (!isFreshPane("left") && layout.panes.left === id) return "left";
    if (!isFreshPane("right") && layout.panes.right === id) return "right";
    return null;
  }

  /**
   * 这条会话要不要拼开工卡：只认**连着左右两栏的那一对**，且这条会话此刻就在某一栏里
   * （评审 D）。不回退去翻整张配对表——同一条会话可能还有别的配对行处在 pending，
   * 分栏里没露面的那一对不该劫持这句话（单选也只投影分栏这一对）。
   */
  function pendingKickoffPairFor(
    agentSessionId: string
  ): ConversationRailPeerPair | null {
    const sessionId = agentSessionId.trim();
    if (!sessionId || pairingUnsupported || pairModeUnsupported) return null;
    if (sideOfSession(sessionId) === null) return null;
    const pairingHost = readPairingHost();
    if (!pairingHost?.previewPairKickoff || !pairingHost.commitPairKickoff) {
      return null;
    }
    const isPending = (pair: ConversationRailPeerPair): boolean =>
      pair.pairMode === "pair" &&
      pair.kickoffState === "pending" &&
      endpointOf(pair, sessionId) !== null &&
      !kickoffInFlight.has(pair.pairId);
    const split = splitPeerPair();
    return split && isPending(split) ? split : null;
  }

  function wantsPairKickoff(agentSessionId: string): boolean {
    return pendingKickoffPairFor(agentSessionId) !== null;
  }

  /**
   * 票 05 的顺序：preview（纯函数、不写库）→ 由提交链路把块拼在用户原文前面发出 →
   * 引擎确认接受后才 commit（PRD D4）。preview 失败不拦截、原样发送并提示；
   * commit 失败提示「搭档没收到开工卡」并保持 pending，下一句重试。
   */
  async function preparePairKickoff(input: {
    agentSessionId: string;
    text: string;
  }): Promise<AgentPromptSubmitPreparation | null> {
    const goal = input.text.trim();
    if (!goal || goal.startsWith("/")) return null;
    const pair = pendingKickoffPairFor(input.agentSessionId);
    const pairingHost = readPairingHost();
    if (
      !pair ||
      !pairingHost?.previewPairKickoff ||
      !pairingHost.commitPairKickoff
    ) {
      return null;
    }
    const sender = endpointOf(pair, input.agentSessionId.trim());
    const senderTaskId = sender?.taskId?.trim() || input.agentSessionId.trim();
    const request = { goal, pairId: pair.pairId, senderTaskId };
    // 评审 A：拍下 preview 时的开发者。块里的角色是按它写的；到 commit 时行里的
    // developer 变了，这张卡就是错的，不能投（本地缓存先挡一道，后端 409 再挡一道）。
    const expectedDeveloperTaskId = pair.developerTaskId?.trim() ?? "";
    kickoffInFlight.add(pair.pairId);
    let block = "";
    try {
      block = (await pairingHost.previewPairKickoff(request)).block ?? "";
    } catch (error) {
      kickoffInFlight.delete(pair.pairId);
      if (isUnsupported(error)) {
        pairModeUnsupported = true;
        emit();
      } else {
        toast.error(
          [labels().kickoffPreviewFailed, errorText(error)]
            .filter(Boolean)
            .join("：")
        );
      }
      // 评审 C：preview 失败多半是缓存旧了（409 kickoff_not_pending：别处已开工 / 已退回
      // 独立模式）。不重拉的话下一句还会按旧行拦截、再失败一次。
      void refreshPairs();
      return null;
    }
    if (!block.trim()) {
      kickoffInFlight.delete(pair.pairId);
      return null;
    }
    const commitHost = pairingHost;
    return {
      prefix: block,
      onAccepted: async () => {
        // 评审 A：等引擎接受的这段时间里用户可能换了角色 / 退回独立模式。缓存里这一行
        // 已经不是 preview 时的样子，就别投了：提示、清在途、重拉，下一句按新行重新拼卡。
        const latest = pairs.find(
          (candidate) => candidate.pairId === pair.pairId
        );
        if (
          !latest ||
          latest.pairMode !== "pair" ||
          (latest.developerTaskId?.trim() ?? "") !== expectedDeveloperTaskId
        ) {
          kickoffInFlight.delete(pair.pairId);
          toast.error(
            labels().kickoffRolesChanged ?? labels().kickoffCommitFailed ?? ""
          );
          emit();
          await refreshPairs();
          return;
        }
        let committed = false;
        try {
          const result = await commitHost.commitPairKickoff!({
            ...request,
            ...(expectedDeveloperTaskId ? { expectedDeveloperTaskId } : {})
          });
          // 行是真相：delivered=false 时后端那一行仍是 pending，照样换进缓存。
          replacePair(result?.pair);
          if (result?.delivered !== true) {
            // 契约补充：搭档那张卡被环路闸 / 限流丢了。按 commit 失败处理——
            // 提示、不动本栏，下一句再带卡重试。
            toast.error(
              [labels().kickoffCommitFailed, result?.reason?.trim()]
                .filter(Boolean)
                .join("：")
            );
          } else {
            committed = true;
          }
        } catch (error) {
          // 保持 pending：缓存里那一行不动，下一句会再拼一次卡、再 commit 一次。
          toast.error(
            [labels().kickoffCommitFailed, errorText(error)]
              .filter(Boolean)
              .join("：")
          );
          // 评审 C：失败不等于没投——超时的那次后端可能已经记成 sent，409
          // kickoff_roles_changed 说明角色已变。重拉让缓存对齐真相，免得下一句按旧行再拼卡。
          void refreshPairs();
        } finally {
          kickoffInFlight.delete(pair.pairId);
          emit();
        }
        if (committed) {
          broadcastPeerPairsChanged();
          await refreshPairs();
        }
      },
      onRejected: () => {
        kickoffInFlight.delete(pair.pairId);
      }
    };
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
    broadcastPeerPairsChanged();
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
    // 两栏时的分界线画在 `ratio + railPush` 处（CSS 的壳几何、分隔线、栏头、
    // 投给 agent-gui 的 fraction 全是这个和）。放下区与命中判定必须同源：
    // 只用 ratio 会把整片主区从正中切开，预览框不跟分界线走，而中间那条
    // 「看着是左栏、判成右栏」的带子还会把会话丢错栏（票 05 回归）。
    return isSplitLayoutSplit(layout)
      ? surface.width * (layout.ratio + railPushRatio())
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
        // 初始化的窗口不算（否则右栏刚 launch 就被当成空栏，落下即配对那一步会被跳过）。
        goingHomeByNodeId.delete(nodeId);
        if (settledNodeIds.has(nodeId)) freshNodeIds.add(nodeId);
        continue;
      }
      // 刚请它回首页、它还没把旧号清掉：这一拍读到的旧号不是「用户切了会话」。
      if (goingHomeByNodeId.get(nodeId) === observed) continue;
      // 刚 activate 到别的会话、它还没换过来：同理，这一拍的旧号不是用户切的。
      const pendingActivate = pendingActivateByNodeId.get(nodeId);
      if (pendingActivate) {
        if (observed === pendingActivate.expected) {
          pendingActivateByNodeId.delete(nodeId);
        } else if (observed === pendingActivate.stale) {
          continue;
        } else {
          // 报的既不是旧号也不是我们请的那条 = 用户真的又切了别的，待换作废。
          pendingActivateByNodeId.delete(nodeId);
        }
      }
      settledNodeIds.add(nodeId);
      freshNodeIds.delete(nodeId);
      if (observed === panes[side] || observed === panes[otherSide(side)])
        continue;
      sessionIdByNodeId.set(nodeId, observed);
      // 侧栏点选走的就是这条路（会话栏只在左栏那个窗口里，右栏用 CSS 隐掉了）：
      // 用户在列表里点了另一条会话，这个窗口自己换了会话号，我们才看见。
      // 正在结对编程的会话要按票 01/02 分三条去向，不能一律原地顶替这一栏。
      const resolved =
        side === "left"
          ? resolveSplitPairSelection({
              activePartnersOf,
              id: observed,
              layout: { ...layout, panes },
              memory: pairSelectMemory
            })
          : { kind: "single" as const, id: observed };
      // 左栏这一下重写的是**整个**布局，右栏窗口这一拍还报着旧会话号（它要等
      // applyLayout 去关/去换）。这时继续看右栏，会把刚被收掉的那条当成「用户在
      // 右栏切了会话」再塞回来——真机上表现为「点没结对的会话收不成单列」。
      if (resolved.kind === "group") {
        panes = { left: resolved.left, right: resolved.right };
        break;
      }
      if (resolved.kind === "single" && side === "left") {
        // 没结对（含空心徽标）→ 收成单列全宽；右栏窗口由 applyLayout 关掉。
        panes = { left: observed, right: null };
        break;
      }
      // swap-left / 右栏自己换了会话：原地顶替这一栏，另一栏不动。
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
      // 耐久存储优先（票 04）：iframe 的 localStorage 会随宿主重启换 origin 而丢，
      // 「退出重进分栏没了」就是这么来的。宿主没这个能力时 store 自己退回本地。
      const restoredStore = layoutStore
        ? await layoutStore.read()
        : { layout: null, pairOrder: {} };
      pairOrder = restoredStore.pairOrder ?? {};
      const stored = restoredStore.layout;
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
      unsubscribePeerPairs();
      unsubscribeController();
      unsubscribeSessions();
      listeners.clear();
    },
    dropSession,
    dropZoneRect,
    getSnapshot: () => snapshot,
    pairPanes,
    preparePairKickoff,
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
    setPairMode,
    sideForSessionId: sideOfSession,
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
    unpairPanes,
    wantsPairKickoff
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
