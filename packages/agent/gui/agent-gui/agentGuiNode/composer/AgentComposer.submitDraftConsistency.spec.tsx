import { act, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentGUIQuickComposer } from "../../../AgentGUIQuickComposer";
import { buildAgentComposerDraft } from "../model/agentComposerDraft";
import {
  consumeComposerSubmittedRevision,
  createComposerDraftVersionTracker,
  recordComposerLocalDraftEdit,
  shouldApplyComposerDraftProp
} from "../model/composerDraftVersion";
import { useComposerDraftAttachments } from "./useComposerDraftAttachments";
import { useComposerSlashActions } from "./useComposerSlashActions";
import { renderHook } from "@testing-library/react";

const agentTargets = [
  {
    agentTargetId: "agent:codex",
    iconUrl: "/codex.png",
    label: "Codex",
    provider: "codex"
  }
] as const;

/**
 * React 19's startTransition callback is synchronous in tests. The bug is the
 * later commit of an older draft prop, not a delayed callback. This harness
 * applies that older prop explicitly instead of mocking startTransition.
 * Editor-document stale-echo coverage lives in AgentRichTextEditor.spec.tsx.
 */
describe("composer submit draft consistency", () => {
  it("sends the live AB snapshot after an old A echo, then clears that version", () => {
    const tracker = createComposerDraftVersionTracker("session:session-1");
    const draftPromptRef = { current: "" };
    const draftImagesRef = { current: [] };
    const draftFilesRef = { current: [] };
    const draftLargeTextsRef = { current: [] };
    const onSubmit = vi.fn();
    const draftByScopeKeyRef = {
      current: {
        "session:session-1": buildAgentComposerDraft({ prompt: "" })
      }
    };

    const attachments = renderHook(() =>
      useComposerDraftAttachments({
        workspaceId: "workspace-1",
        draftContent: buildAgentComposerDraft({ prompt: "" }),
        draftScopeKey: "session:session-1",
        draftByScopeKeyRef,
        goalDraftObjective: null,
        isGoalModeActive: false,
        promptImagesSupported: true,
        promptFilesSupported: false,
        pastedTextStagingSupported: false,
        editorHandleRef: { current: null },
        draftPromptRef,
        draftImagesRef,
        draftFilesRef,
        draftLargeTextsRef,
        draftVersionTrackerRef: { current: tracker },
        setPaletteDraftPrompt: vi.fn(),
        setIsPaletteOpen: vi.fn(),
        clearActiveFileMentionTrigger: vi.fn(),
        onDraftContentChange: vi.fn()
      })
    );

    act(() => attachments.result.current.handleDraftChange("A"));
    act(() => attachments.result.current.handleDraftChange("AB"));
    expect(draftPromptRef.current).toBe("AB");

    const staleEchoApplied = shouldApplyComposerDraftProp({
      prompt: "A",
      revision: 1,
      scopeKey: "session:session-1",
      tracker
    });
    if (staleEchoApplied) {
      draftPromptRef.current = "A";
    }
    expect(staleEchoApplied).toBe(false);
    expect(draftPromptRef.current).toBe("AB");

    const slash = renderHook(() =>
      useComposerSlashActions({
        workspaceId: "workspace-1",
        provider: "codex",
        disabled: false,
        submitDisabled: false,
        canQueueWhileBusy: true,
        isSendingTurn: true,
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
        onDraftContentChange: vi.fn(),
        onSettingsChange: vi.fn(),
        onSubmit,
        onSubmitEmpty: vi.fn(),
        onSubmitGuidance: vi.fn(),
        draftContent: buildAgentComposerDraft({ prompt: "A" }),
        selectedProjectPath: "/workspace",
        slashStatusAgentSessionId: "session-1",
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
        draftImagesRef,
        draftFilesRef,
        draftLargeTextsRef,
        draftScopeKey: "session:session-1",
        draftVersionTrackerRef: { current: tracker },
        setPaletteDraftPrompt: vi.fn(),
        setIsPaletteOpen: vi.fn(),
        setIsReviewPickerOpen: vi.fn(),
        setIsSlashStatusPanelOpen: vi.fn(),
        setHighlightedIndex: vi.fn()
      } as never)
    );

    act(() => slash.result.current.submitCurrentPrompt());

    expect(onSubmit).toHaveBeenCalledWith(
      [{ text: "AB", type: "text" }],
      undefined,
      expect.objectContaining({
        draftRevision: 2,
        sourceScopeKey: "session:session-1",
        submittedDraft: [{ text: "AB", type: "text" }]
      })
    );

    expect(
      shouldApplyComposerDraftProp({
        prompt: "A",
        revision: 1,
        scopeKey: "session:session-1",
        tracker
      })
    ).toBe(false);
    expect(
      shouldApplyComposerDraftProp({
        prompt: "AB",
        revision: 2,
        scopeKey: "session:session-1",
        tracker
      })
    ).toBe(false);
  });

  it("does not resurrect a sent draft or clear a retyped copy", () => {
    const tracker = createComposerDraftVersionTracker("session:session-1");
    recordComposerLocalDraftEdit(tracker, "hello");
    consumeComposerSubmittedRevision(tracker, 1);
    recordComposerLocalDraftEdit(tracker, "hello");

    expect(
      shouldApplyComposerDraftProp({
        prompt: "",
        scopeKey: "session:session-1",
        tracker
      })
    ).toBe(false);
    expect(
      shouldApplyComposerDraftProp({
        prompt: "hello",
        revision: 1,
        scopeKey: "session:session-1",
        tracker
      })
    ).toBe(false);
  });

  it("still submits from a busy session through the send button", () => {
    const onSubmit = vi.fn();
    const { container } = render(
      <AgentGUIQuickComposer
        agentTargets={agentTargets}
        capabilitiesByAgentTargetId={{
          "agent:codex": { imageInput: true, workspaceReferences: true }
        }}
        content={[{ text: "queued-complete", type: "text" }]}
        selectedAgentTargetId="agent:codex"
        workspaceId="workspace:test"
        onAgentTargetChange={vi.fn()}
        onContentChange={vi.fn()}
        onSubmit={onSubmit}
      />
    );

    container
      .querySelector<HTMLButtonElement>(
        '[data-testid="agent-gui-composer-send"]'
      )
      ?.click();

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        content: [{ text: "queued-complete", type: "text" }]
      })
    );
  });
});
