import { act, render, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { buildAgentComposerDraft } from "../model/agentComposerDraft";
import {
  shouldShowAgentComposerStopButton,
  useComposerPresentation
} from "./useComposerPresentation";

describe("shouldShowAgentComposerStopButton", () => {
  it("lets a Tutti aggregate Stop yield to a typed draft", () => {
    expect(
      shouldShowAgentComposerStopButton({
        draftOverridesStopButton: true,
        hasDraftContent: true,
        isQueueMode: false,
        showStopButton: true
      })
    ).toBe(false);
  });

  it("keeps aggregate Stop visible while the draft is empty", () => {
    expect(
      shouldShowAgentComposerStopButton({
        draftOverridesStopButton: true,
        hasDraftContent: false,
        isQueueMode: false,
        showStopButton: true
      })
    ).toBe(true);
  });

  it("preserves the ordinary busy queue behavior", () => {
    expect(
      shouldShowAgentComposerStopButton({
        draftOverridesStopButton: false,
        hasDraftContent: true,
        isQueueMode: true,
        showStopButton: true
      })
    ).toBe(false);
  });
});

describe("composer action admission", () => {
  it.each([
    ["images", "uploading"],
    ["images", "failed"],
    ["files", "uploading"],
    ["files", "failed"],
    ["largeTexts", "uploading"],
    ["largeTexts", "failed"]
  ] as const)(
    "allows the command to explain %s in state %s and preserves genuine locks",
    (kind, state) => {
      const onSubmitCurrentPrompt = vi.fn();
      const input = {
        draftContent: buildAgentComposerDraft({
          prompt: "keep this text",
          [kind]: [
            {
              id: "attachment-1",
              name: "attachment",
              mimeType: "image/png" as const,
              text: "pasted text",
              uploading: state === "uploading",
              uploadError: state === "failed" ? "failed" : undefined
            }
          ]
        }),
        onSubmitCurrentPrompt,
        onInterruptCurrentTurn: vi.fn(),
        onSubmitInteractivePrompt: vi.fn(),
        canQueueWhileBusy: false,
        showStopButton: false,
        draftOverridesStopButton: false,
        isSendingTurn: false,
        isSelectedProjectMissing: false,
        submitDisabled: false,
        allowEmptySubmit: false,
        selectedProjectPath: "",
        previousSelectedProjectPathRef: { current: "" },
        activePrompt: null,
        activePromptTip: null,
        fileDropOverlayHost: null,
        labels: { send: "Send" },
        workspaceId: "workspace:test"
      } as unknown as Parameters<typeof useComposerPresentation>[0];
      const hook = renderHook((props) => useComposerPresentation(props), {
        initialProps: input
      });
      const button = render(hook.result.current.composerActionButton);
      const click = () =>
        act(() => button.getByRole("button", { name: "Send" }).click());
      const update = (patch: Partial<typeof input>) => {
        hook.rerender({ ...input, ...patch });
        button.rerender(hook.result.current.composerActionButton);
      };

      click();
      expect(onSubmitCurrentPrompt).toHaveBeenCalledTimes(1);
      expect(onSubmitCurrentPrompt).toHaveBeenLastCalledWith();
      for (const patch of [
        { submitDisabled: true },
        { isSelectedProjectMissing: true },
        { isSendingTurn: true }
      ]) {
        update(patch);
        click();
        expect(onSubmitCurrentPrompt).toHaveBeenCalledTimes(1);
      }
      update({ isSendingTurn: true, canQueueWhileBusy: true });
      click();
      expect(onSubmitCurrentPrompt).toHaveBeenCalledTimes(2);
    }
  );
});
