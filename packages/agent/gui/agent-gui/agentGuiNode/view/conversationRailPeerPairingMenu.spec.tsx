// 补丁 0103 的真机走查项，用规格测试代替截图（真机截图不可用）。
// 这里直接钉 buildConversationRailPeerPairingMenuEntries（纯函数，不用渲染 Radix），
// 只有「解除后的 toast」必须走状态机，才用 renderHook。
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  resetAgentHostApiForTests,
  setAgentHostApiForTests
} from "../../../agentActivityHost";
import {
  conversationRailPeerAliasPreview,
  conversationRailPeerPairIndex,
  type ConversationRailPeerPair
} from "../model/conversationRailPeerPairing";
import type { ConversationRailPeerPairingHost } from "../model/conversationRailPeerPairingHost";
import {
  useAgentGUIConversationRailPeerPairingState,
  type AgentGUIConversationRailPeerMark,
  type AgentGUIConversationRailPeerPairingValue
} from "./agentGUIConversationRailPeerPairingContext";
import {
  buildConversationRailPeerPairingMenuEntries,
  type ConversationRailPeerPairingMenuEntry,
  type ConversationRailPeerPairingMenuLabels
} from "./conversationRailPeerPairingMenu";

const BOILERPLATE = "请根据下面的「任务元数据」和「用户填写」完成本次任务。";

const LABELS: ConversationRailPeerPairingMenuLabels = {
  peerPairMark: "Mark for pairing",
  peerPairUnmanaged: "Unmanaged sessions cannot be paired",
  peerPairUnmark: "Clear pairing mark",
  peerPairUnpair: (count: number) => `Unpair (${count})`,
  peerPairWith: (title: string) => `Pair with “${title}”`,
  peerPairWithRelaunch: (title: string) =>
    `Pair with “${title}” (reopens it first)`
};

// T3 验收项 1：被标记会话的标题剥完样板句为空时不能出现 `Pair with “”`。
describe("peer pairing menu title", () => {
  it("falls back to the raw title when the boilerplate is all there is", () => {
    const entry = pairWithEntry({
      marked: mark({ title: BOILERPLATE })
    });

    expect(entry.label).toBe(`Pair with “${BOILERPLATE}”`);
    expect(entry.label).not.toBe("Pair with “”");
  });

  it("prefers the backend peer_title over the rail's own prompt title", () => {
    // 配对表里认识 session-a：它当过别人的对端，后端已经把标题剥好了。
    const entry = pairWithEntry({
      marked: mark({ title: `${BOILERPLATE}\n\n侧栏自己的那句` }),
      pairs: [
        pair("pair-xa", endpoint("session-x"), endpoint("session-a", {
          title: "后端剥好的标题"
        }))
      ]
    });

    expect(entry.label).toBe("Pair with “后端剥好的标题”");
  });

  it("truncates the fallback title to 40 characters plus an ellipsis", () => {
    const entry = pairWithEntry({ marked: mark({ title: "长".repeat(60) }) });

    const shown = titleOf(entry.label);
    expect([...shown]).toHaveLength(41);
    expect(shown.endsWith("…")).toBe(true);
  });

  it("never renders an empty title for a realistic rail title", () => {
    for (const title of [
      BOILERPLATE,
      `${BOILERPLATE}\n\n## 用户填写\n把 peer_list 的返回贴出来`,
      "Session A",
      "长".repeat(200)
    ]) {
      expect(titleOf(pairWithEntry({ marked: mark({ title }) }).label)).not.toBe(
        ""
      );
    }
  });
});

// 验收项 2：副标题只给 provider —— 侧栏的 provider 是 Tutti id（claude-code），
// 拼出来的别名预览与后端真别名对不上，所以那段被拿掉了。
describe("peer pairing menu subtitle", () => {
  it("shows only the marked conversation's provider, without an alias preview", () => {
    const marked = mark({ provider: "claude-code", sessionId: "sess-9f31" });
    const entry = pairWithEntry({ marked });

    expect(entry.description).toBe("claude-code");
    expect(entry.description).not.toContain(
      conversationRailPeerAliasPreview(marked.provider, marked.sessionId)
    );
  });
});

// 验收项 3：解除配对必须有 toast，否则唯一反馈是徽标数字变小，真机看不出来。
describe("peer pairing unpair feedback", () => {
  it("toasts the unpaired peer after the host accepts the delete", async () => {
    const toast = { error: vi.fn(), success: vi.fn() };
    setAgentHostApiForTests({ toast } as never);
    try {
      const host = hostStub();
      const { result } = renderHook(() =>
        useAgentGUIConversationRailPeerPairingState({
          activeConversationId: "session-a",
          host,
          labels: {
            peerPairFailed: "Pairing failed",
            peerPairPaired: (from: string, to: string) =>
              `Paired: ${from} ↔ ${to}`,
            peerUnpaired: (title: string) => `Unpaired: “${title}”`,
            peerUnpairFailed: "Unpairing failed"
          }
        })
      );

      result.current.deletePair({
        pairId: "pair-ab",
        peerTitle: `${BOILERPLATE}\n\n## 用户填写\nSession B prompt`,
        taskId: "task-a"
      });

      await waitFor(() =>
        expect(host.deletePeerPair).toHaveBeenCalledWith({
          pairId: "pair-ab",
          taskId: "task-a"
        })
      );
      await waitFor(() =>
        // 剥过样板句的对端标题；至少要有 Unpaired 这个词。
        expect(toast.success).toHaveBeenCalledWith(
          "Unpaired: “Session B prompt”"
        )
      );
    } finally {
      resetAgentHostApiForTests();
    }
  });
});

// 验收项 4：非托管会话不能配对，标签本身就是原因说明。
describe("peer pairing menu on an unmanaged conversation", () => {
  it("disables the pairing entry and explains why", () => {
    const entries = buildEntries({
      conversation: { isImported: true },
      marked: mark({})
    });

    const first = entries[0];
    expect(first.label).toBe(LABELS.peerPairUnmanaged);
    expect(first.disabled).toBe(true);
    // 反向：既不给「标记」也不给「与…配对」，点不出任何写操作。
    expect(entries.map((entry) => entry.label)).not.toContain(
      LABELS.peerPairMark
    );
    expect(
      entries.some((entry) => entry.label.startsWith("Pair with"))
    ).toBe(false);
  });
});

// 验收项 5：老宿主（没注册 list/pair/unpair 这三个能力）里整组配对项不出现。
describe("peer pairing menu on a host without the capability", () => {
  it("renders no pairing entries at all when the capability is missing", () => {
    expect(
      buildEntries({ marked: mark({}), supported: false })
    ).toEqual([]);
    // 有对端记录也一样：整组消失，不是只灰掉。
    expect(
      buildEntries({
        marked: null,
        pairs: [pair("pair-ab", endpoint("session-a"), endpoint("session-b"))],
        supported: false
      })
    ).toEqual([]);
  });

  it("reports unsupported when no pairing host is registered", () => {
    const { result } = renderHook(() =>
      useAgentGUIConversationRailPeerPairingState({
        activeConversationId: "session-a",
        host: null,
        labels: {
          peerPairFailed: "Pairing failed",
          peerPairPaired: () => "",
          peerUnpaired: () => "",
          peerUnpairFailed: "Unpairing failed"
        }
      })
    );

    expect(result.current.supported).toBe(false);
    expect(
      buildEntries({ marked: null, supported: result.current.supported })
    ).toEqual([]);
  });

  it("goes unsupported once the host answers the capability probe with unsupported", async () => {
    const host = hostStub();
    host.listPeerPairs.mockRejectedValue(new Error("unsupported"));
    const { result } = renderHook(() =>
      useAgentGUIConversationRailPeerPairingState({
        activeConversationId: "session-a",
        host,
        labels: {
          peerPairFailed: "Pairing failed",
          peerPairPaired: () => "",
          peerUnpaired: () => "",
          peerUnpairFailed: "Unpairing failed"
        }
      })
    );

    await waitFor(() => expect(result.current.supported).toBe(false));
    expect(
      buildEntries({ marked: null, supported: result.current.supported })
    ).toEqual([]);
  });
});

function titleOf(label: string): string {
  return label.replace(/^Pair with “/, "").replace(/”.*$/, "");
}

function pairWithEntry(input: {
  marked: AgentGUIConversationRailPeerMark;
  pairs?: ConversationRailPeerPair[];
}): ConversationRailPeerPairingMenuEntry {
  const entries = buildEntries(input);
  const entry = entries.find((candidate) => candidate.id === "peer-pair-with");
  if (!entry) throw new Error(`no pair-with entry in ${entries.map((e) => e.id)}`);
  return entry;
}

function buildEntries(input: {
  conversation?: { isImported?: boolean };
  marked: AgentGUIConversationRailPeerMark | null;
  pairs?: ConversationRailPeerPair[];
  supported?: boolean;
}): ConversationRailPeerPairingMenuEntry[] {
  const pairs = input.pairs ?? [];
  const pairing: AgentGUIConversationRailPeerPairingValue = {
    createPair: () => {},
    deletePair: () => {},
    index: conversationRailPeerPairIndex(pairs),
    marked: input.marked,
    pairs,
    supported: input.supported ?? true,
    toggleMarked: () => {}
  };
  return buildConversationRailPeerPairingMenuEntries({
    conversation: {
      id: "session-b",
      isImported: input.conversation?.isImported,
      provider: "codex",
      status: "ready",
      title: "Session B"
    },
    labels: LABELS,
    pairing,
    run: (action) => action()
  });
}

function mark(
  overrides: Partial<AgentGUIConversationRailPeerMark>
): AgentGUIConversationRailPeerMark {
  return {
    provider: "claude",
    sessionId: "session-a",
    status: "ready",
    title: "Session A",
    ...overrides
  };
}

function endpoint(
  sessionId: string,
  overrides: { title?: string } = {}
): ConversationRailPeerPair["a"] {
  return {
    alias: `claude-${sessionId.slice(0, 4)}`,
    cwd: "/workspace",
    provider: "claude",
    sessionId,
    status: "ready",
    taskId: `task-${sessionId}`,
    title: overrides.title ?? `title-${sessionId}`
  };
}

function pair(
  pairId: string,
  a: ConversationRailPeerPair["a"],
  b: ConversationRailPeerPair["b"]
): ConversationRailPeerPair {
  return { a, b, pairId };
}

function hostStub(): ConversationRailPeerPairingHost & {
  createPeerPair: ReturnType<typeof vi.fn>;
  deletePeerPair: ReturnType<typeof vi.fn>;
  listPeerPairs: ReturnType<typeof vi.fn>;
} {
  return {
    createPeerPair: vi.fn(async () => ({ pairId: "pair-1" })),
    deletePeerPair: vi.fn(async () => ({ ok: true })),
    listPeerPairs: vi.fn(async () => ({ pairs: [] }))
  } as never;
}
