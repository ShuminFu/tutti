import type { RefObject } from "react";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor.types";
import {
  agentComposerDraftFiles,
  agentComposerDraftImages,
  agentComposerDraftLargeTexts
} from "./agentComposerDraft";
import type {
  AgentComposerDraft,
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText
} from "./agentGuiNodeTypes";
import {
  shouldApplyComposerDraftProp,
  type ComposerDraftVersionTracker
} from "./composerDraftVersion";

export interface ComposerLiveDraftRefs {
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
  draftFilesRef: RefObject<AgentComposerDraftFile[]>;
  draftImagesRef: RefObject<AgentComposerDraftImage[]>;
  draftLargeTextsRef: RefObject<AgentComposerDraftLargeText[]>;
  draftPromptRef: RefObject<string>;
}

/**
 * Apply a rendered draft to the send refs only when it is a newer external
 * replacement. A lagging echo of an older local revision is acknowledged and
 * left unread, so Enter cannot send a prefix of the live editor.
 */
export function syncComposerLiveDraftFromProp(input: {
  draftContent: AgentComposerDraft;
  draftPrompt: string;
  draftScopeKey: string;
  editorHandleRef?: RefObject<AgentRichTextEditorHandle | null>;
  onApplied?: (prompt: string) => void;
  refs: ComposerLiveDraftRefs;
  tracker: ComposerDraftVersionTracker;
}): { applied: boolean; replaced: boolean } {
  const { draftContent, draftPrompt, draftScopeKey, refs, tracker } = input;
  if (
    !shouldApplyComposerDraftProp({
      prompt: draftPrompt,
      scopeKey: draftScopeKey,
      tracker
    })
  ) {
    return { applied: false, replaced: false };
  }
  const replaced =
    refs.draftPromptRef.current !== draftPrompt && Boolean(draftPrompt);
  refs.draftImagesRef.current = agentComposerDraftImages(draftContent);
  refs.draftFilesRef.current = agentComposerDraftFiles(draftContent);
  refs.draftLargeTextsRef.current = agentComposerDraftLargeTexts(draftContent);
  refs.draftPromptRef.current = draftPrompt;
  refs.draftByScopeKeyRef.current[draftScopeKey] = draftContent;
  input.onApplied?.(draftPrompt);
  if (replaced && input.editorHandleRef) {
    scheduleComposerExternalDraftFocus(input.editorHandleRef);
  }
  return { applied: true, replaced };
}

export function scheduleComposerExternalDraftFocus(
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>
): void {
  // presentation-work: keep caret at end after a true external draft replacement
  window.requestAnimationFrame(() => {
    // presentation-work: keep caret at end after a true external draft replacement
    window.requestAnimationFrame(() => editorHandleRef.current?.focusAtEnd());
  });
}
