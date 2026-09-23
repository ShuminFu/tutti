import { describe, expect, it } from "vitest";
import {
  orderSplitPair,
  pickSplitPairPartner,
  resolveSplitPairSelection,
  type SplitPairSelectMemory
} from "./agentGuiSplitPairSelect";
import type { SplitLayoutState } from "./agentGuiSplitLayout";

// 夹具：A / B / C 三条互为结对，D 没结对，E ↔ F 单独一对。
const PAIRS: Record<string, readonly string[]> = {
  "sess-a": ["sess-b", "sess-c"],
  "sess-b": ["sess-a", "sess-c"],
  "sess-c": ["sess-a", "sess-b"],
  "sess-d": [],
  "sess-e": ["sess-f"],
  "sess-f": ["sess-e"]
};

const activePartnersOf = (id: string): readonly string[] => PAIRS[id] ?? [];

const single = (left: string): SplitLayoutState => ({
  panes: { left, right: null },
  focus: "left",
  ratio: 0.5
});

const split = (left: string, right: string): SplitLayoutState => ({
  panes: { left, right },
  focus: "left",
  ratio: 0.5
});

function memoryOf(input: {
  order?: Record<string, readonly [string, string]>;
  lastPartner?: Record<string, string>;
}): SplitPairSelectMemory {
  return {
    lastPartnerOf: (id) => input.lastPartner?.[id] ?? null,
    orderOf: (a, b) => input.order?.[[a, b].sort().join("|")] ?? null
  };
}

describe("侧栏点选在分栏 + 结对模式下的去向", () => {
  it("单列态点一条正在结对的会话 → 一次开两列，左是它、右是搭档", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-a",
        layout: single("sess-d")
      })
    ).toEqual({ kind: "group", left: "sess-a", right: "sess-b" });
  });

  it("没有正在结对的搭档 → 单列全宽", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-d",
        layout: single("sess-a")
      })
    ).toEqual({ kind: "single", id: "sess-d" });
  });

  it("双列态点没结对的会话 → 收成单列全宽", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-d",
        layout: split("sess-a", "sess-b")
      })
    ).toEqual({ kind: "single", id: "sess-d" });
  });

  it("双列态点和右栏也在结对的会话 → 只换左栏", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-c",
        layout: split("sess-a", "sess-b")
      })
    ).toEqual({ kind: "swap-left", id: "sess-c" });
  });

  it("双列态点与右栏无关但自己有结对的会话 → 整组切换", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-e",
        layout: split("sess-a", "sess-b")
      })
    ).toEqual({ kind: "group", left: "sess-e", right: "sess-f" });
  });

  it("点已经在栏里的那条 → 只移焦点，布局不动", () => {
    for (const id of ["sess-a", "sess-b"]) {
      expect(
        resolveSplitPairSelection({
          activePartnersOf,
          id,
          layout: split("sess-a", "sess-b")
        })
      ).toEqual({ kind: "focus" });
    }
  });

  it("空串不改布局", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "   ",
        layout: split("sess-a", "sess-b")
      })
    ).toEqual({ kind: "focus" });
  });

  it("记住的左右顺序优先于「点的那条在左」", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-a",
        layout: single("sess-d"),
        memory: memoryOf({ order: { "sess-a|sess-b": ["sess-b", "sess-a"] } })
      })
    ).toEqual({ kind: "group", left: "sess-b", right: "sess-a" });
  });

  it("记录没覆盖这两条时当没记过", () => {
    expect(
      orderSplitPair(
        "sess-a",
        "sess-b",
        memoryOf({ order: { "sess-a|sess-b": ["sess-a", "sess-z"] } })
      )
    ).toEqual({ left: "sess-a", right: "sess-b" });
  });

  it("多个搭档时默认第一个，记过就用上次那个", () => {
    expect(pickSplitPairPartner("sess-a", PAIRS["sess-a"] ?? [], null)).toBe(
      "sess-b"
    );
    expect(
      pickSplitPairPartner(
        "sess-a",
        PAIRS["sess-a"] ?? [],
        memoryOf({ lastPartner: { "sess-a": "sess-c" } })
      )
    ).toBe("sess-c");
  });

  it("上次那个搭档已经不在候选里 → 退回第一个", () => {
    expect(
      pickSplitPairPartner(
        "sess-a",
        PAIRS["sess-a"] ?? [],
        memoryOf({ lastPartner: { "sess-a": "sess-gone" } })
      )
    ).toBe("sess-b");
  });

  it("记住的搭档也参与「整组切换」的选边", () => {
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-a",
        layout: single("sess-d"),
        memory: memoryOf({ lastPartner: { "sess-a": "sess-c" } })
      })
    ).toEqual({ kind: "group", left: "sess-a", right: "sess-c" });
  });

  it("双列态下「和右栏结对」优先于记住的搭档", () => {
    // 右栏是 B，点 C：C 记着上次配 A，但 C 和 B 也在结对 → 只换左栏。
    expect(
      resolveSplitPairSelection({
        activePartnersOf,
        id: "sess-c",
        layout: split("sess-a", "sess-b"),
        memory: memoryOf({ lastPartner: { "sess-c": "sess-a" } })
      })
    ).toEqual({ kind: "swap-left", id: "sess-c" });
  });
});
