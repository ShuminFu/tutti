import { useEffect, useState } from "react";

/**
 * 宿主面板可见性。
 *
 * 和 `document.visibilityState` 不是一回事：宿主用 CSS `visibility:hidden`
 * 藏 iframe，文档仍是 visible。非嵌入、或宿主从不发消息时默认 visible，
 * 轮询与现在一致。
 */
type Listener = () => void;

const hostPanelVisibility = {
  visible: true
};
const listeners = new Set<Listener>();

export function isHostPanelVisible(): boolean {
  return hostPanelVisibility.visible;
}

export function subscribeHostPanelVisibility(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function setHostPanelVisible(next: boolean): void {
  if (next === hostPanelVisibility.visible) return;
  hostPanelVisibility.visible = next;
  for (const listener of [...listeners]) listener();
}

/** 测试收尾：单例不能漏到下一例。已经是 visible 时不通知。 */
export function resetHostPanelVisibilityForTests(): void {
  setHostPanelVisible(true);
}

export function useHostPanelVisible(): boolean {
  const [current, setCurrent] = useState(isHostPanelVisible);
  useEffect(
    () =>
      subscribeHostPanelVisibility(() => {
        setCurrent(isHostPanelVisible());
      }),
    []
  );
  return current;
}

/**
 * 面板隐藏时拆掉 interval。回退看红时改成 `return timerId`（不 clear），保留本符号。
 * 卸载清理走调用方自己的 clearInterval，不经过这里。
 */
function pauseIntervalWhileHostHidden(timerId: number | null): number | null {
  if (timerId !== null) {
    window.clearInterval(timerId);
  }
  return null;
}

/**
 * 宿主面板藏起来就拆掉 interval（回调不能挂着空转）；亮回来先补一拍，
 * 再按原来的 `intervalMs` 挂上。不要用 0 延迟的 interval，否则补拍和下一拍叠成两下。
 *
 * `document.visibilityState` 另算：那条只该留在 `intervalTick` 里跳过仍挂着的一拍。
 * 时钟在外部 store 的 subscribe 里挂表，`tickOnAttach: false`，
 * 避免同步通知 listener；补拍只发生在隐藏 → 可见。
 */
export function attachHostPanelPolling(options: {
  intervalMs: number;
  /** 挂上时（除非 tickOnAttach 为 false）、以及从隐藏回到可见时立刻跑的一拍。 */
  tick: () => void;
  /** 间隔回调。缺省等于 tick。 */
  intervalTick?: () => void;
  tickOnAttach?: boolean;
}): () => void {
  let timer: number | null = null;
  const disarm = (): void => {
    if (timer !== null) {
      window.clearInterval(timer);
      timer = null;
    }
  };
  const arm = (): void => {
    disarm();
    // timing: 面板可见才挂主动轮询；藏起来拆掉，亮回来先补一拍再按原间隔。
    timer = window.setInterval(
      options.intervalTick ?? options.tick,
      options.intervalMs
    );
  };
  const show = (tickNow: boolean): void => {
    if (tickNow) options.tick();
    arm();
  };
  if (isHostPanelVisible()) {
    show(options.tickOnAttach !== false);
  }
  const unsubscribe = subscribeHostPanelVisibility(() => {
    if (!isHostPanelVisible()) {
      timer = pauseIntervalWhileHostHidden(timer);
      return;
    }
    show(true);
  });
  return () => {
    unsubscribe();
    disarm();
  };
}
