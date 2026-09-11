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

  // 监控自动化那一档（补丁 0135）。与上面的 Monitor 工具是两码事：它平时没有
  // 进程在跑，所以显示的是「下次心跳还有多久」，不是「已经跑了多久」。
  const EMPTY_MONITORS = {
    runningCount: 0,
    interruptedCount: 0,
    longestRunningStartedAtUnixMs: null,
    runningChildSessionIds: []
  } as const;

  it("counts the monitor automations started here and counts down to the soonest heartbeat", () => {
    vi.useFakeTimers();
    vi.setSystemTime(100_000);

    render(
      <AgentMonitorPresenceBar
        monitors={EMPTY_MONITORS}
        automations={[
          {
            id: "auto_1",
            name: "盯 grok-8014",
            peerTarget: "grok-8014",
            nextFireAtUnixMs: 160_000
          },
          {
            id: "auto_2",
            name: "盯 grok-8014-2",
            peerTarget: "grok-8014-2",
            // 更早的那个才是「还活着」的证据，倒计时要看它。
            nextFireAtUnixMs: 147_000
          }
        ]}
      />
    );

    expect(
      screen.getByTestId("agent-gui-monitor-automation-label").textContent
    ).toBe("我起了 2 个监控器 · 下次 47s");
    expect(
      screen.getByTestId("agent-gui-monitor-automation-chip").title
    ).toBe("grok-8014\ngrok-8014-2");
  });

  it("drops the countdown when the heartbeat is already due, rather than showing a negative one", () => {
    vi.useFakeTimers();
    vi.setSystemTime(200_000);

    render(
      <AgentMonitorPresenceBar
        monitors={EMPTY_MONITORS}
        automations={[
          {
            id: "auto_1",
            name: "盯 grok-8014",
            peerTarget: "grok-8014",
            nextFireAtUnixMs: 100_000
          }
        ]}
      />
    );

    expect(
      screen.getByTestId("agent-gui-monitor-automation-label").textContent
    ).toBe("我起了 1 个监控器");
  });

  it("offers 停用 only when the host wired a stop path", () => {
    const onStopAutomations = vi.fn();
    const automations = [
      {
        id: "auto_1",
        name: "盯 grok-8014",
        peerTarget: "grok-8014",
        nextFireAtUnixMs: null
      }
    ];

    const { rerender } = render(
      <AgentMonitorPresenceBar monitors={EMPTY_MONITORS} automations={automations} />
    );
    expect(
      screen.queryByTestId("agent-gui-monitor-automation-stop")
    ).toBeNull();

    rerender(
      <AgentMonitorPresenceBar
        monitors={EMPTY_MONITORS}
        automations={automations}
        onStopAutomations={onStopAutomations}
      />
    );
    fireEvent.click(screen.getByTestId("agent-gui-monitor-automation-stop"));
    expect(onStopAutomations).toHaveBeenCalledTimes(1);
  });

  it("shows both lanes side by side when a Monitor tool and an automation are both armed", () => {
    vi.useFakeTimers();
    vi.setSystemTime(100_000);

    render(
      <AgentMonitorPresenceBar
        monitors={{
          runningCount: 1,
          interruptedCount: 0,
          longestRunningStartedAtUnixMs: 40_000,
          runningChildSessionIds: ["monitor-a"]
        }}
        automations={[
          {
            id: "auto_1",
            name: "盯 grok-8014",
            peerTarget: "grok-8014",
            nextFireAtUnixMs: 147_000
          }
        ]}
      />
    );

    expect(
      screen.getByTestId("agent-gui-monitor-presence-label").textContent
    ).toBe("监控器运行中 · 1 · 最久 1m 0s");
    expect(
      screen.getByTestId("agent-gui-monitor-automation-label").textContent
    ).toBe("我起了 1 个监控器 · 下次 47s");
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
