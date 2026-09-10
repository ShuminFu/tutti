import { useEffect, useMemo, useRef } from "react";
import {
  sessionLivenessHost,
  type HostSessionLivenessState
} from "./sessionLivenessHost";

// RNDMASTER_RAIL_HOST_ATTACH：回退看红时改 false，保留本符号。
const railHostAttach = true;

/**
 * 一条会话在会话栏眼里的「正在跑一轮吗」+「宿主怎么说」。
 * 只取判定要用的那几维，方便单测直接喂假数据。
 */
export interface HostSessionAttachCandidate {
  id: string;
  /** 这一轮还开着（蓝点那档）：`status` 是 working/waiting，或还有 activeTurn。 */
  working: boolean;
  /** 宿主给的死活；没数据时给 undefined。 */
  hostLiveness: HostSessionLivenessState | undefined;
}

/**
 * 挑出「需要让宿主补挂账」的会话号。
 *
 * 判据只有一条：**这一轮正在跑，宿主却说它不活**。
 *
 * - `working` 是「用户刚说了话」最可靠的旁证 —— 会话栏拿不到发送事件，
 *   但一轮跑起来这件事它一定看得见。
 * - 宿主回 `"live"` 说明账还挂着，什么都不用做。
 * - `"closed"` / `"unknown"` 都要补：前者是被后端重启打成 failed（真机最常见），
 *   后者是宿主一行都没有；补挂请求本身幂等，宿主认不出就原样回 unknown，
 *   多问一次的代价只有一次 SQL。
 * - **没有宿主数据（undefined）时不补**：那是第一拍还没回来，此时补挂等于
 *   在什么都不知道的情况下写状态。
 *
 * 纯函数，无副作用：hook 与单测共用同一份判据。
 */
export function sessionsNeedingHostAttach(
  candidates: readonly HostSessionAttachCandidate[]
): string[] {
  const out: string[] = [];
  for (const candidate of candidates) {
    const id = typeof candidate.id === "string" ? candidate.id.trim() : "";
    if (!id || !candidate.working) continue;
    if (candidate.hostLiveness === undefined) continue;
    if (candidate.hostLiveness === "live") continue;
    if (!out.includes(id)) out.push(id);
  }
  return out;
}

/**
 * 看见「正在跑一轮、宿主却没有活行」就让宿主补挂一次账（补丁 0124）。
 *
 * 三条纪律，都是照 0122 那个轮询钩子的教训来的：
 *
 * 1. **宿主没这个能力就永久停手**：桥把「未嵌入 / 老宿主 / 超时」统一归一成
 *    `Error("unsupported")`，收到就再也不发。
 * 2. **一条会话只补一次**：补过就记下来，直到宿主报它 `"live"` 才解除 ——
 *    宿主那边新挂的行要过几秒才被派工器起跑（起跑后才改写 session_id），
 *    这几秒里会话栏会一直看见同样的「working + 不活」，不记就会连着补好几次。
 *    宿主自己也有 60s 冷却，这里是第二道。
 * 3. **不叠请求**：同一条会话上一次还没回来就不发第二次。
 */
export function useHostSessionAttach(
  candidates: readonly HostSessionAttachCandidate[]
): void {
  const host = sessionLivenessHost();
  const attach = host?.attachSession;
  const enabled = railHostAttach && typeof attach === "function";
  const needing = useMemo(
    () => (enabled ? sessionsNeedingHostAttach(candidates) : []),
    [enabled, candidates]
  );
  // join 出来的 key 才是稳定依赖：会话栏每次重排都换新数组，直接依赖数组会每帧重跑。
  const needingKey = needing.join(",");
  // 三份记忆都要跨依赖变化活着，放 ref 而不是局部变量。
  const attachedRef = useRef(new Set<string>());
  const inFlightRef = useRef(new Set<string>());
  const unsupportedRef = useRef(false);
  // 宿主报 live 的会话要从「补过」里摘掉，否则下一次崩了就再也不补。
  const livenessById = useMemo(() => {
    const map = new Map<string, HostSessionLivenessState | undefined>();
    for (const candidate of candidates) map.set(candidate.id, candidate.hostLiveness);
    return map;
  }, [candidates]);

  useEffect(() => {
    if (!enabled || typeof attach !== "function") return;
    for (const [id, state] of livenessById) {
      if (state === "live") attachedRef.current.delete(id);
    }
    if (unsupportedRef.current || needingKey === "") return;
    let cancelled = false;
    for (const id of needingKey.split(",")) {
      if (attachedRef.current.has(id) || inFlightRef.current.has(id)) continue;
      attachedRef.current.add(id);
      inFlightRef.current.add(id);
      void attach({ agentSessionId: id })
        .catch((cause: unknown) => {
          if (cancelled) return;
          if (cause instanceof Error && cause.message === "unsupported") {
            unsupportedRef.current = true;
            return;
          }
          // 其余错误（宿主临时忙 / 后端正在重启）允许下次再试：把这条从
          // 「补过」里摘掉即可。补挂不是关键路径，不弹任何提示。
          attachedRef.current.delete(id);
        })
        .finally(() => {
          inFlightRef.current.delete(id);
        });
    }
    return () => {
      cancelled = true;
    };
  }, [attach, enabled, livenessById, needingKey]);
}
