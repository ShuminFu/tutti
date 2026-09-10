// 分栏配对（补丁 0108）的宿主端口注册口（形状照 0103 的 conversationRailPeerPairingHost）。
//
// gui 包只负责「侧栏条目被拖起/拖动/放下」的手势与「哪些会话正在视窗里」的高亮；
// 分栏本身（两个 Agent 窗口、放下区预览、分隔线、链条、配对）由 apps/desktop 的
// 嵌入层注册一份实现。未注册（普通 web / 老宿主）时条目不可拖、高亮退回单值。
import type { AgentConversationRailStatus } from "../../../shared/agentGUIConversationSummaryProjection.ts";

/** 拖起时交给宿主的会话摘要：够宿主画拖影、判「已结束」「非托管」。 */
export interface ConversationRailSplitDragSession {
  id: string;
  title: string;
  provider: string | null;
  status: AgentConversationRailStatus | null;
  /** 非托管（升级前遗留）判据，与 isConversationRailPeerPairable 同源。 */
  isImported: boolean;
  iconUrl: string | null;
  /**
   * 项目名（会话所属项目的 label）。栏头的项目胶囊用它（票 04）。
   * 可选：老宿主 / 旧夹具构造这个对象时不带它，缺了就只是不画胶囊。
   */
  projectLabel?: string | null;
}

/** 视口坐标（clientX/clientY）；侧栏与分栏同文档，宿主直接按此判左右半。 */
export interface ConversationRailSplitPoint {
  x: number;
  y: number;
}

export interface ConversationRailSplitHost {
  /**
   * 侧栏每渲染一条会话就报一次它的摘要（票 04）。
   * 为什么需要：栏头要写「这是谁」，而宿主原本只在**拖动**时拿得到标题/图标/项目名
   * （onDragStart）—— 左栏那条从来没被拖过，栏头就永远没有标题。可选方法：
   * 老宿主没实现时侧栏照常工作，只是栏头退回「未命名会话」。
   */
  describeSession?(session: ConversationRailSplitDragSession): void;
  /** 正在任一栏里显示的会话号；侧栏条目据此高亮（focus 栏与非 focus 栏都算）。 */
  getShownSessionIds(): ReadonlySet<string>;
  subscribe(listener: () => void): () => void;
  /** 越过 6px 死区后调用一次；之后 move 任意次；end = 放下、cancel = 放弃（Esc/pointercancel/blur）。 */
  onDragStart(
    session: ConversationRailSplitDragSession,
    point: ConversationRailSplitPoint
  ): void;
  onDragMove(point: ConversationRailSplitPoint): void;
  onDragEnd(point: ConversationRailSplitPoint): void;
  onDragCancel(): void;
}

let registeredHost: ConversationRailSplitHost | null = null;

export function registerConversationRailSplitHost(
  host: ConversationRailSplitHost
): () => void {
  registeredHost = host;
  return () => {
    if (registeredHost === host) {
      registeredHost = null;
    }
  };
}

export function conversationRailSplitHost(): ConversationRailSplitHost | null {
  return registeredHost;
}
