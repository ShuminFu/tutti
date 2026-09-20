import { useCallback, useEffect, type RefObject } from "react";
import type {
  AgentRichTextEditorHandle,
  AgentRichTextPastedImage
} from "../agentRichText/AgentRichTextEditor";
import { useComposerFileDrop } from "./useComposerFileDrop";

interface Input {
  composerControlsHardDisabled: boolean;
  inputDisabled: boolean;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  composerRef: RefObject<HTMLFormElement | null>;
  wasActiveRef: RefObject<boolean>;
  lastComposerFocusRequestRef: RefObject<number | null>;
  isActive: boolean;
  composerFocusRequestSequence: number | null;
  promptFilesSupported: boolean;
  promptImagesSupported: boolean;
  addDraftImages: (images: AgentRichTextPastedImage[]) => void;
  addDraftFiles: (files: readonly File[]) => void;
  onPromptImagesUnsupported?: () => void;
}

export function useComposerFocusAndDrop(input: Input) {
  const {
    composerControlsHardDisabled,
    inputDisabled,
    editorHandleRef,
    composerRef,
    wasActiveRef,
    lastComposerFocusRequestRef,
    isActive,
    composerFocusRequestSequence,
    promptFilesSupported,
    promptImagesSupported,
    addDraftImages,
    addDraftFiles,
    onPromptImagesUnsupported
  } = input;
  const handleMentionPaletteButton = useCallback((): void => {
    if (composerControlsHardDisabled || inputDisabled) {
      return;
    }
    editorHandleRef.current?.openMentionPalette();
  }, [composerControlsHardDisabled, inputDisabled]);
  const scheduleComposerFocus = useCallback(
    (automatic = false) => {
      if (inputDisabled) {
        return;
      }
      // presentation-work: two activation frames only; no recurring scheduler.
      window.requestAnimationFrame(() => {
        // presentation-work: finish the bounded activation focus after layout.
        window.requestAnimationFrame(() => {
          // A pending prompt owns the dock's visible area on activation. Check at
          // execution time too: it may have arrived during the two-frame delay.
          if (
            automatic &&
            composerRef.current
              ?.closest(".agent-gui-node__bottom-dock")
              ?.querySelector("[data-agent-composer-attention]")
          ) {
            return;
          }
          editorHandleRef.current?.focusAtEnd();
        });
      });
    },
    [composerRef, editorHandleRef, inputDisabled]
  );
  const handlePastedImages = useCallback(
    (images: AgentRichTextPastedImage[]): void => {
      addDraftImages(images);
      scheduleComposerFocus();
    },
    [addDraftImages, scheduleComposerFocus]
  );
  const { fileDropOverlayActive, fileDropOverlayHost } = useComposerFileDrop({
    composerRef,
    editorHandleRef,
    inputDisabled,
    promptFilesSupported,
    promptImagesSupported,
    addDraftImages,
    addDraftFiles,
    scheduleComposerFocus,
    onPromptImagesUnsupported
  });
  useEffect(() => {
    if (!isActive) {
      wasActiveRef.current = false;
      return;
    }
    if (!wasActiveRef.current) {
      scheduleComposerFocus(true);
    }
    wasActiveRef.current = true;
  }, [isActive, scheduleComposerFocus]);
  useEffect(() => {
    if (
      composerFocusRequestSequence === null ||
      composerFocusRequestSequence === lastComposerFocusRequestRef.current
    ) {
      return;
    }
    lastComposerFocusRequestRef.current = composerFocusRequestSequence;
    scheduleComposerFocus();
  }, [composerFocusRequestSequence, scheduleComposerFocus]);

  return {
    fileDropOverlayActive,
    fileDropOverlayHost,
    handleMentionPaletteButton,
    handlePastedImages
  };
}
