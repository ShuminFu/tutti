import {
  useCallback,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import {
  selectEngineQueuedPrompt,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import {
  agentComposerDraftImages,
  agentPromptContentToComposerDraft,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import { stripLeadingPairKickoffFromContent } from "../../../shared/agentConversation/components/pairKickoffEnvelope";
import { QueuedPromptImageLoadOwner } from "../queuedPromptImageLoadOwner";
import { createAgentGUIConversationId } from "./agentGuiController.promptHelpers";

export interface UseAgentGUIQueueActionsInput {
  activeConversationIdRef: RefObject<string | null>;
  agentActivityRuntime: AgentGUIRuntime;
  sessionEngine: AgentSessionEngine;
  setDraftByScopeKey: Dispatch<
    SetStateAction<Record<string, AgentComposerDraft>>
  >;
  workspaceId: string;
}

/** Owns queued-prompt mutations without coupling them to session activation. */
export function useAgentGUIQueueActions({
  activeConversationIdRef,
  agentActivityRuntime,
  sessionEngine,
  setDraftByScopeKey,
  workspaceId
}: UseAgentGUIQueueActionsInput) {
  const removeQueuedPrompt = useCallback(
    (queuedPromptId: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedQueuedPromptId = queuedPromptId.trim();
      if (!agentSessionId || !normalizedQueuedPromptId) {
        return;
      }
      const queuedPrompt = selectEngineQueuedPrompt(
        sessionEngine.getSnapshot(),
        agentSessionId,
        normalizedQueuedPromptId
      );
      sessionEngine.dispatch(
        queuedPrompt?.clientSubmitId
          ? {
              agentSessionId,
              clientSubmitId: queuedPrompt.clientSubmitId,
              type: "submit/canceled"
            }
          : {
              agentSessionId,
              promptId: normalizedQueuedPromptId,
              type: "queue/removed"
            }
      );
    },
    [activeConversationIdRef, sessionEngine]
  );

  const editQueuedPrompt = useCallback(
    (queuedPromptId: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedQueuedPromptId = queuedPromptId.trim();
      if (!agentSessionId || !normalizedQueuedPromptId) {
        return;
      }
      const queuedPrompt = selectEngineQueuedPrompt(
        sessionEngine.getSnapshot(),
        agentSessionId,
        normalizedQueuedPromptId
      );
      if (!queuedPrompt) {
        return;
      }
      const draftScopeKey = resolveAgentComposerDraftScopeKey({
        agentSessionId
      });
      // 分栏结对模式：排队的第一句带着开工卡（拼在第一个文字块前面）。取回编辑时只回填
      // 用户原话，重新发出时由宿主按那时的配对状态重新决定拼不拼卡（票 05 评审 E）。
      const restoredDraft = agentPromptContentToComposerDraft(
        stripLeadingPairKickoffFromContent(queuedPrompt.content),
        `restore-${queuedPrompt.id}`
      );
      sessionEngine.dispatch(
        queuedPrompt.clientSubmitId
          ? {
              agentSessionId,
              clientSubmitId: queuedPrompt.clientSubmitId,
              type: "submit/canceled"
            }
          : {
              agentSessionId,
              promptId: normalizedQueuedPromptId,
              type: "queue/removed"
            }
      );
      setDraftByScopeKey((current) => ({
        ...current,
        [draftScopeKey]: restoredDraft
      }));
      for (const restoredImage of agentComposerDraftImages(restoredDraft)) {
        const attachmentId = restoredImage.attachmentId?.trim() ?? "";
        const path = restoredImage.path?.trim() ?? "";
        if (restoredImage.previewUrl || (!attachmentId && !path)) {
          continue;
        }
        const owner = new QueuedPromptImageLoadOwner(
          {
            agentSessionId,
            attachmentId,
            imageKey: restoredImage.id,
            mimeType: restoredImage.mimeType,
            name: restoredImage.name,
            path,
            remoteUrl: restoredImage.url?.trim() ?? "",
            runtime: agentActivityRuntime,
            workspaceId
          },
          (source) => {
            if (!source) {
              return;
            }
            setDraftByScopeKey((current) => {
              const currentDraft = current[draftScopeKey];
              if (!currentDraft) {
                return current;
              }
              let updated = false;
              const images = agentComposerDraftImages(currentDraft).map(
                (currentImage) => {
                  if (
                    currentImage.id !== restoredImage.id ||
                    currentImage.previewUrl ||
                    currentImage.attachmentId !== restoredImage.attachmentId ||
                    currentImage.path !== restoredImage.path ||
                    currentImage.url !== restoredImage.url
                  ) {
                    return currentImage;
                  }
                  updated = true;
                  return { ...currentImage, previewUrl: source };
                }
              );
              return updated
                ? {
                    ...current,
                    [draftScopeKey]: updateAgentComposerDraft(currentDraft, {
                      images
                    })
                  }
                : current;
            });
          }
        );
        owner.start();
      }
    },
    [
      activeConversationIdRef,
      agentActivityRuntime,
      sessionEngine,
      setDraftByScopeKey,
      workspaceId
    ]
  );

  const sendQueuedPromptNext = useCallback(
    (queuedPromptId: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedQueuedPromptId = queuedPromptId.trim();
      if (!agentSessionId || !normalizedQueuedPromptId) {
        return;
      }
      sessionEngine.dispatch({
        agentSessionId,
        awaitingTurnExpiresAtUnixMs: Date.now() + 30_000,
        cancelCommandId: createAgentGUIConversationId(),
        promptId: normalizedQueuedPromptId,
        timeoutMs: 30_000,
        type: "queue/sendNowRequested"
      });
    },
    [activeConversationIdRef, sessionEngine]
  );

  return {
    editQueuedPrompt,
    removeQueuedPrompt,
    sendQueuedPromptNext
  };
}
