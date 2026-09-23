import assert from "node:assert/strict";
import test from "node:test";
import { createEmbeddedSplitLayoutStore } from "./embeddedSplitLayoutStore.ts";

test("拖分界线连写：宿主写入一次只放一个在途，最后落盘的是最后一次", async () => {
  // 每个 pointermove 都会 write；并发 fetch 到宿主时先后不定，存下来的可能是
  // 拖到一半的比例。串行 + 只留最新 = 在途时来的都合成最后那一份。
  const sent: number[] = [];
  const pending: (() => void)[] = [];
  const store = createEmbeddedSplitLayoutStore({
    hostWrite: (args) => {
      sent.push(args.layout.ratio);
      return new Promise((resolve) => {
        pending.push(() =>
          resolve({ layout: args.layout, pairOrder: {} } as never)
        );
      });
    },
    scopeKey: "ws-1:all",
    storage: null,
    targetId: "target-1"
  });
  const layout = (ratio: number) => ({
    focus: "left" as const,
    panes: { left: "session-a", right: "session-b" },
    ratio
  });

  for (const ratio of [0.3, 0.35, 0.4, 0.45, 0.5]) {
    store.write({ layout: layout(ratio), pairOrder: {} });
  }
  assert.deepEqual(sent, [0.3]);

  pending.shift()?.();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(sent, [0.3, 0.5]);

  pending.shift()?.();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(sent, [0.3, 0.5]);
});
