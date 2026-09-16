// 分栏结对模式（peer-pair-mode 票 04）composer 上方那排单选的纯模型。
//
// 与栏头同一个套路（见 embeddedSplitPaneHeader.ts）：判断「这一栏画不画、画成什么、
// 能不能点、键盘往哪跳」是会写错的地方，抽成纯函数用 node --test 钉住；
// EmbeddedSplitPairModeBar.tsx 只负责把模型画出来。
import type { SplitSide } from "@tutti-os/agent-gui/conversation-rail-projection";
import type {
  EmbeddedSplitPairModeChoice,
  EmbeddedSplitViewSnapshot
} from "./embeddedSplitView.ts";

/** 单选从左到右的顺序：独立模式 │ 结对模式：开发者 审查者。键盘左右按这个顺序走。 */
export const EMBEDDED_SPLIT_PAIR_MODE_CHOICES: readonly EmbeddedSplitPairModeChoice[] =
  ["solo", "developer", "reviewer"];

export interface EmbeddedSplitPairModeBarModel {
  /** 焦点栏：可点。非焦点栏画同一排镜像（压暗、不可点），两栏输入框才等高。 */
  focused: boolean;
  /** 焦点栏且没有写在途。 */
  interactive: boolean;
  side: SplitSide;
  value: EmbeddedSplitPairModeChoice;
}

/**
 * 给某条会话的作曲区算那排单选。返回 null = 整排不渲染：
 * 快照里没有 pairMode 投影（不满足票 04 的四条显示条件，见 embeddedSplitView 的
 * pairModeSnapshot），或者这条会话根本不在任何一栏里（例如折叠态、首页态）。
 */
export function buildEmbeddedSplitPairModeBar(
  snapshot: EmbeddedSplitViewSnapshot | null,
  agentSessionId: string | null
): EmbeddedSplitPairModeBarModel | null {
  const pairMode = snapshot?.pairMode ?? null;
  const sessionId = agentSessionId?.trim() ?? "";
  if (!snapshot || !pairMode || !sessionId) return null;
  const side: SplitSide | null =
    snapshot.panes.left?.sessionId === sessionId
      ? "left"
      : snapshot.panes.right?.sessionId === sessionId
        ? "right"
        : null;
  if (!side) return null;
  const role = pairMode.mode === "pair" ? pairMode.roles[side] : null;
  const focused = snapshot.focus === side;
  return {
    focused,
    interactive: focused && !pairMode.busy,
    side,
    value: role ?? "solo"
  };
}

/** radiogroup 的键盘：左右（上下同义）循环切换，Home/End 到两头；其余键返回 null。 */
export function nextEmbeddedSplitPairModeChoice(
  current: EmbeddedSplitPairModeChoice,
  key: string
): EmbeddedSplitPairModeChoice | null {
  const choices = EMBEDDED_SPLIT_PAIR_MODE_CHOICES;
  const index = Math.max(choices.indexOf(current), 0);
  switch (key) {
    case "ArrowRight":
    case "ArrowDown":
      return choices[(index + 1) % choices.length] ?? null;
    case "ArrowLeft":
    case "ArrowUp":
      return choices[(index + choices.length - 1) % choices.length] ?? null;
    case "Home":
      return choices[0] ?? null;
    case "End":
      return choices[choices.length - 1] ?? null;
    default:
      return null;
  }
}
