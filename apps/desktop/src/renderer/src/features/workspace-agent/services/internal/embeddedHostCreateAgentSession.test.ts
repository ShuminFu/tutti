import assert from "node:assert/strict";
import test from "node:test";
import type { AgentActivityCreateSessionInput } from "@tutti-os/agent-activity-core";
import type {
  TuttidClient,
  WorkspaceAgentSession
} from "@tutti-os/client-tuttid-ts";
import { createDesktopAgentActivityAdapter } from "../desktopAgentActivityAdapter.ts";
import { registerEmbeddedHostCreatedSessionOpener } from "./embeddedHostCreatedSessionOpener.ts";
import {
  isEmbeddedHostCreatedSession,
  resetEmbeddedHostCreatedSessionsForTest
} from "./embeddedHostCreatedSessionRegistry.ts";

const workspaceId = "workspace-1";
const hostOrigin = "http://wails.localhost";
const embeddedSearch =
  "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost";

const createInput = {
  // 本客户端发起激活时用的 id；宿主代建会返回另一个 id。
  agentSessionId: "client-requested-1",
  agentTargetId: "local:codex",
  clientSubmitId: "submit-host-create",
  cwd: "/workspace/project",
  initialContent: [{ type: "text" as const, text: "Build the feature" }],
  model: "gpt-5",
  reasoningEffort: "high",
  workspaceId
};

test("embedded host success opens the returned session and does not POST tuttid create", async () => {
  await withEmbeddedHost(
    {
      reply: (request) => ({
        type: "tutti-host-response",
        id: request.id,
        nonce: "nonce-1",
        result: { taskId: "task-1", agentSessionId: "host-session-1" }
      })
    },
    async ({ createCalls, getCalls, opened, posted }) => {
      resetEmbeddedHostCreatedSessionsForTest();
      const adapter = createHostCreateAdapter({ createCalls, getCalls });
      const session = await adapter.createSession(createInput);

      assert.equal(createCalls.length, 0);
      assert.deepEqual(opened, ["host-session-1"]);
      // 会话栏靠这条判据认出「宿主铸了新 id 的新会话」，否则它会被当成选中后
      // 拉详情，所在分组页永远不重取（Chats 恒空）。
      assert.equal(isEmbeddedHostCreatedSession("host-session-1"), true);
      assert.equal(isEmbeddedHostCreatedSession("client-requested-1"), false);
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
          thinkingLevel: "high",
          // 结构化首轮内容随请求过桥：只送 prompt 会让首轮里的图片在宿主侧消失。
          initialContent: [{ type: "text", text: "Build the feature" }]
        }
      ]);
    }
  );
});

// 首轮执行意图（权限 / 计划模式 / 隔离 / 结构化内容）必须逐字段过桥：宿主原样写进任务行，
// 任何一处被丢掉都等于按「用户没表态」重算一遍。
test("embedded host forwards an explicit planMode false and the chosen permission mode", async () => {
  await withEmbeddedHost(
    {
      reply: successfulHostReply
    },
    async ({ createCalls, posted }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      const input: AgentActivityCreateSessionInput = {
        ...createInput,
        permissionModeId: "plan",
        planMode: false
      };
      await adapter.createSession(input);

      const args = hostCreateArgs(posted);
      assert.equal(args.permissionModeId, "plan");
      // 显式 false 必须作为字段存在：整段省略会让这次表态静默失效。
      assert.equal("planMode" in args, true);
      assert.equal(args.planMode, false);
    }
  );
});

test("embedded host forwards worktree isolation together with its project placement", async () => {
  await withEmbeddedHost(
    {
      reply: successfulHostReply
    },
    async ({ createCalls, getCalls, posted }) => {
      const adapter = createHostCreateAdapter({ createCalls, getCalls });
      const input: AgentActivityCreateSessionInput = {
        ...createInput,
        isolation: "worktree",
        railPlacement: {
          version: 1,
          kind: "project",
          projectPath: "/workspace/project",
          sectionKey: "project:/workspace/project"
        }
      };
      await adapter.createSession(input);

      const args = hostCreateArgs(posted);
      // 少了 railPlacement，tuttid 建不了 worktree，这次「隔离」就会静默落成原目录。
      assert.equal(args.isolation, "worktree");
      assert.deepEqual(args.railPlacement, {
        version: 1,
        kind: "project",
        projectPath: "/workspace/project",
        sectionKey: "project:/workspace/project"
      });
    }
  );
});

test("embedded host rejects worktree isolation without a project placement", async () => {
  await withEmbeddedHost(
    {
      reply: successfulHostReply
    },
    async ({ createCalls, opened, posted }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      const withoutPlacement: AgentActivityCreateSessionInput = {
        ...createInput,
        isolation: "worktree"
      };
      const conversationsPlacement: AgentActivityCreateSessionInput = {
        ...createInput,
        isolation: "worktree",
        railPlacement: {
          version: 1,
          kind: "conversations",
          sectionKey: "conversations"
        }
      };
      await assert.rejects(
        adapter.createSession(withoutPlacement),
        isWorktreeProjectRequired
      );
      await assert.rejects(
        adapter.createSession(conversationsPlacement),
        isWorktreeProjectRequired
      );
      assert.equal(createCalls.length, 0);
      assert.deepEqual(opened, []);
      // 宿主请求根本没发出去：不建这条会话，好过静默降级成在原目录里跑。
      assert.equal(
        posted.some((message) => message.capability === "createAgentSession"),
        false
      );
    }
  );
});

test("image-only first turn keeps the prompt empty and carries the image block", async () => {
  await withEmbeddedHost(
    {
      reply: successfulHostReply
    },
    async ({ createCalls, posted }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      const input: AgentActivityCreateSessionInput = {
        ...createInput,
        initialContent: [
          {
            type: "image",
            mimeType: "image/png",
            path: "/workspace/project/screenshot.png",
            name: "screenshot.png"
          }
        ]
      };
      await adapter.createSession(input);

      const args = hostCreateArgs(posted);
      // 纯图片首轮没有文字块：prompt 留空，不编一句假话冒充用户说过的话。
      assert.equal(args.prompt, "");
      assert.deepEqual(args.initialContent, [
        {
          type: "image",
          mimeType: "image/png",
          path: "/workspace/project/screenshot.png",
          name: "screenshot.png"
        }
      ]);
    }
  );
});

test("image block with only an attachmentId is rejected instead of silently dropped", async () => {
  await withEmbeddedHost(
    {
      reply: successfulHostReply
    },
    async ({ createCalls, opened, posted }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      const input: AgentActivityCreateSessionInput = {
        ...createInput,
        initialContent: [
          {
            type: "image",
            mimeType: "image/png",
            attachmentId: "attachment-1"
          }
        ]
      };
      await assert.rejects(
        adapter.createSession(input),
        (error: Error & { code?: string }) =>
          error.code === "host_create_image_reference_missing"
      );
      assert.equal(createCalls.length, 0);
      assert.deepEqual(opened, []);
      // 附件 id 属于另一条会话，搬过去只会指向查无此物：宁可明确失败，也不静默丢图。
      assert.equal(
        posted.some((message) => message.capability === "createAgentSession"),
        false
      );
    }
  );
});

// 与既有用例同形的宿主成功应答：宿主铸了新 id，adapter 走「打开返回的会话」那条路。
function successfulHostReply(request: HostRequest): Record<string, unknown> {
  return {
    type: "tutti-host-response",
    id: request.id,
    nonce: "nonce-1",
    result: { taskId: "task-1", agentSessionId: "host-session-1" }
  };
}

function hostCreateArgs(posted: HostRequest[]): Record<string, unknown> {
  const request = posted.find(
    (message) =>
      message.type === "tutti-host-request" &&
      message.capability === "createAgentSession"
  );
  assert.ok(request, "expected a createAgentSession host request");
  const args: unknown = request.args;
  if (!Array.isArray(args) || args.length !== 1) {
    assert.fail("expected exactly one host create argument");
  }
  const first: unknown = args[0];
  if (first === null || typeof first !== "object") {
    assert.fail("expected the host create argument to be an object");
  }
  return first as Record<string, unknown>;
}

function isWorktreeProjectRequired(error: unknown): boolean {
  return (
    error instanceof Error &&
    (error as Error & { code?: string }).code ===
      "host_create_worktree_project_required"
  );
}

test("embedded host unsupported does not create an unmanaged session", async () => {
  await withEmbeddedHost(
    {
      reply: (request) => ({
        type: "tutti-host-response",
        id: request.id,
        nonce: "nonce-1",
        error: "unsupported"
      })
    },
    async ({ createCalls, opened }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      await assert.rejects(adapter.createSession(createInput), /unsupported/);
      assert.equal(createCalls.length, 0);
      assert.deepEqual(opened, []);
    }
  );
});

test("embedded host timeout does not duplicate a possibly queued session", async () => {
  await withEmbeddedHost(
    {
      reply: null,
      fireTimeout: true
    },
    async ({ createCalls, opened }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      await assert.rejects(adapter.createSession(createInput), /timed out/);
      assert.equal(createCalls.length, 0);
      assert.deepEqual(opened, []);
    }
  );
});

test("not embedded skips the host bridge and uses original create", async () => {
  await withEmbeddedHost(
    {
      search: "?workspace=one",
      reply: (request) => ({
        type: "tutti-host-response",
        id: request.id,
        nonce: "nonce-1",
        result: { taskId: "task-1", agentSessionId: "host-session-1" }
      })
    },
    async ({ createCalls, opened, posted }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      const session = await adapter.createSession(createInput);

      assert.equal(createCalls.length, 1);
      assert.deepEqual(opened, []);
      assert.equal(
        posted.some((message) => message.capability === "createAgentSession"),
        false
      );
      assert.equal(session.agentSessionId, createCalls[0]?.body.agentSessionId);
    }
  );
});

test("embedded host error with code does not fall back and preserves code", async () => {
  await withEmbeddedHost(
    {
      reply: (request) => ({
        type: "tutti-host-response",
        id: request.id,
        nonce: "nonce-1",
        error: "provider is not registered",
        code: "provider_unregistered"
      })
    },
    async ({ createCalls, opened }) => {
      const adapter = createHostCreateAdapter({ createCalls });
      await assert.rejects(
        adapter.createSession(createInput),
        (error: Error & { code?: string }) =>
          error.message === "provider is not registered" &&
          error.code === "provider_unregistered"
      );
      assert.equal(createCalls.length, 0);
      assert.deepEqual(opened, []);
    }
  );
});

async function withEmbeddedHost(
  options: {
    search?: string;
    reply: ((request: HostRequest) => Record<string, unknown>) | null;
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
