import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  resetAgentHostApiForTests,
  setAgentHostApiForTests
} from "../../../agentActivityHost";
import type { AgentComposerDraftImage } from "../model/agentGuiNodeTypes";
import { useComposerSlashActions } from "./useComposerSlashActions";

function createImage(
  overrides: Partial<AgentComposerDraftImage> = {}
): AgentComposerDraftImage {
  return {
    data: "aGVsbG8=",
    id: "image-1",
    mimeType: "image/png",
    name: "shot.png",
    previewUrl: "data:image/png;base64,aGVsbG8=",
    uploading: false,
    ...overrides
  };
}

function createInput(overrides: Record<string, unknown> = {}) {
  const draftPromptRef = { current: "hello" };
  return {
    workspaceId: "workspace-1",
    provider: "claude-code",
    disabled: false,
    submitDisabled: false,
    canQueueWhileBusy: false,
    isSendingTurn: false,
    isSubmittingPrompt: false,
    showStopButton: false,
    promptImagesSupported: true,
    availableSkills: [],
    composerSettings: {
      sessionSettings: null,
      draftSettings: {
        model: null,
        reasoningEffort: null,
        speed: null,
        planMode: false
      },
      supportsModel: false,
      supportsReasoningEffort: false,
      supportsSpeed: false,
      supportsPlanMode: false,
      supportsBrowser: true,
      supportsComputerUse: false,
      isSettingsLoading: false,
      modelUnavailable: false,
      reasoningUnavailable: false,
      speedUnavailable: false,
      availableModels: [],
      availableReasoningEfforts: [],
      availableSpeeds: [],
      projectLocked: false,
      projectPathIsRemote: false,
      slashCommandPolicy: "enabled"
    },
    tuttiModeSupported: false,
    capabilityControlsReadOnly: false,
    onDraftContentChange: vi.fn(),
    onSettingsChange: vi.fn(),
    onSubmit: vi.fn(),
    onSubmitEmpty: vi.fn(),
    onSubmitGuidance: vi.fn(),
    onCapabilitySettingsRequest: vi.fn(),
    onTuttiModeActivate: vi.fn(),
    onSlashStatusOpen: vi.fn(),
    onSlashStatusClose: vi.fn(),
    onPromptImagesUnsupported: vi.fn(),
    onRequestGitBranches: vi.fn(),
    draftContent: [{ text: "hello", type: "text" as const }],
    selectedProjectPath: "/workspace",
    slashStatusAgentSessionId: null,
    isSlashStatusPanelOpen: false,
    slashCommandPolicy: "enabled",
    skillQueryMatch: null,
    promptBeforeSelection: "",
    resolvedSlashCommands: [],
    slashPaletteEntries: [],
    activeHighlight: 0,
    showSlashPalette: false,
    showCommandMenuPanel: false,
    isSelectedProjectMissing: false,
    editorHandleRef: { current: null },
    draftPromptRef,
    draftImagesRef: { current: [] },
    draftFilesRef: { current: [] },
    draftLargeTextsRef: { current: [] },
    setPaletteDraftPrompt: vi.fn(),
    setIsPaletteOpen: vi.fn(),
    setIsReviewPickerOpen: vi.fn(),
    setIsSlashStatusPanelOpen: vi.fn(),
    setHighlightedIndex: vi.fn(),
    ...overrides
  };
}

describe("useComposerSlashActions submit readiness", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    resetAgentHostApiForTests();
  });

  // Regression: a new reply in an existing session carrying an image whose
  // upload had not settled refused every submit with no message and no way out,
  // so the text and the image both silently failed to send.
  it("sends the draft while an image is still uploading", () => {
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftImagesRef: { current: [createImage({ uploading: true })] },
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const [content] = onSubmit.mock.calls[0]!;
    expect(content).toContainEqual({ text: "hello", type: "text" });
    // The unsettled image is dropped rather than blocking the text.
    expect(
      content.some((block: { type: string }) => block.type === "image")
    ).toBe(false);
  });

  it("explains why an uploading attachment was left out", () => {
    const toastInfo = vi.fn();
    const toastError = vi.fn();
    setAgentHostApiForTests({
      toast: { error: toastError, info: toastInfo }
    } as never);
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftImagesRef: { current: [createImage({ uploading: true })] }
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(toastInfo).toHaveBeenCalledWith(
      "Sent without the attachment that is still being prepared"
    );
  });

  it("sends a ready image together with the text", () => {
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftImagesRef: {
            current: [
              createImage({ path: "/tmp/shot.png", previewUrl: undefined })
            ]
          },
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const [content] = onSubmit.mock.calls[0]!;
    expect(content).toContainEqual({ text: "hello", type: "text" });
    expect(content).toContainEqual({
      mimeType: "image/png",
      name: "shot.png",
      path: "/tmp/shot.png",
      type: "image"
    });
  });

  it("reports a failed attachment instead of sending silently", () => {
    const toastInfo = vi.fn();
    setAgentHostApiForTests({
      toast: { error: vi.fn(), info: toastInfo }
    } as never);
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftImagesRef: {
            current: [createImage({ uploadError: "boom", uploading: false })]
          },
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(toastInfo).toHaveBeenCalledWith(
      "Sent without the attachment that failed to prepare"
    );
  });

  it("still refuses to submit while the composer is disabled", () => {
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftImagesRef: { current: [createImage({ uploading: true })] },
          submitDisabled: true,
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).not.toHaveBeenCalled();
  });

  // Regression: an attachment-only draft passed the "has content" check (an
  // image block counts regardless of upload state) but projected to empty
  // content, so the send was discarded downstream with no feedback at all.
  it("does not claim a send when the only content is an unsettled image", () => {
    const toastInfo = vi.fn();
    setAgentHostApiForTests({
      toast: { error: vi.fn(), info: toastInfo }
    } as never);
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftContent: [{ text: "", type: "text" as const }],
          draftPromptRef: { current: "" },
          draftImagesRef: { current: [createImage({ uploading: true })] },
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).not.toHaveBeenCalled();
    expect(toastInfo).toHaveBeenCalledWith(
      "Nothing was sent: the only attachment is not ready yet"
    );
  });

  it("does not claim a send when the only content is a failed image", () => {
    const toastInfo = vi.fn();
    setAgentHostApiForTests({
      toast: { error: vi.fn(), info: toastInfo }
    } as never);
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftContent: [{ text: "", type: "text" as const }],
          draftPromptRef: { current: "" },
          draftImagesRef: {
            current: [createImage({ uploadError: "boom", uploading: false })]
          },
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).not.toHaveBeenCalled();
    expect(toastInfo).toHaveBeenCalledWith(
      "Nothing was sent: the only attachment is not ready yet"
    );
  });

  // A ready image alone is real content and must still send.
  it("sends an attachment-only draft once the image is ready", () => {
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftContent: [{ text: "", type: "text" as const }],
          draftPromptRef: { current: "" },
          draftImagesRef: {
            current: [
              createImage({ path: "/tmp/shot.png", previewUrl: undefined })
            ]
          },
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const [content] = onSubmit.mock.calls[0]!;
    expect(content).toContainEqual({
      mimeType: "image/png",
      name: "shot.png",
      path: "/tmp/shot.png",
      type: "image"
    });
  });

  // Exactly one message must reach the user, never the degraded notice plus a
  // nothing-sent notice.
  it("sends only one notice when an unsettled image accompanies text", () => {
    const toastInfo = vi.fn();
    setAgentHostApiForTests({
      toast: { error: vi.fn(), info: toastInfo }
    } as never);
    const onSubmit = vi.fn();
    const rendered = renderHook(() =>
      useComposerSlashActions(
        createInput({
          draftImagesRef: { current: [createImage({ uploading: true })] },
          onSubmit
        }) as never
      )
    );

    act(() => rendered.result.current.submitCurrentPrompt());

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(toastInfo).toHaveBeenCalledTimes(1);
    expect(toastInfo).toHaveBeenCalledWith(
      "Sent without the attachment that is still being prepared"
    );
  });
});
