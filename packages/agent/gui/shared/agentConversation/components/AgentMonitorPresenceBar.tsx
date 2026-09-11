import { memo, type JSX } from "react";
import { Eye, PlugZap, Telescope } from "lucide-react";
import { translate } from "../../../i18n/index";
import type { HostMonitorAutomation } from "../monitorAutomationHost";
import type { AgentMonitorPresenceVM } from "../projection/monitorPresence";
import { useAgentConversationNowUnixMs } from "./AgentConversationClock";
import { formatAgentToolDurationMs } from "./tool-renderers/render-data/agentToolRenderData";

// A monitor outlives the turn that armed it: once the turn settles its card
// scrolls away with the transcript and nothing on screen says the watcher is
// still there. This strip pins that fact to the top of the conversation - the
// only always-visible place the conversation owns.
export const AgentMonitorPresenceBar = memo(function AgentMonitorPresenceBar({
  monitors,
  onStopMonitors,
  automations,
  onStopAutomations
}: {
  monitors: AgentMonitorPresenceVM | undefined;
  // Stops every running monitor in this conversation. Absent when the host
  // wires no stop path, in which case the chip stays a pure status strip.
  onStopMonitors?: (agentSessionIds: readonly string[]) => void;
  // Monitor automations started from this conversation (patch 0135). A
  // different animal from the Monitor tool above: no process runs between
  // heartbeats, so this lane counts down to the next one instead of counting
  // up elapsed time. Undefined when the host exposes no such capability.
  automations?: readonly HostMonitorAutomation[];
  // Disables every automation in `automations`. Absent when the host only
  // wired the read side, in which case the lane stays a pure status chip.
  onStopAutomations?: () => void;
}): JSX.Element | null {
  "use memo";
  const running = monitors?.runningCount ?? 0;
  const interrupted = monitors?.interruptedCount ?? 0;
  const automationCount = automations?.length ?? 0;
  // The clock ticks while either lane has something live to show: the Monitor
  // lane counts elapsed time up, the automation lane counts the next heartbeat
  // down. Nothing live means no ticking.
  const nowUnixMs = useAgentConversationNowUnixMs(
    (running > 0 &&
      typeof monitors?.longestRunningStartedAtUnixMs === "number") ||
      automationCount > 0
  );
  if (running === 0 && interrupted === 0 && automationCount === 0) {
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
      className="pointer-events-none flex shrink-0 flex-wrap justify-end gap-2 px-4 pt-2"
      data-testid="agent-gui-monitor-presence-bar"
      data-state={isRunning ? "running" : "interrupted"}
      role="status"
    >
      {running === 0 && interrupted === 0 ? null : (
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
      )}
      {automationCount > 0 ? (
        <span
          className="pointer-events-auto inline-flex items-center gap-2 rounded-full border border-[var(--tutti-purple-border)] bg-[var(--tutti-purple-bg)] px-3 py-1 text-[12px] text-[var(--tutti-purple)]"
          data-testid="agent-gui-monitor-automation-chip"
          title={automationTitle(automations ?? [])}
        >
          <Telescope size={14} strokeWidth={2} aria-hidden="true" />
          <span data-testid="agent-gui-monitor-automation-label">
            {automationChipText(automations ?? [], nowUnixMs)}
          </span>
          {onStopAutomations ? (
            <button
              type="button"
              className="cursor-pointer rounded-full px-1 text-[12px] underline-offset-2 hover:underline"
              data-testid="agent-gui-monitor-automation-stop"
              onClick={onStopAutomations}
            >
              {translate("agentHost.monitorPresence.stopAutomations")}
            </button>
          ) : null}
        </span>
      ) : null}
    </div>
  );
});

// "我起了 2 个监控器 · 下次 47s" - the count is the fact, the countdown is the
// reassurance that the thing is still armed. No countdown when the host could
// not give a next-fire time: an invented one is worse than none.
function automationChipText(
  automations: readonly HostMonitorAutomation[],
  nowUnixMs: number | null
): string {
  const label = translate("agentHost.monitorPresence.automationsRunning", {
    count: automations.length
  });
  const remaining = nextFireText(automations, nowUnixMs);
  return remaining ? `${label} · ${remaining}` : label;
}

// Tooltip lists who is being watched. peerTarget can be blank when the host
// does not know the alias; those entries fall back to the automation name.
function automationTitle(
  automations: readonly HostMonitorAutomation[]
): string {
  return automations
    .map((item) => item.peerTarget || item.name)
    .filter((text) => text !== "")
    .join("\n");
}

// Counts down to the *soonest* heartbeat: with several monitors armed, the one
// that fires next is the one that answers "is this thing still alive".
function nextFireText(
  automations: readonly HostMonitorAutomation[],
  nowUnixMs: number | null
): string | null {
  if (typeof nowUnixMs !== "number") return null;
  let soonest: number | null = null;
  for (const item of automations) {
    const at = item.nextFireAtUnixMs;
    if (typeof at !== "number") continue;
    if (soonest === null || at < soonest) soonest = at;
  }
  // Already due (or overdue): the backend fires on its own schedule and a
  // negative countdown reads like a bug, so show the count alone.
  if (soonest === null || soonest <= nowUnixMs) return null;
  return translate("agentHost.monitorPresence.nextFire", {
    remaining: formatAgentToolDurationMs(soonest - nowUnixMs)
  });
}

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
