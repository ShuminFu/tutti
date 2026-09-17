import assert from "node:assert/strict";
import test from "node:test";
import {
  buildEmbeddedSplitPairModeBar,
  nextEmbeddedSplitPairModeChoice
} from "./embeddedSplitPairModeBar.ts";
import type {
  EmbeddedSplitPairModeSnapshot,
  EmbeddedSplitViewSnapshot
} from "./embeddedSplitView.ts";

function snapshot(
  pairMode: EmbeddedSplitPairModeSnapshot | null,
  overrides: Partial<EmbeddedSplitViewSnapshot> = {}
): EmbeddedSplitViewSnapshot {
  return {
    collapsed: false,
    dragging: null,
    focus: "left",
    pairing: "paired",
    pairMode,
    panes: {
      left: { nodeId: "n-left", session: null, sessionId: "session-a" },
      right: { nodeId: "n-right", session: null, sessionId: "session-b" }
    },
    railPushRatio: 0,
    ratio: 0.5,
    ...overrides
  };
}

const pairing: EmbeddedSplitPairModeSnapshot = {
  busy: false,
  kickoffState: "pending",
  mode: "pair",
  pairId: "p1",
  roles: { left: "developer", right: "reviewer" }
};

test("焦点栏可点、非焦点栏是同值镜像且不可点", () => {
  const left = buildEmbeddedSplitPaneBar("session-a");
  const right = buildEmbeddedSplitPaneBar("session-b");
  assert.deepEqual(left, {
    focused: true,
    interactive: true,
    side: "left",
    value: "developer"
  });
  assert.deepEqual(right, {
    focused: false,
    interactive: false,
    side: "right",
    value: "reviewer"
  });

  function buildEmbeddedSplitPaneBar(sessionId: string) {
    return buildEmbeddedSplitPairModeBar(snapshot(pairing), sessionId);
  }
});

test("没有 pairMode 投影（不支持 / 条件不满足）时整排不渲染", () => {
  assert.equal(buildEmbeddedSplitPairModeBar(snapshot(null), "session-a"), null);
  assert.equal(buildEmbeddedSplitPairModeBar(null, "session-a"), null);
});

test("不在任何一栏里的会话不渲染；独立模式两栏都是 solo；写在途时焦点栏也不可点", () => {
  assert.equal(
    buildEmbeddedSplitPairModeBar(snapshot(pairing), "session-z"),
    null
  );
  const solo = {
    ...pairing,
    kickoffState: "" as const,
    mode: "solo" as const,
    roles: { left: null, right: null }
  };
  assert.equal(
    buildEmbeddedSplitPairModeBar(snapshot(solo), "session-b")?.value,
    "solo"
  );
  assert.equal(
    buildEmbeddedSplitPairModeBar(
      snapshot({ ...pairing, busy: true }),
      "session-a"
    )?.interactive,
    false
  );
});

test("键盘左右在三项之间循环", () => {
  assert.equal(nextEmbeddedSplitPairModeChoice("solo", "ArrowRight"), "developer");
  assert.equal(nextEmbeddedSplitPairModeChoice("reviewer", "ArrowRight"), "solo");
  assert.equal(nextEmbeddedSplitPairModeChoice("solo", "ArrowLeft"), "reviewer");
  assert.equal(nextEmbeddedSplitPairModeChoice("developer", "End"), "reviewer");
  assert.equal(nextEmbeddedSplitPairModeChoice("reviewer", "Home"), "solo");
  assert.equal(nextEmbeddedSplitPairModeChoice("solo", "Enter"), null);
});
