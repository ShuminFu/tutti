import { act, cleanup, render, screen } from "@testing-library/react";
import type { JSX } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  resetHostPanelVisibilityForTests,
  setHostPanelVisible
} from "../../hostPanelVisibility";
import {
  AgentConversationClockProvider,
  useAgentConversationMinuteNowUnixMs,
  useAgentConversationNowUnixMs
} from "./AgentConversationClock";
import { useElapsedSeconds } from "./useElapsedSeconds";

describe("AgentConversationClock", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000);
  });

  afterEach(() => {
    cleanup();
    resetHostPanelVisibilityForTests();
    vi.useRealTimers();
  });

  it("shares one interval and pauses it while the AgentGUI is hidden", () => {
    const setInterval = vi.spyOn(window, "setInterval");
    const clearInterval = vi.spyOn(window, "clearInterval");
    const { rerender } = render(<ElapsedPair isVisible />);

    expect(screen.getByTestId("elapsed-a")).toHaveTextContent("0");
    expect(screen.getByTestId("elapsed-b")).toHaveTextContent("0");
    expect(setInterval).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(2_000);
    });
    expect(screen.getByTestId("elapsed-a")).toHaveTextContent("2");
    expect(screen.getByTestId("elapsed-b")).toHaveTextContent("2");

    rerender(<ElapsedPair isVisible={false} />);
    expect(clearInterval).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    expect(vi.getTimerCount()).toBe(0);

    rerender(<ElapsedPair isVisible />);
    expect(screen.getByTestId("elapsed-a")).toHaveTextContent("7");
    expect(screen.getByTestId("elapsed-b")).toHaveTextContent("7");
    expect(setInterval).toHaveBeenCalledTimes(2);
  });

  it("pauses minute updates while the AgentGUI is hidden", () => {
    const setInterval = vi.spyOn(window, "setInterval");
    const clearInterval = vi.spyOn(window, "clearInterval");
    const { rerender } = render(<MinuteValue isVisible />);

    expect(screen.getByTestId("minute-now")).toHaveTextContent("1000");
    expect(setInterval).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(screen.getByTestId("minute-now")).toHaveTextContent("61000");

    rerender(<MinuteValue isVisible={false} />);
    expect(clearInterval).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
    act(() => {
      vi.advanceTimersByTime(120_000);
    });
    expect(screen.getByTestId("minute-now")).toHaveTextContent("61000");

    rerender(<MinuteValue isVisible />);
    expect(screen.getByTestId("minute-now")).toHaveTextContent("181000");
    expect(setInterval).toHaveBeenCalledTimes(2);
  });

  it("宿主面板隐藏时秒针停住，亮回来先跳到当前时刻再继续", () => {
    render(<SecondValue isVisible />);
    expect(screen.getByTestId("second-now")).toHaveTextContent("1000");

    act(() => {
      vi.advanceTimersByTime(3_000);
    });
    expect(screen.getByTestId("second-now")).toHaveTextContent("4000");
    expect(document.visibilityState).toBe("visible");

    act(() => setHostPanelVisible(false));
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByTestId("second-now")).toHaveTextContent("4000");
    expect(vi.getTimerCount()).toBe(0);

    act(() => setHostPanelVisible(true));
    expect(screen.getByTestId("second-now")).toHaveTextContent("14000");
    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    expect(screen.getByTestId("second-now")).toHaveTextContent("15000");
  });

  it("宿主面板隐藏时分针停住，亮回来先跳到当前时刻再继续", () => {
    render(<MinuteValue isVisible />);
    expect(screen.getByTestId("minute-now")).toHaveTextContent("1000");

    act(() => {
      vi.advanceTimersByTime(3 * 60_000);
    });
    expect(screen.getByTestId("minute-now")).toHaveTextContent("181000");

    act(() => setHostPanelVisible(false));
    act(() => {
      vi.advanceTimersByTime(10 * 60_000);
    });
    expect(screen.getByTestId("minute-now")).toHaveTextContent("181000");
    expect(vi.getTimerCount()).toBe(0);

    act(() => setHostPanelVisible(true));
    expect(screen.getByTestId("minute-now")).toHaveTextContent("781000");
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(screen.getByTestId("minute-now")).toHaveTextContent("841000");
  });

  it("挂上时宿主面板已经隐藏：秒针不走，第一次变可见才跳到当前时刻", () => {
    act(() => setHostPanelVisible(false));
    render(<SecondValue isVisible />);
    expect(screen.getByTestId("second-now")).toHaveTextContent("1000");
    expect(vi.getTimerCount()).toBe(0);

    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByTestId("second-now")).toHaveTextContent("1000");

    act(() => setHostPanelVisible(true));
    expect(screen.getByTestId("second-now")).toHaveTextContent("11000");
    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    expect(screen.getByTestId("second-now")).toHaveTextContent("12000");
  });
});

function ElapsedPair({ isVisible }: { isVisible: boolean }): JSX.Element {
  return (
    <AgentConversationClockProvider isVisible={isVisible}>
      <ElapsedValue testId="elapsed-a" />
      <ElapsedValue testId="elapsed-b" />
    </AgentConversationClockProvider>
  );
}

function ElapsedValue({ testId }: { testId: string }): JSX.Element {
  const elapsedSeconds = useElapsedSeconds(1_000);
  return <span data-testid={testId}>{elapsedSeconds}</span>;
}

function MinuteValue({ isVisible }: { isVisible: boolean }): JSX.Element {
  return (
    <AgentConversationClockProvider isVisible={isVisible}>
      <MinuteNow />
    </AgentConversationClockProvider>
  );
}

function MinuteNow(): JSX.Element {
  const nowUnixMs = useAgentConversationMinuteNowUnixMs();
  return <span data-testid="minute-now">{nowUnixMs}</span>;
}

function SecondValue({ isVisible }: { isVisible: boolean }): JSX.Element {
  return (
    <AgentConversationClockProvider isVisible={isVisible}>
      <SecondNow />
    </AgentConversationClockProvider>
  );
}

function SecondNow(): JSX.Element {
  const nowUnixMs = useAgentConversationNowUnixMs(true);
  return <span data-testid="second-now">{nowUnixMs}</span>;
}
