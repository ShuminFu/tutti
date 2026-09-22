/**
 * 「侧栏点一条会话」在分栏 + 结对模式下的去向（rndmaster 票 01/02）。
 *
 * 为什么单独一层：`reduceSplitLayout` 只认布局事件，不认配对关系；而配对表住在
 * 嵌入层（`embeddedSplitView`）。把决策做成纯函数，回退看红才钉得住——三条分支
 * 各自有测试，去掉任何一条都会变红。
 *
 * 判据一律只认**正在结对编程**（配对行 `pairMode === "pair"`，侧栏徽标实心）。
 * 配对还在但切了独立模式（空心徽标）与老宿主报不出 pairMode 的，都按「没结对」
 * 走单列，和徽标两态的口径一致（DINTAL-5331）。
 */
import type { SplitLayoutState } from "./agentGuiSplitLayout.ts";

/** 记住的「这一对上次怎么摆 / 这条会话上次配的是谁」。实现方负责落盘。 */
export interface SplitPairSelectMemory {
  /**
   * 这条会话上次开过的搭档；没记过或那条配对已经不在了返回 null，
   * 调用方退回候选里的第一个。
   */
  lastPartnerOf(sessionId: string): string | null;
  /** 这一对上次的左右顺序（无序入参）；没记过返回 null。 */
  orderOf(a: string, b: string): readonly [string, string] | null;
}

export interface SplitPairSelectInput {
  /**
   * 这条会话**正在结对编程**的对端会话号，按配对表的天然顺序。
   * 空数组 = 没有正在结对的搭档。
   */
  activePartnersOf(sessionId: string): readonly string[];
  id: string;
  layout: SplitLayoutState;
  memory?: SplitPairSelectMemory | null;
}

export type SplitPairSelectOutcome =
  /** 点的就是已经在某一栏里的那条：只移焦点，布局不动。 */
  | { kind: "focus" }
  /** 没在结对：收成单列全宽（单列态下就是顶替那一栏）。 */
  | { kind: "single"; id: string }
  /** 和右栏那条也在结对：只换左栏，右栏原地不动。 */
  | { kind: "swap-left"; id: string }
  /** 一次开两列 / 整组切换。 */
  | { kind: "group"; left: string; right: string };

function trimmed(value: string | null | undefined): string {
  return (value ?? "").trim();
}

/**
 * 从候选搭档里挑一个：上次开过的那个还在候选里就用它，否则用第一个
 * （用户答复：默认第一个，之后尽量记住上次的选择）。
 */
export function pickSplitPairPartner(
  id: string,
  partners: readonly string[],
  memory: SplitPairSelectMemory | null | undefined
): string | null {
  const candidates = partners.map(trimmed).filter((p) => p && p !== id);
  if (candidates.length === 0) return null;
  const last = trimmed(memory?.lastPartnerOf(id) ?? "");
  return last && candidates.includes(last) ? last : (candidates[0] ?? null);
}

/** 这一对按记住的顺序摆；没记过就 `[点的那条, 搭档]`。 */
export function orderSplitPair(
  id: string,
  partner: string,
  memory: SplitPairSelectMemory | null | undefined
): { left: string; right: string } {
  const remembered = memory?.orderOf(id, partner) ?? null;
  if (remembered) {
    const left = trimmed(remembered[0]);
    const right = trimmed(remembered[1]);
    // 记录必须正好覆盖这两条，否则当没记过——免得旧记录把别的会话带进来。
    const covers =
      (left === id && right === partner) || (left === partner && right === id);
    if (covers) return { left, right };
  }
  return { left: id, right: partner };
}

export function resolveSplitPairSelection(
  input: SplitPairSelectInput
): SplitPairSelectOutcome {
  const id = trimmed(input.id);
  if (!id) return { kind: "focus" };
  const { left, right } = input.layout.panes;
  // 已经在栏里 → 只移焦点。放在最前面：点当前那条不该把布局推倒重来。
  if (left === id || right === id) return { kind: "focus" };

  const partner = pickSplitPairPartner(
    id,
    input.activePartnersOf(id),
    input.memory
  );
  if (!partner) return { kind: "single", id };

  // 双列态下「和右栏那条也在结对」→ 只换左栏。右栏窗口不重开，正文不闪。
  if (right !== null && left !== null) {
    const partnersWithRight = input
      .activePartnersOf(id)
      .map(trimmed)
      .includes(right);
    if (partnersWithRight) return { kind: "swap-left", id };
  }

  const ordered = orderSplitPair(id, partner, input.memory);
  return { kind: "group", left: ordered.left, right: ordered.right };
}
