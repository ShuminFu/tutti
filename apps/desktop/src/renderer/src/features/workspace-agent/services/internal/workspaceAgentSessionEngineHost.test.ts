import assert from "node:assert/strict";
import test from "node:test";
import type {
  AgentActivitySession,
  EngineExternalCommand,
  EngineIntent,
  SessionAcknowledgeForkObservedCommand,
  TuttiModeActivationUpdateCommand
} from "@tutti-os/agent-activity-core";
import {
  normalizeAgentActivitySession,
  selectSessionGoalControlSettlement
} from "@tutti-os/agent-activity-core";
import type {
  TuttidClient,
  WorkspaceAgentSession
} from "@tutti-os/client-tuttid-ts";
import { createDesktopAgentActivityAdapter } from "../desktopAgentActivityAdapter.ts";
import {
  createWorkspaceAgentSessionEngineHost,
  executeWorkspaceAgentForkObservedAckCommand,
  reconcileHostMintedActivation,
  executeWorkspaceAgentTuttiModeUpdateCommand
} from "./workspaceAgentSessionEngineHost.ts";

test("fork observation ACK forwards the durable operation identity and abort signal", async () => {
  const controller = new AbortController();
  const calls: unknown[] = [];
  const command = {
    commandId: "ack-command",
    correlationId: "local-mutation",
    operationId: "durable-operation",
    timeoutMs: 10_000,
    type: "session/ackForkObserved",
    workspaceId: "workspace-1"
  } satisfies SessionAcknowledgeForkObservedCommand;

  await executeWorkspaceAgentForkObservedAckCommand(
    {
      async acknowledgeWorkspaceAgentSessionForkOperation(
        workspaceId,
        operationId,
        options
      ) {
        calls.push({ operationId, options, workspaceId });
        return { acknowledged: true };
      }
    },
    command,
    controller.signal
  );

  assert.deepEqual(calls, [
    {
      operationId: "durable-operation",
      options: { signal: controller.signal },
      workspaceId: "workspace-1"
    }
  ]);
});

test("Tutti mode update command preserves CAS revision and zero preferences", async () => {
  const controller = new AbortController();
  let received: unknown;
  await executeWorkspaceAgentTuttiModeUpdateCommand(
    {
      updateTuttiModeActivation: async (input) => {
        received = input;
        return {} as never;
      }
    },
    {
      agentSessionId: "session-1",
      commandId: "tutti-1",
      expectedRevision: 3,
      effect: 0,
      speed: 0,
      source: "slash_command",
      status: "active",
      type: "tuttiMode/update",
      workspaceId: "workspace-1"
    } satisfies TuttiModeActivationUpdateCommand,
    controller.signal
  );

  assert.deepEqual(received, {
    agentSessionId: "session-1",
    expectedRevision: 3,
    effect: 0,
    speed: 0,
    signal: controller.signal,
    source: "slash_command",
    status: "active",
    workspaceId: "workspace-1"
  });
});

test("workspace engine host sends public intents and command settlements to the observer", async () => {
  const commands: EngineExternalCommand[] = [];
  const intents: EngineIntent[] = [];
  const host = createWorkspaceAgentSessionEngineHost({
    activityEventObserver: {
      observeCommand: (command) => commands.push(command),
      observeIntent: (intent) => intents.push(intent)
    },
    executeEngineActivateSession: async () => ({}) as never,
    executeEngineCancelTurn: async () => ({}),
    executeEngineGoalControl: async () => ({}) as never,
    reconcileSession: async () => ({}),
    restorePendingSessionRecording() {},
    runtimeApi: {
      logTerminalDiagnostic: async () => {}
    },
    executeEngineSendInput: async () => ({ ok: true }),
    executeEngineSubmitInteractive: async () => ({}) as never,
    executeEngineSubmitPlanDecision: async () => ({}) as never,
    subscribeSessionEvents: () => () => {},
    takePendingSessionRecording: () => null,
    tuttidClient: {} as TuttidClient,
    unactivateSession: async () => ({}) as never,
    executeEngineUpdateSessionSettings: async () => ({}) as never,
    updateTuttiModeActivation: async () => ({}) as never,
    workspaceId: "workspace-1"
  });

  host.engine.dispatch({ probeId: "probe-1", type: "engine/probeRequested" });
  await new Promise<void>((resolve) => setImmediate(resolve));

  assert.deepEqual(
    commands.map((command) => command.type),
    ["engine/probe"]
  );
  assert.deepEqual(
    intents.map((intent) => intent.type),
    ["engine/probeRequested", "engine/commandResult"]
  );
  host.dispose();
});

test("desktop host follows the shared settings-before-send workflow", async () => {
  const operations: string[] = [];
  const updatedSession = session({ browserUse: true, updatedAtUnixMs: 2 });
  const host = createWorkspaceAgentSessionEngineHost({
    executeEngineActivateSession: async () => ({}) as never,
    executeEngineCancelTurn: async () => ({}),
    executeEngineGoalControl: async () => ({}) as never,
    reconcileSession: async () => ({}),
    restorePendingSessionRecording() {},
    runtimeApi: {
      logTerminalDiagnostic: async () => {}
    },
    executeEngineSendInput: async () => {
      operations.push("send");
      throw new Error("send rejected");
    },
    executeEngineSubmitInteractive: async () => ({}) as never,
    executeEngineSubmitPlanDecision: async () => ({}) as never,
    subscribeSessionEvents: () => () => {},
    takePendingSessionRecording: () => null,
    tuttidClient: {} as TuttidClient,
    unactivateSession: async () => ({}) as never,
    executeEngineUpdateSessionSettings: async () => {
      operations.push("settings");
      return {
        agentSessionId: "session-1",
        session: updatedSession,
        settings: { browserUse: true }
      };
    },
    updateTuttiModeActivation: async () => ({}) as never,
    workspaceId: "workspace-1"
  });
  host.engine.dispatch({
    sessions: [session({ browserUse: false })],
    type: "session/snapshotReceived"
  });
  host.engine.dispatch({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "browse", type: "text" }],
    expiresAtUnixMs: 120_000,
    requestedAtUnixMs: 1,
    requiredSettingsPatch: { browserUse: true },
    type: "submit/requested",
    workspaceId: "workspace-1"
  });
  await new Promise<void>((resolve) => setImmediate(resolve));
  await new Promise<void>((resolve) => setImmediate(resolve));

  assert.deepEqual(operations, ["settings", "send"]);
  assert.equal(
    host.engine.getSnapshot().sessionLifecycle.sessionsById["session-1"]
      ?.settings.browserUse,
    true
  );
  host.dispose();
});

test("desktop host lets the Engine apply Goal Control transport results", async () => {
  const calls: Array<
    Parameters<TuttidClient["goalControlWorkspaceAgentSession"]>
  > = [];
  const tuttidClient = {
    async goalControlWorkspaceAgentSession(
      ...args: Parameters<TuttidClient["goalControlWorkspaceAgentSession"]>
    ) {
      calls.push(args);
      return {
        goal: { objective: "ship it", status: "active" as const },
        operationId: "operation-1",
        session: tuttidSession({
          objective: "ship it",
          status: "active"
        })
      };
    }
  } as TuttidClient;
  const runtimeApi = {
    logTerminalDiagnostic: async () => {}
  };
  const adapter = createDesktopAgentActivityAdapter({
    runtimeApi,
    tuttidClient
  });
  const host = createWorkspaceAgentSessionEngineHost({
    executeEngineActivateSession: async () => ({}) as never,
    executeEngineCancelTurn: async () => ({}),
    executeEngineGoalControl: (input, options) =>
      options === undefined
        ? adapter.goalControl(input)
        : adapter.goalControl(input, options),
    reconcileSession: async () => ({}),
    restorePendingSessionRecording() {},
    runtimeApi,
    executeEngineSendInput: async () => ({ ok: true }),
    executeEngineSubmitInteractive: async () => ({}) as never,
    executeEngineSubmitPlanDecision: async () => ({}) as never,
    subscribeSessionEvents: () => () => {},
    takePendingSessionRecording: () => null,
    tuttidClient,
    unactivateSession: async () => ({}) as never,
    executeEngineUpdateSessionSettings: async () => ({}) as never,
    updateTuttiModeActivation: async () => ({}) as never,
    workspaceId: "workspace-1"
  });
  host.engine.dispatch({
    session: session({}),
    type: "session/upserted"
  });

  assert.equal(
    host.engine.controlGoal({
      action: "set",
      agentSessionId: "session-1",
      clientSubmitId: "goal-submit-1",
      objective: "ship it"
    }).accepted,
    true
  );
  await new Promise<void>((resolve) => setImmediate(resolve));

  assert.deepEqual(calls[0]?.slice(0, 3), [
    "workspace-1",
    "session-1",
    {
      action: "set",
      clientSubmitId: "goal-submit-1",
      objective: "ship it"
    }
  ]);
  assert.deepEqual(
    host.engine.getSnapshot().sessionLifecycle.sessionsById["session-1"]?.goal,
    { objective: "ship it", status: "active" }
  );
  assert.equal(
    selectSessionGoalControlSettlement(host.engine.getSnapshot(), "session-1")
      ?.status,
    "succeeded"
  );
  host.dispose();
});

test("workspace engine host still executes commands when the command observer fails", async () => {
  const host = createWorkspaceAgentSessionEngineHost({
    activityEventObserver: {
      observeCommand: () => {
        throw new Error("recorder failed");
      },
      observeIntent: () => {}
    },
    executeEngineActivateSession: async () => ({}) as never,
    executeEngineCancelTurn: async () => ({}),
    executeEngineGoalControl: async () => ({}) as never,
    reconcileSession: async () => ({}),
    restorePendingSessionRecording() {},
    runtimeApi: {
      logTerminalDiagnostic: async () => {}
    },
    executeEngineSendInput: async () => ({ ok: true }),
    executeEngineSubmitInteractive: async () => ({}) as never,
    executeEngineSubmitPlanDecision: async () => ({}) as never,
    subscribeSessionEvents: () => () => {},
    takePendingSessionRecording: () => null,
    tuttidClient: {} as TuttidClient,
    unactivateSession: async () => ({}) as never,
    executeEngineUpdateSessionSettings: async () => ({}) as never,
    updateTuttiModeActivation: async () => ({}) as never,
    workspaceId: "workspace-1"
  });

  host.engine.dispatch({ probeId: "probe-1", type: "engine/probeRequested" });
  await new Promise<void>((resolve) => setImmediate(resolve));

  assert.equal(
    host.engine.getSnapshot().engineRuntime.lastCommandResult?.outcome,
    "succeeded"
  );
  host.dispose();
});

// 回归（坑142）：嵌入态宿主代建会话铸了新 id，请求 id 的 pending 记录必须被撤掉，
// 否则它以 uncertain 留在引擎里，会话栏对同一次对话画两条。
test("host-minted session id dismisses the stale pending activation and upserts the real session", async () => {
  const hostMinted = normalizeAgentActivitySession({
    activeTurn: null,
    activeTurnId: null,
    agentSessionId: "host-minted-1",
    agentTargetId: "target-1",
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "grok",
    settings: {},
    title: "你现在在哪个目录?",
    updatedAtUnixMs: 5,
    workspaceId: "workspace-1"
  });
  const host = createWorkspaceAgentSessionEngineHost({
    executeEngineActivateSession: async () => ({
      activation: { mode: "new", status: "attached" },
      session: hostMinted
    }),
    executeEngineCancelTurn: async () => ({}),
    executeEngineGoalControl: async () => ({}) as never,
    reconcileSession: async () => ({}),
    restorePendingSessionRecording() {},
    runtimeApi: { logTerminalDiagnostic: async () => {} },
    executeEngineSendInput: async () => ({}) as never,
    executeEngineSubmitInteractive: async () => ({}) as never,
    executeEngineSubmitPlanDecision: async () => ({}) as never,
    subscribeSessionEvents: () => () => {},
    takePendingSessionRecording: () => null,
    tuttidClient: {} as TuttidClient,
    unactivateSession: async () => ({}) as never,
    executeEngineUpdateSessionSettings: async () => ({}) as never,
    updateTuttiModeActivation: async () => ({}) as never,
    workspaceId: "workspace-1"
  });
  host.engine.dispatch({
    agentSessionId: "client-requested-1",
    agentTargetId: "target-1",
    clientSubmitId: "submit-1",
    content: [{ text: "你现在在哪个目录?", type: "text" }],
    cwd: "/workspace",
    expiresAtUnixMs: 120_000,
    mode: "new",
    requestId: "activation:workspace-1:client-requested-1:1:1",
    requestedAtUnixMs: 1,
    type: "activation/requested",
    workspaceId: "workspace-1"
  });
  for (let i = 0; i < 4; i += 1) {
    await new Promise<void>((resolve) => setImmediate(resolve));
  }
  const snapshot = host.engine.getSnapshot();
  const staleRecords = Object.values(
    snapshot.pendingIntents.activationsByRequestId
  ).filter((record) => record.agentSessionId === "client-requested-1");
  assert.deepEqual(staleRecords, []);
  assert.equal(
    snapshot.sessionLifecycle.sessionsById["host-minted-1"]?.title,
    "你现在在哪个目录?"
  );
  host.dispose();
});

test("reconcileHostMintedActivation leaves same-id and existing-mode activations alone", () => {
  const dispatched: EngineIntent[] = [];
  const engine = {
    dispatch: (intent: EngineIntent) => {
      dispatched.push(intent);
    },
    getSnapshot: () =>
      ({
        pendingIntents: {
          activationsByRequestId: {
            "req-1": {
              agentSessionId: "same-1",
              mode: "new",
              requestId: "req-1"
            }
          }
        }
      }) as never
  };
  const same = session({});
  assert.equal(
    reconcileHostMintedActivation(engine, "session-1", {
      activation: { mode: "new", status: "attached" },
      session: same
    }),
    false
  );
  assert.equal(
    reconcileHostMintedActivation(engine, "other-1", {
      activation: { mode: "existing", status: "already_attached" },
      detail: {} as never,
      session: same
    }),
    false
  );
  assert.deepEqual(dispatched, []);
});

function session(
  overrides: Partial<AgentActivitySession["settings"]> & {
    updatedAtUnixMs?: number;
  }
): AgentActivitySession {
  const { updatedAtUnixMs = 1, ...settings } = overrides;
  return normalizeAgentActivitySession({
    activeTurn: null,
    activeTurnId: null,
    agentSessionId: "session-1",
    agentTargetId: "target-1",
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    settings,
    title: "Session",
    updatedAtUnixMs,
    workspaceId: "workspace-1"
  });
}

function tuttidSession(
  goal: WorkspaceAgentSession["goal"]
): WorkspaceAgentSession {
  return {
    activeTurn: null,
    activeTurnId: null,
    agentTargetId: "target-1",
    capabilities: null,
    createdAtUnixMs: 1,
    cwd: "/workspace",
    endedAtUnixMs: null,
    forkedFrom: null,
    goal,
    goalSyncState: null,
    id: "session-1",
    imported: false,
    kind: "root",
    latestTurn: null,
    latestTurnInteractions: [],
    lifecycleCapabilities: { fork: false, forkThroughTurn: false },
    messageVersion: 0,
    parentAgentSessionId: null,
    parentToolCallId: null,
    parentTurnId: null,
    pendingInteractions: [],
    permissionConfig: { configurable: false, modes: [] },
    pinnedAtUnixMs: null,
    provider: "codex",
    providerSessionId: null,
    railSectionKey: "conversations",
    resumable: true,
    rootAgentSessionId: null,
    rootTurnId: null,
    settings: {},
    title: "Session",
    tuttiModeActivation: null,
    updatedAtUnixMs: 2,
    usage: null,
    visible: true
  };
}
