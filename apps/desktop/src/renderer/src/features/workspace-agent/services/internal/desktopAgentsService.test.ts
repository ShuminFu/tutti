import assert from "node:assert/strict";
import test from "node:test";
import type { AgentTarget } from "@tutti-os/client-tuttid-ts";
import {
  DesktopAgentsService,
  mapAgentTargetsToPresentations,
  mapAgentTargetPresentationsToAgents
} from "./desktopAgentsService.ts";

test("desktop agents service publishes explicit idle, loading, and ready lifecycle snapshots", async () => {
  const request = createDeferred<{ targets: AgentTarget[] }>();
  const service = new DesktopAgentsService({
    now: () => 1780272000123,
    tuttidClient: {
      listAgentTargets: () => request.promise
    }
  });
  const snapshots: string[] = [];
  service.subscribe(() => {
    snapshots.push(service.getSnapshot().status);
  });

  assert.equal(service.getSnapshot().status, "idle");
  const loadPromise = service.load();
  assert.equal(service.getSnapshot().status, "loading");

  request.resolve({
    targets: [
      createAgentTarget({
        id: "local:codex",
        heroImageUrl: "data:image/jpeg;base64,hero",
        name: "Codex",
        provider: "codex",
        sortOrder: 10
      })
    ]
  });
  const snapshot = await loadPromise;

  assert.equal(snapshot.status, "ready");
  assert.equal(snapshot.error, null);
  assert.equal(snapshot.capturedAtUnixMs, 1780272000123);
  assert.deepEqual(snapshots, ["loading", "ready"]);
});

test("desktop agents service publishes failures, retains cached data, and owns retry scheduling", async () => {
  const retryCallbacks: Array<() => void> = [];
  let shouldFail = false;
  const target = createAgentTarget({
    id: "local:codex",
    name: "Codex",
    provider: "codex",
    sortOrder: 10
  });
  const service = new DesktopAgentsService({
    retryDelayMs: 25,
    setTimeout(callback) {
      retryCallbacks.push(callback);
      return 1 as unknown as ReturnType<typeof setTimeout>;
    },
    tuttidClient: {
      async listAgentTargets() {
        if (shouldFail) {
          throw new Error("directory unavailable");
        }
        return { targets: [target] };
      }
    }
  });

  const readySnapshot = await service.load();
  shouldFail = true;
  await assert.rejects(service.refresh(), /directory unavailable/);

  const errorSnapshot = service.getSnapshot();
  assert.equal(errorSnapshot.status, "error");
  assert.equal(errorSnapshot.error, "directory unavailable");
  assert.deepEqual(errorSnapshot.agents, readySnapshot.agents);
  assert.equal(retryCallbacks.length, 1);

  shouldFail = false;
  retryCallbacks[0]?.();
  await waitFor(() => service.getSnapshot().status === "ready");
  assert.equal(service.getSnapshot().error, null);
});

test("desktop agents service hydrates a detached-window bootstrap snapshot before refresh", () => {
  const service = new DesktopAgentsService({
    tuttidClient: {
      async listAgentTargets() {
        return { targets: [] };
      }
    }
  });
  const target = createAgentTarget({
    id: "local:codex",
    name: "Codex",
    provider: "codex",
    sortOrder: 10
  });
  const agentTargets = mapAgentTargetsToPresentations([target]);

  service.hydrate({
    agents: mapAgentTargetPresentationsToAgents(agentTargets),
    agentTargets,
    capturedAtUnixMs: 1780272000000,
    error: null,
    status: "ready"
  });

  assert.equal(service.getSnapshot().status, "ready");
  assert.equal(service.getSnapshot().agents[0]?.agentTargetId, "local:codex");
});

test("desktop agents service maps enabled daemon targets into the AgentGUI agents directory", () => {
  const presentations = mapAgentTargetsToPresentations(
    [
      createAgentTarget({
        enabled: false,
        id: "local:claude-code",
        name: "Claude Code",
        provider: "claude-code",
        sortOrder: 20
      }),
      createAgentTarget({
        id: "local:codex",
        heroImageUrl: "data:image/jpeg;base64,hero",
        sidebarIconUrl: "data:image/svg+xml;base64,sidebar",
        iconKey: "codex-descriptor",
        name: "Codex",
        provider: "codex",
        sortOrder: 10
      })
    ],
    {
      resolveAgentTargetIconUrl: ({ iconKey, provider }) =>
        `tutti-asset://agent/${iconKey ?? provider}.png`
    }
  );

  assert.deepEqual(
    presentations.map((target) => ({
      agentTargetId: target.agentTargetId,
      iconUrl: target.iconUrl,
      heroImageUrl: target.heroImageUrl,
      sidebarIconUrl: target.sidebarIconUrl,
      launchRefType: target.launchRefType,
      provider: target.provider
    })),
    [
      {
        agentTargetId: "local:codex",
        iconUrl: "tutti-asset://agent/codex-descriptor.png",
        heroImageUrl: "data:image/jpeg;base64,hero",
        sidebarIconUrl: "data:image/svg+xml;base64,sidebar",
        launchRefType: "builtin_local",
        provider: "codex"
      },
      {
        agentTargetId: "local:claude-code",
        iconUrl: "tutti-asset://agent/claude-code.png",
        heroImageUrl: null,
        sidebarIconUrl: null,
        launchRefType: "builtin_local",
        provider: "claude-code"
      }
    ]
  );

  assert.deepEqual(mapAgentTargetPresentationsToAgents(presentations), [
    {
      agentTargetId: "local:codex",
      availability: { status: "ready" },
      iconUrl: "tutti-asset://agent/codex-descriptor.png",
      heroImageUrl: "data:image/jpeg;base64,hero",
      sidebarIconUrl: "data:image/svg+xml;base64,sidebar",
      name: "Codex",
      provider: "codex"
    }
  ]);
});

test("desktop agents service reuses an Extension sidebar icon as its identity while retaining its mask icon", () => {
  const presentations = mapAgentTargetsToPresentations([
    {
      createdAtUnixMs: 1780272000000,
      enabled: true,
      heroImageUrl: null,
      iconKey: "extension:gemini",
      iconUrl: "data:image/svg+xml;base64,mask",
      id: "extension:gemini",
      launchRef: {
        extensionInstallationId: "gemini@1.0.3",
        type: "agent_extension"
      },
      name: "Gemini CLI",
      provider: "acp:gemini",
      sidebarIconUrl: "data:image/svg+xml;base64,colored",
      sortOrder: 700,
      source: "system",
      updatedAtUnixMs: 1780272000000
    }
  ]);

  assert.deepEqual(presentations.map(selectIconPresentation), [
    {
      agentTargetId: "extension:gemini",
      iconUrl: "data:image/svg+xml;base64,colored",
      maskIconUrl: "data:image/svg+xml;base64,mask",
      sidebarIconUrl: "data:image/svg+xml;base64,colored"
    }
  ]);
  assert.deepEqual(
    mapAgentTargetPresentationsToAgents(presentations).map(
      selectIconPresentation
    ),
    [
      {
        agentTargetId: "extension:gemini",
        iconUrl: "data:image/svg+xml;base64,colored",
        maskIconUrl: "data:image/svg+xml;base64,mask",
        sidebarIconUrl: "data:image/svg+xml;base64,colored"
      }
    ]
  );
});

function selectIconPresentation(input: {
  agentTargetId: string;
  iconUrl: string;
  maskIconUrl?: string | null;
  sidebarIconUrl?: string | null;
}) {
  return {
    agentTargetId: input.agentTargetId,
    iconUrl: input.iconUrl,
    maskIconUrl: input.maskIconUrl ?? null,
    sidebarIconUrl: input.sidebarIconUrl ?? null
  };
}

function createAgentTarget(input: {
  enabled?: boolean;
  id: string;
  iconKey?: string | null;
  heroImageUrl?: string | null;
  sidebarIconUrl?: string | null;
  name: string;
  provider: "claude-code" | "codex";
  sortOrder: number;
}): AgentTarget {
  return {
    createdAtUnixMs: 1780272000000,
    enabled: input.enabled ?? true,
    iconKey: input.iconKey ?? null,
    heroImageUrl: input.heroImageUrl ?? null,
    sidebarIconUrl: input.sidebarIconUrl ?? null,
    id: input.id,
    launchRef: {
      provider: input.provider,
      type: "builtin_local"
    },
    name: input.name,
    provider: input.provider,
    sortOrder: input.sortOrder,
    source: "system",
    updatedAtUnixMs: 1780272000000
  };
}

function createDeferred<T>(): {
  promise: Promise<T>;
  resolve(value: T): void;
} {
  let resolvePromise!: (value: T) => void;
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve;
  });
  return { promise, resolve: resolvePromise };
}

async function waitFor(predicate: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  assert.fail("condition was not reached");
}
