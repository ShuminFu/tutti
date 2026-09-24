import assert from "node:assert/strict";
import test from "node:test";
import {
  conversationRailSplitHost,
  notifyConversationRailPeerPairsChanged,
  publishConversationRailPeerPairsSnapshot,
  subscribeConversationRailPeerPairsChanged
} from "@tutti-os/agent-gui/conversation-rail-projection";
import type { ConversationRailSplitDragSession } from "@tutti-os/agent-gui/conversation-rail-projection";
import { activateEmbeddedDintalDockSession } from "./embeddedDintalDock.ts";
import {
  createAgentGuiNodeSessionSource,
  createEmbeddedSplitViewController,
  embeddedSplitViewPaneDescriptor,
  parseEmbeddedSplitViewPaneDescriptor,
  registerEmbeddedSplitViewController,
  type EmbeddedSplitGeometry,
  type EmbeddedSplitViewController,
  type EmbeddedSplitViewHost
} from "./embeddedSplitView.ts";

interface FakeHostCall {
  args: unknown[];
  kind: "activate" | "close" | "focus" | "launch";
}

interface FakeHost {
  calls: FakeHostCall[];
  host: EmbeddedSplitViewHost;
  launched: string[];
  notify(): void;
  setSurfaceWidth(width: number): void;
}

function createFakeHost(
  input: { seed?: readonly string[]; surfaceWidth?: number } = {}
): FakeHost {
  const calls: FakeHostCall[] = [];
  const launched: string[] = [];
  const listeners = new Set<() => void>();
  let surfaceSize = { height: 800, width: input.surfaceWidth ?? 1200 };
  // seed = 开机时工作台里已经开着的窗口（真机里左栏那个）。不 seed 的用例沿用
  // 老写法：`adopt` 认领一个快照里并不存在的 id，`nodeById` 拿不到就不读会话号。
  let nodes: { data: { typeId: string }; id: string }[] = (
    input.seed ?? []
  ).map((id) => ({ data: { typeId: "agent-gui" }, id }));
  let nextNodeId = 0;
  const host: EmbeddedSplitViewHost = {
    activateNode(target, activation) {
      calls.push({ args: [target, activation], kind: "activate" });
    },
    closeNode(nodeId) {
      calls.push({ args: [nodeId], kind: "close" });
      nodes = nodes.filter((node) => node.id !== nodeId);
    },
    controller: {
      getSnapshot: () => ({ surfaceSize }),
      subscribe(listener) {
        listeners.add(listener);
        return () => listeners.delete(listener);
      }
    },
    focusNode(nodeId) {
      calls.push({ args: [nodeId], kind: "focus" });
    },
    getSnapshot: () => ({ nodes, surfaceSize }),
    async launchNode(launchInput) {
      const id = `agent-launched-${++nextNodeId}`;
      launched.push(id);
      calls.push({ args: [launchInput], kind: "launch" });
      nodes = [...nodes, { data: { typeId: "agent-gui" }, id }];
      return id;
    }
  };
  return {
    calls,
    host,
    launched,
    notify() {
      for (const listener of [...listeners]) listener();
    },
    setSurfaceWidth(width) {
      surfaceSize = { ...surfaceSize, width };
    }
  };
}

const geometry: EmbeddedSplitGeometry = {
  railRightEdge: () => 300,
  surfaceRect: () => ({ height: 800, left: 0, top: 0, width: 1200 })
};

function memoryStorage(seed: Record<string, string> = {}) {
  const map = new Map(Object.entries(seed));
  return {
    getItem: (key: string) => map.get(key) ?? null,
    map,
    setItem: (key: string, value: string) => {
      map.set(key, value);
    }
  };
}

function dragSession(id: string): ConversationRailSplitDragSession {
  return {
    iconUrl: null,
    id,
    isImported: false,
    provider: "claude",
    status: "working",
    title: id
  };
}

function makeController(
  fake: FakeHost,
  overrides: Partial<
    Parameters<typeof createEmbeddedSplitViewController>[0]
  > = {}
): EmbeddedSplitViewController {
  return createEmbeddedSplitViewController({
    geometry,
    host: fake.host,
    // 配对能力默认关掉：本文件只钉布局与窗口编排。
    pairingHost: () => null,
    storage: memoryStorage(),
    workspaceId: "ws-1",
    ...overrides
  });
}

function activations(fake: FakeHost): { nodeId: string; sessionId: string }[] {
  return fake.calls
    .filter(
      (call) =>
        call.kind === "activate" &&
        (call.args[1] as { type: string }).type === "agent-gui:open-session"
    )
    .map((call) => ({
      nodeId: (call.args[0] as { nodeId: string }).nodeId,
      sessionId: (call.args[1] as { payload: { agentSessionId: string } })
        .payload.agentSessionId
    }));
}

test("dropping onto a single pane launches a second window and activates it", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  fake.calls.length = 0;

  await controller.dropSession(dragSession("session-b"), "right");

  assert.equal(fake.launched.length, 1);
  assert.deepEqual(activations(fake), [
    { nodeId: fake.launched[0] as string, sessionId: "session-b" }
  ]);
  const snapshot = controller.getSnapshot();
  assert.deepEqual(snapshot.panes.left, {
    nodeId: "agent-left",
    session: null,
    sessionId: "session-a"
  });
  assert.deepEqual(snapshot.panes.right, {
    nodeId: fake.launched[0],
    session: {
      iconUrl: null,
      projectLabel: null,
      provider: "claude",
      title: "session-b"
    },
    sessionId: "session-b"
  });
  assert.equal(snapshot.focus, "right");
  controller.dispose();
});

test("dropping onto the occupied left half splits instead of taking over", async () => {
  // 真机（2026-09-11）：开着一条会话，把第二条拖到**左半边**，整个窗口被换成
  // 刚拖的那条、右栏根本没出现 —— 状态机把「落在左半」当成了换掉这一栏。
  // 左栏窗口是嵌入模式的锚（会话栏长在它上面），所以让会话对调而不是让窗口换边：
  // 左窗口 activate 成刚放下的那条，原来那条去新起的右窗口。
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  fake.calls.length = 0;

  await controller.dropSession(dragSession("session-b"), "left");

  assert.equal(fake.launched.length, 1);
  const snapshot = controller.getSnapshot();
  assert.equal(snapshot.panes.left?.nodeId, "agent-left");
  assert.equal(snapshot.panes.left?.sessionId, "session-b");
  assert.equal(snapshot.panes.right?.nodeId, fake.launched[0]);
  assert.equal(snapshot.panes.right?.sessionId, "session-a");
  assert.equal(snapshot.focus, "left");
  controller.dispose();
});

test("dropping a session that is already shown only moves focus", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  assert.equal(controller.getSnapshot().focus, "right");
  fake.calls.length = 0;
  const launchedBefore = fake.launched.length;

  await controller.dropSession(dragSession("session-a"), "right");

  assert.equal(fake.launched.length, launchedBefore);
  assert.deepEqual(activations(fake), []);
  assert.equal(controller.getSnapshot().focus, "left");
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  controller.dispose();
});

test("select while split replaces the focused pane through activateNode", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  const rightNodeId = fake.launched[0] as string;
  fake.calls.length = 0;

  controller.select("session-c");

  assert.deepEqual(activations(fake), [
    { nodeId: rightNodeId, sessionId: "session-c" }
  ]);
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-c");
  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  controller.dispose();
});

test("closing the right pane closes its window and keeps the left one", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  const rightNodeId = fake.launched[0] as string;
  fake.calls.length = 0;

  await controller.closePane("right");

  assert.deepEqual(
    fake.calls.filter((call) => call.kind === "close").map((call) => call.args),
    [[rightNodeId]]
  );
  assert.equal(controller.getSnapshot().panes.right, null);
  assert.deepEqual(controller.getSnapshot().panes.left, {
    nodeId: "agent-left",
    session: null,
    sessionId: "session-a"
  });
  assert.deepEqual(activations(fake), []);
  controller.dispose();
});

test("resize clamps both edges at the 320px minimum pane width", async () => {
  const fake = createFakeHost({ surfaceWidth: 1000 });
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");

  controller.resize(0.05);
  // 320/1000 与 1 - 320/1000，浮点算术的余数在这里无关紧要。
  assert.ok(Math.abs(controller.getSnapshot().ratio - 0.32) < 1e-9);
  controller.resize(0.95);
  assert.ok(Math.abs(controller.getSnapshot().ratio - 0.68) < 1e-9);
  controller.resize(0.4);
  assert.equal(controller.getSnapshot().ratio, 0.4);
  controller.resetRatio();
  assert.equal(controller.getSnapshot().ratio, 0.5);
  controller.dispose();
});

test("the collapsed flag follows the surface width and never touches storage", async () => {
  const fake = createFakeHost({ surfaceWidth: 1000 });
  const storage = memoryStorage();
  const controller = makeController(fake, { storage });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  assert.equal(controller.getSnapshot().collapsed, false);
  const persistedBeforeCollapse = storage.map.get(
    "agent-gui:split-layout:ws-1:all"
  );

  fake.setSurfaceWidth(500);
  fake.notify();
  assert.equal(controller.getSnapshot().collapsed, true);
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  assert.equal(
    storage.map.get("agent-gui:split-layout:ws-1:all"),
    persistedBeforeCollapse
  );

  fake.setSurfaceWidth(1000);
  fake.notify();
  assert.equal(controller.getSnapshot().collapsed, false);
  controller.dispose();
});

test("the host open-session funnel routes into select", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  const unregister = registerEmbeddedSplitViewController(controller);
  await controller.adopt(["agent-left"]);

  const bridgeHost = {
    activateNode() {
      throw new Error("the legacy single-window path must not be used");
    },
    closeNode() {},
    exitFullscreenNode() {},
    focusNode() {},
    getSnapshot: () => ({ nodeStack: [], nodes: [] }),
    launchNode: async () => null,
    load: async () => undefined
  };
  const opened = activateEmbeddedDintalDockSession(
    bridgeHost as never,
    "session-from-host"
  );

  assert.equal(opened, true);
  assert.equal(
    controller.getSnapshot().panes.left?.sessionId,
    "session-from-host"
  );
  unregister();
  controller.dispose();
});

test("the stored layout is restored on adopt and split across both windows", async () => {
  const fake = createFakeHost();
  const storage = memoryStorage({
    "agent-gui:split-layout:ws-1:all": JSON.stringify({
      focus: "right",
      left: "session-a",
      ratio: 0.6,
      right: "session-b"
    })
  });
  const controller = makeController(fake, { storage });

  await controller.adopt(["agent-left", "agent-right"]);

  assert.deepEqual(activations(fake), [
    { nodeId: "agent-right", sessionId: "session-b" },
    { nodeId: "agent-left", sessionId: "session-a" }
  ]);
  assert.equal(controller.getSnapshot().ratio, 0.6);
  assert.equal(controller.getSnapshot().focus, "right");
  assert.equal(controller.sideForNodeId("agent-right"), "right");
  controller.dispose();
});

test("a second adopted window is closed when the stored layout is single-pane", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);

  await controller.adopt(["agent-left", "agent-stale"]);

  assert.deepEqual(
    fake.calls.filter((call) => call.kind === "close").map((call) => call.args),
    [["agent-stale"]]
  );
  controller.dispose();
});

test("a conversation picked inside a window updates that pane's session id", async () => {
  const fake = createFakeHost();
  let observed: Record<string, string> = {};
  const controller = makeController(fake, {
    sessions: {
      read: (node) => observed[node.id] ?? null
    }
  });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  const rightNodeId = fake.launched[0] as string;

  observed = { [rightNodeId]: "session-picked-inside" };
  fake.notify();

  assert.equal(
    controller.getSnapshot().panes.right?.sessionId,
    "session-picked-inside"
  );
  controller.dispose();
});

test("adopting a window that already shows a session seeds the empty left pane", async () => {
  // 真机（坑：第一次拖进去仍是单栏）：开 DinTalDock 时 agent-gui 自己恢复了上次那条
  // 会话，没人调过 select，于是分栏层眼里左槽是空的。空左槽 + 拖到右半边 =
  // 状态机的 left-fill 不变式把它挪回左槽 —— 用户看到的是「还是一栏，而且当前
  // 窗口被换成了刚拖的那条」。认领窗口时就得把它正在显示的那条种进左槽。
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observedByNode = new Map<string, string>([["agent-left", "session-a"]]);
  const controller = makeController(fake, {
    sessions: { read: (node) => observedByNode.get(node.id) ?? null }
  });

  await controller.adopt(["agent-left"]);

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  fake.calls.length = 0;

  await controller.dropSession(dragSession("session-b"), "right");

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  assert.equal(fake.launched.length, 1);
  controller.dispose();
});

test("a session that only surfaces after adopt still fills the empty left pane", async () => {
  // 同一个坑的另一半：认领那一刻 agent-gui 可能还没恢复完，会话号要晚一步才读得到。
  // syncFromNodes 原来「槽是空的就跳过」，那一步就永远补不进来。
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observedByNode = new Map<string, string>();
  const listeners = new Set<() => void>();
  const controller = makeController(fake, {
    sessions: {
      read: (node) => observedByNode.get(node.id) ?? null,
      subscribe: (listener: () => void) => {
        listeners.add(listener);
        return () => listeners.delete(listener);
      }
    }
  });
  await controller.adopt(["agent-left"]);
  assert.equal(controller.getSnapshot().panes.left, null);

  observedByNode.set("agent-left", "session-a");
  fake.notify();

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");

  await controller.dropSession(dragSession("session-b"), "right");

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  controller.dispose();
});

test("a window's own boot-time restore does not overwrite the restored split", async () => {
  // 真机（E2E 约 7% 失败）：Dock iframe 重载 → 新控制器 adopt 时左栏窗口还没报会话号，
  // activate(左栏, A) 记不下「在途」（旧号为空）。随后 agent-gui 自己恢复出上次那条 D
  // 报上来，控制器把它当「用户在侧栏点了 D」：配对表还没到 → 判单列 D 并落盘，
  // 把耐久布局 A|B 覆盖成 D。开机回声必须当旧号，并补发一次 activate 把窗口拉到 A。
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observedByNode = new Map<string, string>();
  const listeners = new Set<() => void>();
  const storage = memoryStorage({
    "agent-gui:split-layout:ws-1:all": JSON.stringify({
      focus: "left",
      left: "session-a",
      ratio: 0.5,
      right: "session-b"
    })
  });
  const controller = makeController(fake, {
    sessions: {
      read: (node) => observedByNode.get(node.id) ?? null,
      subscribe: (listener: () => void) => {
        listeners.add(listener);
        return () => listeners.delete(listener);
      }
    },
    storage
  });
  await controller.adopt(["agent-left"]);
  const rightNodeId = fake.launched[0] as string;
  fake.calls.length = 0;

  // 左栏窗口先读成空（还在恢复），再报出它自己恢复的旧会话 D。
  fake.notify();
  observedByNode.set("agent-left", "session-d");
  fake.notify();

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  assert.deepEqual(activations(fake), [
    { nodeId: "agent-left", sessionId: "session-a" }
  ]);
  const persisted = JSON.parse(
    storage.map.get("agent-gui:split-layout:ws-1:all") ?? "{}"
  );
  assert.equal(
    persisted.left ?? persisted.layout?.left ?? "session-a",
    "session-a"
  );

  // 窗口换到 A 之后，用户在侧栏真点了 C：照常换左栏。
  observedByNode.set("agent-left", "session-a");
  observedByNode.set(rightNodeId, "session-b");
  fake.notify();
  observedByNode.set("agent-left", "session-c");
  fake.notify();
  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-c");
  controller.dispose();
});

test("hitting new session in a pane clears its stale identity without collapsing the split", async () => {
  // 真机：在左栏点「新建会话」，正文换成了首页，栏头标题却还是上一条会话
  // （agent-gui 的 handleCreateConversation 把 lastActiveAgentSessionId 抹成 null，
  // 我们原来「读到空就 continue」，于是一直留着旧的那条）。
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observedByNode = new Map<string, string | null>();
  const listeners = new Set<() => void>();
  const controller = makeController(fake, {
    host: {
      ...fake.host,
      getSnapshot: () => ({
        ...fake.host.getSnapshot(),
        nodes: fake.host.getSnapshot().nodes.map((node) => ({
          ...node,
          data: {
            ...node.data,
            snapshotNodeState:
              node.id === "agent-left"
                ? { lastActiveAgentSessionId: "session-a" }
                : undefined
          }
        }))
      })
    },
    sessions: createAgentGuiNodeSessionSource({
      externalStateSource: {
        getNodeState: ({ nodeId }) =>
          observedByNode.has(nodeId)
            ? { lastActiveAgentSessionId: observedByNode.get(nodeId) }
            : null,
        subscribeNodeState: (_request, listener) => {
          listeners.add(listener);
          return () => listeners.delete(listener);
        }
      },
      workspaceId: "ws-1"
    })
  });
  // Before the authoritative state arrives, the node snapshot still restores it.
  await controller.adopt(["agent-left"]);
  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  observedByNode.set("agent-left", "session-a");
  fake.notify();
  conversationRailSplitHost()?.describeSession?.(dragSession("session-a"));
  assert.equal(
    controller.getSnapshot().panes.left?.session?.title,
    "session-a"
  );
  assert.equal(
    conversationRailSplitHost()?.getShownSessionIds().has("session-a"),
    true
  );
  await controller.dropSession(dragSession("session-b"), "right");
  const rightNodeId = fake.launched[0] as string;

  // 右窗刚起来还没报过任何会话号：这不算「新建会话」，右栏身份不能被抹掉，
  // 否则落下即配对那一步会当成空栏跳过。
  fake.notify();
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");

  observedByNode.set(rightNodeId, "session-b");
  fake.notify();
  fake.calls.length = 0;

  // Home is an explicit null in the authoritative state; the old node snapshot
  // still contains session-a after the rail focused that session.
  observedByNode.set("agent-left", null);
  for (const listener of listeners) listener();

  const fresh = controller.getSnapshot();
  assert.equal(fresh.panes.left?.sessionId, null);
  assert.equal(fresh.panes.left?.session, null);
  assert.equal(
    conversationRailSplitHost()?.getShownSessionIds().has("session-a"),
    false
  );
  assert.equal(fresh.panes.right?.sessionId, "session-b");
  // 分栏不许塌：右栏窗口一个都不能关。
  assert.deepEqual(
    fake.calls.filter((call) => call.kind === "close"),
    []
  );

  // 新会话落地后自己换成新号。
  observedByNode.set("agent-left", "session-new");
  fake.notify();
  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-new");
  controller.dispose();
});

test("a freshly launched right window echoing the left session does not collapse the split", async () => {
  // 真机验收 03 复现：放下第二栏后，右窗在 activate 落地前会从 externalStateSource
  // 回声出「当前全局激活的会话」——也就是左栏那条。syncFromNodes 若把这次回声当成
  // 用户切换，两栏就撞成同一条、被去重逻辑收成单栏，刚放下的第二栏瞬间消失。
  const fake = createFakeHost();
  const observedByNode = new Map<string, string>();
  const listeners = new Set<() => void>();
  const sessions = {
    read: (node: { id: string }) => observedByNode.get(node.id) ?? null,
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  };
  const controller = makeController(fake, { sessions });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  const rightNodeId = fake.launched[0] as string;
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");

  // 右窗尚未打开 session-b，先回声了左栏的 session-a；宿主快照变化触发 syncFromNodes。
  observedByNode.set("agent-left", "session-a");
  observedByNode.set(rightNodeId, "session-a");
  fake.notify();

  const snapshot = controller.getSnapshot();
  assert.deepEqual(snapshot.panes.left, {
    nodeId: "agent-left",
    session: null,
    sessionId: "session-a"
  });
  assert.deepEqual(snapshot.panes.right, {
    nodeId: rightNodeId,
    session: {
      iconUrl: null,
      projectLabel: null,
      provider: "claude",
      title: "session-b"
    },
    sessionId: "session-b"
  });
  controller.dispose();
});

test("the launch request carries the session id and forces its own window", async () => {
  // 真机验收 03 第三轮：不带 payload 的启动会命中 agent-gui 的 `dock-entry` 复用分支，
  // 宿主把左栏那个窗口原样还回来，右半边于是永远是白的。判据钉在「发出去的请求」上。
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  fake.calls.length = 0;

  await controller.dropSession(dragSession("session-b"), "right");

  const launches = fake.calls.filter((call) => call.kind === "launch");
  assert.equal(launches.length, 1);
  const payload = (
    launches[0]?.args[0] as { payload?: Record<string, unknown> } | undefined
  )?.payload;
  assert.equal(payload?.agentSessionId, "session-b");
  assert.equal(payload?.openInNewWindow, true);
  assert.equal(payload?.forceNewInstance, true);
  controller.dispose();
});

test("a launch that hands back the other pane's window is not adopted", async () => {
  // 宿主复用了已有窗口（返回左栏的 nodeId）。认领它等于两栏共用一个窗口：
  // 状态看着是两栏、右半边却是白的。宁可这一栏暂时没有窗口，也不认领别人的。
  const fake = createFakeHost();
  let handBackLeft = true;
  const originalLaunch = fake.host.launchNode.bind(fake.host);
  fake.host.launchNode = async (input) => {
    if (handBackLeft) {
      fake.calls.push({ args: [input], kind: "launch" });
      return "agent-left";
    }
    return originalLaunch(input);
  };
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  fake.calls.length = 0;

  await controller.dropSession(dragSession("session-b"), "right");

  const snapshot = controller.getSnapshot();
  assert.deepEqual(snapshot.panes.left, {
    nodeId: "agent-left",
    session: null,
    sessionId: "session-a"
  });
  // 槽位仍是 session-b（用户的意图不丢），但绝不会把左栏那个窗口标成右栏。
  assert.equal(snapshot.panes.right?.sessionId, "session-b");
  assert.equal(snapshot.panes.right?.nodeId, null);
  assert.equal(
    fake.calls.filter((call) => call.kind === "activate").length,
    0,
    "别人的窗口不该被我们激活成右栏那条会话"
  );
  handBackLeft = false;
  controller.dispose();
});

test("a session-scoped launch refused by the contribution falls back to a plain new window", async () => {
  // agent-gui 的 onLaunchRequest 认不出 provider 时直接返回 null。真机上右半边
  // 因此一直是白的：状态有两栏、窗口一个都没多。退路必须存在，且仍然 openInNewWindow。
  const fake = createFakeHost();
  const originalLaunch = fake.host.launchNode.bind(fake.host);
  fake.host.launchNode = async (input) => {
    const payload = (input as { payload?: Record<string, unknown> }).payload;
    if (payload?.agentSessionId) {
      // 被拒的那次也要记一笔（真实 host 会记，这里手动补）。
      fake.calls.push({ args: [input], kind: "launch" });
      return null;
    }
    return originalLaunch(input);
  };
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  fake.calls.length = 0;

  await controller.dropSession(dragSession("session-b"), "right");

  const launches = fake.calls.filter((call) => call.kind === "launch");
  assert.equal(launches.length, 2, "先试带会话号那次，被拒后再试新开窗口那次");
  const fallback = (
    launches[1]?.args[0] as { payload?: Record<string, unknown> } | undefined
  )?.payload;
  assert.equal(fallback?.agentSessionId, undefined);
  assert.equal(fallback?.openInNewWindow, true);
  const rightNodeId = controller.getSnapshot().panes.right?.nodeId;
  assert.notEqual(rightNodeId, null);
  assert.notEqual(rightNodeId, "agent-left");
  assert.deepEqual(activations(fake), [
    { nodeId: rightNodeId as string, sessionId: "session-b" }
  ]);
  controller.dispose();
});

test("a pane whose window cannot be launched stops asking instead of retrying forever", async () => {
  // 不记住失败就会每次 applyLayout 再要一遍：恢复一个右栏起不来的布局时
  // 整个面板会被重试风暴卡死（真机上就是这样白屏的）。
  const fake = createFakeHost();
  fake.host.launchNode = async (input) => {
    fake.calls.push({ args: [input], kind: "launch" });
    return null;
  };
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  fake.calls.length = 0;

  await controller.dropSession(dragSession("session-b"), "right");
  const afterDrop = fake.calls.filter((call) => call.kind === "launch").length;
  controller.resize(0.6);
  controller.setFocus("left");
  fake.notify();
  await controller.dropSession(dragSession("session-b"), "right");

  assert.equal(
    fake.calls.filter((call) => call.kind === "launch").length,
    afterDrop,
    "同一条会话不该被反复重试"
  );
  assert.equal(controller.getSnapshot().panes.right?.nodeId, null);
  controller.dispose();
});

// 栏头（票 04）要写「这是谁」，而左栏那条会话从来没被拖过 —— 摘要只能靠侧栏
// 每渲染一条就上报一次。上报进来的标题/图标/项目名必须落进这一栏的快照。
test("侧栏上报的会话摘要会落进对应栏的快照", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");

  assert.equal(controller.getSnapshot().panes.left?.session, null);

  conversationRailSplitHost()?.describeSession?.({
    iconUrl: "claude.png",
    id: "session-a",
    isImported: false,
    projectLabel: "rndmaster",
    provider: "claude",
    status: "working",
    title: "改栏头"
  });

  assert.deepEqual(controller.getSnapshot().panes.left?.session, {
    iconUrl: "claude.png",
    projectLabel: "rndmaster",
    provider: "claude",
    title: "改栏头"
  });
  controller.dispose();
});

// 侧栏每渲染一条就报一次，条数又多：不加判重的话「上报 → emit → 侧栏重渲染 →
// 再上报」会转起来。同一份摘要重复上报必须一声不吭。
test("重复上报同一份摘要不再通知订阅者", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  const summary = {
    iconUrl: null,
    id: "session-a",
    isImported: false,
    projectLabel: null,
    provider: "claude",
    status: "working",
    title: "改栏头"
  } as const;
  conversationRailSplitHost()?.describeSession?.({ ...summary });

  let notified = 0;
  const unsubscribe = controller.subscribe(() => {
    notified += 1;
  });
  conversationRailSplitHost()?.describeSession?.({ ...summary });
  assert.equal(notified, 0);

  conversationRailSplitHost()?.describeSession?.({
    ...summary,
    title: "改栏头（新）"
  });
  assert.equal(notified, 1);
  unsubscribe();
  controller.dispose();
});

// 栏头 ⋮ 的「交换左右」：换的是两个槽里的会话，两个窗口各自 activate 一次；
// 窗口本身不换边（左栏那个窗口是嵌入模式的锚）。焦点跟着原来那条会话走。
test("交换左右把两栏的会话对调，窗口不换边", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  const rightNodeId = fake.launched[0] as string;
  assert.equal(controller.getSnapshot().focus, "right");
  fake.calls.length = 0;

  controller.swapPanes();

  const snapshot = controller.getSnapshot();
  assert.equal(snapshot.panes.left?.sessionId, "session-b");
  assert.equal(snapshot.panes.left?.nodeId, "agent-left");
  assert.equal(snapshot.panes.right?.sessionId, "session-a");
  assert.equal(snapshot.panes.right?.nodeId, rightNodeId);
  assert.equal(snapshot.focus, "left");
  assert.deepEqual(activations(fake), [
    { nodeId: rightNodeId, sessionId: "session-a" },
    { nodeId: "agent-left", sessionId: "session-b" }
  ]);
  controller.dispose();
});

// 配对时后端「先重开已结束的对端」只还回一个算出来的会话号，那条 Tutti 会话要等派工器
// 异步起来才存在。拿到号就 activate 会让那一栏打开一条还不存在的会话（空标题 + 红条
// `workspace agent session not found`），之后也不会自己重试。所以要等侧栏报到它再换槽。
function relaunchingPairingHost(relaunched: string) {
  return {
    createPeerPair: async () => ({
      pairId: "p1",
      relaunchedSessionId: relaunched
    }),
    deletePeerPair: async () => undefined,
    listPeerPairs: async () => ({
      pairs: [
        {
          a: { sessionId: "session-a", taskId: "ta", title: "a" },
          b: { sessionId: relaunched, taskId: "tb2", title: "b2" },
          pairId: "p1"
        }
      ]
    })
  } as never;
}

test("a relaunched peer is swapped in only after the rail reports it", async () => {
  const fake = createFakeHost();
  const errors: string[] = [];
  const controller = makeController(fake, {
    pairingHost: () => relaunchingPairingHost("session-b-relaunched"),
    toast: { error: (m) => errors.push(m), info() {}, success() {} }
  });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(
    { ...dragSession("session-b"), status: "completed" },
    "right"
  );
  controller.setFocus("left");
  const before = activations(fake).length;

  await controller.pairPanes();

  // 槽里仍是旧的已结束会话，没有对新号的 activate。
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  assert.equal(activations(fake).length, before);
  // 链条已按新号判「已配对」。
  assert.equal(controller.getSnapshot().pairing, "paired");

  conversationRailSplitHost()?.describeSession?.(
    dragSession("session-b-relaunched")
  );
  assert.equal(
    controller.getSnapshot().panes.right?.sessionId,
    "session-b-relaunched"
  );
  assert.deepEqual(activations(fake).slice(before), [
    { nodeId: "agent-launched-1", sessionId: "session-b-relaunched" }
  ]);
  assert.deepEqual(errors, []);
  controller.dispose();
});

test("a relaunched peer that never shows up is given up with a toast", async () => {
  const fake = createFakeHost();
  const errors: string[] = [];
  const controller = makeController(fake, {
    labels: () => ({
      paired: "已配对",
      relaunchTimeout: "重开超时",
      unmanaged: "x"
    }),
    pairingHost: () => relaunchingPairingHost("session-b-relaunched"),
    relaunchWaitMs: 10,
    toast: { error: (m) => errors.push(m), info() {}, success() {} }
  });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(
    { ...dragSession("session-b"), status: "completed" },
    "right"
  );
  controller.setFocus("left");
  await controller.pairPanes();
  await new Promise((resolve) => setTimeout(resolve, 30));

  assert.deepEqual(errors, ["重开超时"]);
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  // 超时后侧栏才报到：不再换槽（用户可自己点开）。
  conversationRailSplitHost()?.describeSession?.(
    dragSession("session-b-relaunched")
  );
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  controller.dispose();
});

// 票 05：会话栏在分栏左栏里展开，空间从右栏挤出来 —— 左栏加宽、右栏变窄，
// 分隔线跟着右移。判据钉在 fraction 上，因为窗口 body 拿到的容器宽度就是它算的
// （embeddedDintalDock.ts 的 projectEmbeddedDintalDockNodeContext），
// 而 CSS 画壳用的是同一个 railPushRatio —— 两者必须同源。
test("会话栏展开时左栏加宽、右栏同步变窄，两栏仍然铺满", async () => {
  const fake = createFakeHost({ surfaceWidth: 1600 });
  const controller = makeController(fake);
  await controller.adopt(["agent-left", "agent-right"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  const unregister = registerEmbeddedSplitViewController(controller);

  assert.equal(controller.getSnapshot().railPushRatio, 0);

  // 1600 宽、比例 0.5：右栏 800px，让出 280px 后仍有 520px，没碰到夹逼。
  controller.setRailPushPx(280);
  assert.ok(Math.abs(controller.getSnapshot().railPushRatio - 0.175) < 1e-9);
  const left = parseEmbeddedSplitViewPaneDescriptor(
    embeddedSplitViewPaneDescriptor("agent-left")
  );
  // 右栏的窗口是 dropSession 现起的，node id 由假宿主发号。
  const right = parseEmbeddedSplitViewPaneDescriptor(
    embeddedSplitViewPaneDescriptor(fake.launched[0] ?? "")
  );
  assert.ok(Math.abs((left?.fraction ?? 0) - 0.675) < 1e-3);
  assert.ok(Math.abs((right?.fraction ?? 0) - 0.325) < 1e-3);
  assert.ok(
    Math.abs((left?.fraction ?? 0) + (right?.fraction ?? 0) - 1) < 1e-3
  );

  controller.setRailPushPx(0);
  assert.equal(controller.getSnapshot().railPushRatio, 0);
  unregister();
  controller.dispose();
});

// 放下区/命中判定与 CSS 画的分界线必须同源（`ratio + railPush`）。只按 ratio 算时，
// 预览框把整片主区从正中切开、不跟分界线走，中间那条带子还会把会话丢进右栏。
test("放下区与命中带跟着分界线走，不是主区中线", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake);
  await controller.adopt(["agent-left", "agent-right"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");

  // 1200 宽、比例 0.5：右栏 600px，让出 240px 后仍有 360px，没碰到夹逼。
  controller.setRailPushPx(240);
  assert.ok(Math.abs(controller.getSnapshot().railPushRatio - 0.2) < 1e-9);

  // 分界线在 (0.5 + 0.2) * 1200 = 840；左栏放下区从会话栏右缘 300 起。
  assert.deepEqual(controller.dropZoneRect("left"), { left: 300, width: 540 });
  assert.deepEqual(controller.dropZoneRect("right"), { left: 840, width: 360 });

  // 700 在主区中线右边、分界线左边——看着是左栏，就必须判成左栏。
  const host = conversationRailSplitHost();
  host?.onDragStart?.(dragSession("session-c"), { x: 700, y: 400 });
  assert.equal(controller.getSnapshot().dragging?.hoverSide, "left");
  host?.onDragMove?.({ x: 900, y: 400 });
  assert.equal(controller.getSnapshot().dragging?.hoverSide, "right");
  host?.onDragCancel?.();

  controller.dispose();
});

// 右栏被推到 320px 以下就等于分栏被会话栏吃掉了；宁可推得少一点，也不能推没。
test("推力被夹在右栏 320px 最小宽度上", async () => {
  const fake = createFakeHost({ surfaceWidth: 1000 });
  const controller = makeController(fake);
  await controller.adopt(["agent-left", "agent-right"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");

  // 右栏原有 500px，最多只让出 180px。
  controller.setRailPushPx(400);
  assert.ok(Math.abs(controller.getSnapshot().railPushRatio - 0.18) < 1e-9);
  controller.dispose();
});

// 单栏没有「另一栏」可挤；折叠态只显示焦点栏，推了也只会让壳算错。
test("单栏与折叠态一律不推", async () => {
  const fake = createFakeHost({ surfaceWidth: 1000 });
  const controller = makeController(fake);
  await controller.adopt(["agent-left"]);
  controller.select("session-a");

  controller.setRailPushPx(280);
  assert.equal(controller.getSnapshot().railPushRatio, 0);

  await controller.dropSession(dragSession("session-b"), "right");
  assert.ok(controller.getSnapshot().railPushRatio > 0);

  fake.setSurfaceWidth(600);
  fake.notify();
  assert.equal(controller.getSnapshot().collapsed, true);
  assert.equal(controller.getSnapshot().railPushRatio, 0);
  controller.dispose();
});

// 补丁 0130：配对表有两份缓存（侧栏右键菜单一份、本控制器一份），靠一条广播对齐。
// 没有这条广播时，拖进分栏配好的对在侧栏右键里仍是 `Unpair (0)`。
test("拖放配对成功后向侧栏那份缓存广播一次", async () => {
  const fake = createFakeHost();
  let broadcasts = 0;
  const unsubscribe = subscribeConversationRailPeerPairsChanged(() => {
    broadcasts += 1;
  });
  // 配对表一开始是空的，createPeerPair 之后才有那一条 —— 不这样写，落下那一刻
  // 就已经「已配对」，pairPanes 会走 `already` 分支，钉不到广播。
  let pairs: unknown[] = [];
  const controller = makeController(fake, {
    pairingHost: () =>
      ({
        createPeerPair: async () => {
          pairs = [
            {
              a: { sessionId: "session-a", taskId: "ta", title: "a" },
              b: { sessionId: "session-b", taskId: "tb", title: "b" },
              pairId: "p1"
            }
          ];
          return { pairId: "p1" };
        },
        deletePeerPair: async () => undefined,
        listPeerPairs: async () => ({ pairs })
      }) as never
  });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  controller.setFocus("left");

  const result = await controller.pairPanes();

  assert.equal(result.outcome, "paired");
  assert.equal(controller.getSnapshot().pairing, "paired");
  assert.equal(broadcasts, 1);
  unsubscribe();
  controller.dispose();
});

test("侧栏解除配对后本控制器跟着重拉配对表", async () => {
  const fake = createFakeHost();
  let lists = 0;
  let pairs: unknown[] = [
    {
      a: { sessionId: "session-a", taskId: "ta", title: "a" },
      b: { sessionId: "session-b", taskId: "tb", title: "b" },
      pairId: "p1"
    }
  ];
  const controller = makeController(fake, {
    pairingHost: () =>
      ({
        createPeerPair: async () => ({ pairId: "p1" }),
        deletePeerPair: async () => undefined,
        listPeerPairs: async () => {
          lists += 1;
          return { pairs };
        }
      }) as never
  });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  controller.setFocus("left");
  await controller.pairPanes();
  assert.equal(controller.getSnapshot().pairing, "paired");
  const listsBefore = lists;

  // 用户在侧栏右键里解除：那一头写完只广播，不碰本控制器的缓存。
  pairs = [];
  notifyConversationRailPeerPairsChanged();
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(lists, listsBefore + 1);
  assert.equal(controller.getSnapshot().pairing, "unpaired");
  controller.dispose();
});

test("dispose 之后不再响应配对表广播", async () => {
  const fake = createFakeHost();
  let lists = 0;
  const controller = makeController(fake, {
    pairingHost: () =>
      ({
        createPeerPair: async () => ({ pairId: "p1" }),
        deletePeerPair: async () => undefined,
        listPeerPairs: async () => {
          lists += 1;
          return { pairs: [] };
        }
      }) as never
  });
  await controller.adopt(["agent-left"]);
  const listsBefore = lists;
  controller.dispose();

  notifyConversationRailPeerPairsChanged();
  await Promise.resolve();

  assert.equal(lists, listsBefore);
});

test("closing the only pane sends the window home instead of leaving it on the session", async () => {
  // 真机：单栏栏头的 ✕ 点了像没反应 —— 栏头没了、正文还停在刚关掉的那条会话。
  // 左栏窗口是嵌入模式的锚，不能关，所以要显式请它回首页；不请的话它照旧报着
  // 旧会话号，syncFromNodes 下一拍就把那条认回左槽。
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observedByNode = new Map<string, string>([["agent-left", "session-a"]]);
  const controller = makeController(fake, {
    sessions: { read: (node) => observedByNode.get(node.id) ?? null }
  });
  await controller.adopt(["agent-left"]);
  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  fake.calls.length = 0;

  await controller.closePane("left");

  assert.deepEqual(
    fake.calls
      .filter((call) => call.kind === "activate")
      .map((call) => [
        (call.args[0] as { nodeId: string }).nodeId,
        (call.args[1] as { type: string }).type
      ]),
    [["agent-left", "agent-gui:go-home"]]
  );
  assert.equal(controller.getSnapshot().panes.left, null);
  assert.deepEqual(
    fake.calls.filter((call) => call.kind === "close").map((call) => call.args),
    []
  );

  // 窗口还没把会话号清掉的那一拍：旧号不算「用户切了会话」。
  fake.notify();
  assert.equal(controller.getSnapshot().panes.left, null);

  // 清掉之后仍然是首页态。
  observedByNode.delete("agent-left");
  fake.notify();
  assert.equal(controller.getSnapshot().panes.left, null);
  controller.dispose();
});

// ---------------------------------------------------------------------------
// 分栏结对模式（peer-pair-mode 票 04）：单选的显示条件 / 联动 / 角色跟会话走。
// 配对表是唯一真相：桩里的 `rows` 就是「后端 pair 行」，setPeerPairMode 改它、
// listPeerPairs 读它；控制器不许另存一份。
// ---------------------------------------------------------------------------

interface PairModeRow {
  developerTaskId: string;
  kickoffState: "" | "pending" | "sent";
  pairMode?: "solo" | "pair";
}

function pairModeHost(
  input: {
    commitPairKickoff?: (args: {
      goal: string;
      pairId: string;
      senderTaskId: string;
    }) => Promise<unknown>;
    omitCapabilities?: boolean;
    previewPairKickoff?: (args: {
      goal: string;
      pairId: string;
      senderTaskId: string;
    }) => Promise<{ block: string }>;
    row?: Partial<PairModeRow>;
    setPeerPairMode?: (args: {
      developerTaskId?: string;
      mode: "solo" | "pair";
      pairId: string;
    }) => Promise<unknown>;
  } = {}
) {
  const row: PairModeRow = {
    developerTaskId: "",
    kickoffState: "",
    pairMode: "solo",
    ...input.row
  };
  const calls: { args: unknown; kind: string }[] = [];
  const pairRow = () => ({
    a: { sessionId: "session-a", taskId: "task-a", title: "a" },
    b: { sessionId: "session-b", taskId: "task-b", title: "b" },
    pairId: "p1",
    ...(row.pairMode === undefined
      ? {}
      : {
          developerTaskId: row.developerTaskId,
          kickoffState: row.kickoffState,
          pairMode: row.pairMode
        })
  });
  const host = {
    createPeerPair: async () => ({ pairId: "p1" }),
    deletePeerPair: async () => undefined,
    listPeerPairs: async () => {
      calls.push({ args: null, kind: "list" });
      return { pairs: [pairRow()] };
    },
    ...(input.omitCapabilities
      ? {}
      : {
          commitPairKickoff: async (args: {
            goal: string;
            pairId: string;
            senderTaskId: string;
          }) => {
            calls.push({ args, kind: "commit" });
            if (input.commitPairKickoff) await input.commitPairKickoff(args);
            row.kickoffState = "sent";
            return { delivered: true, pair: pairRow() };
          },
          previewPairKickoff: async (args: {
            goal: string;
            pairId: string;
            senderTaskId: string;
          }) => {
            calls.push({ args, kind: "preview" });
            if (input.previewPairKickoff) return input.previewPairKickoff(args);
            return {
              block: '<pair-kickoff role="developer">\n协议\n</pair-kickoff>'
            };
          },
          setPeerPairMode: async (args: {
            developerTaskId?: string;
            mode: "solo" | "pair";
            pairId: string;
          }) => {
            calls.push({ args, kind: "setMode" });
            if (input.setPeerPairMode) await input.setPeerPairMode(args);
            row.pairMode = args.mode;
            row.developerTaskId =
              args.mode === "pair" ? (args.developerTaskId ?? "") : "";
            row.kickoffState = args.mode === "pair" ? "pending" : "";
            return { pair: pairRow() };
          }
        })
  };
  return { calls, host, row };
}

async function splitPairedController(
  host: unknown,
  overrides: Partial<
    Parameters<typeof createEmbeddedSplitViewController>[0]
  > = {},
  sessions: {
    a?: ConversationRailSplitDragSession;
    b?: ConversationRailSplitDragSession;
  } = {}
) {
  const fake = createFakeHost();
  const controller = makeController(fake, {
    pairingHost: () => host as never,
    ...overrides
  });
  await controller.adopt(["agent-left"]);
  // 左栏那条也要有摘要：托管与否（isImported）靠侧栏上报的摘要判。
  conversationRailSplitHost()?.describeSession?.(
    sessions.a ?? dragSession("session-a")
  );
  controller.select("session-a");
  await controller.dropSession(sessions.b ?? dragSession("session-b"), "right");
  await controller.pairPanes();
  controller.setFocus("left");
  return { controller, fake };
}

test("结对模式单选：分栏 + 已配对 + 两栏托管 + 宿主支持时才有投影", async () => {
  const { host } = pairModeHost();
  const { controller } = await splitPairedController(host);
  const pairMode = controller.getSnapshot().pairMode;
  assert.equal(pairMode?.pairId, "p1");
  assert.equal(pairMode?.mode, "solo");
  assert.deepEqual(pairMode?.roles, { left: null, right: null });
  controller.dispose();
});

test("结对模式单选显示条件①：未分栏（单栏）不给投影", async () => {
  const { host } = pairModeHost();
  const fake = createFakeHost();
  const controller = makeController(fake, { pairingHost: () => host as never });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  assert.equal(controller.getSnapshot().pairMode, null);
  controller.dispose();
});

test("结对模式单选显示条件②：两栏没配对不给投影", async () => {
  const fake = createFakeHost();
  const controller = makeController(fake, {
    pairingHost: () =>
      ({
        commitPairKickoff: async () => ({}),
        createPeerPair: async () => {
          throw new Error("后端拒绝");
        },
        deletePeerPair: async () => undefined,
        listPeerPairs: async () => ({ pairs: [] }),
        previewPairKickoff: async () => ({ block: "x" }),
        setPeerPairMode: async () => ({})
      }) as never
  });
  await controller.adopt(["agent-left"]);
  controller.select("session-a");
  await controller.dropSession(dragSession("session-b"), "right");
  await controller.pairPanes();
  assert.notEqual(controller.getSnapshot().pairing, "paired");
  assert.equal(controller.getSnapshot().pairMode, null);
  controller.dispose();
});

test("结对模式单选显示条件③：任一栏是非托管会话不给投影", async () => {
  const { host } = pairModeHost();
  const { controller } = await splitPairedController(
    host,
    {},
    {
      b: { ...dragSession("session-b"), isImported: true }
    }
  );
  assert.equal(controller.getSnapshot().pairMode, null);
  controller.dispose();
});

test("结对模式单选显示条件④：宿主不支持（行上无 pairMode / 三件套缺 / 回 unsupported）不给投影", async () => {
  const oldRow = pairModeHost({ row: { pairMode: undefined } });
  const first = await splitPairedController(oldRow.host);
  assert.equal(first.controller.getSnapshot().pairing, "paired");
  assert.equal(first.controller.getSnapshot().pairMode, null);
  first.controller.dispose();

  const noCapabilities = pairModeHost({ omitCapabilities: true });
  const second = await splitPairedController(noCapabilities.host);
  assert.equal(second.controller.getSnapshot().pairMode, null);
  second.controller.dispose();

  const unsupported = pairModeHost({
    setPeerPairMode: async () => {
      throw new Error("unsupported");
    }
  });
  const errors: string[] = [];
  const third = await splitPairedController(unsupported.host, {
    toast: { error: (m) => errors.push(m), info() {}, success() {} }
  });
  assert.notEqual(third.controller.getSnapshot().pairMode, null);
  await third.controller.setPairMode("left", "developer");
  assert.equal(third.controller.getSnapshot().pairMode, null);
  // unsupported 是「整排收起」，不是一条要端给用户的错误。
  assert.deepEqual(errors, []);
  third.controller.dispose();
});

test("左栏选开发者：developerTaskId 是左栏的 task，右栏投影成审查者，并广播 + 重拉", async () => {
  const { calls, host } = pairModeHost();
  const { controller } = await splitPairedController(host);
  let broadcasts = 0;
  const unsubscribe = subscribeConversationRailPeerPairsChanged(() => {
    broadcasts += 1;
  });
  const listsBefore = calls.filter((call) => call.kind === "list").length;

  await controller.setPairMode("left", "developer");

  assert.deepEqual(
    calls.filter((call) => call.kind === "setMode").map((call) => call.args),
    [{ developerTaskId: "task-a", mode: "pair", pairId: "p1" }]
  );
  const pairMode = controller.getSnapshot().pairMode;
  assert.equal(pairMode?.mode, "pair");
  assert.equal(pairMode?.kickoffState, "pending");
  assert.deepEqual(pairMode?.roles, { left: "developer", right: "reviewer" });
  assert.equal(broadcasts, 1);
  assert.equal(
    calls.filter((call) => call.kind === "list").length,
    listsBefore + 1
  );
  unsubscribe();
  controller.dispose();
});

test("一栏选审查者 = 另一栏是开发者；任一栏选独立模式两栏一起退出", async () => {
  const { calls, host } = pairModeHost();
  const { controller } = await splitPairedController(host);

  await controller.setPairMode("left", "reviewer");
  assert.deepEqual(
    calls.filter((call) => call.kind === "setMode").at(-1)?.args,
    { developerTaskId: "task-b", mode: "pair", pairId: "p1" }
  );
  assert.deepEqual(controller.getSnapshot().pairMode?.roles, {
    left: "reviewer",
    right: "developer"
  });

  await controller.setPairMode("right", "solo");
  assert.deepEqual(
    calls.filter((call) => call.kind === "setMode").at(-1)?.args,
    { mode: "solo", pairId: "p1" }
  );
  assert.equal(controller.getSnapshot().pairMode?.mode, "solo");
  assert.deepEqual(controller.getSnapshot().pairMode?.roles, {
    left: null,
    right: null
  });
  controller.dispose();
});

test("交换左右之后角色跟着会话走，不跟着左右走", async () => {
  const { host } = pairModeHost();
  const { controller } = await splitPairedController(host);
  await controller.setPairMode("left", "developer");
  assert.deepEqual(controller.getSnapshot().pairMode?.roles, {
    left: "developer",
    right: "reviewer"
  });

  controller.swapPanes();

  // session-a（task-a，开发者）现在在右栏。
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-a");
  assert.deepEqual(controller.getSnapshot().pairMode?.roles, {
    left: "reviewer",
    right: "developer"
  });
  controller.dispose();
});

test("设模式失败：toast 后端原文，单选仍是行里的值", async () => {
  const errors: string[] = [];
  const { host } = pairModeHost({
    setPeerPairMode: async () => {
      throw new Error("配对已撤销");
    }
  });
  const { controller } = await splitPairedController(host, {
    toast: { error: (m) => errors.push(m), info() {}, success() {} }
  });

  await controller.setPairMode("left", "developer");

  assert.deepEqual(errors, ["配对已撤销"]);
  assert.equal(controller.getSnapshot().pairMode?.mode, "solo");
  assert.equal(controller.getSnapshot().pairMode?.busy, false);
  controller.dispose();
});

test("写在途时连点（评审补充 2）：第二次选择被挡掉，只写一次", async () => {
  const pending: (() => void)[] = [];
  const release = () => pending.splice(0).forEach((resolve) => resolve());
  const { calls, host } = pairModeHost({
    setPeerPairMode: () =>
      new Promise<void>((resolve) => {
        pending.push(resolve);
      })
  });
  const { controller } = await splitPairedController(host);

  const first = controller.setPairMode("left", "developer");
  assert.equal(controller.getSnapshot().pairMode?.busy, true);
  const second = controller.setPairMode("left", "reviewer");
  release();
  await Promise.all([first, second]);

  assert.deepEqual(
    calls.filter((call) => call.kind === "setMode").map((call) => call.args),
    [{ developerTaskId: "task-a", mode: "pair", pairId: "p1" }]
  );
  assert.deepEqual(controller.getSnapshot().pairMode?.roles, {
    left: "developer",
    right: "reviewer"
  });
  controller.dispose();
});

// ---------------------------------------------------------------------------
// 侧栏点选在结对模式下的三条去向（rndmaster 票 01/02）+ 顺序记忆（票 03）+
// 退出重进恢复（票 04）。
//
// 侧栏那份会话列表只长在**左栏**那个窗口里（右栏用 CSS 隐掉了），所以「用户在
// 列表里点了另一条会话」在本层看到的样子就是：左栏窗口自己报了一个新的会话号。
// 下面的用例都用 `observed` + `fake.notify()` 模拟这一步。
// ---------------------------------------------------------------------------

function pairEndpoint(sessionId: string) {
  return {
    alias: sessionId,
    cwd: "/tmp",
    provider: "claude",
    sessionId,
    status: "working",
    taskId: `task-${sessionId}`,
    title: sessionId
  };
}

/** 夹具：A/B/C 三条互为结对，E↔F 一对，D 没结对。可整对改成 solo（空心徽标）。 */
function railPairsHost(
  rows: readonly (readonly [string, string, "pair" | "solo"])[]
) {
  return {
    commitPairKickoff: async () => ({}),
    createPeerPair: async () => ({ pairId: "new" }),
    deletePeerPair: async () => undefined,
    listPeerPairs: async () => ({
      pairs: rows.map(([a, b, pairMode], index) => ({
        a: pairEndpoint(a),
        b: pairEndpoint(b),
        developerTaskId: "",
        kickoffState: "" as const,
        pairId: `p${index}`,
        pairMode
      }))
    }),
    previewPairKickoff: async () => ({ block: "x" }),
    setPeerPairMode: async () => ({})
  };
}

const RAIL_PAIRS = [
  ["session-a", "session-b", "pair"],
  ["session-a", "session-c", "pair"],
  ["session-b", "session-c", "pair"],
  ["session-e", "session-f", "pair"]
] as const;

/** 左栏窗口开着 `start` 那条会话的控制器；`switchTo` 模拟用户在侧栏点了另一条。 */
async function railController(
  rows: readonly (readonly [string, string, "pair" | "solo"])[] = RAIL_PAIRS,
  overrides: Partial<
    Parameters<typeof createEmbeddedSplitViewController>[0]
  > = {},
  start = "session-d"
) {
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observed = new Map<string, string>([["agent-left", start]]);
  const controller = makeController(fake, {
    pairingHost: () => railPairsHost(rows) as never,
    sessions: { read: (node) => observed.get(node.id) ?? null },
    ...overrides
  });
  await controller.adopt(["agent-left"]);
  // 换会话号之后要把 launchNode 的 promise 排空：右栏窗口是异步起的，
  // 不等它落地，nodeIdBySide.right 还是 null，下一步的关窗判据就看不到。
  const switchTo = async (sessionId: string): Promise<void> => {
    observed.set("agent-left", sessionId);
    fake.notify();
    await new Promise((resolve) => setTimeout(resolve, 0));
  };
  const panes = () => ({
    left: controller.getSnapshot().panes.left?.sessionId ?? null,
    right: controller.getSnapshot().panes.right?.sessionId ?? null
  });
  return { controller, fake, observed, panes, switchTo };
}

test("票01：侧栏点一条正在结对的会话 → 一次开出两列", async () => {
  const { controller, fake, panes, switchTo } = await railController();
  assert.deepEqual(panes(), { left: "session-d", right: null });

  await switchTo("session-a");

  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  assert.equal(fake.launched.length, 1);
  controller.dispose();
});

test("票01：空心徽标（配对还在、已切独立）点了仍是单列", async () => {
  const { controller, panes, switchTo } = await railController([
    ["session-a", "session-b", "solo"]
  ]);

  await switchTo("session-a");

  assert.deepEqual(panes(), { left: "session-a", right: null });
  controller.dispose();
});

test("票02：双列时点没结对的会话 → 收成单列全宽", async () => {
  const { controller, fake, panes, switchTo } = await railController();
  await switchTo("session-a");
  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  const rightNodeId = fake.launched[0] as string;
  fake.calls.length = 0;

  await switchTo("session-d");

  assert.deepEqual(panes(), { left: "session-d", right: null });
  assert.deepEqual(
    fake.calls.filter((call) => call.kind === "close").map((call) => call.args),
    [[rightNodeId]]
  );
  controller.dispose();
});

test("票02：右栏窗口还报着旧会话号时，点没结对的会话照样收成单列", async () => {
  // 真机上右栏那个窗口每一拍都报自己的会话号（它要等 applyLayout 去关）。
  // 左栏这一下已经把布局改写成单列，如果还接着看右栏，就会把刚收掉的那条当成
  // 「用户在右栏切了会话」塞回来 —— 表现为「点没结对的会话收不成单列」。
  const { controller, fake, observed, panes, switchTo } =
    await railController();
  await switchTo("session-a");
  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  const rightNodeId = fake.launched[0] as string;
  observed.set(rightNodeId, "session-b");
  fake.notify();
  await new Promise((resolve) => setTimeout(resolve, 0));
  fake.calls.length = 0;

  await switchTo("session-d");

  assert.deepEqual(panes(), { left: "session-d", right: null });
  assert.deepEqual(
    fake.calls.filter((call) => call.kind === "close").map((call) => call.args),
    [[rightNodeId]]
  );
  controller.dispose();
});

test("票02：双列时点和右栏也结对的会话 → 只换左栏", async () => {
  const { controller, fake, panes, switchTo } = await railController();
  await switchTo("session-a");
  const rightNodeId = fake.launched[0] as string;
  fake.calls.length = 0;

  await switchTo("session-c");

  assert.deepEqual(panes(), { left: "session-c", right: "session-b" });
  // 右栏窗口既不关也不重新 activate：正文不闪。
  assert.deepEqual(
    fake.calls.filter(
      (call) =>
        call.kind === "close" ||
        (call.kind === "activate" &&
          (call.args[0] as { nodeId: string }).nodeId === rightNodeId)
    ),
    []
  );
  controller.dispose();
});

test("票02：双列时点与右栏无关但自己有结对的会话 → 整组切换", async () => {
  const { controller, panes, switchTo } = await railController();
  await switchTo("session-a");

  await switchTo("session-e");

  assert.deepEqual(panes(), { left: "session-e", right: "session-f" });
  controller.dispose();
});

test("票02：整组切换时右栏窗口还在回声旧会话号，不能把它塞回来", async () => {
  // activateNode 是异步的：整组切换后右栏那个窗口还要几拍才换过来，中间照旧报旧号。
  // 真机上这一拍被当成「用户在右栏切了会话」，分支③就退化成了「只换左栏」。
  const { controller, fake, observed, panes, switchTo } =
    await railController();
  await switchTo("session-a");
  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  const rightNodeId = fake.launched[0] as string;
  observed.set(rightNodeId, "session-b");
  fake.notify();
  await new Promise((resolve) => setTimeout(resolve, 0));

  // e 只和 f 结对，与右栏的 b 无关 → 整组换成 e/f。
  await switchTo("session-e");
  assert.deepEqual(panes(), { left: "session-e", right: "session-f" });

  // 右栏这一拍仍报 session-b（还没换过来）。
  fake.notify();
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.deepEqual(panes(), { left: "session-e", right: "session-f" });
  controller.dispose();
});

test("票03：上次开过的搭档优先于候选里的第一个", async () => {
  const { controller, panes, switchTo } = await railController();
  await switchTo("session-a");
  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  // 用户把右栏换成 C（A 的另一个搭档），再走开，再点回 A。
  await controller.dropSession(dragSession("session-c"), "right");
  assert.deepEqual(panes(), { left: "session-a", right: "session-c" });
  await switchTo("session-d");

  await switchTo("session-a");

  assert.deepEqual(panes(), { left: "session-a", right: "session-c" });
  controller.dispose();
});

test("票03：记住的左右顺序压过「点的那条在左」", async () => {
  const layoutStore = {
    read: async () => ({
      layout: null,
      pairOrder: {
        "session-a|session-b": {
          left: "session-b",
          right: "session-a",
          usedAt: 10
        }
      }
    }),
    write: () => {}
  };
  const { controller, panes, switchTo } = await railController(RAIL_PAIRS, {
    layoutStore
  });

  await switchTo("session-a");

  assert.deepEqual(panes(), { left: "session-b", right: "session-a" });
  controller.dispose();
});

test("票04：耐久存储里的分栏在 adopt 时恢复，之后的改动写回去", async () => {
  const writes: unknown[] = [];
  const layoutStore = {
    read: async () => ({
      layout: {
        focus: "left" as const,
        panes: { left: "session-a", right: "session-b" },
        ratio: 0.4
      },
      pairOrder: {}
    }),
    write: (snapshot: unknown) => {
      writes.push(snapshot);
    }
  };
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observed = new Map<string, string>([["agent-left", "session-a"]]);
  const controller = makeController(fake, {
    layoutStore,
    pairingHost: () => railPairsHost(RAIL_PAIRS) as never,
    sessions: { read: (node) => observed.get(node.id) ?? null }
  });

  await controller.adopt(["agent-left"]);

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  assert.equal(controller.getSnapshot().ratio, 0.4);

  controller.resize(0.6);

  const last = writes.at(-1) as {
    layout: { ratio: number };
    pairOrder: Record<string, { left: string; right: string }>;
  };
  assert.equal(last.layout.ratio, 0.6);
  // 恢复出来的这一对也算「刚用过」，顺序跟着记下来。
  assert.deepEqual(last.pairOrder["session-a|session-b"]?.left, "session-a");
  controller.dispose();
});

test("评审1：开机读存储期间窗口报了会话号，不能拿空布局把上次的两栏写成单列", async () => {
  // 读后端要来回一趟；这期间 syncFromNodes 若已认领窗口，就会按「空布局 +
  // 空 pairOrder」判成单列并 persist —— 屏幕上两栏、磁盘上一栏，下次开机两栏没了。
  const writes: {
    layout: { panes: { left: string | null; right: string | null } } | null;
  }[] = [];
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const layoutStore = {
    read: async () => {
      await gate;
      return {
        layout: {
          focus: "left" as const,
          panes: { left: "session-a", right: "session-b" },
          ratio: 0.5
        },
        pairOrder: {}
      };
    },
    write: (snapshot: (typeof writes)[number]) => {
      writes.push(snapshot);
    }
  };
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observed = new Map<string, string>([["agent-left", "session-a"]]);
  const controller = makeController(fake, {
    layoutStore: layoutStore as never,
    pairingHost: () => railPairsHost(RAIL_PAIRS) as never,
    sessions: { read: (node) => observed.get(node.id) ?? null }
  });

  const adopting = controller.adopt(["agent-left"]);
  fake.notify();
  await new Promise((resolve) => setTimeout(resolve, 0));
  // 用 length 判：deepEqual 的 asserts 会把 writes 收窄成 never[]。
  assert.equal(writes.length, 0);

  release();
  await adopting;

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-a");
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");
  assert.ok(
    writes.every((write) => write.layout?.panes.right !== null),
    "没有任何一次把右栏写成空"
  );
  controller.dispose();
});

test("评审3：请它换的那条打不开（退回首页读成空），之后点回旧那条不能被当回声吞掉", async () => {
  const layoutStore = {
    read: async () => ({
      layout: {
        focus: "left" as const,
        panes: { left: "session-dead", right: null },
        ratio: 0.5
      },
      pairOrder: {}
    }),
    write: () => {}
  };
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observed = new Map<string, string>([["agent-left", "session-x"]]);
  const controller = makeController(fake, {
    layoutStore,
    sessions: { read: (node) => observed.get(node.id) ?? null }
  });
  await controller.adopt(["agent-left"]);
  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-dead");

  // gui 打不开 session-dead，退回首页：读成空。
  observed.delete("agent-left");
  fake.notify();
  // 用户在侧栏点 session-x。
  observed.set("agent-left", "session-x");
  fake.notify();
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.equal(controller.getSnapshot().panes.left?.sessionId, "session-x");
  controller.dispose();
});

test("评审2：侧栏拉到的配对表分栏直接用 —— 外部建的结对，点了也开两列", async () => {
  // 控制器自己那份只在 adopt 时拉过一次（此时 d 没结对）；之后 agent 在外面给
  // d/g 建了结对，侧栏换会话时重拉看到了、徽标变实心。分栏必须认同一份表。
  const { controller, panes, switchTo } = await railController(
    RAIL_PAIRS,
    {},
    "session-x"
  );
  const fresh = await railPairsHost([
    ...RAIL_PAIRS,
    ["session-d", "session-g", "pair"]
  ]).listPeerPairs();
  publishConversationRailPeerPairsSnapshot(fresh.pairs);

  await switchTo("session-d");

  assert.deepEqual(panes(), { left: "session-d", right: "session-g" });
  controller.dispose();
});

test("评审4：在侧栏点右栏正显示的那条 → 焦点给右栏，左栏请回原来那条", async () => {
  const { controller, fake, panes, switchTo } = await railController();
  await switchTo("session-a");
  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  fake.calls.length = 0;

  // 左栏窗口（侧栏在这里）报出右栏那条。
  await switchTo("session-b");

  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  assert.equal(controller.getSnapshot().focus, "right");
  assert.deepEqual(activations(fake), [
    { nodeId: "agent-left", sessionId: "session-a" }
  ]);

  // 窗口还没换过来、下一拍仍报 session-b：不能再发一次，也不能改布局。
  fake.notify();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(activations(fake).length, 1);
  assert.deepEqual(panes(), { left: "session-a", right: "session-b" });
  controller.dispose();
});
