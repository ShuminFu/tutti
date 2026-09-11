import { memo, type JSX } from "react";
import { Eye, PlugZap } from "lucide-react";
import { translate } from "../../../i18n/index";
import type { AgentMonitorPresenceVM } from "../projection/monitorPresence";
import { useAgentConversationNowUnixMs } from "./AgentConversationClock";
import { formatAgentToolDurationMs } from "./tool-renderers/render-data/agentToolRenderData";

// A monitor outlives the turn that armed it: once the turn settles its card
// scrolls away with the transcript and nothing on screen says the watcher is
// still there. This strip pins that fact to the top of the conversation - the
// only always-visible place the conversation owns.
export const AgentMonitorPresenceBar = memo(function AgentMonitorPresenceBar({
  monitors,
  onStopMonitors
}: {
  monitors: AgentMonitorPresenceVM | undefined;
  // Stops every running monitor in this conversation. Absent when the host
  // wires no stop path, in which case the chip stays a pure status strip.
  onStopMonitors?: (agentSessionIds: readonly string[]) => void;
}): JSX.Element | null {
  "use memo";
  const running = monitors?.runningCount ?? 0;
  const interrupted = monitors?.interruptedCount ?? 0;
  // The clock ticks only while something is actually being watched; an
  // interrupted monitor has no elapsed time worth counting up.
  const nowUnixMs = useAgentConversationNowUnixMs(
    running > 0 && typeof monitors?.longestRunningStartedAtUnixMs === "number"
  );
  if (running === 0 && interrupted === 0) {
    return null;
  }
  const isRunning = running > 0;
  const count = isRunning ? running : interrupted;
  const runningIds = monitors?.runningChildSessionIds ?? [];
  const elapsedText = monitorElapsedText(
    monitors?.longestRunningStartedAtUnixMs ?? null,
    nowUnixMs
  );
  const label = isRunning
    ? translate("agentHost.monitorPresence.running")
    : translate("agentHost.monitorPresence.interrupted");
  return (
    <div
      className="pointer-events-none flex shrink-0 justify-end px-4 pt-2"
      data-testid="agent-gui-monitor-presence-bar"
      data-state={isRunning ? "running" : "interrupted"}
      role="status"
    >
      <span
        className={`pointer-events-auto inline-flex items-center gap-2 rounded-full border px-3 py-1 text-[12px] ${
          isRunning
            ? "border-[var(--tutti-purple-border)] bg-[var(--tutti-purple-bg)] text-[var(--tutti-purple)]"
            : "border-[var(--tutti-status-danger)] bg-[var(--tutti-surface-2)] text-[var(--tutti-status-danger)]"
        }`}
      >
        {isRunning ? (
          <Eye size={14} strokeWidth={2} aria-hidden="true" />
        ) : (
          <PlugZap size={14} strokeWidth={2} aria-hidden="true" />
        )}
        <span data-testid="agent-gui-monitor-presence-label">
          {monitorChipText(label, count, isRunning ? elapsedText : null)}
        </span>
        {isRunning && onStopMonitors && runningIds.length > 0 ? (
          <button
            type="button"
            className="cursor-pointer rounded-full px-1 text-[12px] underline-offset-2 hover:underline"
            data-testid="agent-gui-monitor-presence-stop"
            onClick={() => onStopMonitors(runningIds)}
          >
            {translate("agentHost.monitorPresence.stopAll")}
          </button>
        ) : null}
      </span>
    </div>
  );
});

// "监控器运行中 · 2 · 最久 04:12" - count first because it is the fact that
// changes least often, elapsed last because it ticks.
function monitorChipText(
  label: string,
  count: number,
  elapsedText: string | null
): string {
  const parts = [label, String(count)];
  if (elapsedText) {
    parts.push(
      translate("agentHost.monitorPresence.longestElapsed", {
        elapsed: elapsedText
      })
    );
  }
  return parts.join(" · ");
}

function monitorElapsedText(
  startedAtUnixMs: number | null,
  nowUnixMs: number | null
): string | null {
  if (
    typeof startedAtUnixMs !== "number" ||
    typeof nowUnixMs !== "number" ||
    nowUnixMs <= startedAtUnixMs
  ) {
    return null;
  }
  return formatAgentToolDurationMs(nowUnixMs - startedAtUnixMs);
}
