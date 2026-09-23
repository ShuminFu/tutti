import { flushSync } from "react-dom";
import {
  startTransition,
  useCallback,
  useRef,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import type { WorkspaceFileReference } from "@tutti-os/workspace-file-reference/contracts";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import { useOptionalAgentGUIRuntime } from "../../../agentActivityRuntime";
import { translate } from "../../../i18n/index";
import type {
  AgentComposerDraft,
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText
} from "../model/agentGuiNodeTypes";
import {
  applyPastedTextStagingResult,
  agentComposerDraftFiles,
  agentComposerDraftImages,
  agentComposerDraftLargeTexts,
  agentComposerDraftPrompt,
  buildAgentComposerDraft,
  MAX_AGENT_COMPOSER_DRAFT_IMAGES,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import type {
  AgentRichTextEditorHandle,
  AgentRichTextPastedImage
} from "../agentRichText/AgentRichTextEditor";
import type { AgentContextMentionItem } from "../agentRichText/agentFileMentionExtension";
import {
  agentExternalPromptFileErrorI18nKey,
  createAgentExternalPromptFilePreparation,
  remainingAgentComposerPromptAssetSlots,
  type AgentExternalPromptFilePreparer
} from "../model/agentExternalPromptFiles";
import {
  agentComposerFileMentionReferences,
  updateAgentComposerFileMentions
} from "../agentRichText/agentMentionMarkdown";
import { updateDraftPromptAndReconcileFiles } from "../model/agentComposerDraftFileReconciliation";
import { type WorkspaceLinkAction } from "../../../actions/workspaceLinkActions";
import { dispatchComposerDraftMarkdownLinkClick } from "./resolveComposerFileMentionLinkAction";
import {
  AGENT_COMPOSER_PASTED_TEXT_FILE_PREFIX,
  agentComposerTextByteLength,
  buildGoalModePrompt,
  goalDraftObjectiveFromPrompt
} from "./composerDraftUtils";
import { reportAgentComposerDiagnostic } from "./agentComposerDiagnostics";
import { settleWithTimeout } from "./composerAssetUploadTimeout";
import { settlePastedComposerImage } from "./composerDraftImageUpload";
import {
  mergePastedComposerImages,
  revokeComposerImagePreviewUrl
} from "./composerPastedImagePreview";
import type { AgentGUIComposerContentType } from "../engagement/agentGUIEngagement.types";
import type { ComposerDraftVersionTracker } from "../model/composerDraftVersion";
import { recordComposerLocalDraftEdit } from "../model/composerDraftVersion";

export interface WorkspaceReferencePickResult {
  files: readonly WorkspaceFileReference[];
  mentionItems: readonly AgentContextMentionItem[];
}

function useStableEventCallback<Args extends unknown[], Result>(
  callback: (...args: Args) => Result
): (...args: Args) => Result {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;
  return useCallback((...args: Args) => callbackRef.current(...args), []);
}
interface UseComposerDraftAttachmentsInput {
  workspaceId: string;
  workspacePath?: string | null;
  draftContent: AgentComposerDraft;
  draftScopeKey: string;
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
  goalDraftObjective: string | null;
  isGoalModeActive: boolean;
  promptImagesSupported: boolean;
  promptFilesSupported: boolean;
  promptAssetLimit?: number | null;
  pastedTextStagingSupported: boolean;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  draftPromptRef: RefObject<string>;
  draftImagesRef: RefObject<AgentComposerDraftImage[]>;
  draftFilesRef: RefObject<AgentComposerDraftFile[]>;
  draftLargeTextsRef: RefObject<AgentComposerDraftLargeText[]>;
  setPaletteDraftPrompt: Dispatch<SetStateAction<string>>;
  setIsPaletteOpen: Dispatch<SetStateAction<boolean>>;
  clearActiveFileMentionTrigger: () => void;
  onDraftContentChange: (
    draft: AgentComposerDraft,
    sourceScopeKey?: string,
    meta?: { revision: number }
  ) => void;
  draftVersionTrackerRef: RefObject<ComposerDraftVersionTracker>;
  onPromptImagesUnsupported?: () => void;
  onContentEntered?: (contentType: AgentGUIComposerContentType) => void;
  onRequestWorkspaceReferences?:
    | ((
        entity?: AgentContextMentionItem | null
      ) => Promise<WorkspaceReferencePickResult>)
    | null;
  prepareExternalPromptFiles?: AgentExternalPromptFilePreparer | null;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
}

export function useComposerDraftAttachments({
  workspaceId,
  workspacePath,
  draftScopeKey,
  draftByScopeKeyRef,
  goalDraftObjective,
  isGoalModeActive,
  promptImagesSupported,
  promptFilesSupported,
  promptAssetLimit,
  pastedTextStagingSupported,
  editorHandleRef,
  draftPromptRef,
  draftImagesRef,
  draftFilesRef,
  draftLargeTextsRef,
  setPaletteDraftPrompt,
  setIsPaletteOpen,
  clearActiveFileMentionTrigger,
  onDraftContentChange,
  draftVersionTrackerRef,
  onPromptImagesUnsupported,
  onContentEntered,
  onRequestWorkspaceReferences,
  prepareExternalPromptFiles,
  onLinkAction
}: UseComposerDraftAttachmentsInput) {
  const agentHostApi = useOptionalAgentHostApi();
  const agentActivityRuntime = useOptionalAgentGUIRuntime();
  const removedDraftImageIdsRef = useRef(new Set<string>());
  const activeDraftScopeKeyRef = useRef(draftScopeKey);
  activeDraftScopeKeyRef.current = draftScopeKey;
  const reportContentEntered = useStableEventCallback(
    (contentType: AgentGUIComposerContentType): void => {
      onContentEntered?.(contentType);
    }
  );
  const showErrorToast = useStableEventCallback((message: string): void => {
    agentHostApi?.toast?.error(message);
  });
  const showInfoToast = useStableEventCallback((message: string): void => {
    (agentHostApi?.toast?.info ?? agentHostApi?.toast?.error)?.(message);
  });
  const publishScopedDraft = useStableEventCallback(
    (
      sourceScopeKey: string,
      nextDraft: AgentComposerDraft,
      revision?: number
    ): void => {
      draftByScopeKeyRef.current[sourceScopeKey] = nextDraft;
      const meta = revision != null ? { revision } : undefined;
      if (sourceScopeKey === draftScopeKey) {
        draftPromptRef.current = agentComposerDraftPrompt(nextDraft);
        draftImagesRef.current = agentComposerDraftImages(nextDraft);
        draftFilesRef.current = agentComposerDraftFiles(nextDraft);
        draftLargeTextsRef.current = agentComposerDraftLargeTexts(nextDraft);
        onDraftContentChange(nextDraft, undefined, meta);
      } else {
        onDraftContentChange(nextDraft, sourceScopeKey, meta);
      }
    }
  );
  const updateScopedDraft = useStableEventCallback(
    (
      sourceScopeKey: string,
      update: (current: AgentComposerDraft) => AgentComposerDraft,
      revision?: number
    ): AgentComposerDraft | null => {
      const current = draftByScopeKeyRef.current[sourceScopeKey];
      if (!current) {
        // The scope has no registered draft any more (for example the session
        // was closed while an upload was in flight). Dropping the write is
        // correct, but it must not be invisible: a silently discarded result is
        // how an attachment once stayed `uploading` forever.
        reportAgentComposerDiagnostic(agentActivityRuntime, {
          details: { sourceScopeKey },
          event: "agent.gui.composer.draft_update.dropped_missing_scope",
          level: "debug",
          source: "agent-gui",
          workspaceId
        });
        return null;
      }
      const next = update(current);
      const effectiveRevision =
        revision ?? draftVersionTrackerRef.current.nextRevision;
      if (
        effectiveRevision <= draftVersionTrackerRef.current.consumedRevision
      ) {
        return next;
      }
      publishScopedDraft(sourceScopeKey, next, effectiveRevision);
      return next;
    }
  );
  const publishLocalScopedDraft = useStableEventCallback(
    (sourceScopeKey: string, nextDraft: AgentComposerDraft): void => {
      const revision = recordComposerLocalDraftEdit(
        draftVersionTrackerRef.current,
        agentComposerDraftPrompt(nextDraft)
      );
      publishScopedDraft(sourceScopeKey, nextDraft, revision);
    }
  );
  const openReferencesForEntityRef = useRef<
    ((entity: AgentContextMentionItem) => void) | null
  >(null);
  const handleDraftChange = useStableEventCallback(
    (nextDraft: string): void => {
      const tracker = draftVersionTrackerRef.current;
      if (isGoalModeActive) {
        const nextGoalPrompt = buildGoalModePrompt(nextDraft);
        draftPromptRef.current = nextGoalPrompt;
        const revision = recordComposerLocalDraftEdit(tracker, nextGoalPrompt);
        startTransition(() => {
          if (revision <= tracker.consumedRevision) {
            return;
          }
          setPaletteDraftPrompt(nextDraft);
          setIsPaletteOpen(true);
          updateScopedDraft(
            draftScopeKey,
            (currentDraft) =>
              updateDraftPromptAndReconcileFiles(currentDraft, nextGoalPrompt),
            revision
          );
        });
        return;
      }
      const nextGoalObjective = goalDraftObjectiveFromPrompt(nextDraft);
      if (nextGoalObjective !== null) {
        const nextGoalPrompt = buildGoalModePrompt(nextGoalObjective);
        draftPromptRef.current = nextGoalPrompt;
        const revision = recordComposerLocalDraftEdit(tracker, nextGoalPrompt);
        startTransition(() => {
          if (revision <= tracker.consumedRevision) {
            return;
          }
          setPaletteDraftPrompt(nextGoalObjective);
          setIsPaletteOpen(true);
          updateScopedDraft(
            draftScopeKey,
            (currentDraft) =>
              updateDraftPromptAndReconcileFiles(currentDraft, nextGoalPrompt),
            revision
          );
        });
        return;
      }
      draftPromptRef.current = nextDraft;
      const revision = recordComposerLocalDraftEdit(tracker, nextDraft);
      startTransition(() => {
        if (revision <= tracker.consumedRevision) {
          return;
        }
        setPaletteDraftPrompt(nextDraft);
        setIsPaletteOpen(true);
        updateScopedDraft(
          draftScopeKey,
          (currentDraft) =>
            updateDraftPromptAndReconcileFiles(currentDraft, nextDraft),
          revision
        );
      });
    }
  );

  const clearGoalModeBadge = useCallback((): void => {
    if (!isGoalModeActive) {
      return;
    }
    const nextPrompt = goalDraftObjective ?? "";
    draftPromptRef.current = nextPrompt;
    setPaletteDraftPrompt(nextPrompt);
    const revision = recordComposerLocalDraftEdit(
      draftVersionTrackerRef.current,
      nextPrompt
    );
    updateScopedDraft(
      draftScopeKey,
      (currentDraft) =>
        updateDraftPromptAndReconcileFiles(currentDraft, nextPrompt),
      revision
    );
  }, [draftScopeKey, goalDraftObjective, isGoalModeActive, updateScopedDraft]);

  const addDraftImages = useCallback(
    (images: AgentRichTextPastedImage[]): void => {
      if (images.length === 0) {
        return;
      }
      if (!promptImagesSupported) {
        onPromptImagesUnsupported?.();
        return;
      }
      const uploadPromptContent =
        agentActivityRuntime?.uploadPromptContent &&
        (agentActivityRuntime.promptContentUploadSupport?.image ?? true)
          ? agentActivityRuntime.uploadPromptContent
          : undefined;
      const merged = mergePastedComposerImages({
        current: draftImagesRef.current,
        incoming: images,
        remainingSlots: Math.min(
          Math.max(
            0,
            MAX_AGENT_COMPOSER_DRAFT_IMAGES - draftImagesRef.current.length
          ),
          remainingAgentComposerPromptAssetSlots({
            images: draftImagesRef.current.length,
            files: draftFilesRef.current.length,
            largeTexts: draftLargeTextsRef.current.length,
            limit: promptAssetLimit
          })
        ),
        removedIds: removedDraftImageIdsRef.current,
        uploadEnabled: Boolean(uploadPromptContent)
      });
      if (!merged.changed) {
        return;
      }
      for (const previewUrl of merged.revokedPreviewUrls) {
        revokeComposerImagePreviewUrl(previewUrl);
      }
      draftImagesRef.current = merged.images;
      if (merged.addedCount > 0) {
        reportContentEntered("image");
        reportAgentComposerDiagnostic(agentActivityRuntime, {
          details: {
            imageCount: merged.addedCount,
            promptImagesSupported,
            runtimeAvailable: Boolean(agentActivityRuntime),
            uploadFunctionAvailable: Boolean(uploadPromptContent),
            uploadSupportDeclared:
              agentActivityRuntime?.promptContentUploadSupport?.image ?? null
          },
          event: "agent.gui.composer.image_upload.requested",
          level: "info",
          source: "agent-gui",
          workspaceId
        });
      }
      publishLocalScopedDraft(
        draftScopeKey,
        buildAgentComposerDraft({
          prompt: draftPromptRef.current,
          images: merged.images,
          files: draftFilesRef.current,
          largeTexts: draftLargeTextsRef.current
        })
      );
      for (const job of merged.jobs) {
        settlePastedComposerImage({
          job,
          runtime: agentActivityRuntime,
          updateScopedDraft: (update) =>
            updateScopedDraft(draftScopeKey, update),
          uploadPromptContent,
          workspaceId
        });
      }
    },
    [
      agentActivityRuntime,
      draftScopeKey,
      onPromptImagesUnsupported,
      publishLocalScopedDraft,
      promptImagesSupported,
      promptAssetLimit,
      reportContentEntered,
      updateScopedDraft,
      workspaceId
    ]
  );

  const removeDraftImage = useCallback(
    (id: string): void => {
      const removed = draftImagesRef.current.find((image) => image.id === id);
      removedDraftImageIdsRef.current.add(id);
      revokeComposerImagePreviewUrl(removed?.previewUrl);
      const nextDraftImages = draftImagesRef.current.filter(
        (image) => image.id !== id
      );
      draftImagesRef.current = nextDraftImages;
      publishLocalScopedDraft(
        draftScopeKey,
        buildAgentComposerDraft({
          prompt: draftPromptRef.current,
          images: nextDraftImages,
          files: draftFilesRef.current,
          largeTexts: draftLargeTextsRef.current
        })
      );
    },
    [draftScopeKey, publishLocalScopedDraft]
  );

  const addDraftFiles = useCallback(
    (files: readonly File[]): void => {
      if (
        files.length === 0 ||
        !promptFilesSupported ||
        !prepareExternalPromptFiles ||
        !editorHandleRef.current
      ) {
        return;
      }
      const remainingSlots = remainingAgentComposerPromptAssetSlots({
        images: draftImagesRef.current.length,
        files: draftFilesRef.current.length,
        largeTexts: draftLargeTextsRef.current.length,
        limit: promptAssetLimit
      });
      if (remainingSlots === 0) {
        return;
      }
      const preparation = createAgentExternalPromptFilePreparation(
        files.slice(0, remainingSlots)
      );
      reportAgentComposerDiagnostic(agentActivityRuntime, {
        details: {
          acceptedFileCount: preparation.pendingFiles.length,
          existingFileCount: draftFilesRef.current.length,
          requestedFileCount: files.length
        },
        event: "agent.gui.composer.file_preparation.requested",
        level: "info",
        source: "agent-gui",
        workspaceId
      });
      const nextDraftFiles = [
        ...draftFilesRef.current,
        ...preparation.pendingFiles
      ];
      draftFilesRef.current = nextDraftFiles;
      publishLocalScopedDraft(
        draftScopeKey,
        buildAgentComposerDraft({
          prompt: draftPromptRef.current,
          images: draftImagesRef.current,
          files: nextDraftFiles,
          largeTexts: draftLargeTextsRef.current
        })
      );
      editorHandleRef.current.insertComposerFiles(
        preparation.pendingFiles.map((file) => ({
          id: file.id,
          name: file.name,
          status: "uploading"
        }))
      );
      void preparation.complete(prepareExternalPromptFiles).then((settled) => {
        const settledById = new Map(settled.map((file) => [file.id, file]));
        const sourceScopeKey = draftScopeKey;
        const currentDraft = draftByScopeKeyRef.current[sourceScopeKey];
        const referencedIds = new Set(
          currentDraft
            ? agentComposerFileMentionReferences(
                agentComposerDraftPrompt(currentDraft)
              ).map((reference) => reference.id)
            : []
        );
        const visibleSettled = settled.filter((file) =>
          referencedIds.has(file.id)
        );
        const editorUpdated =
          activeDraftScopeKeyRef.current === sourceScopeKey &&
          editorHandleRef.current?.updateComposerFiles(
            visibleSettled.map((file) => ({
              errorCode: file.uploadErrorCode,
              id: file.id,
              name: file.name,
              status: file.uploadError ? "error" : "ready"
            }))
          );
        const mentionUpdatesById = new Map(
          visibleSettled.map((file) => [
            file.id,
            {
              errorCode: file.uploadErrorCode,
              status: file.uploadError ? ("error" as const) : ("ready" as const)
            }
          ])
        );
        const updatedDraft = updateScopedDraft(sourceScopeKey, (latestDraft) =>
          updateAgentComposerDraft(latestDraft, {
            prompt: editorUpdated
              ? agentComposerDraftPrompt(latestDraft)
              : updateAgentComposerFileMentions(
                  agentComposerDraftPrompt(latestDraft),
                  mentionUpdatesById
                ),
            files: agentComposerDraftFiles(latestDraft).map(
              (file) => settledById.get(file.id) ?? file
            )
          })
        );
        const errorCount = settled.filter((file) => file.uploadError).length;
        if (errorCount > 0) {
          const failedFiles = settled.filter((file) => file.uploadError);
          const firstErrorCode = failedFiles[0]?.uploadErrorCode;
          const sharedErrorCode = failedFiles.every(
            (file) => file.uploadErrorCode === firstErrorCode
          )
            ? firstErrorCode
            : undefined;
          const errorMessage = translate(
            agentExternalPromptFileErrorI18nKey(sharedErrorCode)
          );
          showErrorToast(errorMessage);
        }
        reportAgentComposerDiagnostic(agentActivityRuntime, {
          details: {
            draftUpdateApplied: Boolean(updatedDraft),
            errorCount,
            settledFileCount: settled.length,
            visibleFileCount: updatedDraft
              ? agentComposerDraftFiles(updatedDraft).length
              : null
          },
          event: "agent.gui.composer.file_preparation.settled",
          level: errorCount > 0 || !updatedDraft ? "warn" : "info",
          source: "agent-gui",
          workspaceId
        });
      });
    },
    [
      agentActivityRuntime,
      draftScopeKey,
      prepareExternalPromptFiles,
      promptAssetLimit,
      promptFilesSupported,
      publishLocalScopedDraft,
      showErrorToast,
      updateScopedDraft,
      workspaceId
    ]
  );

  const removeDraftLargeText = useCallback(
    (id: string): void => {
      const nextDraftLargeTexts = draftLargeTextsRef.current.filter(
        (item) => item.id !== id
      );
      draftLargeTextsRef.current = nextDraftLargeTexts;
      publishLocalScopedDraft(
        draftScopeKey,
        buildAgentComposerDraft({
          prompt: draftPromptRef.current,
          images: draftImagesRef.current,
          files: draftFilesRef.current,
          largeTexts: nextDraftLargeTexts
        })
      );
    },
    [draftScopeKey, publishLocalScopedDraft]
  );

  // "Show in text field": dissolve a pasted-text chip back into the composer as
  // inline prompt text and drop the attachment. Only possible while the full
  // body is still in memory (a fresh paste); a chip restored from a queued
  // message carries only the landed path, so expansion is unavailable there.
  const expandDraftLargeTextToPrompt = useCallback(
    (id: string): void => {
      const item = draftLargeTextsRef.current.find((entry) => entry.id === id);
      if (!item || !item.text.trim()) {
        return;
      }
      const currentPrompt = draftPromptRef.current;
      const nextPrompt = currentPrompt.trim()
        ? `${currentPrompt}\n${item.text}`
        : item.text;
      const nextDraftLargeTexts = draftLargeTextsRef.current.filter(
        (entry) => entry.id !== id
      );
      draftPromptRef.current = nextPrompt;
      draftLargeTextsRef.current = nextDraftLargeTexts;
      setPaletteDraftPrompt(nextPrompt);
      publishLocalScopedDraft(
        draftScopeKey,
        buildAgentComposerDraft({
          prompt: nextPrompt,
          images: draftImagesRef.current,
          files: draftFilesRef.current,
          largeTexts: nextDraftLargeTexts
        })
      );
      window.requestAnimationFrame(() => {
        editorHandleRef.current?.focusAtEnd();
      });
    },
    [draftScopeKey, publishLocalScopedDraft]
  );

  const handlePastedLargeText = useCallback(
    (text: string): void => {
      const normalizedText = text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
      if (!normalizedText.trim()) {
        return;
      }
      if (
        remainingAgentComposerPromptAssetSlots({
          images: draftImagesRef.current.length,
          files: draftFilesRef.current.length,
          largeTexts: draftLargeTextsRef.current.length,
          limit: promptAssetLimit
        }) === 0
      ) {
        return;
      }
      reportContentEntered("large_text");
      const stagePastedText = pastedTextStagingSupported
        ? agentActivityRuntime?.stagePastedText
        : undefined;
      const id = crypto.randomUUID();
      const name = `${AGENT_COMPOSER_PASTED_TEXT_FILE_PREFIX}.txt`;
      const sizeBytes = agentComposerTextByteLength(normalizedText);
      const nextDraftLargeTexts = [
        ...draftLargeTextsRef.current,
        {
          id,
          name,
          text: normalizedText,
          sizeBytes,
          uploading: Boolean(stagePastedText),
          ...(!stagePastedText
            ? {
                uploadError:
                  "Pasted text staging is not supported by this agent runtime."
              }
            : {})
        }
      ];
      draftLargeTextsRef.current = nextDraftLargeTexts;
      publishLocalScopedDraft(
        draftScopeKey,
        buildAgentComposerDraft({
          prompt: draftPromptRef.current,
          images: draftImagesRef.current,
          files: draftFilesRef.current,
          largeTexts: nextDraftLargeTexts
        })
      );
      if (!stagePastedText) {
        return;
      }
      void settleWithTimeout(
        stagePastedText({
          workspaceId,
          text: normalizedText,
          name
        })
      )
        .then((result) => {
          updateScopedDraft(draftScopeKey, (currentDraft) =>
            updateAgentComposerDraft(currentDraft, {
              largeTexts: agentComposerDraftLargeTexts(currentDraft).map(
                (item) =>
                  item.id === id
                    ? applyPastedTextStagingResult(item, result)
                    : item
              )
            })
          );
        })
        .catch((error: unknown) => {
          const message =
            error instanceof Error ? error.message : String(error);
          updateScopedDraft(draftScopeKey, (currentDraft) =>
            updateAgentComposerDraft(currentDraft, {
              largeTexts: agentComposerDraftLargeTexts(currentDraft).map(
                (item) =>
                  item.id === id
                    ? { ...item, uploading: false, uploadError: message }
                    : item
              )
            })
          );
        });
    },
    [
      agentActivityRuntime,
      draftScopeKey,
      pastedTextStagingSupported,
      promptAssetLimit,
      publishLocalScopedDraft,
      reportContentEntered,
      updateScopedDraft,
      workspaceId
    ]
  );

  const applyReferencePickResult = useCallback(
    async (result: WorkspaceReferencePickResult) => {
      if (result.files.length > 0) {
        editorHandleRef.current?.insertWorkspaceReferences(result.files);
      }
      if (result.mentionItems.length > 0) {
        editorHandleRef.current?.insertMentionItems(result.mentionItems);
      }
    },
    []
  );

  const handleWorkspaceReferencePicker = useCallback(async () => {
    if (!onRequestWorkspaceReferences) {
      return;
    }
    await applyReferencePickResult(await onRequestWorkspaceReferences());
  }, [applyReferencePickResult, onRequestWorkspaceReferences]);

  // @ 面板里点任务/应用行的「查看产物」入口:保留面板,打开引用 picker 并定位到该实体;
  // 选中的文件仍按常规插入,但不会把该任务/应用本身作为 mention 插入。
  const handleOpenReferencesForEntity = useCallback(
    (entity: AgentContextMentionItem): void => {
      if (!onRequestWorkspaceReferences) {
        return;
      }
      void onRequestWorkspaceReferences(entity).then((result) => {
        if (result.files.length > 0 || result.mentionItems.length > 0) {
          flushSync(clearActiveFileMentionTrigger);
        }
        return applyReferencePickResult(result);
      });
    },
    [
      clearActiveFileMentionTrigger,
      applyReferencePickResult,
      onRequestWorkspaceReferences
    ]
  );
  // 让 handleLinkClick(定义在前)能转发到此处:点击 workspace-reference chip 即定位打开 picker。
  openReferencesForEntityRef.current = handleOpenReferencesForEntity;

  const handleLinkClick = useCallback(
    (href: string): void => {
      dispatchComposerDraftMarkdownLinkClick({
        href,
        activeFiles: draftFilesRef.current,
        draftsByScope: draftByScopeKeyRef.current,
        workspaceRoot: workspacePath,
        onWorkspaceReference: (item) => {
          if (item.kind === "workspace-reference") {
            openReferencesForEntityRef.current?.(item);
          }
        },
        onLinkAction,
        showError: showErrorToast,
        showInfo: showInfoToast
      });
    },
    [
      draftByScopeKeyRef,
      draftFilesRef,
      onLinkAction,
      showErrorToast,
      showInfoToast,
      workspacePath
    ]
  );

  return {
    addDraftImages,
    addDraftFiles,
    clearGoalModeBadge,
    expandDraftLargeTextToPrompt,
    handleDraftChange,
    handleLinkClick,
    handleOpenReferencesForEntity,
    handlePastedLargeText,
    handleWorkspaceReferencePicker,
    removeDraftImage,
    removeDraftLargeText
  };
}
