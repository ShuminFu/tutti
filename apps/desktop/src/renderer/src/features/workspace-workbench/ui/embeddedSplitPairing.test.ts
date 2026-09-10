import assert from "node:assert/strict";
import test from "node:test";
import type {
  ConversationRailCreatePeerPairInput,
  ConversationRailPeerPair
} from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  pairOnDrop,
  type EmbeddedSplitPairingSide,
  type EmbeddedSplitToast
} from "./embeddedSplitPairing.ts";

const labels = {
  paired: "已配对",
  relaunchTimeout: "重开超时",
  unmanaged: "未托管会话不能配对"
};

function toastSpy(): EmbeddedSplitToast & { calls: [string, string][] } {
  const calls: [string, string][] = [];
  return {
    calls,
    error: (message) => calls.push(["error", message]),
    info: (message) => calls.push(["info", message]),
    success: (message) => calls.push(["success", message])
  };
}

function side(
  sessionId: string,
  overrides: Partial<EmbeddedSplitPairingSide> = {}
): EmbeddedSplitPairingSide {
  return { isImported: false, sessionId, status: "running", ...overrides };
}

function endpoint(sessionId: string) {
  return {
    alias: sessionId,
    cwd: "/tmp",
    provider: "claude",
    sessionId,
    status: "running",
    taskId: `task-${sessionId}`,
    title: sessionId
  };
}

function pairOf(a: string, b: string): ConversationRailPeerPair {
  return { a: endpoint(a), b: endpoint(b), pairId: `${a}~${b}` };
}

function hostSpy(
  result:
    | { ok: { pairId: string; relaunchedSessionId?: string } }
    | { error: Error },
  pairs: ConversationRailPeerPair[] = []
) {
  const created: ConversationRailCreatePeerPairInput[] = [];
  const listed: number[] = [];
  return {
    created,
    host: {
      createPeerPair: async (input: ConversationRailCreatePeerPairInput) => {
        created.push(input);
        if ("error" in result) throw result.error;
        return result.ok;
      },
      deletePeerPair: async () => undefined,
      listPeerPairs: async () => {
        listed.push(1);
        return { pairs };
      }
    },
    listed
  };
}

test("already paired panes pair silently: zero stub calls, zero toasts", async () => {
  const toast = toastSpy();
  const spy = hostSpy({ ok: { pairId: "p1" } });
  const result = await pairOnDrop({
    focus: "right",
    host: spy.host,
    labels,
    left: side("a"),
    pairs: [pairOf("a", "b")],
    right: side("b"),
    toast
  });

  // 「零调用」放在最前面：回退看红时先炸的就该是它（PRD 回退看红那一条）。
  assert.deepEqual(spy.created, []);
  assert.deepEqual(spy.listed, []);
  assert.deepEqual(toast.calls, []);
  assert.equal(result.outcome, "already");
});

test("an unmanaged pane skips pairing with the unmanaged toast", async () => {
  const toast = toastSpy();
  const spy = hostSpy({ ok: { pairId: "p1" } });
  const result = await pairOnDrop({
    focus: "right",
    host: spy.host,
    labels,
    left: side("a"),
    pairs: [],
    right: side("b", { isImported: true }),
    toast
  });

  assert.equal(result.outcome, "unmanaged");
  assert.deepEqual(spy.created, []);
  assert.deepEqual(spy.listed, []);
  assert.deepEqual(toast.calls, [["info", "未托管会话不能配对"]]);
});

test("a closed target is paired with relaunchClosed and reports the new session", async () => {
  const toast = toastSpy();
  const spy = hostSpy({
    ok: { pairId: "p1", relaunchedSessionId: "b-relaunched" }
  });
  const result = await pairOnDrop({
    focus: "left",
    host: spy.host,
    labels,
    left: side("a"),
    pairs: [],
    right: side("b", { status: "completed" }),
    toast
  });

  assert.deepEqual(spy.created, [
    {
      aliasForFrom: "",
      aliasForTo: "",
      from: "a",
      relaunchClosed: true,
      to: "b"
    }
  ]);
  assert.equal(result.outcome, "paired");
  assert.equal(result.relaunchedSessionId, "b-relaunched");
  assert.deepEqual(toast.calls, [["success", "已配对"]]);
  assert.deepEqual(spy.listed, [1]);
});

test("the focused pane is the pairing initiator", async () => {
  const toast = toastSpy();
  const spy = hostSpy({ ok: { pairId: "p1" } });
  await pairOnDrop({
    focus: "right",
    host: spy.host,
    labels,
    left: side("a"),
    pairs: [],
    right: side("b"),
    toast
  });

  assert.equal(spy.created[0]?.from, "b");
  assert.equal(spy.created[0]?.to, "a");
  assert.equal(spy.created[0]?.relaunchClosed, false);
});

test("a failed pairing toasts the backend message verbatim and keeps the layout", async () => {
  const toast = toastSpy();
  const spy = hostSpy({ error: new Error("对端已被删除，无法配对") });
  const result = await pairOnDrop({
    focus: "left",
    host: spy.host,
    labels,
    left: side("a"),
    pairs: [],
    right: side("b"),
    toast
  });

  assert.equal(result.outcome, "failed");
  assert.equal(result.relaunchedSessionId, undefined);
  assert.deepEqual(toast.calls, [["error", "对端已被删除，无法配对"]]);
});

test("an unsupported bridge error skips the whole step without a toast", async () => {
  const toast = toastSpy();
  const spy = hostSpy({ error: new Error("unsupported") });
  const result = await pairOnDrop({
    focus: "left",
    host: spy.host,
    labels,
    left: side("a"),
    pairs: [],
    right: side("b"),
    toast
  });

  assert.equal(result.outcome, "unsupported");
  assert.deepEqual(toast.calls, []);
});

test("a missing pairing host makes zero calls and zero toasts", async () => {
  const toast = toastSpy();
  const result = await pairOnDrop({
    focus: "left",
    host: null,
    labels,
    left: side("a"),
    pairs: [],
    right: side("b"),
    toast
  });

  assert.equal(result.outcome, "unsupported");
  assert.deepEqual(toast.calls, []);
});

test("an empty pane skips pairing entirely", async () => {
  const toast = toastSpy();
  const spy = hostSpy({ ok: { pairId: "p1" } });
  const result = await pairOnDrop({
    focus: "left",
    host: spy.host,
    labels,
    left: side("a"),
    pairs: [],
    right: null,
    toast
  });

  assert.equal(result.outcome, "skipped");
  assert.deepEqual(spy.created, []);
  assert.deepEqual(toast.calls, []);
});
