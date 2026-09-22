import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
  isHostPanelVisible,
  resetHostPanelVisibilityForTests,
  setHostPanelVisible,
  useHostPanelVisible
} from "./hostPanelVisibility";

afterEach(() => {
  cleanup();
  resetHostPanelVisibilityForTests();
});

describe("hostPanelVisibility", () => {
  it("defaults to visible until a test or the host bridge says otherwise", () => {
    expect(isHostPanelVisible()).toBe(true);
    const hook = renderHook(() => useHostPanelVisible());
    expect(hook.result.current).toBe(true);

    act(() => setHostPanelVisible(false));
    expect(isHostPanelVisible()).toBe(false);
    expect(hook.result.current).toBe(false);

    act(() => setHostPanelVisible(false));
    expect(hook.result.current).toBe(false);

    act(() => setHostPanelVisible(true));
    expect(hook.result.current).toBe(true);
  });
});
