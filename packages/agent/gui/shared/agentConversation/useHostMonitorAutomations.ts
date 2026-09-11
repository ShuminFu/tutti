import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  monitorAutomationHost,
  MONITOR_AUTOMATIONS_MAX_IDS,
  type HostMonitorAutomation
} from "./monitorAutomationHost";

// RNDMASTER_MONITOR_AUTOMATION_CHIP：回退看红时改 false，保留本符号。
const monitorAutomationChip = true;

/**
 * 20s 一拍。心跳最细 1 分钟，比这更密只是白问宿主一次 SQL；
 * 而比 20s 更疏的话，用户刚建完监控器要盯着屏幕等太久才看见胶囊。
 */
export const HOST_MONITOR_AUTOMATIONS_POLL_INTERVAL_MS = 20_000;

export interface HostMonitorAutomationsResult {
  /** 这条会话起的、还启用着的监控自动化。没有就是空数组。 */
  monitors: readonly HostMonitorAutomation[];
  /**
   * 能不能停：宿主实现了停用能力、且确实有东西可停时为 true。
   * 界面据此决定给不给「停用」按钮——给了却点不动比不给更糟。
   */
  canStop: boolean;
  /** 停掉全部。宿主没实现时是 no-op（`canStop` 已经为 false）。 */
  stopAll: () => void;
}

const EMPTY_MONITORS: readonly HostMonitorAutomation[] = [];

function monitorsEqual(
  left: readonly HostMonitorAutomation[],
  right: readonly HostMonitorAutomation[]
): boolean {
  if (left.length !== right.length) return false;
  for (let index = 0; index < left.length; index += 1) {
    const a = left[index];
    const b = right[index];
    // 每一拍都是新对象，不能用 !== 比整条，否则胶囊那层 memo 全废。
    if (!a || !b) return false;
    if (a.id !== b.id) return false;
    if (a.name !== b.name) return false;
    if (a.peerTarget !== b.peerTarget) return false;
    if (a.nextFireAtUnixMs !== b.nextFireAtUnixMs) return false;
  }
  return true;
}

/**
 * 轮询宿主，问「这条会话起过哪些还启用着的监控自动化」。
 *
 * 三件事与 0122 的 `useHostSessionLiveness` 同源，都是踩过的坑：
 *
 * 1. **宿主没这组能力就永久停拍**：桥把「未嵌入 / 老宿主 / 超时」归一成
 *    `Error("unsupported")`，收到就再也不发请求。
 * 2. **不叠请求**：上一拍还没回来时这一拍直接跳过。
 * 3. **页面不可见不拍**。
 *
 * 另外：停用成功后立刻补拍一次，别让用户点完「停用」还要盯着胶囊等 20 秒。
 */
export function useHostMonitorAutomations(
  agentSessionId: string | null | undefined
): HostMonitorAutomationsResult {
  const host = monitorAutomationHost();
  const requestedId = useMemo(() => {
    const trimmed = typeof agentSessionId === "string" ? agentSessionId.trim() : "";
    return trimmed;
  }, [agentSessionId]);
  const enabled = monitorAutomationChip && host !== null && requestedId !== "";
  const [monitors, setMonitors] =
    useState<readonly HostMonitorAutomation[]>(EMPTY_MONITORS);
  // 「宿主不支持」跨依赖变化也要记住：会话一切 effect 会重跑，
  // 用局部变量记会被重置成「再试一次」。
  const unsupportedRef = useRef(false);
  // 停用之后让下一拍立刻发生，不等定时器。
  const refreshRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!enabled || !host) {
      setMonitors(EMPTY_MONITORS);
      return;
    }
    let cancelled = false;
    let inFlight = false;
    const tick = async (): Promise<void> => {
      if (cancelled || unsupportedRef.current || inFlight) return;
      inFlight = true;
      try {
        const result = await host.queryMonitorAutomations({
          creatorSessionIds: [requestedId].slice(0, MONITOR_AUTOMATIONS_MAX_IDS)
        });
        if (cancelled) return;
        const next = Array.isArray(result?.monitors) ? result.monitors : [];
        setMonitors((previous) =>
          monitorsEqual(previous, next) ? previous : next
        );
      } catch (cause) {
        // 老宿主：停拍，胶囊整个不显示。
        if (cause instanceof Error && cause.message === "unsupported") {
          unsupportedRef.current = true;
          if (!cancelled) setMonitors(EMPTY_MONITORS);
        }
        // 其余错误（宿主临时忙 / 后端重启）沉默重试：胶囊不是关键路径，
        // 报错弹条只会打扰用户。
      } finally {
        inFlight = false;
      }
    };
    refreshRef.current = () => void tick();
    void tick();
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "hidden") void tick();
    }, HOST_MONITOR_AUTOMATIONS_POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      refreshRef.current = null;
      window.clearInterval(timer);
    };
  }, [enabled, host, requestedId]);

  const stopAll = useCallback(() => {
    const stop = host?.setMonitorAutomationsEnabled;
    if (!stop || monitors.length === 0) return;
    const ids = monitors.map((item) => item.id).filter((id) => id !== "");
    if (ids.length === 0) return;
    void Promise.resolve(stop.call(host, { ids, enabled: false }))
      .then(() => {
        // 立刻补一拍：停用是用户主动动作，等 20 秒才消失像是没点中。
        refreshRef.current?.();
      })
      .catch(() => {
        // 停失败就让下一拍自己纠正：胶囊会继续显示，用户看得出没停掉。
      });
  }, [host, monitors]);

  const canStop =
    enabled &&
    typeof host?.setMonitorAutomationsEnabled === "function" &&
    monitors.length > 0;

  return {
    monitors: enabled ? monitors : EMPTY_MONITORS,
    canStop,
    stopAll
  };
}
