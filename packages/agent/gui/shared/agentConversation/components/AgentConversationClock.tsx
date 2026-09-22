import { createContext, useContext, type JSX, type ReactNode } from "react";
import {
  useExternalStoreSnapshot,
  type ExternalStoreSnapshotSource
} from "@tutti-os/ui-react-hooks";
import { attachHostPanelPolling } from "../../hostPanelVisibility";

const AgentConversationClockVisibilityContext = createContext(true);
type ClockListener = () => void;

class AgentConversationClockStore {
  private readonly minuteListeners = new Set<ClockListener>();
  private readonly secondListeners = new Set<ClockListener>();
  private minuteTimeMs = Date.now();
  private minutePollingStop: (() => void) | null = null;
  private secondTimeMs = Date.now();
  private secondPollingStop: (() => void) | null = null;

  readonly disabled: ExternalStoreSnapshotSource<number> = {
    getSnapshot: () => 0,
    subscribe: () => () => undefined
  };

  readonly minute: ExternalStoreSnapshotSource<number> = {
    getSnapshot: () => this.minuteTimeMs,
    subscribe: (listener) =>
      this.subscribe(listener, this.minuteListeners, 60_000, "minuteTimeMs")
  };

  readonly second: ExternalStoreSnapshotSource<number> = {
    getSnapshot: () => this.secondTimeMs,
    subscribe: (listener) =>
      this.subscribe(listener, this.secondListeners, 1_000, "secondTimeMs")
  };

  private subscribe(
    listener: ClockListener,
    listeners: Set<ClockListener>,
    intervalMs: number,
    timeKey: "minuteTimeMs" | "secondTimeMs"
  ): () => void {
    listeners.add(listener);
    if (listeners.size === 1) {
      this[timeKey] = Date.now();
      // timing: share one cadence timer across all mounted consumers.
      // tickOnAttach 为 false：这里跑在外部 store 的 subscribe 里，
      // 不能同步通知 listener。宿主面板从隐藏回到可见时才补一拍。
      const stop = attachHostPanelPolling({
        intervalMs,
        tickOnAttach: false,
        tick: () => {
          this[timeKey] = Date.now();
          listeners.forEach((candidate) => candidate());
        }
      });
      if (timeKey === "minuteTimeMs") this.minutePollingStop = stop;
      else this.secondPollingStop = stop;
    }
    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) {
        if (timeKey === "minuteTimeMs") {
          this.minutePollingStop?.();
          this.minutePollingStop = null;
        } else {
          this.secondPollingStop?.();
          this.secondPollingStop = null;
        }
      }
    };
  }
}

const agentConversationClock = new AgentConversationClockStore();

export function AgentConversationClockProvider({
  children,
  isVisible
}: {
  children: ReactNode;
  isVisible: boolean;
}): JSX.Element {
  return (
    <AgentConversationClockVisibilityContext.Provider value={isVisible}>
      {children}
    </AgentConversationClockVisibilityContext.Provider>
  );
}

export function useAgentConversationNowUnixMs(enabled: boolean): number | null {
  const isVisible = useContext(AgentConversationClockVisibilityContext);
  const shouldTick = enabled && isVisible;
  const nowUnixMs = useExternalStoreSnapshot(
    shouldTick ? agentConversationClock.second : agentConversationClock.disabled
  );
  return shouldTick ? nowUnixMs : null;
}

export function useAgentConversationMinuteNowUnixMs(): number {
  const isVisible = useContext(AgentConversationClockVisibilityContext);
  const nowUnixMs = useExternalStoreSnapshot(
    isVisible ? agentConversationClock.minute : agentConversationClock.disabled
  );
  return isVisible ? nowUnixMs : agentConversationClock.minute.getSnapshot();
}
