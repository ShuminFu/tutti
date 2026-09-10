import assert from "node:assert/strict";
import test from "node:test";
import type { TuttidClient, WorkspaceAgentSession } from "@tutti-os/client-tuttid-ts";
import { createDesktopAgentActivityAdapter } from "../desktopAgentActivityAdapter.ts";
import { registerEmbeddedHostCreatedSessionOpener } from "./embeddedHostCreatedSessionOpener.ts";

const workspaceId = "workspace-1";
const hostOrigin = "http://wails.localhost";
const embeddedSearch =
  "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost";

const createInput = {
  agentTargetId: "local:codex",
  clientSubmitId: "submit-host-create",
  cwd: "/workspace/project",
  initialContent: [{ type: "text" as const, text: "Build the feature" }],
  model: "gpt-5",
  reasoningEffort: "high",
  workspaceId
};

test("embedded host success opens the returned session and does not POST tuttid create", async () => {
  await withEmbeddedHost({
    reply: (request) => ({
      type: "tutti-host-response",
      id: request.id,
      nonce: "nonce-1",
      result: { taskId: "task-1", agentSessionId: "host-session-1" }
    })
  }, async ({ createCalls, getCalls, opened, posted }) => {
    const adapter = createHostCreateAdapter({ createCalls, getCalls });
    const session = await adapter.createSession(createInput);

    assert.equal(createCalls.length, 0);
    assert.deepEqual(opened, ["host-session-1"]);
    assert.equal(session.agentSessionId, "host-session-1");
    assert.equal(getCalls[0]?.agentSessionId, "host-session-1");
    const request = posted.find(
      (message) =>
        message.type === "tutti-host-request" &&
        message.capability === "createAgentSession"
    );
    assert.deepEqual(request?.args, [
      {
        provider: "codex",
        cwd: "/workspace/project",
        prompt: "Build the feature",
        model: "gpt-5",
        thinkingLevel: "high"
      }
    ]);
  });
});

test("embedded host unsupported falls back to original create once", async () => {
  await withEmbeddedHost({
    reply: (request) => ({
      type: "tutti-host-response",
      id: request.id,
      nonce: "nonce-1",
      error: "unsupported"
    })
  }, async ({ createCalls, opened }) => {
    const adapter = createHostCreateAdapter({ createCalls });
    const session = await adapter.createSession(createInput);

    assert.equal(createCalls.length, 1);
    assert.deepEqual(opened, []);
    assert.equal(session.agentSessionId, createCalls[0]?.body.agentSessionId);
  });
});

test("embedded host timeout falls back to original create", async () => {
  await withEmbeddedHost({
    reply: null,
    fireTimeout: true
  }, async ({ createCalls, opened }) => {
    const adapter = createHostCreateAdapter({ createCalls });
    const session = await adapter.createSession(createInput);

    assert.equal(createCalls.length, 1);
    assert.deepEqual(opened, []);
    assert.equal(session.agentSessionId, createCalls[0]?.body.agentSessionId);
  });
});

test("not embedded skips the host bridge and uses original create", async () => {
  await withEmbeddedHost({
    search: "?workspace=one",
    reply: (request) => ({
      type: "tutti-host-response",
      id: request.id,
      nonce: "nonce-1",
      result: { taskId: "task-1", agentSessionId: "host-session-1" }
    })
  }, async ({ createCalls, opened, posted }) => {
    const adapter = createHostCreateAdapter({ createCalls });
    const session = await adapter.createSession(createInput);

    assert.equal(createCalls.length, 1);
    assert.deepEqual(opened, []);
    assert.equal(
      posted.some((message) => message.capability === "createAgentSession"),
      false
    );
    assert.equal(session.agentSessionId, createCalls[0]?.body.agentSessionId);
  });
});

test("embedded host error with code does not fall back and preserves code", async () => {
  await withEmbeddedHost({
    reply: (request) => ({
      type: "tutti-host-response",
      id: request.id,
      nonce: "nonce-1",
      error: "provider is not registered",
      code: "provider_unregistered"
    })
  }, async ({ createCalls, opened }) => {
    const adapter = createHostCreateAdapter({ createCalls });
    await assert.rejects(
      adapter.createSession(createInput),
      (error: Error & { code?: string }) =>
        error.message === "provider is not registered" &&
        error.code === "provider_unregistered"
    );
    assert.equal(createCalls.length, 0);
    assert.deepEqual(opened, []);
  });
});

async function withEmbeddedHost(
  options: {
    search?: string;
    reply:
      | ((request: HostRequest) => Record<string, unknown>)
      | null;
    fireTimeout?: boolean;
  },
  run: (harness: {
    createCalls: CreateCall[];
    getCalls: Array<{ agentSessionId: string; workspaceId: string }>;
    opened: string[];
    posted: HostRequest[];
  }) => Promise<void>
): Promise<void> {
  const previousWindow = globalThis.window;
  const previousLocation = globalThis.location;
  let messageListener: ((event: MessageEvent) => void) | null = null;
  let timeoutCallback: (() => void) | null = null;
  const posted: HostRequest[] = [];
  const opened: string[] = [];
  const createCalls: CreateCall[] = [];
  const getCalls: Array<{ agentSessionId: string; workspaceId: string }> = [];
  const parent = {
    postMessage(message: HostRequest, _origin: string) {
      posted.push(message);
      if (!options.reply || message.capability !== "createAgentSession") {
        return;
      }
      const response = options.reply(message);
      queueMicrotask(() =>
        messageListener?.({
          data: response,
          origin: hostOrigin,
          source: parent
        } as MessageEvent)
      );
    }
  } as unknown as WindowProxy;
  const windowRef = {
    addEventListener(type: string, listener: EventListener) {
      if (type === "message") {
        messageListener = listener as (event: MessageEvent) => void;
      }
    },
    clearTimeout() {
      timeoutCallback = null;
    },
    location: { search: options.search ?? embeddedSearch },
    parent,
    removeEventListener() {},
    setTimeout(callback: () => void) {
      timeoutCallback = callback;
      return 1;
    }
  } as unknown as Window;
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: windowRef
  });
  Object.defineProperty(globalThis, "location", {
    configurable: true,
    value: windowRef.location
  });
  const unregisterOpener = registerEmbeddedHostCreatedSessionOpener((id) => {
    opened.push(id);
    return true;
  });
  try {
    const pending = run({ createCalls, getCalls, opened, posted });
    if (options.fireTimeout) {
      await Promise.resolve();
      timeoutCallback?.();
    }
    await pending;
  } finally {
    unregisterOpener();
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: previousWindow
    });
    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: previousLocation
    });
  }
}

function createHostCreateAdapter(input: {
  createCalls: CreateCall[];
  getCalls?: Array<{ agentSessionId: string; workspaceId: string }>;
}) {
  return createDesktopAgentActivityAdapter({
    runtimeApi: {
      logTerminalDiagnostic() {
        return Promise.resolve();
      }
    },
    tuttidClient: {
      async createWorkspaceAgentSession(requestWorkspaceId, body) {
        input.createCalls.push({ workspaceId: requestWorkspaceId, body });
        return hostCreatedSession(body.agentSessionId);
      },
      async getWorkspaceAgentSession(requestWorkspaceId, agentSessionId) {
        input.getCalls?.push({
          agentSessionId,
          workspaceId: requestWorkspaceId
        });
        return {
          childSessions: [],
          editRetry: {
            availableActions: [],
            eligible: false,
            historyRevision: 0,
            supported: false
          },
          lifecycleCapabilitiesProjected: true,
          projection: "full" as const,
          session: hostCreatedSession(agentSessionId),
          turns: []
        };
      }
    } as unknown as TuttidClient
  });
}

function hostCreatedSession(id: string): WorkspaceAgentSession {
  return {
    activeTurn: null,
    activeTurnId: null,
    agentTargetId: "local:codex",
    capabilities: null,
    createdAtUnixMs: 1,
    cwd: "/workspace/project",
    endedAtUnixMs: null,
    forkedFrom: null,
    goal: null,
    goalSyncState: null,
    id,
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
    title: "Codex",
    tuttiModeActivation: null,
    updatedAtUnixMs: 2,
    usage: null,
    visible: true
  };
}

interface HostRequest {
  args?: unknown;
  capability?: unknown;
  id?: unknown;
  nonce?: unknown;
  type?: unknown;
}

interface CreateCall {
  body: { agentSessionId: string };
  workspaceId: string;
}
