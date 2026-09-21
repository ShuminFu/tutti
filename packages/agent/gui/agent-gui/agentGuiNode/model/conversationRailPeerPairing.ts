// 会话配对（rndmaster 补丁 0103）的纯模型层。
//
// 这里只放「给定配对表 + 条目列表 + 当前会话 → 徽标数 / 顺序」这类不依赖 React、
// 不依赖宿主桥的计算，方便单测直接钉住外部行为（回退看红要能生效）。
//
// 宿主契约（票 03 实现，形状不可自创）：
//   listPeerPairs()  -> { pairs: [{ pairId, a: Endpoint, b: Endpoint }] }
//   Endpoint         =  { taskId, sessionId, provider, alias, title, cwd, status }
// 其中 sessionId 是 Tutti 会话号，与侧栏条目 id 同一体系。

export interface ConversationRailPeerPairEndpoint {
  alias: string;
  cwd: string;
  provider: string;
  sessionId: string;
  status: string;
  taskId: string;
  title: string;
}

/** 分栏结对模式（peer-pair-mode PRD）：solo = 独立模式，pair = 结对模式。 */
export type ConversationRailPeerPairMode = "solo" | "pair";

/** 结对开工卡：`""` 未进入结对、`pending` 等用户下一句、`sent` 已投给搭档。 */
export type ConversationRailPeerPairKickoffState = "" | "pending" | "sent";

export interface ConversationRailPeerPair {
  a: ConversationRailPeerPairEndpoint;
  b: ConversationRailPeerPairEndpoint;
  pairId: string;
  // 以下三项来自 peer-pair-mode 接口契约（宿主 listPeerPairs 驼峰投影）。
  // 可选是为了兼容老宿主：老宿主的配对行上**没有** pairMode 字段，
  // 分栏层据此判「宿主不支持结对模式」、整排单选不渲染（PRD D6）。
  pairMode?: ConversationRailPeerPairMode;
  /** 开发者那一侧的 task id；可与 `a.taskId` / `b.taskId` 直接比较。solo 时为空串。 */
  developerTaskId?: string;
  kickoffState?: ConversationRailPeerPairKickoffState;
}

/** 「本条会话的一个对端」：解除配对子菜单与吸附都按这个形状取值。 */
export interface ConversationRailPeerPairLink {
  /** 本条会话在这条配对里那一头的 taskId：deletePeerPair 要用它。 */
  ownTaskId: string;
  pairId: string;
  /**
   * 这条配对此刻的模式，原样抄自配对行。徽标要分「正在结对」与「只是还拴着」
   * 两态（DINTAL-5331），靠的就是它。老宿主的行上没有这个字段 → `undefined`，
   * 徽标退回单态，不凭空画出「未结对」的样子。
   */
  pairMode?: ConversationRailPeerPairMode;
  peer: ConversationRailPeerPairEndpoint;
}

/**
 * 徽标两态（DINTAL-5331）：
 *   `pair`    至少有一条配对正在结对编程 → 实心。
 *   `solo`    配对都还在，但一条都没在结对（切了独立模式 / 关了分栏）→ 空心。
 *   `unknown` 老宿主没报 pairMode，分不出来 → 沿用改造前的单态外观。
 * 一条会话可能同时挂着好几条配对，所以「有一条是 pair 就算 pair」。
 */
export type ConversationRailPeerPairBadgeMode = "pair" | "solo" | "unknown";

export type ConversationRailPeerPairIndex = ReadonlyMap<
  string,
  readonly ConversationRailPeerPairLink[]
>;

/** 已结束（终态）的会话：配对要走「先重开」这条路。 */
const CONVERSATION_RAIL_PEER_CLOSED_STATUSES: ReadonlySet<string> = new Set([
  "completed",
  "failed",
  "canceled",
  "cancelled"
]);

export function isConversationRailPeerClosedStatus(
  status: string | null | undefined
): boolean {
  return CONVERSATION_RAIL_PEER_CLOSED_STATUSES.has(
    (status ?? "").trim().toLowerCase()
  );
}

// 非托管判定。基线的侧栏条目上没有票 14 说的 managed 字段（0099 也没引入），
// 目前能拿到的最近似判据是 isImported：那是从 provider 原生会话记录导入的历史
// 会话，没有 cli_agent_tasks 归属，正是「未托管」这一类。等真正的 managed 标记
// 落到 AgentGUIConversationSummary 上，只需要换掉这一行的判据。
export function isConversationRailPeerPairable(item: {
  isImported?: boolean;
}): boolean {
  return item.isImported !== true;
}

/** 别名预览（只给人看）：`<provider>-<会话号前 4 位>`；真别名由宿主算。 */
export function conversationRailPeerAliasPreview(
  provider: string | null | undefined,
  sessionId: string | null | undefined
): string {
  const normalizedProvider = (provider ?? "").trim().toLowerCase();
  const normalizedSessionId = (sessionId ?? "").trim();
  return `${normalizedProvider}-${normalizedSessionId.slice(0, 4)}`;
}

/** 把配对表翻成「会话号 → 对端列表」，徽标数与解除子菜单都读它。 */
export function conversationRailPeerPairIndex(
  pairs: readonly ConversationRailPeerPair[]
): ConversationRailPeerPairIndex {
  const index = new Map<string, ConversationRailPeerPairLink[]>();
  const push = (
    own: ConversationRailPeerPairEndpoint,
    peer: ConversationRailPeerPairEndpoint,
    pairId: string,
    pairMode: ConversationRailPeerPairMode | undefined
  ): void => {
    const sessionId = own.sessionId?.trim() ?? "";
    if (!sessionId) return;
    const links = index.get(sessionId) ?? [];
    links.push({
      ownTaskId: own.taskId?.trim() ?? "",
      pairId,
      ...(pairMode ? { pairMode } : {}),
      peer
    });
    index.set(sessionId, links);
  };
  for (const pair of pairs) {
    if (!pair?.a || !pair?.b) continue;
    const pairMode = normalizePairMode(pair.pairMode);
    push(pair.a, pair.b, pair.pairId, pairMode);
    push(pair.b, pair.a, pair.pairId, pairMode);
  }
  return index;
}

export function conversationRailPeerPairLinks(
  index: ConversationRailPeerPairIndex,
  sessionId: string | null | undefined
): readonly ConversationRailPeerPairLink[] {
  return index.get((sessionId ?? "").trim()) ?? [];
}

/** 只认契约里那两个词；老宿主的 undefined 与任何脏值一律当「没报过」。 */
function normalizePairMode(
  value: unknown
): ConversationRailPeerPairMode | undefined {
  return value === "pair" || value === "solo" ? value : undefined;
}

/** 徽标数 = 该条会话未撤销的配对数；0 时不画徽标。 */
export function conversationRailPeerPairCount(
  index: ConversationRailPeerPairIndex,
  sessionId: string | null | undefined
): number {
  return conversationRailPeerPairLinks(index, sessionId).length;
}

/** 徽标画哪一态；无配对时也返回 `unknown`（此时压根不画徽标）。 */
export function conversationRailPeerPairBadgeMode(
  index: ConversationRailPeerPairIndex,
  sessionId: string | null | undefined
): ConversationRailPeerPairBadgeMode {
  const links = conversationRailPeerPairLinks(index, sessionId);
  if (links.length === 0) return "unknown";
  if (links.some((link) => link.pairMode === "pair")) return "pair";
  // 只要还有一条说不清（老宿主 / 混连），就不敢断言「没在结对」。
  if (links.some((link) => link.pairMode === undefined)) return "unknown";
  return "solo";
}

export interface ConversationRailPeerPairAdjacency<T> {
  /** 吸附成一组的会话号（当前会话 + 被提上来的对端）；无对端时为空集。 */
  groupedSessionIds: ReadonlySet<string>;
  items: T[];
}

// 吸附重排：在既有排序**之后**跑一次，把当前打开会话的对端提到它正下方，
// 其余条目相对顺序完全不变。切换当前会话即整体重算（本函数无状态）。
export function applyConversationRailPeerPairAdjacency<
  T extends { id: string }
>(input: {
  activeConversationId: string | null | undefined;
  index: ConversationRailPeerPairIndex;
  items: readonly T[];
}): ConversationRailPeerPairAdjacency<T> {
  const items = [...input.items];
  const activeId = (input.activeConversationId ?? "").trim();
  const empty: ReadonlySet<string> = new Set<string>();
  if (!activeId || !items.some((item) => item.id === activeId)) {
    return { groupedSessionIds: empty, items };
  }
  const peerIds = new Set(
    conversationRailPeerPairLinks(input.index, activeId)
      .map((link) => link.peer.sessionId?.trim() ?? "")
      .filter((sessionId) => sessionId && sessionId !== activeId)
  );
  // 只搬「本来就在列表里」的对端，且保持它们彼此原有的相对顺序。
  const movedItems = items.filter((item) => peerIds.has(item.id));
  if (movedItems.length === 0) {
    return { groupedSessionIds: empty, items };
  }
  const movedIds = new Set(movedItems.map((item) => item.id));
  const reordered: T[] = [];
  for (const item of items) {
    if (movedIds.has(item.id)) continue;
    reordered.push(item);
    if (item.id === activeId) {
      reordered.push(...movedItems);
    }
  }
  return {
    groupedSessionIds: new Set([activeId, ...movedIds]),
    items: reordered
  };
}

// 菜单项 / toast / 解除子菜单显示的标题（补丁 0103 真机走查 T3）。
//
// 侧栏条目的 title 在本仓的托管会话里就是**整段 prompt**：开头恒是后端拼的同一句
// 模板，正文可以有几百字。直接拿它当「与〈X〉配对」的 X，菜单会宽到窗口外，而且
// 每条 peer 长得一模一样。剥法与后端 peerTaskTitle 同规则（那边给 peer_list 用），
// 人和 agent 才看到同一个标题；最后再按 40 字截断，因为菜单比 peer_list 窄得多。
const CONVERSATION_RAIL_PEER_TITLE_BOILERPLATE =
  /^请根据下面的[「"“]?任务元数据[」"”]?和[「"“]?用户填写[」"”]?完成本次任务。[\s]*/;

const CONVERSATION_RAIL_PEER_TITLE_MAX_CHARS = 40;

export function conversationRailPeerDisplayTitle(
  title: string | null | undefined
): string {
  const stripped = (title ?? "")
    .trim()
    .replace(CONVERSATION_RAIL_PEER_TITLE_BOILERPLATE, "")
    .trim();
  // 侧栏标题常常**只有**那句模板（Tutti 取的是首行）：剥完为空就退回原标题截断，
  // 别让菜单里出现 Pair with ""。
  if (!stripped) {
    return conversationRailPeerTruncateTitle((title ?? "").trim());
  }
  const lines = stripped.split("\n");
  // 与后端一致：最后一个 `#` 小节下的第一行正文才是用户自己写的那句。
  let start = 0;
  lines.forEach((line, i) => {
    if (line.trim().startsWith("#")) start = i + 1;
  });
  const picked =
    conversationRailPeerFirstProseLine(lines.slice(start)) ||
    conversationRailPeerFirstProseLine(lines) ||
    // 侧栏标题是 prompt 首行：剥完模板只剩「## 补充说明 你是驱动会话 DRV7…」这一行时，
    // 上面两步都把它当小节标题跳过 → 空串，真机上 toast 变成「Paired: ↔」。
    // 退一步把 `#` 剥掉当正文用，总比空好。
    stripped.replace(/^#+\s*/, "").trim();
  return conversationRailPeerTruncateTitle(picked);
}

function conversationRailPeerTruncateTitle(text: string): string {
  const runes = [...text];
  return runes.length > CONVERSATION_RAIL_PEER_TITLE_MAX_CHARS
    ? `${runes.slice(0, CONVERSATION_RAIL_PEER_TITLE_MAX_CHARS).join("")}…`
    : text;
}

/**
 * 后端给这条会话起的标题（peer_title，已剥模板取用户那句）：只要它在任何一条配对里
 * 当过对端，配对表里就有。侧栏自己的标题只是 prompt 首行，能用后端的就用后端的。
 */
export function conversationRailPeerKnownTitle(
  index: ConversationRailPeerPairIndex,
  sessionId: string | null | undefined
): string | null {
  const wanted = (sessionId ?? "").trim();
  if (!wanted) return null;
  for (const links of index.values()) {
    for (const link of links) {
      if ((link.peer.sessionId ?? "").trim() === wanted) {
        const title = (link.peer.title ?? "").trim();
        if (title) return title;
      }
    }
  }
  return null;
}

function conversationRailPeerFirstProseLine(lines: readonly string[]): string {
  for (const raw of lines) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    return line;
  }
  return "";
}
