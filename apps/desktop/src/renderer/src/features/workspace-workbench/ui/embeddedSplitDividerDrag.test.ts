import assert from "node:assert/strict";
import test from "node:test";
import {
  beginEmbeddedSplitDividerDrag,
  embeddedSplitDividerDragRatio
} from "./embeddedSplitDividerDrag.ts";

const surface = { surfaceLeft: 100, surfaceWidth: 1000 };

test("按在线正中间、原地不动，ratio 不变（不会先弹一下）", () => {
  // 会话栏展开：push = 0.25，分隔线画在 0.6 + 0.25 = 0.85 → 客户端 x = 950。
  const drag = beginEmbeddedSplitDividerDrag({
    clientX: 950,
    dividerRatio: 0.85,
    ...surface
  });
  assert.ok(drag);
  assert.equal(
    embeddedSplitDividerDragRatio(drag, 950, 0.25),
    0.6
  );
});

test("分隔线跟着指针走：指针移 100px，ratio 正好加 0.1", () => {
  const drag = beginEmbeddedSplitDividerDrag({
    clientX: 950,
    dividerRatio: 0.85,
    ...surface
  });
  assert.ok(drag);
  const next = embeddedSplitDividerDragRatio(drag, 1050, 0.25);
  assert.ok(next !== null);
  // 新的分隔线位置 = ratio + push = 0.7 + 0.25 = 0.95 → x = 1050，和指针重合。
  assert.ok(Math.abs(next - 0.7) < 1e-9);
  assert.ok(Math.abs((next + 0.25) * 1000 + 100 - 1050) < 1e-6);
});

test("抓在命中带边缘也不跳：按下时的偏移一直保留", () => {
  // 命中带 10px 宽，抓在线右边 4px。
  const drag = beginEmbeddedSplitDividerDrag({
    clientX: 954,
    dividerRatio: 0.85,
    ...surface
  });
  assert.ok(drag);
  assert.equal(embeddedSplitDividerDragRatio(drag, 954, 0.25), 0.6);
});

test("拖动中 railPush 被夹小，分隔线仍贴着指针", () => {
  const drag = beginEmbeddedSplitDividerDrag({
    clientX: 950,
    dividerRatio: 0.85,
    ...surface
  });
  assert.ok(drag);
  const next = embeddedSplitDividerDragRatio(drag, 1000, 0.18);
  assert.ok(next !== null);
  assert.ok(Math.abs((next + 0.18) * 1000 + 100 - 1000) < 1e-6);
});

test("主区宽度为 0 时不开始拖动", () => {
  assert.equal(
    beginEmbeddedSplitDividerDrag({
      clientX: 950,
      dividerRatio: 0.85,
      surfaceLeft: 100,
      surfaceWidth: 0
    }),
    null
  );
});
