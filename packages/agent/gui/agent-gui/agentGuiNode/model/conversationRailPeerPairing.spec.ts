import { describe, expect, it } from "vitest";
import {
  applyConversationRailPeerPairAdjacency,
  conversationRailPeerAliasPreview,
  conversationRailPeerDisplayTitle,
  conversationRailPeerPairBadgeMode,
  conversationRailPeerPairCount,
  conversationRailPeerPairIndex,
  type ConversationRailPeerPair
} from "./conversationRailPeerPairing";

// 夹具刻意让 P 是**无关**条目并排在最前：全是相关条目就测不出「其余不动」。
const ITEMS = [
  { id: "session-p" },
  { id: "session-a" },
  { id: "session-b" },
  { id: "session-c" }
];

const PAIRS: ConversationRailPeerPair[] = [
  {
    a: endpoint("session-a", "task-a", "claude", "会话 A"),
    b: endpoint("session-c", "task-c", "codex", "会话 C"),
    pairId: "pair-ac"
  }
];

describe("conversation rail peer pair adjacency", () => {
  it("lifts the active conversation's peer directly below it and leaves the rest in place", () => {
    const index = conversationRailPeerPairIndex(PAIRS);

    const adjacency = applyConversationRailPeerPairAdjacency({
      activeConversationId: "session-a",
      index,
      items: ITEMS
    });

    expect(adjacency.items.map((item) => item.id)).toEqual([
      "session-p",
      "session-a",
      "session-c",
      "session-b"
    ]);
    expect([...adjacency.groupedSessionIds].sort()).toEqual([
      "session-a",
      "session-c"
    ]);
    expect(conversationRailPeerPairCount(index, "session-a")).toBe(1);
    expect(conversationRailPeerPairCount(index, "session-c")).toBe(1);
    expect(conversationRailPeerPairCount(index, "session-b")).toBe(0);
    expect(conversationRailPeerPairCount(index, "session-p")).toBe(0);
  });

  it("restores the original order once the active conversation has no peers", () => {
    const index = conversationRailPeerPairIndex(PAIRS);

    const adjacency = applyConversationRailPeerPairAdjacency({
      activeConversationId: "session-b",
      index,
      items: ITEMS
    });

    expect(adjacency.items.map((item) => item.id)).toEqual([
      "session-p",
      "session-a",
      "session-b",
      "session-c"
    ]);
    expect(adjacency.groupedSessionIds.size).toBe(0);
  });

  it("previews the alias the host will assign as provider plus the first 4 id characters", () => {
    expect(conversationRailPeerAliasPreview("Codex", "abcd1234")).toBe(
      "codex-abcd"
    );
  });
});

function endpoint(
  sessionId: string,
  taskId: string,
  provider: string,
  title: string
) {
  return {
    alias: `${provider}-${sessionId.slice(0, 4)}`,
    cwd: "/workspace",
    provider,
    sessionId,
    status: "ready",
    taskId,
    title
  };
}

// T3：菜单/toast/子菜单显示的标题。真机上条目 title 就是整段 prompt，
// 不剥就把菜单撑到窗口外，而且每条 peer 开头一模一样。
describe("conversation rail peer display title", () => {
  it("strips the backend prompt boilerplate so the title is the user's own line", () => {
    expect(
      conversationRailPeerDisplayTitle(
        "请根据下面的「任务元数据」和「用户填写」完成本次任务。\n\n把 peer_list 的返回贴出来"
      )
    ).toBe("把 peer_list 的返回贴出来");
    // 半角引号写法同样要剥掉。
    expect(
      conversationRailPeerDisplayTitle(
        '请根据下面的"任务元数据"和"用户填写"完成本次任务。 裸的一行'
      )
    ).toBe("裸的一行");
  });

  it("takes the first prose line of the last section, matching the backend peerTaskTitle", () => {
    expect(
      conversationRailPeerDisplayTitle(
        "请根据下面的「任务元数据」和「用户填写」完成本次任务。\n\n" +
          "## 任务元数据\n不是这一行\n\n## 用户填写\n这才是用户写的那句"
      )
    ).toBe("这才是用户写的那句");
  });

  it("truncates past 40 characters with an ellipsis", () => {
    const long = "а".repeat(60);
    const got = conversationRailPeerDisplayTitle(long);
    expect([...got]).toHaveLength(41);
    expect(got.endsWith("…")).toBe(true);
  });

  it("leaves an ordinary short title untouched", () => {
    expect(conversationRailPeerDisplayTitle("会话 A")).toBe("会话 A");
  });
});

// T5：竖线要从当前会话行画到最后一个对端行，所以分组集合必须含当前行本身。
describe("conversation rail peer pair grouping", () => {
  it("includes the active conversation itself in the grouped ids", () => {
    const adjacency = applyConversationRailPeerPairAdjacency({
      activeConversationId: "session-a",
      index: conversationRailPeerPairIndex(PAIRS),
      items: ITEMS
    });

    expect([...adjacency.groupedSessionIds].sort()).toEqual([
      "session-a",
      "session-c"
    ]);
  });
});

describe("conversationRailPeerDisplayTitle fallbacks", () => {
  it("falls back to the truncated raw title when stripping leaves nothing", () => {
    const only = "请根据下面的「任务元数据」和「用户填写」完成本次任务。";
    expect(conversationRailPeerDisplayTitle(only)).toBe(only);
    expect(conversationRailPeerDisplayTitle(only)).not.toBe("");
  });
});

describe("conversationRailPeerDisplayTitle · 单行标题只剩小节标题", () => {
  it("剥掉模板后只剩 `## 补充说明 …` 一行时，不返回空串（真机 toast 曾是「Paired: ↔」）", () => {
    const title =
      "请根据下面的「任务元数据」和「用户填写」完成本次任务。 ## 补充说明 你是驱动会话 DRV7。只回「DRV7 就绪」，然后等待。";
    const got = conversationRailPeerDisplayTitle(title);
    expect(got).not.toBe("");
    expect(got.startsWith("补充说明 你是驱动会话 DRV7")).toBe(true);
    expect([...got].length).toBeLessThanOrEqual(41);
  });
});

// DINTAL-5331：配对行还在 ≠ 还在结对编程。切独立模式、关掉分栏都只动
// pairMode / 布局，行原样留着——徽标必须能把这两件事分开画。
describe("conversationRailPeerPairBadgeMode", () => {
  const pair = (pairMode?: "pair" | "solo"): ConversationRailPeerPair => ({
    a: endpoint("session-a", "task-a", "claude", "会话 A"),
    b: endpoint("session-c", "task-c", "codex", "会话 C"),
    pairId: "pair-ac",
    ...(pairMode ? { pairMode } : {})
  });

  it("结对模式 → pair（两头都是）", () => {
    const index = conversationRailPeerPairIndex([pair("pair")]);
    expect(conversationRailPeerPairBadgeMode(index, "session-a")).toBe("pair");
    expect(conversationRailPeerPairBadgeMode(index, "session-c")).toBe("pair");
  });

  it("切成独立模式 → solo：配对数不变，只有外观变", () => {
    const index = conversationRailPeerPairIndex([pair("solo")]);
    expect(conversationRailPeerPairBadgeMode(index, "session-a")).toBe("solo");
    expect(conversationRailPeerPairCount(index, "session-a")).toBe(1);
  });

  it("老宿主没报 pairMode → unknown：外观退回改造前的单态", () => {
    const index = conversationRailPeerPairIndex([pair()]);
    expect(conversationRailPeerPairBadgeMode(index, "session-a")).toBe(
      "unknown"
    );
  });

  it("一条会话挂多对时，有一条在结对就算 pair", () => {
    const index = conversationRailPeerPairIndex([
      pair("solo"),
      {
        a: endpoint("session-a", "task-a", "claude", "会话 A"),
        b: endpoint("session-b", "task-b", "codex", "会话 B"),
        pairId: "pair-ab",
        pairMode: "pair"
      }
    ]);
    expect(conversationRailPeerPairBadgeMode(index, "session-a")).toBe("pair");
    expect(conversationRailPeerPairBadgeMode(index, "session-c")).toBe("solo");
  });

  it("solo 与老宿主行混在一起时不敢断言「没在结对」，退回 unknown", () => {
    const index = conversationRailPeerPairIndex([
      pair("solo"),
      {
        a: endpoint("session-a", "task-a", "claude", "会话 A"),
        b: endpoint("session-b", "task-b", "codex", "会话 B"),
        pairId: "pair-ab"
      }
    ]);
    expect(conversationRailPeerPairBadgeMode(index, "session-a")).toBe(
      "unknown"
    );
  });

  it("没有配对时返回 unknown（此时压根不画徽标）", () => {
    const index = conversationRailPeerPairIndex([pair("pair")]);
    expect(conversationRailPeerPairBadgeMode(index, "session-x")).toBe(
      "unknown"
    );
  });
});
