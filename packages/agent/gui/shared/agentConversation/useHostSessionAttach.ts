import { useEffect, useMemo, useRef } from "react";
import { sessionLivenessHost } from "./sessionLivenessHost";

// RNDMASTER_RAIL_HOST_ATTACH：回退看红时改 false，保留本符号。
const railHostAttach = true;

/**
 * 一条会话在会话栏眼里的「正在跑一轮吗」+「宿主有没有在盯」。
 * 只取判定要用的那几维，方便单测直接喂假数据。
 */
export interface HostSessionAttachCandidate {
  id: string;
  /** 这一轮还开着（蓝点那档）：`status` 是 working/waiting，或还有 activeTurn。 */
  working: boolean;
  /**
   * 宿主说「rndmaster 那边还有非终态任务行盯着这条会话吗」；
   * 第一拍还没回来时给 undefined。
   */
  hostAttached: boolean | undefined;
}

/**
 * 挑出「需要让宿主补挂账」的会话号。
 *
 * 判据只有一条：**这一轮正在跑，可宿主没在盯**（`working === true &&
 * hostAttached === false`）。
 *
 * ## 为什么不能再看 `state`（补丁 0128 的由来）
 *
 * 0124 当初的判据是「working 且宿主说不活（`state !== "live"`）」。后来宿主把
 * `state` 的数据源换成了「tuttid 里这条会话有没有活的 ACP 进程」—— 用户只要
 * 还在聊天，tuttid 那边必然有活进程，`state` 就恒为 `"live"`，于是这里**永远
 * 挑不出人**，补挂（peer 消息自愈）整档变成死代码。
 *
 * 根子在于：`state` 回答的是「对面的进程在不在」，而补挂要问的是「**我这边的账
 * 还在不在**」—— 两个正交的问题，宿主现在分成两个字段各自回答，各看各的：
 *
 * - 圆点只看 `state`（进程死活）；
 * - 补挂只看 `attached`（rndmaster 有没有在记账）。
 *
 * ## 新判据合起来的语义
 *
 * 「tuttid 说这一轮真在跑，可 rndmaster 没在盯」—— 这正是被后端重启打断的那种
 * 会话：任务行被打成 failed（`attached === false`），而用户在 DinTalDock 里
 * 直接聊天不经过 rndmaster，一行都不会新建，于是这条会话的账永远补不回来，
 * peer 消息永远投不进去（坑144）。
 *
 * 几点纪律不变：
 * - `working` 是「用户刚说了话」最可靠的旁证 —— 会话栏拿不到发送事件，
 *   但一轮跑起来这件事它一定看得见。
 * - **`hostAttached === undefined` 时什么都不做**：那是第一拍还没回来，
 *   此时补挂等于在什么都不知道的情况下写状态。
 * - 补挂请求本身幂等，宿主已有活行会原样返回，且自带 60s 冷却。
 *
 * 纯函数，无副作用：hook 与单测共用同一份判据。
 */
export function sessionsNeedingHostAttach(
  candidates: readonly HostSessionAttachCandidate[]
): string[] {
  const out: string[] = [];
  for (const candidate of candidates) {
    const id = typeof candidate.id === "string" ? candidate.id.trim() : "";
    if (!id || candidate.working !== true) continue;
    // 只认写死的 false：undefined（还没回来）与 true（在盯）都不补。
    if (candidate.hostAttached !== false) continue;
    if (!out.includes(id)) out.push(id);
  }
  return out;
}

/**
 * 看见「正在跑一轮、宿主却没在盯」就让宿主补挂一次账
 *（补丁 0124；判据在补丁 0128 从 `state` 换成 `attached`，理由见上）。
 *
 * 三条纪律，都是照 0122 那个轮询钩子的教训来的：
 *
 * 1. **宿主没这个能力就永久停手**：桥把「未嵌入 / 老宿主 / 超时」统一归一成
 *    `Error("unsupported")`，收到就再也不发。
 * 2. **一条会话只补一次**：补过就记下来，直到宿主报它 `attached === true` 才解除 ——
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
  // 宿主报「已经在盯」的会话要从「补过」里摘掉，否则下一次账断了就再也不补。
  const attachedById = useMemo(() => {
    const map = new Map<string, boolean | undefined>();
    for (const candidate of candidates) map.set(candidate.id, candidate.hostAttached);
    return map;
  }, [candidates]);

  useEffect(() => {
    if (!enabled || typeof attach !== "function") return;
    for (const [id, hostAttached] of attachedById) {
      if (hostAttached === true) attachedRef.current.delete(id);
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
  }, [attach, attachedById, enabled, needingKey]);
}
