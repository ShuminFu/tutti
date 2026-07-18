import type { ComponentProps } from "react";
import { useExternalStoreSnapshot } from "@tutti-os/ui-react-hooks";
import { ConversationMeta } from "../agentGuiNodeViewConversation";
import type { AgentGUINodeViewModel } from "../model/agentGuiNodeTypes";

type ClockListener = () => void;

class AgentGUIConversationRailClockStore {
  private currentTimeMs = Date.now();
  private readonly listeners = new Set<ClockListener>();
  private timer: number | null = null;

  readonly getSnapshot = (): number => this.currentTimeMs;

  readonly subscribe = (listener: ClockListener): (() => void) => {
    this.listeners.add(listener);
    if (this.listeners.size === 1) {
      this.start();
    }
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {
        this.stop();
      }
    };
  };

  private start(): void {
    this.currentTimeMs = Date.now();
    // timing: refresh relative timestamps in the rail once a minute
    this.timer = window.setInterval(() => {
      this.currentTimeMs = Date.now();
      for (const listener of this.listeners) {
        listener();
      }
    }, 60_000);
  }

  private stop(): void {
    if (this.timer !== null) {
      window.clearInterval(this.timer);
      this.timer = null;
    }
  }
}

const agentGUIConversationRailClock = new AgentGUIConversationRailClockStore();

export function AgentGUIConversationRailRelativeTime({
  item,
  labels
}: {
  item: AgentGUINodeViewModel["rail"]["conversations"][number];
  labels: ComponentProps<typeof ConversationMeta>["labels"];
}): React.JSX.Element {
  const currentTimeMs = useExternalStoreSnapshot(agentGUIConversationRailClock);
  return <ConversationMeta item={item} nowMs={currentTimeMs} labels={labels} />;
}
