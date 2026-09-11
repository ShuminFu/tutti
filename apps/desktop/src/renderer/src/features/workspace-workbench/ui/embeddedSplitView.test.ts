import assert from "node:assert/strict";
import test from "node:test";
import {
  conversationRailSplitHost,
  notifyConversationRailPeerPairsChanged,
  subscribeConversationRailPeerPairsChanged
} from "@tutti-os/agent-gui/conversation-rail-projection";
import type { ConversationRailSplitDragSession } from "@tutti-os/agent-gui/conversation-rail-projection";
import { activateEmbeddedDintalDockSession } from "./embeddedDintalDock.ts";
import {
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
    .filter((call) => call.kind === "activate")
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

test("hitting new session in a pane clears its stale identity without collapsing the split", async () => {
  // 真机：在左栏点「新建会话」，正文换成了首页，栏头标题却还是上一条会话
  // （agent-gui 的 handleCreateConversation 把 lastActiveAgentSessionId 抹成 null，
  // 我们原来「读到空就 continue」，于是一直留着旧的那条）。
  const fake = createFakeHost({ seed: ["agent-left"] });
  const observedByNode = new Map<string, string>([["agent-left", "session-a"]]);
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
  await controller.dropSession(dragSession("session-b"), "right");
  const rightNodeId = fake.launched[0] as string;

  // 右窗刚起来还没报过任何会话号：这不算「新建会话」，右栏身份不能被抹掉，
  // 否则落下即配对那一步会当成空栏跳过。
  fake.notify();
  assert.equal(controller.getSnapshot().panes.right?.sessionId, "session-b");

  observedByNode.set(rightNodeId, "session-b");
  fake.notify();
  fake.calls.length = 0;

  observedByNode.delete("agent-left");
  fake.notify();

  const fresh = controller.getSnapshot();
  assert.equal(fresh.panes.left?.sessionId, null);
  assert.equal(fresh.panes.left?.session, null);
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
  const payload = (launches[0]?.args[0] as { payload?: Record<string, unknown> })
    .payload;
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
    launches[1]?.args[0] as { payload?: Record<string, unknown> }
  ).payload;
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
    pairingHost: () => ({
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
    pairingHost: () => ({
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
