import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { useAgentMessageLocatorSelection } from "./useAgentMessageLocatorSelection";

const items = Array.from({ length: 20 }, (_, index) => ({
  key: `message-${index}`,
  rowKey: `row-${index}`,
  turnGroupIndex: index,
  rowIndex: index,
  summary: `${index}`,
  hasAgentResponse: true
}));
afterEach(() => document.body.replaceChildren());

it.each(["none", "wheel", "keyboard"])(
  "settles after a single reverse scroll with %s intent",
  async (intent) => {
    const timeline = document.createElement("div");
    timeline.dataset.testid = "agent-gui-timeline";
    const locator = document.createElement("nav");
    timeline.append(locator);
    document.body.append(timeline);
    timeline.scrollTop = 760;
    const source = {
      scrollOffset: 1000,
      scrollRect: { height: 480 },
      getVirtualItemForOffset: (offset: number) => ({
        index: Math.floor(offset / 100)
      })
    };
    const locatorRef = { current: locator };
    const { result } = renderHook(() =>
      useAgentMessageLocatorSelection({
        items,
        isVisible: true,
        locatorRef,
        virtualSelectionSource: source
      })
    );
    await waitFor(() => expect(result.current.selectedKey).toBe("message-10"));
    act(() => {
      if (intent === "wheel")
        timeline.dispatchEvent(new WheelEvent("wheel", { deltaY: -500 }));
      if (intent === "keyboard")
        timeline.dispatchEvent(
          new KeyboardEvent("keydown", { key: "ArrowUp" })
        );
      timeline.scrollTop = 260;
      timeline.dispatchEvent(new Event("scroll"));
    });
    await waitFor(() => expect(result.current.selectedKey).toBe("message-5"));
    act(() => {
      timeline.scrollTop = 360;
      timeline.dispatchEvent(new Event("scroll"));
    });
    await waitFor(() => expect(result.current.selectedKey).toBe("message-6"));
  }
);
