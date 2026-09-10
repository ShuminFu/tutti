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

export type HostSessionLivenessMap = ReadonlyMap<
  string,
  HostSessionLivenessState
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
        const next = new Map<string, HostSessionLivenessState>();
        for (const [id, entry] of Object.entries(result?.sessions ?? {})) {
          if (entry && typeof entry.state === "string") {
            next.set(id, entry.state);
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
  for (const [id, state] of right) {
    if (left.get(id) !== state) return false;
  }
  return true;
}
