// 分栏结对模式（peer-pair-mode 票 05）：第一句带开工卡。
//
// 把两段真代码接起来测：分栏 controller 的 preparePairKickoff（判 pending、preview、
// commit）+ gui 包的 runPreparedAgentPromptSubmit（拼块、发送、等引擎结局、回调）。
// 只有「发送」与「引擎结局」是桩：send 记下真正发出去的内容，settle 由用例决定。
import assert from "node:assert/strict";
import test from "node:test";
import {
  conversationRailSplitHost,
  runPreparedAgentPromptSubmit,
  type AgentComposerHostExtension,
  type ConversationRailSplitDragSession
} from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  createEmbeddedSplitViewController,
  type EmbeddedSplitViewController,
  type EmbeddedSplitViewHost
} from "./embeddedSplitView.ts";

const BLOCK =
  '<pair-kickoff role="developer" partner="codex-b" partner_provider="codex" partner_role="reviewer" reason="start">\n协议正文\n</pair-kickoff>';

interface Row {
  developerTaskId: string;
  kickoffState: "" | "pending" | "sent";
  pairMode: "solo" | "pair";
}

function fakeWorkbenchHost(): EmbeddedSplitViewHost {
  let nodes: { data: { typeId: string }; id: string }[] = [];
  let next = 0;
  const surfaceSize = { height: 800, width: 1200 };
  return {
    activateNode() {},
    closeNode(nodeId) {
      nodes = nodes.filter((node) => node.id !== nodeId);
    },
    controller: {
      getSnapshot: () => ({ surfaceSize }),
      subscribe: () => () => {}
    },
    focusNode() {},
    getSnapshot: () => ({ nodes, surfaceSize }),
    async launchNode() {
      const id = `agent-launched-${++next}`;
      nodes = [...nodes, { data: { typeId: "agent-gui" }, id }];
      return id;
    }
  };
}

function session(id: string): ConversationRailSplitDragSession {
  return {
    iconUrl: null,
    id,
    isImported: false,
    provider: "claude",
    status: "working",
    title: id
  };
}

interface Harness {
  calls: { args: unknown; kind: string }[];
  controller: EmbeddedSplitViewController;
  errors: string[];
  extension: AgentComposerHostExtension;
  row: Row;
}

async function harness(
  options: {
    commit?: () => Promise<{ delivered: boolean; reason?: string } | void>;
    preview?: () => Promise<{ block: string }>;
    row?: Partial<Row>;
  } = {}
): Promise<Harness> {
  const row: Row = {
    developerTaskId: "task-a",
    kickoffState: "pending",
    pairMode: "pair",
    ...options.row
  };
  const calls: { args: unknown; kind: string }[] = [];
  const errors: string[] = [];
  const pairRow = () => ({
    a: { sessionId: "session-a", taskId: "task-a", title: "a" },
    b: { sessionId: "session-b", taskId: "task-b", title: "b" },
    developerTaskId: row.developerTaskId,
    kickoffState: row.kickoffState,
    pairId: "p1",
    pairMode: row.pairMode
  });
  const pairingHost = {
    async commitPairKickoff(args: unknown) {
      calls.push({ args, kind: "commit" });
      const outcome = options.commit ? await options.commit() : undefined;
      if (outcome && outcome.delivered === false) {
        return { delivered: false, pair: pairRow(), reason: outcome.reason };
      }
      row.kickoffState = "sent";
      return { delivered: true, pair: pairRow() };
    },
    createPeerPair: async () => ({ pairId: "p1" }),
    deletePeerPair: async () => undefined,
    listPeerPairs: async () => ({ pairs: [pairRow()] }),
    async previewPairKickoff(args: unknown) {
      calls.push({ args, kind: "preview" });
      return options.preview ? options.preview() : { block: BLOCK };
    },
    setPeerPairMode: async () => ({ pair: pairRow() })
  };
  const controller = createEmbeddedSplitViewController({
    geometry: {
      railRightEdge: () => 300,
      surfaceRect: () => ({ height: 800, left: 0, top: 0, width: 1200 })
    },
    host: fakeWorkbenchHost(),
    labels: () => ({
      kickoffCommitFailed: "搭档没收到开工卡",
      kickoffPreviewFailed: "开工卡没准备好",
      paired: "已配对",
      relaunchTimeout: "超时",
      unmanaged: "未托管"
    }),
    pairingHost: () => pairingHost as never,
    storage: null,
    toast: { error: (m) => errors.push(m), info() {}, success() {} },
    workspaceId: "ws-1"
  });
  await controller.adopt(["agent-left"]);
  conversationRailSplitHost()?.describeSession?.(session("session-a"));
  controller.select("session-a");
  await controller.dropSession(session("session-b"), "right");
  await controller.pairPanes();
  controller.setFocus("left");
  // 与生产里 installEmbeddedSplitComposerHost 同一份转发。
  const extension: AgentComposerHostExtension = {
    prepareSubmit: (input) => controller.preparePairKickoff(input),
    wantsSubmitPreparation: ({ agentSessionId }) =>
      controller.wantsPairKickoff(agentSessionId)
  };
  return { calls, controller, errors, extension, row };
}

type Content = { text?: string; type: "image" | "text"; url?: string }[];

/** 跑一次提交，返回真正发出去的内容；settle 决定引擎结局。 */
async function submit(
  h: Harness,
  content: Content,
  settle: () => Promise<boolean> = async () => true
): Promise<{ sent: Content[] }> {
  const sent: Content[] = [];
  const text = content
    .filter((block) => block.type === "text")
    .map((block) => block.text ?? "")
    .join("")
    .trim();
  // 与 useAgentGUISubmitInteractionActions 一致：同步预判为 false 就直接发，不走异步。
  if (
    h.extension.wantsSubmitPreparation?.({ agentSessionId: "session-a", text }) !==
    true
  ) {
    sent.push(content);
    return { sent };
  }
  await runPreparedAgentPromptSubmit({
    agentSessionId: "session-a",
    content: content as never,
    extension: h.extension,
    send: (next) => {
      sent.push(next as Content);
      return { settle };
    }
  });
  return { sent };
}

const kinds = (h: Harness, kind: string) =>
  h.calls.filter((call) => call.kind === kind);

test("pending 时拦截：块拼进第一个文字块，图片块不动；被接受后才 commit", async () => {
  const h = await harness();
  const image = { type: "image" as const, url: "blob:1" };
  const { sent } = await submit(h, [image, { text: "修登录页", type: "text" }]);

  assert.deepEqual(sent, [
    [image, { text: `${BLOCK}\n\n修登录页`, type: "text" }]
  ]);
  assert.deepEqual(
    kinds(h, "preview").map((call) => call.args),
    [{ goal: "修登录页", pairId: "p1", senderTaskId: "task-a" }]
  );
  assert.deepEqual(
    kinds(h, "commit").map((call) => call.args),
    [{ goal: "修登录页", pairId: "p1", senderTaskId: "task-a" }]
  );
  // commit 之后配对表重拉，行是 sent：下一句不再拦截。
  assert.equal(h.controller.getSnapshot().pairMode?.kickoffState, "sent");
  assert.equal(h.controller.wantsPairKickoff("session-a"), false);
  h.controller.dispose();
});

test("非 pending（独立模式 / 已开工）不拦截，零 preview", async () => {
  for (const row of [
    { developerTaskId: "", kickoffState: "" as const, pairMode: "solo" as const },
    { kickoffState: "sent" as const }
  ]) {
    const h = await harness({ row });
    const content: Content = [{ text: "继续", type: "text" }];
    const { sent } = await submit(h, content);
    assert.deepEqual(sent, [content]);
    assert.equal(kinds(h, "preview").length, 0);
    assert.equal(kinds(h, "commit").length, 0);
    h.controller.dispose();
  }
});

test("斜杠命令不拦截：原样发出，零 preview", async () => {
  const h = await harness();
  const content: Content = [{ text: "/compact", type: "text" }];
  const sent: Content[] = [];
  await runPreparedAgentPromptSubmit({
    agentSessionId: "session-a",
    content: content as never,
    extension: h.extension,
    send: (next) => {
      sent.push(next as Content);
      return { settle: async () => true };
    }
  });
  assert.deepEqual(sent, [content]);
  assert.equal(kinds(h, "preview").length, 0);
  assert.equal(kinds(h, "commit").length, 0);
  h.controller.dispose();
});

test("提交没被接受：不 commit，保持 pending，下一句还会拼卡", async () => {
  const h = await harness();
  await submit(h, [{ text: "第一句", type: "text" }], async () => false);

  assert.equal(kinds(h, "commit").length, 0);
  assert.equal(h.controller.getSnapshot().pairMode?.kickoffState, "pending");
  assert.equal(h.controller.wantsPairKickoff("session-a"), true);

  const { sent } = await submit(h, [{ text: "再发一次", type: "text" }]);
  assert.deepEqual(sent, [[{ text: `${BLOCK}\n\n再发一次`, type: "text" }]]);
  assert.equal(kinds(h, "commit").length, 1);
  h.controller.dispose();
});

test("commit 失败：提示「搭档没收到开工卡」，保持 pending，下一句重试", async () => {
  let failures = 1;
  const h = await harness({
    commit: async () => {
      if (failures-- > 0) throw new Error("backend 502");
    }
  });
  await submit(h, [{ text: "第一句", type: "text" }]);

  assert.deepEqual(h.errors, ["搭档没收到开工卡：backend 502"]);
  assert.equal(h.controller.getSnapshot().pairMode?.kickoffState, "pending");
  assert.equal(h.controller.wantsPairKickoff("session-a"), true);

  const { sent } = await submit(h, [{ text: "第二句", type: "text" }]);
  assert.deepEqual(sent, [[{ text: `${BLOCK}\n\n第二句`, type: "text" }]]);
  assert.equal(kinds(h, "commit").length, 2);
  assert.equal(h.controller.getSnapshot().pairMode?.kickoffState, "sent");
  h.controller.dispose();
});

test("commit 回 delivered=false（环路闸 / 限流丢卡）按失败处理，附 reason，保持 pending", async () => {
  const h = await harness({
    commit: async () => ({ delivered: false, reason: "hop_limit" })
  });
  await submit(h, [{ text: "第一句", type: "text" }]);

  assert.deepEqual(h.errors, ["搭档没收到开工卡：hop_limit"]);
  assert.equal(h.controller.getSnapshot().pairMode?.kickoffState, "pending");
  assert.equal(h.controller.wantsPairKickoff("session-a"), true);
  h.controller.dispose();
});

test("preview 失败：不拦截、原样发送并提示，不 commit", async () => {
  const h = await harness({
    preview: async () => {
      throw new Error("kickoff_not_pending");
    }
  });
  const content: Content = [{ text: "第一句", type: "text" }];
  const { sent } = await submit(h, content);

  assert.deepEqual(sent, [content]);
  assert.deepEqual(h.errors, ["开工卡没准备好：kickoff_not_pending"]);
  assert.equal(kinds(h, "commit").length, 0);
  h.controller.dispose();
});

test("开工卡在途时同一对的第二句不再拼卡", async () => {
  let release: (value: boolean) => void = () => {};
  const h = await harness();
  const first = submit(
    h,
    [{ text: "第一句", type: "text" }],
    () =>
      new Promise<boolean>((resolve) => {
        release = resolve;
      })
  );
  // 让第一句走到「已发出、等引擎结局」。
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(h.controller.wantsPairKickoff("session-a"), false);
  release(true);
  await first;
  assert.equal(kinds(h, "commit").length, 1);
  h.controller.dispose();
});
