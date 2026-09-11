import { describe, expect, it } from "vitest";
import {
  createEmptySplitLayout,
  isSplitLayoutSplit,
  loadSplitLayout,
  parseSplitLayout,
  reduceSplitLayout,
  saveSplitLayout,
  serializeSplitLayout,
  splitLayoutActiveConversationId,
  splitLayoutStorageKey,
  type SplitLayoutState
} from "./agentGuiSplitLayout";

// Fixtures. The two-pane fixture MUST have both slots filled and focus on
// RIGHT: otherwise "replace the focused pane" and "replace the left pane"
// are indistinguishable.
const single: SplitLayoutState = {
  panes: { left: "sess-a", right: null },
  focus: "left",
  ratio: 0.5
};
const twoPanes: SplitLayoutState = {
  panes: { left: "sess-a", right: "sess-b" },
  focus: "right",
  ratio: 0.4
};

describe("agent GUI split layout state machine", () => {
  it("starts empty with focus left and ratio 0.5", () => {
    expect(createEmptySplitLayout()).toEqual({
      panes: { left: null, right: null },
      focus: "left",
      ratio: 0.5
    });
    expect(isSplitLayoutSplit(createEmptySplitLayout())).toBe(false);
    expect(isSplitLayoutSplit(twoPanes)).toBe(true);
  });

  describe("drop", () => {
    it("drops a new id on the left of a single pane and opens split", () => {
      // The sitting conversation moves to the empty right slot instead of
      // being overwritten: a drop on either half of a single pane splits.
      const next = reduceSplitLayout(single, {
        type: "drop",
        id: "sess-c",
        side: "left"
      });
      expect(next.panes).toEqual({ left: "sess-c", right: "sess-a" });
      expect(next.focus).toBe("left");
    });

    it("drops a new id on the right of a single pane and opens split", () => {
      const next = reduceSplitLayout(single, {
        type: "drop",
        id: "sess-c",
        side: "right"
      });
      expect(next.panes).toEqual({ left: "sess-a", right: "sess-c" });
      expect(next.focus).toBe("right");
    });

    it("drops on the right of an empty layout into the left slot", () => {
      const next = reduceSplitLayout(createEmptySplitLayout(), {
        type: "drop",
        id: "sess-c",
        side: "right"
      });
      expect(next.panes).toEqual({ left: "sess-c", right: null });
      expect(next.focus).toBe("left");
    });

    it("drops a third id on a two-pane layout and replaces only that side", () => {
      const left = reduceSplitLayout(twoPanes, {
        type: "drop",
        id: "sess-c",
        side: "left"
      });
      expect(left.panes).toEqual({ left: "sess-c", right: "sess-b" });
      expect(left.focus).toBe("left");

      const right = reduceSplitLayout(twoPanes, {
        type: "drop",
        id: "sess-c",
        side: "right"
      });
      expect(right.panes).toEqual({ left: "sess-a", right: "sess-c" });
      expect(right.focus).toBe("right");
    });

    it("drops an id already in the other slot and only moves focus", () => {
      // sess-a lives in left; dropping it on the right must not duplicate it.
      const next = reduceSplitLayout(twoPanes, {
        type: "drop",
        id: "sess-a",
        side: "right"
      });
      expect(next.panes).toEqual({ left: "sess-a", right: "sess-b" });
      expect(next.focus).toBe("left");
    });

    it("drops an id on its own slot and focuses that slot", () => {
      const focusedLeft: SplitLayoutState = { ...twoPanes, focus: "left" };
      const next = reduceSplitLayout(focusedLeft, {
        type: "drop",
        id: "sess-b",
        side: "right"
      });
      expect(next.panes).toEqual(twoPanes.panes);
      expect(next.focus).toBe("right");
    });
  });

  describe("select", () => {
    it("replaces only the focused (right) slot with an unknown id", () => {
      const next = reduceSplitLayout(twoPanes, {
        type: "select",
        id: "sess-c"
      });
      expect(next.panes).toEqual({ left: "sess-a", right: "sess-c" });
      expect(next.focus).toBe("right");
    });

    it("moves focus to left when the id is already in the left slot", () => {
      const next = reduceSplitLayout(twoPanes, {
        type: "select",
        id: "sess-a"
      });
      expect(next.panes).toEqual(twoPanes.panes);
      expect(next.focus).toBe("left");
    });

    it("replaces the single pane (left) with an unknown id", () => {
      const next = reduceSplitLayout(single, {
        type: "select",
        id: "sess-c"
      });
      expect(next.panes).toEqual({ left: "sess-c", right: null });
      expect(next.focus).toBe("left");
    });

    it("fills the left slot of an empty layout", () => {
      const next = reduceSplitLayout(createEmptySplitLayout(), {
        type: "select",
        id: "sess-c"
      });
      expect(next.panes).toEqual({ left: "sess-c", right: null });
    });
  });

  describe("close", () => {
    it("closes left and moves the survivor into left", () => {
      const next = reduceSplitLayout(twoPanes, { type: "close", side: "left" });
      expect(next.panes).toEqual({ left: "sess-b", right: null });
      expect(next.focus).toBe("left");
      expect(next.ratio).toBe(0.4);
    });

    it("closes right and keeps the survivor in left", () => {
      const next = reduceSplitLayout(twoPanes, {
        type: "close",
        side: "right"
      });
      expect(next.panes).toEqual({ left: "sess-a", right: null });
      expect(next.focus).toBe("left");
    });

    it("closes the only pane into an empty layout", () => {
      const next = reduceSplitLayout(single, { type: "close", side: "left" });
      expect(next.panes).toEqual({ left: null, right: null });
      expect(next.focus).toBe("left");
    });
  });

  describe("resize", () => {
    it("clamps the ratio to [min, 1 - min] on both ends", () => {
      expect(
        reduceSplitLayout(twoPanes, { type: "resize", ratio: 0.05 }).ratio
      ).toBe(0.2);
      expect(
        reduceSplitLayout(twoPanes, { type: "resize", ratio: 0.97 }).ratio
      ).toBe(0.8);
      expect(
        reduceSplitLayout(twoPanes, { type: "resize", ratio: 0.1 }, {
          minRatio: 0.3
        }).ratio
      ).toBe(0.3);
      expect(
        reduceSplitLayout(twoPanes, { type: "resize", ratio: 0.55 }).ratio
      ).toBe(0.55);
    });

    it("ignores resize while in single pane", () => {
      const next = reduceSplitLayout(single, { type: "resize", ratio: 0.3 });
      expect(next).toBe(single);
    });
  });

  describe("restore", () => {
    const stored: SplitLayoutState = {
      panes: { left: "sess-a", right: "sess-b" },
      focus: "right",
      ratio: 0.35
    };

    it("keeps the stored layout when both conversations are alive", () => {
      const next = reduceSplitLayout(createEmptySplitLayout(), {
        type: "restore",
        stored,
        alive: new Set(["sess-a", "sess-b", "sess-z"])
      });
      expect(next).toEqual(stored);
    });

    it("falls back to a single pane when only one conversation is alive", () => {
      const onlyRight = reduceSplitLayout(createEmptySplitLayout(), {
        type: "restore",
        stored,
        alive: ["sess-b"]
      });
      expect(onlyRight).toEqual({
        panes: { left: "sess-b", right: null },
        focus: "left",
        ratio: 0.35
      });

      const onlyLeft = reduceSplitLayout(createEmptySplitLayout(), {
        type: "restore",
        stored,
        alive: ["sess-a"]
      });
      expect(onlyLeft.panes).toEqual({ left: "sess-a", right: null });
      expect(onlyLeft.focus).toBe("left");
    });

    it("returns an empty layout when neither conversation is alive", () => {
      const next = reduceSplitLayout(single, {
        type: "restore",
        stored,
        alive: []
      });
      expect(next).toEqual(createEmptySplitLayout());
    });

    it("leaves state unchanged when nothing was stored", () => {
      const next = reduceSplitLayout(single, {
        type: "restore",
        stored: null,
        alive: ["sess-a"]
      });
      expect(next).toBe(single);
    });

    it("clamps a stored out-of-range ratio", () => {
      const next = reduceSplitLayout(createEmptySplitLayout(), {
        type: "restore",
        stored: { ...stored, ratio: 0.01 },
        alive: ["sess-a", "sess-b"]
      });
      expect(next.ratio).toBe(0.2);
    });
  });

  describe("activeConversationId", () => {
    it("reports the focused slot", () => {
      expect(splitLayoutActiveConversationId(twoPanes)).toBe("sess-b");
      expect(
        splitLayoutActiveConversationId({ ...twoPanes, focus: "left" })
      ).toBe("sess-a");
    });

    it("falls back to the other slot when the focused one is empty", () => {
      expect(
        splitLayoutActiveConversationId({ ...single, focus: "right" })
      ).toBe("sess-a");
      expect(splitLayoutActiveConversationId(createEmptySplitLayout())).toBe(
        null
      );
    });
  });

  describe("persistence", () => {
    it("keys storage by workspace and exact target like the rail view state", () => {
      expect(
        splitLayoutStorageKey({
          workspaceId: " workspace-1 ",
          targetId: " local:codex "
        })
      ).toBe("agent-gui:split-layout:workspace-1:agentTarget:local:codex");
      expect(
        splitLayoutStorageKey({ workspaceId: "workspace-1", targetId: null })
      ).toBe("agent-gui:split-layout:workspace-1:all");
    });

    it("round-trips through serialize / parse", () => {
      expect(parseSplitLayout(serializeSplitLayout(twoPanes))).toEqual(
        twoPanes
      );
      expect(parseSplitLayout(serializeSplitLayout(single))).toEqual(single);
    });

    it("tolerates garbage input", () => {
      expect(parseSplitLayout(null)).toBeNull();
      expect(parseSplitLayout("")).toBeNull();
      expect(parseSplitLayout("not json")).toBeNull();
      expect(parseSplitLayout("[1,2]")).toBeNull();
      expect(parseSplitLayout('{"left":42}')).toBeNull();
      // Missing fields default; a lone right pane is normalized into left.
      expect(parseSplitLayout('{"right":"sess-b"}')).toEqual({
        panes: { left: "sess-b", right: null },
        focus: "left",
        ratio: 0.5
      });
    });

    it("loads and saves through a storage object without throwing", () => {
      const store = new Map<string, string>();
      const storage = {
        getItem: (key: string) => store.get(key) ?? null,
        setItem: (key: string, value: string) => {
          store.set(key, value);
        }
      };
      saveSplitLayout(storage, "k", twoPanes);
      expect(loadSplitLayout(storage, "k")).toEqual(twoPanes);
      expect(loadSplitLayout(storage, "missing")).toBeNull();

      const broken = {
        getItem: () => {
          throw new Error("quota");
        },
        setItem: () => {
          throw new Error("quota");
        }
      };
      expect(() => saveSplitLayout(broken, "k", twoPanes)).not.toThrow();
      expect(loadSplitLayout(broken, "k")).toBeNull();
    });
  });
});
