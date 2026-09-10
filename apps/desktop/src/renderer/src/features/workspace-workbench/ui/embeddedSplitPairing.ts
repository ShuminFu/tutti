// 分栏配对（补丁 0108）的「放下即配对」纯逻辑。
//
// 这里不碰布局、不碰 DOM、不 import toast 实现：桩（host / toast）全从入参进来，
// 单测才能只断言「调了什么、toast 了什么」（PRD Testing Decisions 第 2 条接缝）。
import {
  isConversationRailPeerClosedStatus,
  isConversationRailPeerPairable,
  type ConversationRailPeerPair,
  type ConversationRailPeerPairingHost
} from "@tutti-os/agent-gui/conversation-rail-projection";
import type { SplitSide } from "@tutti-os/agent-gui/conversation-rail-projection";

/** 一栏里那条会话的配对判据：够判「非托管」「已结束」。 */
export interface EmbeddedSplitPairingSide {
  sessionId: string;
  /** 非托管（导入的历史会话）：允许并列，跳过配对。 */
  isImported: boolean;
  status: string | null;
}

/** 宿主桥不可用时统一归一成这个词（见 embeddedRailPeerPairingHost.ts）。 */
export const EMBEDDED_SPLIT_PAIRING_UNSUPPORTED = "unsupported";

export interface EmbeddedSplitToast {
  error(message: string): void;
  info(message: string): void;
  success(message: string): void;
}

export interface EmbeddedSplitPairingLabels {
  /** 「已配对」 */
  paired: string;
  /** 「重开的会话迟迟没有出现」：等重开对端在侧栏露面超时（见 embeddedSplitView）。 */
  relaunchTimeout: string;
  /** 「未托管会话不能配对」 */
  unmanaged: string;
}

export interface PairOnDropInput {
  focus: SplitSide;
  host: ConversationRailPeerPairingHost | null;
  labels: EmbeddedSplitPairingLabels;
  left: EmbeddedSplitPairingSide | null;
  pairs: readonly ConversationRailPeerPair[];
  right: EmbeddedSplitPairingSide | null;
  toast: EmbeddedSplitToast;
}

export type PairOnDropOutcome =
  | "paired"
  | "already"
  | "unmanaged"
  | "unsupported"
  | "failed"
  /** 有一栏是空的：这一步整段不适用（既不是失败也不是「桥不支持」）。 */
  | "skipped";

export interface PairOnDropResult {
  outcome: PairOnDropOutcome;
  /** 已结束目标被后端先重开时的新会话号：调用方用它换掉那一槽。 */
  relaunchedSessionId?: string;
  /** 真调过接口时回填的最新配对表；短路路径不带（那几条要求桩零调用）。 */
  pairs?: readonly ConversationRailPeerPair[];
}

function normalizeSessionId(value: string | null | undefined): string {
  return value?.trim() ?? "";
}

/** 两条会话是否已经是一对（无序比较，缓存未到就当没有，后端幂等兜底）。 */
export function hasEmbeddedSplitPeerPair(
  pairs: readonly ConversationRailPeerPair[],
  a: string,
  b: string
): boolean {
  const left = normalizeSessionId(a);
  const right = normalizeSessionId(b);
  if (!left || !right) return false;
  return pairs.some((pair) => {
    const first = normalizeSessionId(pair?.a?.sessionId);
    const second = normalizeSessionId(pair?.b?.sessionId);
    return (
      (first === left && second === right) ||
      (first === right && second === left)
    );
  });
}

function isUnsupportedError(error: unknown): boolean {
  return (
    error instanceof Error &&
    error.message.trim() === EMBEDDED_SPLIT_PAIRING_UNSUPPORTED
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}

/**
 * PRD「放下时的配对逻辑」①–⑥，顺序不可换：
 *   ① 两槽都非空才走          —— 空栏时整段不适用
 *   ② 任一非托管 → 只 toast，零调用
 *   ③ 缓存里已有这对 → 静默返回，零调用（缓存未到就照调，后端幂等）
 *   ④ 桥不可用 / unsupported → 整段跳过，零 toast（老宿主照常并列）
 *   ⑤ createPeerPair：目标已结束带 relaunchClosed；失败只 toast 后端原文，布局不回滚
 *   ⑥ 真调过接口之后才刷 listPeerPairs —— ②③ 要求「桩零调用」，刷新不能提到前面
 * 「from」永远是焦点栏那条：换掉一栏后新配对的对端是留下的那栏（PRD 故事 17）。
 */
export async function pairOnDrop(
  input: PairOnDropInput
): Promise<PairOnDropResult> {
  const otherSide: SplitSide = input.focus === "left" ? "right" : "left";
  const from = input[input.focus];
  const to = input[otherSide];
  const fromId = normalizeSessionId(from?.sessionId);
  const toId = normalizeSessionId(to?.sessionId);

  // ① 两槽都非空、且不是同一条。
  if (!from || !to || !fromId || !toId || fromId === toId) {
    return { outcome: "skipped" };
  }

  // ② 非托管：允许并列，跳过配对，给一句人话。
  if (
    !isConversationRailPeerPairable(from) ||
    !isConversationRailPeerPairable(to)
  ) {
    input.toast.info(input.labels.unmanaged);
    return { outcome: "unmanaged" };
  }

  // ③ 已配对：静默，零调用。
  if (hasEmbeddedSplitPeerPair(input.pairs, fromId, toId)) {
    return { outcome: "already" };
  }

  // ④ 老宿主 / 桥不支持：整段跳过，链条不渲染。
  const host = input.host;
  if (!host) {
    return { outcome: "unsupported" };
  }

  const refreshPairs = async (): Promise<
    readonly ConversationRailPeerPair[] | undefined
  > => {
    try {
      return (await host.listPeerPairs()).pairs;
    } catch {
      // ⑥ 刷新失败不改变本次结论，链条沿用上一份缓存。
      return undefined;
    }
  };

  try {
    const result = await host.createPeerPair({
      aliasForFrom: "",
      aliasForTo: "",
      from: fromId,
      relaunchClosed: isConversationRailPeerClosedStatus(to.status),
      to: toId
    });
    const relaunchedSessionId = normalizeSessionId(result?.relaunchedSessionId);
    input.toast.success(input.labels.paired);
    const pairs = await refreshPairs();
    return {
      outcome: "paired",
      ...(pairs ? { pairs } : {}),
      ...(relaunchedSessionId ? { relaunchedSessionId } : {})
    };
  } catch (error) {
    if (isUnsupportedError(error)) {
      return { outcome: "unsupported" };
    }
    // ⑤ 失败：布局不回滚，只把后端那句人话原样端出来。
    input.toast.error(errorMessage(error));
    const pairs = await refreshPairs();
    return { outcome: "failed", ...(pairs ? { pairs } : {}) };
  }
}
