import type { RefObject } from "react";
import { useCallback, useEffect, useRef } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { subscribe, subscribeCoalesced } from "../../../host/agentHostEventBus";
import type {
  AgentSessionComposerSettings,
  AgentSessionReasoningEffort
} from "../../../shared/agentSessionTypes";
import type { AgentGUINodeData } from "../../../types";
import { readNodeDefaultDraftSettings } from "./agentGuiController.composerHelpers";
import {
  composerTargetDataForConversation,
  type AgentGUIActiveSessionTarget,
  type AgentGUIComposerTargetData
} from "./agentGuiController.composerPresentation";
import {
  composerDefaultsPatchFromSettings,
  composerOptionsForTarget,
  mergeAgentModelCatalogInvalidationEvents,
  withoutAcknowledgedComposerDefaults
} from "./agentGuiController.providerHelpers";
import type { AgentGUIComposerDefaultsAuthorityReconciler } from "./agentGuiComposerDefaultsReconciliation";
import { setComposerModelListProbe } from "./composerModelListProbe";

export function useAgentGUIComposerOptionsSync(input: {
  activeConversationId: string | null;
  activeConversationIdRef: RefObject<string | null>;
  activeSessionTarget: AgentGUIActiveSessionTarget | null;
  agentActivityRuntime: AgentGUIRuntime;
  composerTargetData: AgentGUIComposerTargetData;
  conversationFilter: unknown;
  currentUserId: string | null | undefined;
  data: AgentGUINodeData;
  dataRef: RefObject<AgentGUINodeData>;
  defaultReasoningEffort: AgentSessionReasoningEffort | null;
  draftSettingsBySessionIdRef: RefObject<
    Record<string, AgentSessionComposerSettings>
  >;
  isComposerHome: boolean;
  isComposerHomeRef: RefObject<boolean>;
  isCreatingConversation: boolean;
  loadDraftComposerOptionsRef: RefObject<() => void>;
  loadSessionState(agentSessionId: string): void;
  onComposerDefaultsAuthorityReloadedRef: RefObject<AgentGUIComposerDefaultsAuthorityReconciler>;
  providerComposerOptions:
    | { behavior?: { prewarmDraftSession?: boolean } | null }
    | null
    | undefined;
  selectedComposerTargetDataRef: RefObject<AgentGUIComposerTargetData>;
  selectedProjectPath: string | null;
  selectedProjectPathRef: RefObject<string | null>;
  syncConversationListProjection(agentSessionId?: string | null): Promise<void>;
  workspaceId: string;
  workspacePath: string;
}) {
  const previousIsCreatingConversationRef = useRef(
    input.isCreatingConversation
  );
  // 挂载和下拉刷新必须共用这一份 settings。扩展模型缓存的键含它的签名，
  // 只在一边带上会写成另一条记录，重启后读不回，打开下拉又会再探测。
  const resolveComposerRequest = useCallback(
    (
      targetData: AgentGUIComposerTargetData,
      options?: {
        excludePersistentDefaults?: boolean;
        reconcileAcknowledgedDefaults?: boolean;
        settings?: AgentSessionComposerSettings;
      }
    ) => {
      const localSettings =
        options?.settings ??
        readNodeDefaultDraftSettings({
          data: targetData.data,
          defaultReasoningEffort: input.defaultReasoningEffort,
          drafts: input.draftSettingsBySessionIdRef.current
        });
      const authorityRead = options?.reconcileAcknowledgedDefaults
        ? input.onComposerDefaultsAuthorityReloadedRef.current.prepareRead(
            targetData,
            localSettings
          )
        : { force: false, receipt: null, settings: localSettings };
      const localDefaults = options?.excludePersistentDefaults
        ? composerDefaultsPatchFromSettings(localSettings, localSettings)
        : null;
      const requestSettings = localDefaults
        ? withoutAcknowledgedComposerDefaults(
            authorityRead.settings,
            localDefaults
          )
        : authorityRead.settings;
      const cwd =
        input.selectedProjectPathRef.current?.trim() ||
        input.workspacePath.trim() ||
        "";
      return { authorityRead, cwd, requestSettings };
    },
    [
      input.defaultReasoningEffort,
      input.draftSettingsBySessionIdRef,
      input.onComposerDefaultsAuthorityReloadedRef,
      input.selectedProjectPathRef,
      input.workspacePath
    ]
  );
  const loadComposerOptionsForTarget = useCallback(
    (
      targetData: AgentGUIComposerTargetData,
      options?: {
        allowWhileCreating?: boolean;
        excludePersistentDefaults?: boolean;
        force?: boolean;
        reconcileAcknowledgedDefaults?: boolean;
        settings?: AgentSessionComposerSettings;
      }
    ): Promise<void> => {
      if (
        (input.isCreatingConversation && !options?.allowWhileCreating) ||
        !targetData.agentTargetId
      ) {
        return Promise.resolve();
      }
      const { authorityRead, cwd, requestSettings } = resolveComposerRequest(
        targetData,
        options
      );
      return Promise.resolve(
        input.agentActivityRuntime.getComposerOptions({
          workspaceId: input.workspaceId,
          cwd,
          force: options?.force || authorityRead.force ? true : undefined,
          provider: targetData.provider,
          agentTargetId: targetData.agentTargetId,
          settings: requestSettings
        })
      ).then((returnedOptions) => {
        const loadedOptions =
          composerOptionsForTarget({
            snapshot: input.agentActivityRuntime.getSnapshot(input.workspaceId),
            target: targetData
          }) ?? returnedOptions;
        input.onComposerDefaultsAuthorityReloadedRef.current.reconcileHomeDefaults(
          targetData,
          loadedOptions
        );
        if (options?.reconcileAcknowledgedDefaults) {
          input.onComposerDefaultsAuthorityReloadedRef.current.reloaded(
            authorityRead.receipt,
            returnedOptions
          );
        }
      });
    },
    [
      input.agentActivityRuntime,
      input.isCreatingConversation,
      input.workspaceId,
      resolveComposerRequest
    ]
  );
  const loadDraftComposerOptions = useCallback(
    (options?: { force?: boolean }) => {
      void loadComposerOptionsForTarget(
        composerTargetDataForConversation({
          activeConversationId: input.activeConversationIdRef.current,
          activeSessionTarget: input.activeSessionTarget,
          data: input.dataRef.current,
          optimisticTarget: null,
          selectedTarget: input.selectedComposerTargetDataRef.current
        }),
        {
          ...options,
          reconcileAcknowledgedDefaults:
            input.activeConversationIdRef.current === null &&
            input.isComposerHomeRef.current
        }
      ).catch(() => undefined);
    },
    [
      input.activeConversationIdRef,
      input.activeSessionTarget?.agentTargetId,
      input.activeSessionTarget?.agentSessionId,
      input.activeSessionTarget?.provider,
      input.dataRef,
      input.isComposerHomeRef,
      input.selectedComposerTargetDataRef,
      loadComposerOptionsForTarget
    ]
  );
  const reloadComposerOptionsForTarget = useCallback(
    (reloadInput: {
      settings: AgentSessionComposerSettings;
      target: AgentGUIComposerTargetData;
    }): Promise<void> =>
      loadComposerOptionsForTarget(reloadInput.target, {
        allowWhileCreating: true,
        force: true,
        reconcileAcknowledgedDefaults: true,
        settings: reloadInput.settings
      }),
    [loadComposerOptionsForTarget]
  );
  input.loadDraftComposerOptionsRef.current = loadDraftComposerOptions;

  useEffect(() => {
    // 挂载和切 provider 走上面的 getComposerOptions，不在这里探测。
    // 下拉打开或点「刷新」才调用这一支。
    setComposerModelListProbe((force) => {
      const refresh = input.agentActivityRuntime.refreshComposerModels;
      if (!refresh) return;
      const target = composerTargetDataForConversation({
        activeConversationId: input.activeConversationIdRef.current,
        activeSessionTarget: input.activeSessionTarget,
        data: input.dataRef.current,
        optimisticTarget: null,
        selectedTarget: input.selectedComposerTargetDataRef.current
      });
      if (!target.agentTargetId) return;
      const { cwd, requestSettings } = resolveComposerRequest(target, {
        reconcileAcknowledgedDefaults:
          input.activeConversationIdRef.current === null &&
          input.isComposerHomeRef.current
      });
      void refresh({
        workspaceId: input.workspaceId,
        cwd,
        force,
        provider: target.provider,
        agentTargetId: target.agentTargetId,
        settings: requestSettings
      });
    });
    return () => setComposerModelListProbe(null);
  }, [
    input.activeConversationIdRef,
    input.activeSessionTarget,
    input.agentActivityRuntime,
    input.dataRef,
    input.isComposerHomeRef,
    input.selectedComposerTargetDataRef,
    input.workspaceId,
    resolveComposerRequest
  ]);

  useEffect(() => {
    const disposeModelCatalog = subscribeCoalesced(
      "agent-model-catalog-invalidated",
      {
        delayMs: 150,
        key: () => "agent-model-catalog-invalidated",
        merge: mergeAgentModelCatalogInvalidationEvents
      },
      (event) => {
        const provider = composerTargetDataForConversation({
          activeConversationId: input.activeConversationIdRef.current,
          activeSessionTarget: input.activeSessionTarget,
          data: input.dataRef.current,
          optimisticTarget: null,
          selectedTarget: input.selectedComposerTargetDataRef.current
        }).provider;
        const activeId = input.activeConversationIdRef.current;
        if (!event.providers.some((candidate) => candidate === provider))
          return;
        loadDraftComposerOptions({ force: true });
        if (
          !activeId ||
          (activeId === null && input.isComposerHomeRef.current)
        ) {
          return;
        }
        input.loadSessionState(activeId);
      }
    );
    const disposeConnectorCatalog = subscribe(
      "agent-connector-catalog-invalidated",
      () => loadDraftComposerOptions({ force: true })
    );
    return () => {
      disposeModelCatalog();
      disposeConnectorCatalog();
    };
  }, [input.loadSessionState, loadDraftComposerOptions]);

  useEffect(() => {
    return subscribe("agent-composer-defaults-invalidated", (event) => {
      const selectedTarget = composerTargetDataForConversation({
        activeConversationId: input.activeConversationIdRef.current,
        activeSessionTarget: input.activeSessionTarget,
        data: input.dataRef.current,
        optimisticTarget: null,
        selectedTarget: input.selectedComposerTargetDataRef.current
      });
      if (selectedTarget.agentTargetId !== event.agentTargetId) {
        return;
      }
      const localIntent = readNodeDefaultDraftSettings({
        data: selectedTarget.data,
        defaultReasoningEffort: input.defaultReasoningEffort,
        drafts: input.draftSettingsBySessionIdRef.current
      });
      // The target-only event is always a reread signal. Exclude local
      // persistent intent from the request so effectiveSettings can reflect
      // daemon authority, but do not retire that intent without its own ack.
      void loadComposerOptionsForTarget(selectedTarget, {
        allowWhileCreating: true,
        excludePersistentDefaults: true,
        force: true,
        reconcileAcknowledgedDefaults: true,
        settings: localIntent
      }).catch(() => undefined);
    });
  }, [
    input.defaultReasoningEffort,
    input.activeSessionTarget?.agentTargetId,
    input.activeSessionTarget?.agentSessionId,
    input.activeSessionTarget?.provider,
    input.onComposerDefaultsAuthorityReloadedRef,
    loadComposerOptionsForTarget
  ]);

  useEffect(() => {
    // Session creation can finish after an earlier request cached the
    // provider's selected-model-only fallback. Once creation settles, bypass
    // request-signature deduplication so runtime-discovered
    // model options replace that bootstrap snapshot.
    const conversationCreationSettled =
      previousIsCreatingConversationRef.current &&
      !input.isCreatingConversation;
    previousIsCreatingConversationRef.current = input.isCreatingConversation;
    loadDraftComposerOptions(
      conversationCreationSettled ||
        (input.providerComposerOptions?.behavior?.prewarmDraftSession ===
          true &&
          input.isComposerHome)
        ? { force: true }
        : undefined
    );
  }, [
    input.activeConversationId,
    input.composerTargetData.agentTargetId,
    input.composerTargetData.provider,
    input.isComposerHome,
    input.isCreatingConversation,
    input.providerComposerOptions?.behavior?.prewarmDraftSession,
    input.selectedProjectPath,
    loadDraftComposerOptions
  ]);

  useEffect(() => {
    {
      void input.syncConversationListProjection(
        input.dataRef.current.lastActiveAgentSessionId
      );
    }
  }, [
    input.conversationFilter,
    input.currentUserId,
    input.data.provider,
    input.syncConversationListProjection
  ]);

  return { loadDraftComposerOptions, reloadComposerOptionsForTarget };
}
