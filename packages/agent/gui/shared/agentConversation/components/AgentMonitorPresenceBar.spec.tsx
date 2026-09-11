import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setAgentGuiI18nTestLocale } from "../../../i18n/testUtils";
import { AgentMonitorPresenceBar } from "./AgentMonitorPresenceBar";

describe("AgentMonitorPresenceBar", () => {
  afterEach(() => {
    vi.useRealTimers();
    setAgentGuiI18nTestLocale("zh-CN");
  });

  it("shows nothing when no monitor is armed", () => {
    const { container } = render(
      <AgentMonitorPresenceBar
        monitors={{
          runningCount: 0,
          interruptedCount: 0,
          longestRunningStartedAtUnixMs: null,
          runningChildSessionIds: []
        }}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("names the count and keeps the longest elapsed ticking", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(100_000);

    render(
      <AgentMonitorPresenceBar
        monitors={{
          runningCount: 2,
          interruptedCount: 0,
          longestRunningStartedAtUnixMs: 40_000,
          runningChildSessionIds: ["monitor-a", "monitor-b"]
        }}
      />
    );

    const label = screen.getByTestId("agent-gui-monitor-presence-label");
    expect(label.textContent).toBe("监控器运行中 · 2 · 最久 1m 0s");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });

    expect(label.textContent).toBe("监控器运行中 · 2 · 最久 1m 2s");
  });

  it("stops every running monitor by its own child session id", () => {
    const onStopMonitors = vi.fn();
    render(
      <AgentMonitorPresenceBar
        monitors={{
          runningCount: 2,
          interruptedCount: 0,
          longestRunningStartedAtUnixMs: 40_000,
          runningChildSessionIds: ["monitor-a", "monitor-b"]
        }}
        onStopMonitors={onStopMonitors}
      />
    );

    fireEvent.click(screen.getByTestId("agent-gui-monitor-presence-stop"));

    expect(onStopMonitors).toHaveBeenCalledWith(["monitor-a", "monitor-b"]);
  });

  it("offers no stop control for monitors that are already cut off", () => {
    render(
      <AgentMonitorPresenceBar
        monitors={{
          runningCount: 0,
          interruptedCount: 1,
          longestRunningStartedAtUnixMs: 40_000,
          runningChildSessionIds: []
        }}
        onStopMonitors={vi.fn()}
      />
    );

    expect(
      screen.queryByTestId("agent-gui-monitor-presence-stop")
    ).not.toBeInTheDocument();
  });

  it("says interrupted, without an elapsed clock, once the monitors were cut off", () => {
    render(
      <AgentMonitorPresenceBar
        monitors={{
          runningCount: 0,
          interruptedCount: 2,
          longestRunningStartedAtUnixMs: 40_000,
          runningChildSessionIds: []
        }}
      />
    );

    expect(
      screen.getByTestId("agent-gui-monitor-presence-bar")
    ).toHaveAttribute("data-state", "interrupted");
    expect(
      screen.getByTestId("agent-gui-monitor-presence-label").textContent
    ).toBe("监控器已中断 · 2");
  });
});
