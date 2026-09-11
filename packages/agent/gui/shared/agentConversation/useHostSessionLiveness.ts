import { useEffect, useMemo, useRef, useState } from "react";
import {
  sessionLivenessHost,
  SESSION_LIVENESS_MAX_IDS,
  type HostSessionLivenessState
} from "./sessionLivenessHost";

// RNDMASTER_RAIL_HOST_LIVENESS：回退看红时改 false，保留本符号。
const railHostLiveness = true;

// 会话栏一直在眼前，4s 一拍与 0117 的审批卡同频；页面不可见时不拍。
export const HOST_SESSION_LIVENESS_POLL_INTERVAL_MS = 4000;

/**
 * 宿主对一条会话的两句话（补丁 0128 起是两句，之前只有 `state` 一句）：
 *
 * - `state`：tuttid 里这条会话有没有活的 ACP 进程 —— **圆点只看它**。
 * - `attached`：rndmaster 那边有没有非终态任务行盯着 —— **补挂只看它**。
 *
 * 两者正交，别互相推断：用户在聊天时 `state` 必然 `"live"`，可宿主的账早就被
 * 后端重启打断了。
 */
export interface HostSessionLivenessRecord {
  state: HostSessionLivenessState;
  attached: boolean;
}

export type HostSessionLivenessMap = ReadonlyMap<
  string,
  HostSessionLivenessRecord
>;

const EMPTY_LIVENESS: HostSessionLivenessMap = new Map();

/**
 * 轮询宿主，问「会话栏里现在画着的这些会话，宿主那边还活着吗」。
 *
 * 判定顺序上的三件事，都是踩过的坑：
 *
 * 1. **宿主没这组能力就永久停拍**：桥把「未嵌入 / 老宿主 / 超时」统一归一成
 *    `Error("unsupported")`，收到就再也不发请求，否则老宿主上每 4s 一次白等。
 * 2. **不叠请求**：上一拍还没回来时这一拍直接跳过。会话栏行数可以到 200，
 *    宿主那边是一次 SQL 查询，慢一拍比堆积一串在途请求好。
 * 3. **页面不可见不拍**：后台标签页里 WebKit 本来就会把定时器钳到每分钟一拍，
 *    与其拍出一串迟到结果不如不拍。
 *
 * 返回的 Map 只在**内容真的变了**时换新对象，让会话栏那几层 memo 还能挡住
 * 无谓重渲染。
 */
export function useHostSessionLiveness(
  visibleSessionIds: readonly string[]
): HostSessionLivenessMap {
  const host = sessionLivenessHost();
  const enabled = railHostLiveness && host !== null;
  // 去重 + 截断 + 排序：排序后 join 出来的 key 才是稳定的依赖项，
  // 会话栏重排（新消息上浮）不该触发一次额外请求。
  const requestedIds = useMemo(() => {
    if (!enabled) return [] as string[];
    const unique = Array.from(
      new Set(
        visibleSessionIds.filter(
          (id): id is string => typeof id === "string" && id.trim() !== ""
        )
      )
    );
    unique.sort();
    return unique.slice(0, SESSION_LIVENESS_MAX_IDS);
  }, [enabled, visibleSessionIds]);
  const requestKey = requestedIds.join(",");
  const [liveness, setLiveness] =
    useState<HostSessionLivenessMap>(EMPTY_LIVENESS);
  // 「宿主不支持」跨依赖变化也要记住：ids 一变 effect 会重跑，
  // 用局部变量记会被重置成「再试一次」。
  const unsupportedRef = useRef(false);

  useEffect(() => {
    if (!enabled || !host || requestKey === "") {
      return;
    }
    let cancelled = false;
    let inFlight = false;
    const ids = requestKey.split(",");
    const tick = async (): Promise<void> => {
      if (cancelled || unsupportedRef.current || inFlight) return;
      inFlight = true;
      try {
        const result = await host.querySessionLiveness({
          agentSessionIds: ids
        });
        if (cancelled) return;
        const next = new Map<string, HostSessionLivenessRecord>();
        for (const [id, entry] of Object.entries(result?.sessions ?? {})) {
          if (entry && typeof entry.state === "string") {
            // attached 缺字段兜 false：老宿主只回 state，当「没在盯」处理，
            // 补挂那条路自己会去把账挂上（补丁 0128）。
            next.set(id, {
              state: entry.state,
              attached: entry.attached === true
            });
          }
        }
        setLiveness((previous) =>
          livenessMapsEqual(previous, next) ? previous : next
        );
      } catch (cause) {
        // 老宿主：停拍，会话栏退回 0119 的纯 Tutti 判据。
        if (cause instanceof Error && cause.message === "unsupported") {
          unsupportedRef.current = true;
        }
        // 其余错误（宿主临时忙 / 后端重启）沉默重试：圆点不是关键路径，
        // 报错弹条只会打扰用户。
      } finally {
        inFlight = false;
      }
    };
    void tick();
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "hidden") void tick();
    }, HOST_SESSION_LIVENESS_POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [enabled, host, requestKey]);

  return enabled ? liveness : EMPTY_LIVENESS;
}

function livenessMapsEqual(
  left: HostSessionLivenessMap,
  right: HostSessionLivenessMap
): boolean {
  if (left.size !== right.size) return false;
  for (const [id, record] of right) {
    // 值变成小对象后不能再用 !== 比：每一拍都是新对象，那样恒不相等，
    // 会话栏那几层 memo 就全废了（补丁 0128）。
    const previous = left.get(id);
    if (!previous) return false;
    if (previous.state !== record.state) return false;
    if (previous.attached !== record.attached) return false;
  }
  return true;
}
