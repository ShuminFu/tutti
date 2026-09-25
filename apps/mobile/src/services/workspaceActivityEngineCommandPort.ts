import type {
  AgentSessionActivateEffectInput,
  AgentSessionActivateEffectResult,
  AgentSessionEffectPort,
  AgentActivityCancelTurnInput,
  AgentActivityDeleteSessionsResult,
  AgentActivityGoalControlResult,
  AgentSessionGoalControlEffectInput,
  AgentActivitySendInput,
  AgentActivitySession,
  AgentActivitySessionDetailSnapshot,
  AgentActivitySessionSettings,
  AgentActivitySubmitInteractiveInput,
  EngineEffectOptions,
  EngineExtensionCommand,
  SessionReconcileCommand
} from "@tutti-os/agent-activity-core";
import {
  agentActivityComposerOptionsFromTuttidResult,
  agentActivitySessionFromTuttidSession,
  agentActivityTurnFromTuttidTurn,
  tuttiAgentSessionComposerSettingsFromActivity,
  tuttiCreateWorkspaceAgentSessionRequestFromActivation,
  tuttiSendWorkspaceAgentSessionInputRequestFromActivity
} from "@tutti-os/agent-activity-tuttid-adapter";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import { mobileLocale } from "../i18n";

interface WorkspaceActivityEngineCommandContext {
  client: TuttidClient;
  mapSession(
    session: Parameters<typeof agentActivitySessionFromTuttidSession>[1]
  ): AgentActivitySession;
  mapSessionDetail(
    expectedAgentSessionId: string,
    detail: Awaited<ReturnType<TuttidClient["getWorkspaceAgentSession"]>>
  ): AgentActivitySessionDetailSnapshot;
  mapGoalControlResult(
    response: Awaited<
      ReturnType<TuttidClient["goalControlWorkspaceAgentSession"]>
    >
  ): AgentActivityGoalControlResult;
  reconcileSession(
    command: SessionReconcileCommand,
    signal?: AbortSignal
  ): Promise<unknown>;
  reconcileWorkspace(): Promise<unknown>;
}

export function createWorkspaceActivityEffectPort(
  getContext: () => WorkspaceActivityEngineCommandContext
): AgentSessionEffectPort {
  return {
    activateSession: (input, options) =>
      activateSession(getContext(), input, options?.signal),
    cancelTurn: (input, options) =>
      cancelTurn(getContext(), input, options?.signal),
    controlGoal: (input, options) =>
      controlGoal(getContext(), input, options?.signal),
    deleteSessions: (input, options) =>
      deleteSessions(getContext(), input, options?.signal),
    renameSession: (input, options) =>
      renameSession(getContext(), input, options?.signal),
    respondToInteraction: (input, options) =>
      respondToInteraction(getContext(), input, options?.signal),
    sendInput: (input, options) =>
      sendPrompt(getContext(), input, options?.signal),
    setSessionPinned: (input, options) =>
      setSessionPinned(getContext(), input, options?.signal),
    updateSessionSettings: (input, options) =>
      updateSessionSettings(getContext(), input, options?.signal)
  };
}

function controlGoal(
  context: WorkspaceActivityEngineCommandContext,
  input: AgentSessionGoalControlEffectInput,
  signal?: AbortSignal
): Promise<AgentActivityGoalControlResult> {
  return context.client
    .goalControlWorkspaceAgentSession(
      input.workspaceId,
      input.agentSessionId,
      {
        action: input.action,
        clientSubmitId: input.clientSubmitId,
        ...(input.objective ? { objective: input.objective } : {})
      },
      ...requestOptionsArgs(signal)
    )
    .then(context.mapGoalControlResult);
}

export function executeWorkspaceActivityExtensionCommand(
  context: WorkspaceActivityEngineCommandContext,
  command: EngineExtensionCommand,
  options?: EngineEffectOptions
): Promise<unknown> {
  const signal = options?.signal;
  switch (command.type) {
    case "engine/probe":
      return Promise.resolve({ ok: true });
    case "engine/reconcileWorkspace":
      return context.reconcileWorkspace();
    case "session/reconcile":
      return context.reconcileSession(command, signal);
    case "composerOptions/load":
      return context.client
        .getAgentProviderComposerOptions(
          command.provider as Parameters<
            TuttidClient["getAgentProviderComposerOptions"]
          >[0],
          {
            agentTargetId: command.targetKey,
            ...(command.cwd ? { cwd: command.cwd } : {}),
            locale: mobileLocale,
            workspaceId: command.workspaceId,
            settings: tuttiAgentSessionComposerSettingsFromActivity(
              command.settings
            )
          },
          { signal }
        )
        .then((result) =>
          agentActivityComposerOptionsFromTuttidResult(command.provider, result)
        );
    case "attention/readState/read":
    case "attention/readState/write":
    case "session/ackForkObserved":
    case "session/forkThroughTurn":
    case "session/unactivate":
    case "tuttiMode/update":
    // Mobile does not expose edit and retry yet. Keep this explicit so the
    // shared Engine command union cannot silently reach the exhaustive case.
    case "turn/editRetry":
    case "turn/recoverEditRetry":
      return Promise.reject(
        new Error(`unsupported mobile agent command: ${command.type}`)
      );
    default:
      return assertNeverEngineCommand(command);
  }
}

function assertNeverEngineCommand(command: never): never {
  throw new Error(
    `unhandled mobile agent command: ${(command as { type?: unknown }).type}`
  );
}

async function activateSession(
  context: WorkspaceActivityEngineCommandContext,
  input: AgentSessionActivateEffectInput,
  signal?: AbortSignal
): Promise<AgentSessionActivateEffectResult> {
  if (input.mode === "existing") {
    if (signal?.aborted) throw signal.reason;
    const detail = await context.client.getWorkspaceAgentSession(
      input.workspaceId,
      input.agentSessionId,
      undefined,
      ...requestOptionsArgs(signal)
    );
    const mapped = context.mapSessionDetail(input.agentSessionId, detail);
    return {
      activation: { mode: "existing", status: "already_attached" },
      detail: mapped,
      session: mapped.session
    };
  }
  const session = await context.client.createWorkspaceAgentSession(
    input.workspaceId,
    tuttiCreateWorkspaceAgentSessionRequestFromActivation(input),
    { signal }
  );
  const activitySession = context.mapSession(session);
  return {
    activation: { mode: "new", status: "attached" },
    session: activitySession
  };
}

function cancelTurn(
  context: WorkspaceActivityEngineCommandContext,
  input: AgentActivityCancelTurnInput,
  signal?: AbortSignal
): Promise<unknown> {
  return context.client
    .cancelWorkspaceAgentTurn(
      input.workspaceId,
      input.agentSessionId,
      input.turnId,
      ...requestOptionsArgs(signal)
    )
    .then((response) => ({
      ...response,
      ...(response.turn
        ? { turn: agentActivityTurnFromTuttidTurn(response.turn) }
        : {})
    }));
}

function deleteSessions(
  context: WorkspaceActivityEngineCommandContext,
  input: {
    agentSessionIds: readonly string[];
    workspaceId: string;
  },
  signal?: AbortSignal
): Promise<AgentActivityDeleteSessionsResult> {
  return context.client
    .deleteWorkspaceAgentSessionsBatch(
      input.workspaceId,
      { sessionIds: [...input.agentSessionIds] },
      ...requestOptionsArgs(signal)
    )
    .then((response) => ({
      cleanupFailedSessionIds: response.cleanupFailedSessionIds,
      removedMessages: response.removedMessages,
      removedSessionIds: response.removedSessionIds,
      removedSessions: response.removedSessions
    }));
}

function respondToInteraction(
  context: WorkspaceActivityEngineCommandContext,
  input: AgentActivitySubmitInteractiveInput,
  signal?: AbortSignal
): Promise<unknown> {
  return context.client
    .submitWorkspaceAgentInteractive(
      input.workspaceId,
      input.agentSessionId,
      input.requestId,
      {
        action: input.action ?? null,
        optionId: input.optionId ?? null,
        payload: input.payload ?? null,
        turnId: input.turnId
      },
      ...requestOptionsArgs(signal)
    )
    .then((session) => ({ session: context.mapSession(session) }));
}

function renameSession(
  context: WorkspaceActivityEngineCommandContext,
  input: {
    agentSessionId: string;
    title: string;
    workspaceId: string;
  },
  signal?: AbortSignal
): Promise<{ session: AgentActivitySession }> {
  return context.client
    .updateWorkspaceAgentSessionTitle(
      input.workspaceId,
      input.agentSessionId,
      { title: input.title },
      ...requestOptionsArgs(signal)
    )
    .then((session) => ({ session: context.mapSession(session) }));
}

function setSessionPinned(
  context: WorkspaceActivityEngineCommandContext,
  input: {
    agentSessionId: string;
    pinned: boolean;
    workspaceId: string;
  },
  signal?: AbortSignal
): Promise<{ session: AgentActivitySession }> {
  return context.client
    .updateWorkspaceAgentSessionPin(
      input.workspaceId,
      input.agentSessionId,
      { pinned: input.pinned },
      ...requestOptionsArgs(signal)
    )
    .then((session) => ({ session: context.mapSession(session) }));
}

function updateSessionSettings(
  context: WorkspaceActivityEngineCommandContext,
  input: {
    agentSessionId: string;
    settings: AgentActivitySessionSettings;
    workspaceId: string;
  },
  signal?: AbortSignal
): Promise<unknown> {
  return context.client
    .updateWorkspaceAgentSessionSettings(
      input.workspaceId,
      input.agentSessionId,
      tuttiAgentSessionComposerSettingsFromActivity(input.settings),
      ...requestOptionsArgs(signal)
    )
    .then((session) => {
      const activitySession = context.mapSession(session);
      return {
        agentSessionId: input.agentSessionId,
        session: activitySession,
        settings: activitySession.settings
      };
    });
}

async function sendPrompt(
  context: WorkspaceActivityEngineCommandContext,
  input: AgentActivitySendInput,
  signal?: AbortSignal
): Promise<unknown> {
  const result = await context.client.sendWorkspaceAgentSessionInput(
    input.workspaceId,
    input.agentSessionId,
    tuttiSendWorkspaceAgentSessionInputRequestFromActivity(input),
    ...requestOptionsArgs(signal)
  );
  if (result.kind === "goalControl") {
    return {
      kind: "goalControl",
      goal: result.goal ?? result.session.goal ?? null,
      session: context.mapSession(result.session)
    };
  }
  if (result.kind === "codexDesktopHeld") {
    return {
      kind: "codexDesktopHeld",
      session: context.mapSession(result.session)
    };
  }
  if (result.kind === "queued") {
    // Accepted, not dispatched: the daemon parked this prompt behind the
    // session's running turn and starts it when the slot frees.
    return {
      kind: "queued",
      session: context.mapSession(result.session),
      turnId: result.turnId
    };
  }
  return {
    kind: "turn",
    session: context.mapSession(result.session),
    turn: agentActivityTurnFromTuttidTurn(result.turn),
    turnId: result.turnId
  };
}

function requestOptionsArgs(
  signal: AbortSignal | undefined
): [] | [{ signal: AbortSignal }] {
  return signal === undefined ? [] : [{ signal }];
}
