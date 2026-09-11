import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  registerMonitorAutomationHost,
  type HostMonitorAutomation,
  type MonitorAutomationHost
} from "./monitorAutomationHost";
import {
  useHostMonitorAutomations,
  type HostMonitorAutomationsResult
} from "./useHostMonitorAutomations";

// 每个用例自己装一份宿主实现，跑完摘掉，免得互相串。
let unregister: (() => void) | null = null;

function installHost(host: MonitorAutomationHost): void {
  unregister?.();
  unregister = registerMonitorAutomationHost(host);
}

function automation(over: Partial<HostMonitorAutomation> = {}): HostMonitorAutomation {
  return {
    id: "auto_1",
    name: "盯 grok-8014",
    peerTarget: "grok-8014",
    nextFireAtUnixMs: 160_000,
    ...over
  };
}

function Probe({
  agentSessionId,
  onResult
}: {
  agentSessionId: string | null;
  onResult: (result: HostMonitorAutomationsResult) => void;
}): null {
  onResult(useHostMonitorAutomations(agentSessionId));
  return null;
}

describe("useHostMonitorAutomations", () => {
  afterEach(() => {
    unregister?.();
    unregister = null;
    vi.useRealTimers();
  });

  it("asks the host for this conversation only and hands back what it gets", async () => {
    const queryMonitorAutomations = vi
      .fn()
      .mockResolvedValue({ monitors: [automation()] });
    installHost({ queryMonitorAutomations });

    let latest: HostMonitorAutomationsResult | null = null;
    render(<Probe agentSessionId="sess-a" onResult={(r) => (latest = r)} />);

    await waitFor(() => {
      expect(latest?.monitors).toHaveLength(1);
    });
    expect(queryMonitorAutomations).toHaveBeenCalledWith({
      creatorSessionIds: ["sess-a"]
    });
    // 只有查询没有停用能力：胶囊照常显示，但不给「停用」按钮。
    expect(latest?.canStop).toBe(false);
  });

  it("never asks when there is no conversation open", async () => {
    const queryMonitorAutomations = vi.fn().mockResolvedValue({ monitors: [] });
    installHost({ queryMonitorAutomations });

    let latest: HostMonitorAutomationsResult | null = null;
    render(<Probe agentSessionId={null} onResult={(r) => (latest = r)} />);

    await waitFor(() => {
      expect(latest).not.toBeNull();
    });
    expect(queryMonitorAutomations).not.toHaveBeenCalled();
    expect(latest?.monitors).toEqual([]);
  });

  it("stops polling for good once the host says unsupported", async () => {
    vi.useFakeTimers();
    const queryMonitorAutomations = vi
      .fn()
      .mockRejectedValue(new Error("unsupported"));
    installHost({ queryMonitorAutomations });

    render(<Probe agentSessionId="sess-a" onResult={() => {}} />);

    await vi.advanceTimersByTimeAsync(0);
    expect(queryMonitorAutomations).toHaveBeenCalledTimes(1);
    // 老宿主上每 20s 白问一次是纯浪费：收到 unsupported 就永久停拍。
    await vi.advanceTimersByTimeAsync(120_000);
    expect(queryMonitorAutomations).toHaveBeenCalledTimes(1);
  });

  it("disables every automation it is showing, then refreshes without waiting for the next tick", async () => {
    const monitors = [automation(), automation({ id: "auto_2" })];
    const queryMonitorAutomations = vi.fn().mockResolvedValue({ monitors });
    const setMonitorAutomationsEnabled = vi.fn().mockResolvedValue({ updated: 2 });
    installHost({ queryMonitorAutomations, setMonitorAutomationsEnabled });

    let latest: HostMonitorAutomationsResult | null = null;
    render(<Probe agentSessionId="sess-a" onResult={(r) => (latest = r)} />);

    await waitFor(() => {
      expect(latest?.canStop).toBe(true);
    });
    const callsBefore = queryMonitorAutomations.mock.calls.length;
    latest?.stopAll();

    await waitFor(() => {
      expect(setMonitorAutomationsEnabled).toHaveBeenCalledWith({
        ids: ["auto_1", "auto_2"],
        enabled: false
      });
    });
    // 点完「停用」等 20 秒胶囊才消失，像是没点中：立刻补一拍。
    await waitFor(() => {
      expect(queryMonitorAutomations.mock.calls.length).toBeGreaterThan(
        callsBefore
      );
    });
  });

  it("offers no stop when there is nothing armed", async () => {
    const queryMonitorAutomations = vi.fn().mockResolvedValue({ monitors: [] });
    const setMonitorAutomationsEnabled = vi.fn();
    installHost({ queryMonitorAutomations, setMonitorAutomationsEnabled });

    let latest: HostMonitorAutomationsResult | null = null;
    render(<Probe agentSessionId="sess-a" onResult={(r) => (latest = r)} />);

    await waitFor(() => {
      expect(latest?.monitors).toEqual([]);
    });
    expect(latest?.canStop).toBe(false);
    latest?.stopAll();
    expect(setMonitorAutomationsEnabled).not.toHaveBeenCalled();
  });
});
