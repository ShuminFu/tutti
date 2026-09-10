import assert from "node:assert/strict";
import test from "node:test";
import { buildEmbeddedSplitPaneHeaders } from "./embeddedSplitPaneHeader.ts";
import type {
  EmbeddedSplitPaneSnapshot,
  EmbeddedSplitViewSnapshot
} from "./embeddedSplitView.ts";

const labels = {
  closePane: "关闭此栏",
  menu: "更多",
  pair: "配对",
  resetRatio: "重置分栏比例",
  swapPanes: "交换左右",
  unpair: "解除",
  untitled: "未命名会话"
};

function pane(
  sessionId: string,
  session: EmbeddedSplitPaneSnapshot["session"] = null
): EmbeddedSplitPaneSnapshot {
  return { nodeId: `node-${sessionId}`, session, sessionId };
}

function snapshot(
  overrides: Partial<EmbeddedSplitViewSnapshot> = {}
): EmbeddedSplitViewSnapshot {
  return {
    collapsed: false,
    dragging: null,
    focus: "left",
    pairing: "unpaired",
    panes: {
      left: pane("session-a", {
        iconUrl: "claude.png",
        projectLabel: "rndmaster",
        provider: "claude",
        title: "改栏头"
      }),
      right: pane("session-b", {
        iconUrl: null,
        projectLabel: null,
        provider: "codex",
        title: "跑验证"
      })
    },
    ratio: 0.6,
    ...overrides
  };
}

test("两栏各一条栏头，标题 / 项目胶囊 / 宽度都跟着快照走", () => {
  const headers = buildEmbeddedSplitPaneHeaders(snapshot(), labels);

  assert.deepEqual(
    headers.map((header) => [
      header.side,
      header.title,
      header.projectLabel,
      header.leftFraction,
      header.widthFraction
    ]),
    [
      ["left", "改栏头", "rndmaster", 0, 0.6],
      ["right", "跑验证", null, 0.6, 0.4]
    ]
  );
  assert.equal(headers[0]?.iconUrl, "claude.png");
});

// 焦点只体现在「哪一栏的标题加重」上：票 04 拿掉了那条 2px 蓝横线，
// 模型里也不该再冒出别的焦点装饰。
test("焦点只切换 focused 这一位", () => {
  const left = buildEmbeddedSplitPaneHeaders(snapshot(), labels);
  const right = buildEmbeddedSplitPaneHeaders(
    snapshot({ focus: "right" }),
    labels
  );

  assert.deepEqual(
    left.map((header) => header.focused),
    [true, false]
  );
  assert.deepEqual(
    right.map((header) => header.focused),
    [false, true]
  );
});

// 侧栏还没把摘要报上来（例如刚从本地记录恢复布局）时不能把会话号糊在栏头上。
test("没有摘要时退回兜底标题，不写会话号", () => {
  const headers = buildEmbeddedSplitPaneHeaders(
    snapshot({
      panes: { left: pane("session-a"), right: pane("session-b") }
    }),
    labels
  );

  assert.deepEqual(
    headers.map((header) => header.title),
    ["未命名会话", "未命名会话"]
  );
  assert.equal(headers[0]?.projectLabel, null);
});

test("链条：已配对给「解除」，配对能力不可用时整枚不画，忙的时候不可点", () => {
  const unpaired = buildEmbeddedSplitPaneHeaders(snapshot(), labels);
  assert.equal(unpaired[0]?.pairingState, "unpaired");
  assert.equal(unpaired[0]?.pairingLabel, "配对");
  assert.equal(unpaired[0]?.pairingEnabled, true);

  const paired = buildEmbeddedSplitPaneHeaders(
    snapshot({ pairing: "paired" }),
    labels
  );
  assert.equal(paired[1]?.pairingLabel, "解除");

  const pending = buildEmbeddedSplitPaneHeaders(
    snapshot({ pairing: "pending" }),
    labels
  );
  assert.equal(pending[0]?.pairingEnabled, false);

  const unsupported = buildEmbeddedSplitPaneHeaders(
    snapshot({ pairing: "unsupported" }),
    labels
  );
  assert.equal(unsupported[0]?.pairingState, null);
});

// ⋯ 里只放 controller 真的做得到的两件事（resetRatio / swapPanes）：
// 没有对应能力的入口不能出现在菜单里。
test("⋯ 菜单只有重置比例与交换左右", () => {
  const headers = buildEmbeddedSplitPaneHeaders(snapshot(), labels);
  assert.deepEqual(headers[0]?.menuItems, [
    { id: "resetRatio", label: "重置分栏比例" },
    { id: "swapPanes", label: "交换左右" }
  ]);
});

test("单栏时没有栏头；折叠时只有焦点栏那一条、占满整宽", () => {
  const single = buildEmbeddedSplitPaneHeaders(
    snapshot({ panes: { left: pane("session-a"), right: null } }),
    labels
  );
  assert.deepEqual(single, []);

  const collapsed = buildEmbeddedSplitPaneHeaders(
    snapshot({ collapsed: true, focus: "right" }),
    labels
  );
  assert.equal(collapsed.length, 1);
  assert.equal(collapsed[0]?.side, "right");
  assert.equal(collapsed[0]?.leftFraction, 0);
  assert.equal(collapsed[0]?.widthFraction, 1);
});
