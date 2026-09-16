import {
  parseAgentActivityGoalControlText,
  type AgentActivityGoalControlAction,
  type AgentActivityInteraction,
  type AgentActivityTurn,
  type AgentSessionEngine,
  type SessionGoalControlSettlement
} from "@tutti-os/agent-activity-core";
import type { Dispatch, RefObject, SetStateAction } from "react";
import { useCallback, useEffect, useRef } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { translate } from "../../../i18n/index";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import type { AgentInteractionResponseInput } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type {
  AgentGUINodeData,
  AgentGUIInteractionReadinessSource
} from "../../../types";
import {
  agentPromptContentDisplayText,
  agentPromptContentHasImage,
  emptyAgentComposerDraft,
  normalizeAgentPromptContentBlocks,
  snapshotAgentComposerDraft
} from "../model/agentComposerDraft";
import type {
  AgentComposerDraft,
  SubmittedDraftSnapshot
} from "../model/agentGuiNodeTypes";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import type { AgentComposerSubmitOptions } from "../composer/AgentComposer.types";
import {
  PLAN_IMPLEMENTATION_ACTION_FEEDBACK,
  PLAN_IMPLEMENTATION_ACTION_IMPLEMENT,
  PLAN_IMPLEMENTATION_ACTION_SKIP
} from "../../../shared/agentConversation/planImplementationPresentation";
import {
  clearSubmittedDraftIfUnchanged,
  deleteUnacceptedSubmittedDraftSnapshot,
  toRuntimeSendContent
} from "./agentGuiController.draftMessageHelpers";
import { clearSubmittedAgentGUIHomeDraft } from "./agentGuiController.homeDraftHelpers";
import {
  AgentGUIEngineSettlementController,
  type AgentGUIGoalControlPendingSettlement
} from "./AgentGUIEngineSettlementController";
import {
  AGENT_RESUME_SESSION_NOT_LOCAL_ERROR,
  buildProviderSessionNotFoundActivationError,
  buildResumeSessionNotLocalActivationError,
  getAgentGUIErrorMessage,
  isNonRetryableResumeErrorCode
} from "./agentGuiController.errors";
import {
  agentSubmitTraceDiagnostics,
  createAgentSubmitTraceState,
  reportAgentGUISubmitRecoveredActiveConversation,
  reportAgentGUISubmitWithoutActiveConversation,
  reportAgentSubmitTraceDiagnostic,
  scheduleAgentSubmitTracePaint
} from "./agentGuiController.reporting";
import { resolveAgentGUIInteractionReadinessIdentity } from "./agentGuiController.interactionHelpers";
import { readAgentGUIInteractionReadiness } from "./useAgentGUIInteractionReadiness";
import {
  resolveConversationSummaryById,
  type ConversationIntent
} from "./useAgentConversationSelection";
import type { useAgentGUIActivation } from "./useAgentGUIActivation";
import type { AgentGUINewConversationActivationResult } from "./agentGuiNewConversationActivation.types";
import { useAgentGUIGoalControlActions } from "./useAgentGUIGoalControlActions";
import { waitForAgentSubmitSettlement } from "./agentSubmitSettlement";
import {
  agentComposerHostExtension,
  agentPromptSubmitText,
  isAgentPromptSubmitPreparable,
  runPreparedAgentPromptSubmit,
  type AgentPromptSubmitReceipt
} from "../../../shared/agentConversation/agentComposerHostExtension";

/** executePrompt 交给引擎之后的同步结果；给「提交被接受后」的宿主回调用。 */
interface AgentGUIExecutedPrompt {
  accepted: boolean;
  clientSubmitId: string;
  queued: boolean;
}

interface UseAgentGUISubmitInteractionActionsInput {
  activation: ReturnType<typeof useAgentGUIActivation>;
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  activeEngineActiveTurn: AgentActivityTurn | null;
  activeEnginePendingInteractions: readonly AgentActivityInteraction[];
  agentActivityRuntime: AgentGUIRuntime;
  conversationListQuery: unknown | null;
  conversationsRef: RefObject<AgentGUIConversationSummary[]>;
  dataRef: RefObject<AgentGUINodeData>;
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
  executePromptRef: RefObject<
    (
      agentSessionId: string,
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: {
        immediate?: boolean;
        requiredSettingsPatch?: AgentComposerSubmitOptions["requiredSettingsPatch"];
        sendNow?: boolean;
        targetTurnId?: AgentComposerSubmitOptions["targetTurnId"];
        sourceScopeKey?: string;
        trackDraft?: boolean;
      }
    ) => void
  >;
  isComposerHomeRef: RefObject<boolean>;
  isCurrentConversation(agentSessionId: string): boolean;
  isRespondingToInteraction: boolean;
  interactionReadinessSource?: AgentGUIInteractionReadinessSource | null;
  isSessionMarkedNonResumable(agentSessionId: string): boolean;
  persistActiveConversation(agentSessionId: string | null): void;
  planActionsRef: RefObject<{
    implement(): boolean;
    feedback(value: string): boolean;
    skip(): boolean;
  }>;
  promptImagesSupported: boolean;
  sessionEngine: AgentSessionEngine;
  setActiveConversationId: Dispatch<SetStateAction<string | null>>;
  setDetailError: Dispatch<SetStateAction<string | null>>;
  setDraftByScopeKey: Dispatch<
    SetStateAction<Record<string, AgentComposerDraft>>
  >;
  setGoalClearNoticeSequence: Dispatch<SetStateAction<number>>;
  setIntent: Dispatch<SetStateAction<ConversationIntent>>;
  submittedDraftSnapshotsRef: RefObject<Record<string, SubmittedDraftSnapshot>>;
  startConversation(
    content: AgentPromptContentBlock[],
    displayPrompt?: string,
    options?: AgentComposerSubmitOptions,
    initialTurnExpected?: boolean,
    initialGoalControl?: {
      action: AgentActivityGoalControlAction;
      objective?: string;
    }
  ): AgentGUINewConversationActivationResult | null;
  submitPromptRef: RefObject<
    (
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: AgentComposerSubmitOptions
    ) => void
  >;
  transientConversation: AgentGUIConversationSummary | null;
  workspaceId: string;
}

export function typedGoalControlFromComposer(
  content: AgentPromptContentBlock[],
  _displayPrompt?: string
): { action: AgentActivityGoalControlAction; objective?: string } | null {
  if (content.length !== 1 || content[0]?.type !== "text") {
    return null;
  }
  // Structured content owns command semantics. displayPrompt may collapse a
  // bundle into a chip, but it must neither hide nor manufacture a control.
  return parseAgentActivityGoalControlText(content[0].text ?? "");
}
export function useAgentGUISubmitInteractionActions(
  input: UseAgentGUISubmitInteractionActionsInput
) {
  const {
    activation,
    activeConversationIdRef,
    activeEngineActiveTurn,
    activeEnginePendingInteractions,
    agentActivityRuntime,
    conversationListQuery,
    conversationsRef,
    dataRef,
    draftByScopeKeyRef,
    executePromptRef,
    isComposerHomeRef,
    isCurrentConversation,
    isRespondingToInteraction,
    interactionReadinessSource,
    isSessionMarkedNonResumable,
    persistActiveConversation,
    planActionsRef,
    promptImagesSupported,
    sessionEngine,
    setActiveConversationId,
    setDetailError,
    setDraftByScopeKey,
    setGoalClearNoticeSequence,
    setIntent,
    submittedDraftSnapshotsRef,
    startConversation,
    submitPromptRef,
    transientConversation,
    workspaceId
  } = input;
  // 正在向宿主要开工卡（异步）的会话号：挡同一条会话的重复提交（票 05）。
  const preparingSubmitSessionIdsRef = useRef<Set<string>>(new Set());
  const goalControlSettlementsRef = useRef<
    Record<string, AgentGUIGoalControlPendingSettlement>
  >({});
  const { goalControl } = useAgentGUIGoalControlActions({
    activeConversationIdRef,
    draftByScopeKeyRef,
    goalControlSettlementsRef,
    sessionEngine,
    setDetailError
  });
  const retryActivation = useCallback(() => {
    const agentSessionId = activeConversationIdRef.current;
    if (!agentSessionId) {
      return;
    }
    if (isSessionMarkedNonResumable(agentSessionId)) {
      return;
    }
    if (isNonRetryableResumeErrorCode(activation.codeFor(agentSessionId))) {
      return;
    }
    setDetailError(null);
    activation.activate({ mode: "existing", agentSessionId });
  }, [
    agentActivityRuntime,
    activation,
    isCurrentConversation,
    isSessionMarkedNonResumable,
    workspaceId
  ]);

  const executePrompt = useCallback(
    (
      agentSessionId: string,
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: {
        capabilityRefs?: AgentComposerSubmitOptions["capabilityRefs"];
        immediate?: boolean;
        requiredSettingsPatch?: AgentComposerSubmitOptions["requiredSettingsPatch"];
        sendNow?: boolean;
        targetTurnId?: AgentComposerSubmitOptions["targetTurnId"];
        sourceScopeKey?: string;
        /** 提交那一刻的草稿快照；不给时现读。异步准备过的提交必须给（见 submitPrompt）。 */
        submittedDraft?: AgentComposerDraft;
        trackDraft?: boolean;
      }
    ) => {
      const normalizedContent = normalizeAgentPromptContentBlocks(content);
      if (!agentSessionId || normalizedContent.length === 0) {
        return null;
      }
      const targetIsActiveConversation =
        activeConversationIdRef.current === agentSessionId;
      // displayPrompt(如 bundle 折叠成单 chip)优先用于回显;否则回退到 content 派生文本。
      const submittedPromptText =
        displayPrompt && displayPrompt.trim()
          ? displayPrompt
          : agentPromptContentDisplayText(normalizedContent);
      const submittedAtUnixMs = Date.now();
      const submitTrace = createAgentSubmitTraceState({
        agentSessionId,
        content: normalizedContent,
        prompt: submittedPromptText,
        queued: false,
        startedAtUnixMs: submittedAtUnixMs
      });
      if (options?.trackDraft === true) {
        const sourceScopeKey =
          options.sourceScopeKey ??
          resolveAgentComposerDraftScopeKey({ agentSessionId });
        const submittedDraft =
          options.submittedDraft ??
          draftByScopeKeyRef.current[sourceScopeKey] ??
          emptyAgentComposerDraft();
        submittedDraftSnapshotsRef.current[submitTrace.clientSubmitId] = {
          sourceScopeKey,
          content: snapshotAgentComposerDraft(submittedDraft),
          targetAgentSessionId: agentSessionId
        };
      }
      const targetConversation = resolveConversationSummaryById(
        conversationsRef.current,
        agentSessionId,
        transientConversation
      );
      reportAgentSubmitTraceDiagnostic({
        event: "submit.begin",
        runtime: agentActivityRuntime,
        trace: submitTrace,
        workspaceId,
        fields: {
          activeConversationId: activeConversationIdRef.current,
          conversationKnown: targetConversation !== null,
          conversationStatus: targetConversation?.status ?? null,
          isComposerHome: isComposerHomeRef.current,
          targetIsActiveConversation,
          targetMode: "existing"
        }
      });
      const { accepted, queued } = sessionEngine.submitPrompt({
        agentSessionId,
        ...(options?.capabilityRefs?.length
          ? { capabilityRefs: options.capabilityRefs }
          : {}),
        clientSubmitId: submitTrace.clientSubmitId,
        content: normalizedContent,
        ...(displayPrompt && displayPrompt.trim() ? { displayPrompt } : {}),
        submitDiagnostics: agentSubmitTraceDiagnostics(submitTrace),
        ...(options?.requiredSettingsPatch
          ? {
              requiredSettingsPatch: {
                ...options.requiredSettingsPatch
              }
            }
          : {}),
        ...(options?.targetTurnId?.trim()
          ? { targetTurnId: options.targetTurnId.trim() }
          : {}),
        ...(options?.immediate === true
          ? { routing: "immediate" as const }
          : options?.sendNow === true
            ? { routing: "send_now" as const }
            : {}),
        runtimeContent: toRuntimeSendContent(normalizedContent)
      });
      submitTrace.queued = queued;
      setDetailError(null);
      // Clear the composer optimistically the instant the engine takes the
      // prompt — whether it was queued behind a busy turn or accepted straight
      // into an idle session. The snapshot is retained so
      // AgentGUIEngineSettlementController can restore it if the send is
      // later rejected. A submit the engine never accepted is left untouched so
      // its text is not lost (deleteUnacceptedSubmittedDraftSnapshot cleans up).
      const submittedSnapshot =
        submittedDraftSnapshotsRef.current[submitTrace.clientSubmitId];
      if ((accepted || queued) && submittedSnapshot) {
        setDraftByScopeKey((current) => {
          const next = clearSubmittedDraftIfUnchanged({
            drafts: current,
            snapshot: submittedSnapshot
          });
          draftByScopeKeyRef.current = next;
          return next;
        });
      }
      deleteUnacceptedSubmittedDraftSnapshot({
        snapshots: submittedDraftSnapshotsRef.current,
        clientSubmitId: submitTrace.clientSubmitId,
        accepted,
        queued
      });
      reportAgentSubmitTraceDiagnostic({
        event: "send_input.requested",
        runtime: agentActivityRuntime,
        trace: submitTrace,
        workspaceId
      });
      scheduleAgentSubmitTracePaint({
        runtime: agentActivityRuntime,
        trace: submitTrace,
        workspaceId
      });
      const executed: AgentGUIExecutedPrompt = {
        accepted,
        clientSubmitId: submitTrace.clientSubmitId,
        queued
      };
      return executed;
    },
    [agentActivityRuntime, sessionEngine, setDraftByScopeKey, workspaceId]
  );

  useEffect(() => {
    executePromptRef.current = executePrompt;
  }, [executePrompt]);

  useEffect(() => {
    const controller = new AgentGUIEngineSettlementController({
      applyDraftUpdate: (update) => {
        setDraftByScopeKey((current) => {
          const next = update(current);
          draftByScopeKeyRef.current = next;
          return next;
        });
      },
      engine: sessionEngine,
      goalControlSettlements: goalControlSettlementsRef.current,
      isCurrentConversation,
      onGoalControlCleared: () =>
        setGoalClearNoticeSequence((current) => current + 1),
      onGoalControlFailed: (settlement) => {
        setDetailError(
          settlement.errorMessage
            ? getAgentGUIErrorMessage(goalControlSettlementError(settlement))
            : translate("agentHost.agentGui.goalControlFailed")
        );
      },
      snapshots: submittedDraftSnapshotsRef.current
    });
    return controller.attach();
  }, [
    draftByScopeKeyRef,
    isCurrentConversation,
    sessionEngine,
    setDetailError,
    setDraftByScopeKey,
    setGoalClearNoticeSequence,
    submittedDraftSnapshotsRef
  ]);

  const submitExistingPrompt = useCallback(
    (
      agentSessionId: string,
      normalizedContent: AgentPromptContentBlock[],
      displayPromptText?: string,
      options?: {
        capabilityRefs?: AgentComposerSubmitOptions["capabilityRefs"];
        requiredSettingsPatch?: AgentComposerSubmitOptions["requiredSettingsPatch"];
        sendNow?: boolean;
        targetTurnId?: AgentComposerSubmitOptions["targetTurnId"];
        sourceScopeKey?: string;
        submittedDraft?: AgentComposerDraft;
        trackDraft?: boolean;
      }
    ) => {
      if (isSessionMarkedNonResumable(agentSessionId)) {
        setDetailError(
          getAgentGUIErrorMessage(buildResumeSessionNotLocalActivationError())
        );
        return null;
      }
      if (isNonRetryableResumeErrorCode(activation.codeFor(agentSessionId))) {
        setDetailError(
          getAgentGUIErrorMessage(
            activation.codeFor(agentSessionId) ===
              AGENT_RESUME_SESSION_NOT_LOCAL_ERROR
              ? buildResumeSessionNotLocalActivationError(
                  activation.errorFor(agentSessionId)
                )
              : buildProviderSessionNotFoundActivationError(
                  activation.errorFor(agentSessionId)
                )
          )
        );
        return null;
      }
      return executePrompt(agentSessionId, normalizedContent, displayPromptText, {
        capabilityRefs: options?.capabilityRefs,
        requiredSettingsPatch: options?.requiredSettingsPatch,
        targetTurnId: options?.targetTurnId,
        sendNow: options?.sendNow === true,
        sourceScopeKey: options?.sourceScopeKey,
        ...(options?.submittedDraft
          ? { submittedDraft: options.submittedDraft }
          : {}),
        trackDraft: options?.trackDraft === true
      });
    },
    [activation, executePrompt, isSessionMarkedNonResumable, workspaceId]
  );

  const submitPrompt = useCallback(
    (
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: AgentComposerSubmitOptions
    ) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedContent = normalizeAgentPromptContentBlocks(content);
      if (normalizedContent.length === 0) {
        return;
      }
      const displayPromptText =
        displayPrompt && displayPrompt.trim() ? displayPrompt : undefined;
      const typedGoal = typedGoalControlFromComposer(
        normalizedContent,
        displayPromptText
      );
      if (
        !promptImagesSupported &&
        agentPromptContentHasImage(normalizedContent)
      ) {
        setDetailError(translate("agentHost.agentGui.promptImagesUnsupported"));
        return;
      }
      if (!agentSessionId) {
        if (!isComposerHomeRef.current) {
          const promptLength =
            agentPromptContentDisplayText(normalizedContent).length;
          reportAgentGUISubmitWithoutActiveConversation({
            blockCount: normalizedContent.length,
            conversationCount: conversationsRef.current.length,
            conversationListQueryReady: conversationListQuery !== null,
            dataLastActiveAgentSessionId:
              dataRef.current.lastActiveAgentSessionId ?? null,
            isComposerHome: isComposerHomeRef.current,
            promptLength,
            provider: dataRef.current.provider ?? null,
            runtime: agentActivityRuntime,
            workspaceId
          });
          const recoveredAgentSessionId =
            dataRef.current.lastActiveAgentSessionId?.trim() ?? "";
          if (recoveredAgentSessionId) {
            reportAgentGUISubmitRecoveredActiveConversation({
              blockCount: normalizedContent.length,
              conversationCount: conversationsRef.current.length,
              conversationListQueryReady: conversationListQuery !== null,
              promptLength,
              provider: dataRef.current.provider ?? null,
              recoveredAgentSessionId,
              runtime: agentActivityRuntime,
              workspaceId
            });
            activeConversationIdRef.current = recoveredAgentSessionId;
            setActiveConversationId(recoveredAgentSessionId);
            setIntent({ tag: "active", id: recoveredAgentSessionId });
            persistActiveConversation(recoveredAgentSessionId);
            if (typedGoal) {
              goalControl(
                typedGoal.action,
                typedGoal.objective,
                resolveAgentComposerDraftScopeKey({})
              );
              return;
            }
            submitExistingPrompt(
              recoveredAgentSessionId,
              normalizedContent,
              displayPromptText,
              {
                capabilityRefs: options?.capabilityRefs,
                requiredSettingsPatch: options?.requiredSettingsPatch,
                sourceScopeKey: resolveAgentComposerDraftScopeKey({}),
                trackDraft: true
              }
            );
            return;
          }
        }
        const homeDraftKey = resolveAgentComposerDraftScopeKey({});
        const submittedHomeDraft = snapshotAgentComposerDraft(
          draftByScopeKeyRef.current[homeDraftKey] ?? emptyAgentComposerDraft()
        );
        const activationResult = startConversation(
          normalizedContent,
          displayPromptText,
          options,
          typedGoal ? false : undefined,
          typedGoal ?? undefined
        );
        if (activationResult) {
          draftByScopeKeyRef.current = clearSubmittedAgentGUIHomeDraft({
            draftKey: homeDraftKey,
            drafts: draftByScopeKeyRef.current,
            submittedDraft: submittedHomeDraft
          });
          setDraftByScopeKey((current) =>
            clearSubmittedAgentGUIHomeDraft({
              draftKey: homeDraftKey,
              drafts: current,
              submittedDraft: submittedHomeDraft
            })
          );
        }
        return;
      }
      if (typedGoal) {
        goalControl(
          typedGoal.action,
          typedGoal.objective,
          resolveAgentComposerDraftScopeKey({ agentSessionId })
        );
        return;
      }
      // 评审 F：按下发送这一刻就给草稿拍照。结对模式要先异步问宿主要开工卡，这段时间
      // 用户可能已经在输入框里打下一句了；到真正发送时才读草稿，就会把新打的字当成
      // 「已提交的草稿」清掉，或在被拒时恢复成错的内容。
      const submittedDraft = snapshotAgentComposerDraft(
        draftByScopeKeyRef.current[
          resolveAgentComposerDraftScopeKey({ agentSessionId })
        ] ?? emptyAgentComposerDraft()
      );
      const sendExisting = (
        sendContent: AgentPromptContentBlock[],
        sendDisplayPrompt: string | undefined
      ): AgentPromptSubmitReceipt | null => {
        const executed = submitExistingPrompt(
          agentSessionId,
          sendContent,
          sendDisplayPrompt,
          {
            capabilityRefs: options?.capabilityRefs,
            requiredSettingsPatch: options?.requiredSettingsPatch,
            submittedDraft,
            trackDraft: true
          }
        );
        if (!executed || (!executed.accepted && !executed.queued)) {
          return null;
        }
        return {
          settle: () =>
            waitForAgentSubmitSettlement(
              sessionEngine,
              agentSessionId,
              executed.clientSubmitId
            )
        };
      };
      // 分栏结对模式（peer-pair-mode 票 05）：宿主扩展口说「这句要拼开工卡」时才走异步
      // 那条路（先问宿主要块 → 拼进第一个文字块 → 发 → 等引擎接受 → 回调宿主投卡）。
      // 为什么接在这里而不是作曲区：只有这一层同时握着「这句发给哪条会话」、
      // 引擎的 clientSubmitId 与引擎本身，「被接受之后才开工」（PRD D4）不必把返回值
      // 穿过作曲区 → 详情面板 → 工作流三层 void 回调。斜杠命令在作曲区已经分流走了，
      // 漏到这里的 `/xxx` 文本再由 isAgentPromptSubmitPreparable 挡一次。
      const extension = agentComposerHostExtension();
      const submitText = agentPromptSubmitText(normalizedContent);
      if (
        extension?.prepareSubmit &&
        isAgentPromptSubmitPreparable(submitText) &&
        extension.wantsSubmitPreparation?.({
          agentSessionId,
          text: submitText
        }) === true
      ) {
        // 准备在途时草稿还留在输入框里；这段时间里再按一次发送会把同一句发两遍，
        // 所以同一条会话的重复提交直接丢掉（草稿没清，什么都不会丢）。
        if (preparingSubmitSessionIdsRef.current.has(agentSessionId)) {
          return;
        }
        preparingSubmitSessionIdsRef.current.add(agentSessionId);
        void runPreparedAgentPromptSubmit({
          agentSessionId,
          content: normalizedContent,
          displayPrompt: displayPromptText,
          extension,
          onDispatched: () => {
            preparingSubmitSessionIdsRef.current.delete(agentSessionId);
          },
          send: sendExisting
        })
          .catch((error: unknown) => {
            // 同步路径里 send 抛错会直接冒到作曲区；异步路径没人接，这里接住显示出来，
            // 宿主的在途标记已由 runPreparedAgentPromptSubmit 清掉（评审补充 3）。
            setDetailError(getAgentGUIErrorMessage(error));
          })
          .finally(() => {
            preparingSubmitSessionIdsRef.current.delete(agentSessionId);
          });
        return;
      }
      sendExisting(normalizedContent, displayPromptText);
    },
    [
      agentActivityRuntime,
      conversationListQuery,
      promptImagesSupported,
      goalControl,
      persistActiveConversation,
      sessionEngine,
      startConversation,
      submitExistingPrompt,
      workspaceId
    ]
  );

  useEffect(() => {
    submitPromptRef.current = submitPrompt;
  }, [submitPrompt]);

  const submitGuidancePrompt = useCallback(
    (
      content: AgentPromptContentBlock[],
      displayPrompt?: string,
      options?: AgentComposerSubmitOptions
    ) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedContent = normalizeAgentPromptContentBlocks(content);
      if (!agentSessionId || normalizedContent.length === 0) {
        return;
      }
      if (
        !promptImagesSupported &&
        agentPromptContentHasImage(normalizedContent)
      ) {
        setDetailError(translate("agentHost.agentGui.promptImagesUnsupported"));
        return;
      }
      const activeTurnId = activeEngineActiveTurn?.turnId.trim() ?? "";
      if (activeTurnId === "") {
        return;
      }
      const displayPromptText =
        displayPrompt && displayPrompt.trim() ? displayPrompt : undefined;
      submitExistingPrompt(
        agentSessionId,
        normalizedContent,
        displayPromptText,
        {
          capabilityRefs: options?.capabilityRefs,
          sendNow: true,
          targetTurnId: activeTurnId,
          trackDraft: true
        }
      );
    },
    [
      activeEngineActiveTurn,
      promptImagesSupported,
      submitExistingPrompt,
      translate
    ]
  );

  const showPromptImagesUnsupported = useCallback(() => {
    setDetailError(translate("agentHost.agentGui.promptImagesUnsupported"));
  }, []);

  const submitInteractivePrompt = useCallback(
    (input: AgentInteractionResponseInput): boolean => {
      // Plan-implementation actions are client-orchestrated; route them to the
      // plan decision handlers instead of submitInteractive.
      if (input.action === PLAN_IMPLEMENTATION_ACTION_IMPLEMENT) {
        return planActionsRef.current.implement();
      }
      if (input.action === PLAN_IMPLEMENTATION_ACTION_FEEDBACK) {
        return planActionsRef.current.feedback(
          typeof input.payload?.text === "string" ? input.payload.text : ""
        );
      }
      if (input.action === PLAN_IMPLEMENTATION_ACTION_SKIP) {
        return planActionsRef.current.skip();
      }
      const normalizedOptionId = input.optionId?.trim() ?? "";
      const target = resolveAgentGUIInteractionReadinessIdentity({
        agentSessionId: input.agentSessionId,
        requestId: input.requestId,
        turnId: input.turnId,
        workspaceId
      });
      const exactPendingInteraction =
        target !== null &&
        activeEnginePendingInteractions.some(
          (interaction) =>
            interaction.status === "pending" &&
            interaction.agentSessionId.trim() === target.agentSessionId &&
            interaction.turnId.trim() === target.turnId &&
            interaction.requestId.trim() === target.requestId
        );
      if (!target || !exactPendingInteraction || isRespondingToInteraction) {
        return false;
      }
      if (
        readAgentGUIInteractionReadiness({
          identity: target,
          source: interactionReadinessSource
        })?.status === "blocked"
      ) {
        return false;
      }
      setDetailError(null);
      return sessionEngine.submitInteractionResponse({
        ...(input.action?.trim() ? { action: input.action.trim() } : {}),
        agentSessionId: target.agentSessionId,
        ...(normalizedOptionId ? { optionId: normalizedOptionId } : {}),
        ...(input.payload ? { payload: { ...input.payload } } : {}),
        requestId: target.requestId,
        turnId: target.turnId
      });
    },
    [
      activeEnginePendingInteractions,
      interactionReadinessSource,
      isRespondingToInteraction,
      sessionEngine,
      workspaceId
    ]
  );

  const submitApprovalOption = useCallback(
    (input: AgentInteractionResponseInput): boolean =>
      submitInteractivePrompt(input),
    [submitInteractivePrompt]
  );

  const interruptCurrentTurn = useCallback(
    (noRunningResponseMessage: string) => {
      const agentSessionId = activeConversationIdRef.current;
      if (!agentSessionId) return;
      void noRunningResponseMessage;
      setDetailError(null);
      sessionEngine.stopSession({ agentSessionId });
    },
    [sessionEngine]
  );

  // Monitors live in their own child sessions, so stopping one is a turn
  // cancel against that child - not against the conversation the user is
  // looking at. The daemon maps a child-scoped cancel onto the SDK's targeted
  // stopTask, leaving the root query and the other monitors alone.
  const stopBackgroundMonitors = useCallback(
    (agentSessionIds: readonly string[]) => {
      for (const agentSessionId of agentSessionIds) {
        const trimmed = agentSessionId.trim();
        if (!trimmed) continue;
        sessionEngine.stopSession({ agentSessionId: trimmed });
      }
    },
    [sessionEngine]
  );

  const updateDraftContent = useCallback(
    (draftContent: AgentComposerDraft, sourceScopeKey?: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const draftKey =
        sourceScopeKey ??
        resolveAgentComposerDraftScopeKey({
          agentSessionId
        });
      draftByScopeKeyRef.current = {
        ...draftByScopeKeyRef.current,
        [draftKey]: draftContent
      };
      setDraftByScopeKey((current) => ({
        ...current,
        [draftKey]: draftContent
      }));
    },
    []
  );

  return {
    goalControl,
    interruptCurrentTurn,
    stopBackgroundMonitors,
    retryActivation,
    showPromptImagesUnsupported,
    submitApprovalOption,
    submitGuidancePrompt,
    submitInteractivePrompt,
    submitPrompt,
    updateDraftContent
  };
}

function goalControlSettlementError(
  settlement: SessionGoalControlSettlement
): Error {
  const error = new Error(settlement.errorMessage ?? "") as Error & {
    code?: string;
    reason?: string;
  };
  if (settlement.errorCode) error.code = settlement.errorCode;
  if (settlement.errorReason) error.reason = settlement.errorReason;
  return error;
}
