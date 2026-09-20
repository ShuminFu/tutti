import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useComposerFocusAndDrop } from "./useComposerFocusAndDrop";

vi.mock("./useComposerFileDrop", () => ({
  useComposerFileDrop: () => ({
    fileDropOverlayActive: false,
    fileDropOverlayHost: null
  })
}));
afterEach(() => vi.restoreAllMocks());

describe("composer activation focus", () => {
  it("does not scroll away from attention arriving during scheduled focus; explicit focus still works", () => {
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) =>
      frames.push(callback)
    );
    const dock = document.createElement("div");
    dock.className = "agent-gui-node__bottom-dock";
    const form = document.createElement("form");
    dock.append(form);
    const focusAtEnd = vi.fn();
    const input = {
      composerControlsHardDisabled: false,
      inputDisabled: false,
      editorHandleRef: {
        current: { focusAtEnd, openMentionPalette: vi.fn() } as never
      },
      composerRef: { current: form },
      wasActiveRef: { current: false },
      lastComposerFocusRequestRef: { current: null as number | null },
      isActive: true,
      composerFocusRequestSequence: null as number | null,
      promptFilesSupported: false,
      promptImagesSupported: false,
      addDraftImages: vi.fn(),
      addDraftFiles: vi.fn()
    };
    const { rerender } = renderHook(
      ({ sequence }) =>
        useComposerFocusAndDrop({
          ...input,
          composerFocusRequestSequence: sequence
        }),
      { initialProps: { sequence: null as number | null } }
    );
    act(() => frames.shift()!(0));
    const attention = document.createElement("div");
    attention.dataset.agentComposerAttention = "true";
    dock.prepend(attention);
    act(() => frames.shift()!(0));
    expect(focusAtEnd).not.toHaveBeenCalled();
    rerender({ sequence: 1 });
    act(() => {
      frames.shift()!(0);
      frames.shift()!(0);
    });
    expect(focusAtEnd).toHaveBeenCalledOnce();
  });
});
