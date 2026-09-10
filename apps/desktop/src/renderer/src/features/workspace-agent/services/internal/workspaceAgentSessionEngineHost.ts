import {
  AGENT_SESSION_ENGINE_LOCAL_ORIGIN,
  createAgentSessionEngine,
  type AgentActivityAdapter,
  type AgentActivityGoalControlInput,
  type AgentActivityGoalControlResult,
  type AgentActivitySendInput,
  type AgentActivitySubmitInteractiveInput,
  type AgentActivitySubmitInteractiveResult,
  type AgentSessionActivateEffectResult,
  type AgentSessionEngine,
  type EngineEffectOptions,
  type EngineExternalCommand,
  type EngineIntent,
  type PlanSubmitDecisionResult,
  type SessionAcknowledgeForkObservedCommand,
  type SessionReconcileCommand,
  type TuttiModeActivationUpdateCommand
} from "@tutti-os/agent-activity-core";
import type { AgentActivityRuntimeActivateSessionInput } from "@tutti-os/agent-gui";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import type { DesktopRuntimeApi } from "@preload/types";
import type { AgentHostAgentSessionComposerSettings } from "@shared/contracts/dto";
import {
  createDesktopAgentActivityAdapter,
  type DesktopAgentActivityCommandAdapter
} from "../desktopAgentActivityAdapter.ts";
import {
  readDesktopWorkspaceAgentReadState,
  writeDesktopWorkspaceAgentReadState
} from "../createDesktopAgentHostApi.ts";
import { editRetryResultFromTuttid } from "./workspaceAgentEditRetry.ts";
import type { IWorkspaceAgentActivityService } from "../workspaceAgentActivityService.interface.ts";

// 回退看红：保留符号，设为 false 后宿主铸新 id 时旧 pending 记录留下，会话栏出现
// 同题两条（一条是永远 uncertain 的 pending_activation 幽灵行）。
export const dismissStaleActivationOnHostMintedSessionId = true;

/**
 * 宿主代建会话返回的 id 与本次激活请求的 id 不同时（补丁 0099 的嵌入态路径），
 * 引擎会把命令结果判成 invalid、记录留成 uncertain：它「可能还会产出会话」，
 * 于是会话栏一直画着它；而真正的会话是另一个 id，两条并排。这里在结果回到引擎
 * 之前把请求 id 的 pending 记录撤掉，并直接把宿主铸的会话喂进引擎。
 * 返回是否做了撤销，便于测试钉住。
 */
export function reconcileHostMintedActivation(
  engine: Pick<AgentSessionEngine, "dispatch" | "getSnapshot">,
  requestedAgentSessionId: string,
  result: AgentSessionActivateEffectResult
): boolean {
  if (!dismissStaleActivationOnHostMintedSessionId) return false;
  if (!result || result.activation?.mode !== "new") return false;
  const requested = requestedAgentSessionId.trim();
  const minted = result.session?.agentSessionId?.trim() ?? "";
  if (!requested || !minted || minted === requested) return false;
  const activations =
    engine.getSnapshot().pendingIntents.activationsByRequestId;
  let dismissed = false;
  for (const record of Object.values(activations)) {
    if (record.mode !== "new" || record.agentSessionId !== requested) continue;
    engine.dispatch({ requestId: record.requestId, type: "activation/dismissed" });
    dismissed = true;
  }
  if (dismissed) {
    engine.dispatch({ session: result.session, type: "session/upserted" });
  }
  return dismissed;
}

export interface WorkspaceAgentSessionEngineHost {
  adapter: AgentActivityAdapter;
  commandAdapter: DesktopAgentActivityCommandAdapter;
  engine: AgentSessionEngine;
  dispose(): void;
}

export interface WorkspaceAgentSessionEngineActivityObserver {
  observeCommand(command: EngineExternalCommand): void;
  observeIntent(intent: EngineIntent): void;
}

interface CreateWorkspaceAgentSessionEngineHostInput {
  activityEventObserver?: WorkspaceAgentSessionEngineActivityObserver;
  executeEngineActivateSession(
    input: AgentActivityRuntimeActivateSessionInput,
    options: EngineEffectOptions
  ): Promise<AgentSessionActivateEffectResult>;
  executeEngineCancelTurn(
    input: {
      agentSessionId: string;
      signal?: AbortSignal;
      turnId: string;
      workspaceId: string;
    },
    options: EngineEffectOptions
  ): Promise<unknown>;
  executeEngineGoalControl(
    input: AgentActivityGoalControlInput,
    options?: EngineEffectOptions
  ): Promise<AgentActivityGoalControlResult>;
  reconcileSession(
    command: SessionReconcileCommand,
    signal?: AbortSignal
  ): Promise<unknown>;
  runtimeApi: Pick<DesktopRuntimeApi, "logTerminalDiagnostic">;
  takePendingSessionRecording?(workspaceId: string): string | null;
  restorePendingSessionRecording?(
    workspaceId: string,
    recordingId: string
  ): void;
  executeEngineSendInput(
    input: AgentActivitySendInput,
    options: EngineEffectOptions
  ): Promise<unknown>;
  executeEngineSubmitInteractive(
    input: AgentActivitySubmitInteractiveInput,
    options: EngineEffectOptions
  ): Promise<AgentActivitySubmitInteractiveResult>;
  executeEngineSubmitPlanDecision(
    input: {
      action: "implement";
      agentSessionId: string;
      idempotencyKey: string;
      promptKind: "plan-implementation";
      requestId: string;
      turnId: string;
      workspaceId: string;
    },
    options: EngineEffectOptions
  ): Promise<PlanSubmitDecisionResult>;
  subscribeSessionEvents(
    workspaceId: string,
    listener: (event: unknown) => void
  ): () => void;
  unactivateSession: IWorkspaceAgentActivityService["unactivateSession"];
  executeEngineUpdateSessionSettings(
    input: Parameters<
      IWorkspaceAgentActivityService["updateSessionSettings"]
    >[0],
    options: EngineEffectOptions
  ): ReturnType<IWorkspaceAgentActivityService["updateSessionSettings"]>;
  updateTuttiModeActivation: IWorkspaceAgentActivityService["updateTuttiModeActivation"];
  tuttidClient: TuttidClient;
  workspaceId: string;
}

interface WorkspaceAgentForkObservationAckClient {
  acknowledgeWorkspaceAgentSessionForkOperation(
    workspaceId: string,
    operationId: string,
    options: { signal?: AbortSignal }
  ): Promise<unknown>;
}

export function executeWorkspaceAgentTuttiModeUpdateCommand(
  input: Pick<
    CreateWorkspaceAgentSessionEngineHostInput,
    "updateTuttiModeActivation"
  >,
  command: TuttiModeActivationUpdateCommand,
  signal?: AbortSignal
): Promise<unknown> {
  return input.updateTuttiModeActivation({
    agentSessionId: command.agentSessionId,
    ...(command.expectedRevision === undefined
      ? {}
      : { expectedRevision: command.expectedRevision }),
    ...(command.effect === undefined &&
    command.orchestrationIntensity === undefined
      ? {}
      : {
          effect: command.effect ?? command.orchestrationIntensity
        }),
    ...(command.speed === undefined ? {} : { speed: command.speed }),
    signal,
    source: command.source,
    status: command.status,
    workspaceId: command.workspaceId
  });
}

export function executeWorkspaceAgentForkObservedAckCommand(
  client: WorkspaceAgentForkObservationAckClient,
  command: SessionAcknowledgeForkObservedCommand,
  signal?: AbortSignal
): Promise<unknown> {
  return client.acknowledgeWorkspaceAgentSessionForkOperation(
    command.workspaceId,
    command.operationId,
    { ...(signal === undefined ? {} : { signal }) }
  );
}

export function createWorkspaceAgentSessionEngineHost(
  input: CreateWorkspaceAgentSessionEngineHostInput
): WorkspaceAgentSessionEngineHost {
  const adapter = createDesktopAgentActivityAdapter({
    tuttidClient: input.tuttidClient,
    runtimeApi: input.runtimeApi,
    takePendingSessionRecording: input.takePendingSessionRecording,
    restorePendingSessionRecording: input.restorePendingSessionRecording
  });
  const engine = createAgentSessionEngine({
    clock: { nowUnixMs: () => Date.now() },
    commandPort: {
      kind: "typed",
      effects: {
        activateSession: async (effectInput, options) => {
          const result = await input.executeEngineActivateSession(
            {
              ...effectInput,
              ...(effectInput.settings
                ? {
                    settings:
                      effectInput.settings as AgentHostAgentSessionComposerSettings
                  }
                : {}),
              signal: options.signal
            },
            options
          );
          // 嵌入态宿主代建会话（补丁 0099）会铸另一个 agentSessionId；引擎里按
          // 请求 id 记的 pending 记录永远等不到同名会话，会话栏就多出一条幽灵行。
          reconcileHostMintedActivation(engine, effectInput.agentSessionId, result);
          return result;
        },
        cancelTurn: (effectInput, options) =>
          input.executeEngineCancelTurn(
            { ...effectInput, signal: options.signal },
            options
          ),
        controlGoal: (effectInput, options) =>
          input.executeEngineGoalControl(
            { ...effectInput, signal: options?.signal },
            options
          ),
        deleteSessions: (effectInput, options) =>
          adapter.deleteSessions({
            ...effectInput,
            signal: options?.signal
          }),
        renameSession: async (effectInput, options) => {
          const session = await adapter.renameSession({
            ...effectInput,
            signal: options?.signal
          });
          return { session };
        },
        respondToInteraction: (effectInput, options) =>
          input.executeEngineSubmitInteractive(
            {
              ...effectInput,
              signal: options.signal
            },
            options
          ),
        sendInput: (effectInput, options) =>
          input.executeEngineSendInput(
            {
              ...effectInput,
              signal: options.signal
            },
            options
          ),
        setSessionPinned: async (effectInput, options) => {
          const session = await adapter.setSessionPinned({
            ...effectInput,
            signal: options?.signal
          });
          return { session };
        },
        updateSessionSettings: (
          { agentSessionId, settings, workspaceId },
          options
        ) =>
          input.executeEngineUpdateSessionSettings(
            {
              agentSessionId,
              signal: options.signal,
              settings: settings as AgentHostAgentSessionComposerSettings,
              workspaceId
            },
            options
          )
      },
      executePlanDecision: (command, options) => {
        return input.executeEngineSubmitPlanDecision(
          {
            action: command.action,
            agentSessionId: command.agentSessionId,
            idempotencyKey: command.idempotencyKey,
            promptKind: command.promptKind,
            requestId: command.requestId,
            turnId: command.turnId,
            workspaceId: command.workspaceId
          },
          requiredEngineEffectOptions(options)
        );
      },
      execute: async (command, options): Promise<unknown> => {
        switch (command.type) {
          case "attention/readState/read":
            return readDesktopWorkspaceAgentReadState({
              roomId: command.workspaceId,
              userId: command.userId
            });
          case "attention/readState/write":
            return Promise.all([
              writeDesktopWorkspaceAgentReadState({
                roomId: command.workspaceId,
                userId: command.userId,
                kind: "completed",
                readIds: [...command.completed.readIds],
                unreadIds: [...command.completed.unreadIds]
              }),
              writeDesktopWorkspaceAgentReadState({
                roomId: command.workspaceId,
                userId: command.userId,
                kind: "failed",
                readIds: [...command.failed.readIds],
                unreadIds: [...command.failed.unreadIds]
              })
            ]);
          case "composerOptions/load":
            return adapter.loadComposerOptions({
              agentTargetId: command.targetKey,
              ...(command.cwd !== undefined ? { cwd: command.cwd } : {}),
              provider: command.provider,
              ...(command.settings !== undefined
                ? { settings: command.settings }
                : {}),
              signal: options?.signal,
              workspaceId: command.workspaceId
            });
          case "turn/editRetry":
            return input.tuttidClient
              .editRetry(
                command.workspaceId.trim(),
                command.agentSessionId.trim(),
                command.turnId,
                {
                  clientOperationId: command.clientOperationId,
                  editedText: command.editedText,
                  expectedHistoryRevision: command.expectedHistoryRevision
                },
                { signal: options?.signal }
              )
              .then(editRetryResultFromTuttid);
          case "turn/recoverEditRetry":
            return input.tuttidClient
              .recoverEditRetry(
                command.workspaceId.trim(),
                command.agentSessionId.trim(),
                command.operationId,
                { action: command.action },
                { signal: options?.signal }
              )
              .then(editRetryResultFromTuttid);
          case "session/forkThroughTurn":
            return adapter.forkSession({
              requestId: command.requestId,
              signal: options?.signal,
              sourceAgentSessionId: command.sourceAgentSessionId,
              targetAgentSessionId: command.targetAgentSessionId,
              turnId: command.turnId,
              workspaceId: command.workspaceId
            });
          case "session/ackForkObserved":
            return executeWorkspaceAgentForkObservedAckCommand(
              input.tuttidClient,
              command,
              options?.signal
            );
          case "tuttiMode/update":
            return executeWorkspaceAgentTuttiModeUpdateCommand(
              input,
              command,
              options?.signal
            );
          case "engine/probe":
            return Promise.resolve({ ok: true });
          case "engine/reconcileWorkspace": {
            // Historical/pull path: fetch the authoritative session list over
            // HTTP and hand it to the engine as a historical snapshot. This
            // never lights attention (live=false); realtime completions come in
            // via turn/upserted on the reconcile push path instead.
            const list = await adapter.listSessions({
              workspaceId: command.workspaceId
            });
            engine.dispatch({
              sessions: list.sessions,
              type: "session/snapshotReceived"
            });
            return list;
          }
          case "session/reconcile":
            return input.reconcileSession(command, options?.signal);
          case "session/unactivate":
            return input.unactivateSession({
              agentSessionId: command.agentSessionId,
              workspaceId: command.workspaceId
            });
        }
      },
      observe: (command) =>
        observeWorkspaceAgentEngineCommand(input.activityEventObserver, command)
    },
    identity: {
      origin: AGENT_SESSION_ENGINE_LOCAL_ORIGIN,
      workspaceId: input.workspaceId
    },
    ...(input.activityEventObserver
      ? {
          intentObserver: input.activityEventObserver.observeIntent.bind(
            input.activityEventObserver
          )
        }
      : {}),
    scheduler: {
      schedule(delayMs, task) {
        const timer = setTimeout(task, delayMs);
        return { cancel: () => clearTimeout(timer) };
      }
    }
  });
  const unsubscribeSessionEvents = input.subscribeSessionEvents(
    input.workspaceId,
    (event) => {
      if (!event || typeof event !== "object") return;
      const candidate = event as {
        eventType?: unknown;
        data?: { agentSessionId?: unknown; commands?: unknown };
      };
      if (
        candidate.eventType !== "available_commands_update" ||
        typeof candidate.data?.agentSessionId !== "string" ||
        !Array.isArray(candidate.data.commands)
      )
        return;
      engine.dispatch({
        agentSessionId: candidate.data.agentSessionId,
        commands: candidate.data.commands,
        type: "session/availableCommandsReceived",
        workspaceId: input.workspaceId
      });
    }
  );
  return {
    adapter,
    commandAdapter: adapter,
    engine,
    dispose() {
      unsubscribeSessionEvents();
      engine.dispose();
    }
  };
}

function observeWorkspaceAgentEngineCommand(
  observer: WorkspaceAgentSessionEngineActivityObserver | undefined,
  command: EngineExternalCommand
): void {
  try {
    observer?.observeCommand(command);
  } catch {
    // Recording is optional developer instrumentation. It must not block the
    // command that owns the actual product behavior.
  }
}

function requiredEngineEffectOptions(
  options: EngineEffectOptions | undefined
): EngineEffectOptions {
  if (!options)
    throw new Error("workspace_agent.engine_effect_options_required");
  return options;
}
