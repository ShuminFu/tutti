import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  resetHostPanelVisibilityForTests,
  setHostPanelVisible
} from "../../shared/hostPanelVisibility";
import {
  AgentProbeUsageFreshness,
  FRESHNESS_TICK_MS,
  type AgentProbeUsageFreshnessLabels
} from "./AgentProbeUsageFreshness";

const labels: AgentProbeUsageFreshnessLabels = {
  justUpdated: "just now",
  minutesAgo: (count) => `${count}m ago`,
  hoursAgo: (count) => `${count}h ago`,
  updating: "updating",
  refreshFailed: "refresh failed",
  refreshAria: "refresh usage"
};

const NOW = 1_700_000_000_000;

function renderControl(
  overrides: Partial<Parameters<typeof AgentProbeUsageFreshness>[0]> = {}
) {
  const onRefresh = overrides.onRefresh ?? vi.fn();
  const utils = render(
    <AgentProbeUsageFreshness
      testId="freshness"
      capturedAtUnixMs={NOW - 3 * 60_000}
      isLoading={false}
      didFail={false}
      onRefresh={onRefresh}
      labels={labels}
      {...overrides}
    />
  );
  return { ...utils, onRefresh };
}

describe("AgentProbeUsageFreshness", () => {
  beforeEach(() => {
    vi.spyOn(Date, "now").mockReturnValue(NOW);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders a relative freshness stamp from the capture time", () => {
    const { getByTestId } = renderControl();
    expect(getByTestId("freshness")).toHaveTextContent("3m ago");
  });

  it("shows 'just now' under a minute old", () => {
    const { getByTestId } = renderControl({ capturedAtUnixMs: NOW - 20_000 });
    expect(getByTestId("freshness")).toHaveTextContent("just now");
  });

  it("shows the updating state and disables the control while loading", () => {
    const { getByTestId } = renderControl({ isLoading: true });
    const button = getByTestId("freshness");
    expect(button).toHaveTextContent("updating");
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(button).toHaveAttribute("data-state", "loading");
  });

  it("surfaces a retryable failed state without disabling the control", () => {
    const onRefresh = vi.fn();
    const { getByTestId } = renderControl({ didFail: true, onRefresh });
    const button = getByTestId("freshness");
    expect(button).toHaveTextContent("refresh failed");
    expect(button).not.toBeDisabled();
    expect(button).toHaveAttribute("data-state", "failed");
    fireEvent.click(button);
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("invokes onRefresh when clicked in the idle state", () => {
    const { getByTestId, onRefresh } = renderControl();
    fireEvent.click(getByTestId("freshness"));
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("does not fire onRefresh while loading (disabled)", () => {
    const { getByTestId, onRefresh } = renderControl({ isLoading: true });
    fireEvent.click(getByTestId("freshness"));
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it("plays a one-shot reassurance spin on click even when the refresh is a no-op", () => {
    // The refresh can be served from the main-process cache and never flip
    // isLoading, so a click must still visibly move the icon.
    const { getByTestId } = renderControl();
    const icon = () => getByTestId("freshness").querySelector("svg");
    expect(icon()?.getAttribute("class") ?? "").not.toContain("animate-");
    fireEvent.click(getByTestId("freshness"));
    expect(icon()?.getAttribute("class") ?? "").toContain(
      "motion-safe:animate-[spin_0.6s_linear]"
    );
  });
});

describe("AgentProbeUsageFreshness host panel visibility", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    cleanup();
    resetHostPanelVisibilityForTests();
    vi.useRealTimers();
  });

  it("宿主面板隐藏时新鲜度停走，亮回来先跳到当前时刻再继续", () => {
    const { getByTestId } = renderControl({ capturedAtUnixMs: NOW });
    const stamp = () => getByTestId("freshness").getAttribute("data-now-ms");
    expect(stamp()).toBe(String(NOW));

    act(() => {
      vi.advanceTimersByTime(3 * FRESHNESS_TICK_MS);
    });
    expect(stamp()).toBe(String(NOW + 3 * FRESHNESS_TICK_MS));
    expect(getByTestId("freshness")).toHaveTextContent("1m ago");
    expect(document.visibilityState).toBe("visible");

    act(() => setHostPanelVisible(false));
    act(() => {
      vi.advanceTimersByTime(10 * FRESHNESS_TICK_MS);
    });
    expect(stamp()).toBe(String(NOW + 3 * FRESHNESS_TICK_MS));
    expect(getByTestId("freshness")).toHaveTextContent("1m ago");
    expect(vi.getTimerCount()).toBe(0);

    act(() => setHostPanelVisible(true));
    expect(stamp()).toBe(String(NOW + 13 * FRESHNESS_TICK_MS));
    expect(getByTestId("freshness")).toHaveTextContent("4m ago");
    act(() => {
      vi.advanceTimersByTime(FRESHNESS_TICK_MS);
    });
    expect(stamp()).toBe(String(NOW + 14 * FRESHNESS_TICK_MS));
  });

  it("挂上时宿主面板已经隐藏：不走表，第一次变可见才跳到当前时刻", () => {
    act(() => setHostPanelVisible(false));
    const { getByTestId } = renderControl({ capturedAtUnixMs: NOW });
    const stamp = () => getByTestId("freshness").getAttribute("data-now-ms");
    expect(stamp()).toBe(String(NOW));
    expect(vi.getTimerCount()).toBe(0);

    act(() => {
      vi.advanceTimersByTime(10 * FRESHNESS_TICK_MS);
    });
    expect(stamp()).toBe(String(NOW));

    act(() => setHostPanelVisible(true));
    expect(stamp()).toBe(String(NOW + 10 * FRESHNESS_TICK_MS));
    act(() => {
      vi.advanceTimersByTime(FRESHNESS_TICK_MS);
    });
    expect(stamp()).toBe(String(NOW + 11 * FRESHNESS_TICK_MS));
  });
});
