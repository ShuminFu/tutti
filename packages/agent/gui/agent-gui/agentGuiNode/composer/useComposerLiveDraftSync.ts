import {
  useEffect,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor.types";
import {
  syncComposerLiveDraftFromProp,
  type ComposerLiveDraftRefs
} from "../model/composerDraftLiveSync";
import type { ComposerDraftVersionTracker } from "../model/composerDraftVersion";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";

export function useComposerLiveDraftSync(input: {
  draftContent: AgentComposerDraft;
  draftPrompt: string;
  draftScopeKey: string;
  draftVersionTrackerRef: RefObject<ComposerDraftVersionTracker>;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  goalDraftObjective: string | null;
  refs: ComposerLiveDraftRefs;
  setPaletteDraftPrompt: Dispatch<SetStateAction<string>>;
  settlePendingInputHistory: () => void;
}): void {
  const {
    draftContent,
    draftPrompt,
    draftScopeKey,
    draftVersionTrackerRef,
    editorHandleRef,
    goalDraftObjective,
    refs,
    setPaletteDraftPrompt,
    settlePendingInputHistory
  } = input;
  const {
    draftByScopeKeyRef,
    draftFilesRef,
    draftImagesRef,
    draftLargeTextsRef,
    draftPromptRef
  } = refs;
  useEffect(() => {
    syncComposerLiveDraftFromProp({
      draftContent,
      draftPrompt,
      draftScopeKey,
      editorHandleRef,
      onApplied: (prompt) =>
        setPaletteDraftPrompt(goalDraftObjective ?? prompt),
      refs: {
        draftByScopeKeyRef,
        draftFilesRef,
        draftImagesRef,
        draftLargeTextsRef,
        draftPromptRef
      },
      tracker: draftVersionTrackerRef.current
    });
    settlePendingInputHistory();
  }, [
    draftByScopeKeyRef,
    draftContent,
    draftFilesRef,
    draftImagesRef,
    draftLargeTextsRef,
    draftPrompt,
    draftPromptRef,
    draftScopeKey,
    draftVersionTrackerRef,
    editorHandleRef,
    goalDraftObjective,
    setPaletteDraftPrompt,
    settlePendingInputHistory
  ]);
}
