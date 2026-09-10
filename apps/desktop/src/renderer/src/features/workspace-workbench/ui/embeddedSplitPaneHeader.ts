// 分栏栏头（票 04）的纯模型：把 controller 快照 + 文案算成「每栏画什么」。
//
// 为什么单独一层：栏头本身是 React 覆盖层（EmbeddedSplitChrome）的一小块 JSX，
// 而这一层的判断（哪几栏有栏头、标题取哪一份、链条能不能点、⋮ 里放几项、
// 每栏占多宽）才是会写错的地方。抽成纯函数后可以用 node --test 直接钉住，
// 不必为了断言「焦点栏标题加重」去搭一套 React 渲染环境。
import type { SplitSide } from "@tutti-os/agent-gui/conversation-rail-projection";
import type {
  EmbeddedSplitPairingState,
  EmbeddedSplitViewSnapshot
} from "./embeddedSplitView.ts";

/** ⋮ 菜单里的一项。id 决定点它调 controller 的哪个方法（见 EmbeddedSplitChrome）。 */
export interface EmbeddedSplitPaneHeaderMenuItem {
  id: "resetRatio" | "swapPanes";
  label: string;
}

export interface EmbeddedSplitPaneHeaderModel {
  /** ✕ 的无障碍名。 */
  closeLabel: string;
  /** 焦点栏：标题加重；非焦点栏的正文由 CSS 按 data 属性压暗，这里只给判据。 */
  focused: boolean;
  iconUrl: string | null;
  /** 相对主区宽度的起点（0..1），组件拿去拼 calc()。 */
  leftFraction: number;
  menuItems: readonly EmbeddedSplitPaneHeaderMenuItem[];
  menuLabel: string;
  /** 链条能不能点：配对能力不可用（unsupported）或正在忙（pending）时不可点。 */
  pairingEnabled: boolean;
  /** 链条的提示文案：已配对给「解除」，否则给「配对」。 */
  pairingLabel: string;
  /** 链条的状态色：paired 主色、其余灰。unsupported 时整枚不画。 */
  pairingState: EmbeddedSplitPairingState | null;
  projectLabel: string | null;
  provider: string | null;
  /** 窗口刚点过「新建会话」、还没落地新号时为 null。 */
  sessionId: string | null;
  side: SplitSide;
  title: string;
  widthFraction: number;
}

export interface EmbeddedSplitPaneHeaderLabels {
  closePane: string;
  menu: string;
  pair: string;
  resetRatio: string;
  swapPanes: string;
  unpair: string;
  /** 侧栏还没报过这条会话的摘要时的兜底标题。 */
  untitled: string;
}

/**
 * 每栏一条栏头。没有分栏（右栏为空）时返回空数组——单栏时工作台自己的界面就够了，
 * 我们不该在上面再叠一条。折叠态（主区太窄，只显示焦点栏）只给焦点栏一条、占满整宽。
 */
export function buildEmbeddedSplitPaneHeaders(
  snapshot: EmbeddedSplitViewSnapshot | null,
  labels: EmbeddedSplitPaneHeaderLabels
): readonly EmbeddedSplitPaneHeaderModel[] {
  if (snapshot === null) return [];
  const { left, right } = snapshot.panes;
  if (left === null || right === null) return [];

  const pairingState =
    snapshot.pairing === "unsupported" ? null : snapshot.pairing;
  const pairingEnabled =
    snapshot.pairing !== "unsupported" && snapshot.pairing !== "pending";
  const pairingLabel =
    snapshot.pairing === "paired" ? labels.unpair : labels.pair;
  // 「交换左右」只有两栏都在时才有意义；controller 真的有这个能力（swapPanes），
  // 所以它是一个真入口，不是占位。
  const menuItems: readonly EmbeddedSplitPaneHeaderMenuItem[] = [
    { id: "resetRatio", label: labels.resetRatio },
    { id: "swapPanes", label: labels.swapPanes }
  ];

  const sides: readonly SplitSide[] = snapshot.collapsed
    ? [snapshot.focus]
    : ["left", "right"];

  return sides.map((side) => {
    const pane = side === "left" ? left : right;
    const geometry = snapshot.collapsed
      ? { leftFraction: 0, widthFraction: 1 }
      : side === "left"
        ? { leftFraction: 0, widthFraction: snapshot.ratio }
        : { leftFraction: snapshot.ratio, widthFraction: 1 - snapshot.ratio };
    return {
      closeLabel: labels.closePane,
      focused: snapshot.focus === side,
      iconUrl: pane.session?.iconUrl ?? null,
      leftFraction: geometry.leftFraction,
      menuItems,
      menuLabel: labels.menu,
      pairingEnabled,
      pairingLabel,
      pairingState,
      projectLabel: pane.session?.projectLabel ?? null,
      provider: pane.session?.provider ?? null,
      sessionId: pane.sessionId,
      side,
      // 标题拿不到时用兜底文案，绝不把会话号糊在栏头上（那对用户没有意义）。
      title: pane.session?.title?.trim() ? pane.session.title : labels.untitled,
      widthFraction: geometry.widthFraction
    };
  });
}
