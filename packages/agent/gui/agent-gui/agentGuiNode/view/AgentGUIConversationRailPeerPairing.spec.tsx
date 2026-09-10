import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { TooltipProvider } from "@tutti-os/ui-system";
import {
  resetAgentHostApiForTests,
  setAgentHostApiForTests
} from "../../../agentActivityHost";
import { describe, expect, it, vi } from "vitest";
import type {
  AgentGUIConversationStatus,
  AgentGUIConversationSummary
} from "../model/agentGuiConversationModel";
import type { ConversationRailPeerPair } from "../model/conversationRailPeerPairing";
import type { ConversationRailPeerPairingHost } from "../model/conversationRailPeerPairingHost";
import type { AgentGUIViewLabels } from "./AgentGUINodeView.types";
import { AgentGUIConversationRailItem } from "./AgentGUIConversationRailItem";
import {
  AgentGUIConversationRailPeerPairingProvider,
  useAgentGUIConversationRailPeerPairingState
} from "./agentGUIConversationRailPeerPairingContext";

describe("conversation rail peer pairing menu", () => {
  it("offers the mark entry and a disabled unpair entry when nothing is marked", async () => {
    renderPairingRail({});

    await openMenu("session-a");

    expect(
      await screen.findByRole("menuitem", { name: "Mark for pairing" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Unpair (0)" })
    ).toHaveAttribute("data-disabled");
  });

  it("offers clearing the mark on the marked conversation itself", async () => {
    renderPairingRail({});

    await mark("session-a");
    await openMenu("session-a");

    expect(
      await screen.findByRole("menuitem", { name: "Clear pairing mark" })
    ).toBeInTheDocument();
  });

  it("offers pairing with the marked conversation and previews its provider", async () => {
    renderPairingRail({});

    await mark("session-a");
    await openMenu("session-b");

    const entry = await screen.findByRole("menuitem", {
      name: /Pair with “Session A”/
    });
    expect(entry).toBeInTheDocument();
    expect(entry.textContent).toContain("claude");
    expect(entry).not.toHaveAttribute("data-disabled");
  });

  it("disables the pairing entry with an explanation on an unmanaged conversation", async () => {
    renderPairingRail({ bImported: true });

    await mark("session-a");
    await openMenu("session-b");

    expect(
      await screen.findByRole("menuitem", {
        name: "Unmanaged sessions cannot be paired"
      })
    ).toHaveAttribute("data-disabled");
  });

  it("clears the mark and reloads the pairs after a successful pairing", async () => {
    const host = pairingHostStub();
    renderPairingRail({ host });
    await waitFor(() => expect(host.listPeerPairs).toHaveBeenCalledTimes(1));

    await mark("session-a");
    await openMenu("session-b");
    fireEvent.pointerUp(
      await screen.findByRole("menuitem", { name: /Pair with “Session A”/ }),
      { button: 0 }
    );

    await waitFor(() =>
      expect(host.createPeerPair).toHaveBeenCalledTimes(1)
    );
    await waitFor(() => expect(host.listPeerPairs).toHaveBeenCalledTimes(2));
    // 标记清空的判据：同一条上重新出现「标记为待配对」而不是「取消标记」。
    await openMenu("session-a");
    expect(
      await screen.findByRole("menuitem", { name: "Mark for pairing" })
    ).toBeInTheDocument();
  });

  // T4：后端重开的是**被标记的那一头**（请求里的 to），文案与开关都要按它判。
  it("asks the host to reopen the marked target, and names it in the label", async () => {
    const host = pairingHostStub();
    renderPairingRail({ aStatus: "completed", host });

    await mark("session-a");
    await openMenu("session-b");
    fireEvent.pointerUp(
      await screen.findByRole("menuitem", {
        name: /Pair with “Session A” \(reopens it first\)/
      }),
      { button: 0 }
    );

    await waitFor(() =>
      expect(host.createPeerPair).toHaveBeenCalledWith({
        aliasForFrom: "",
        aliasForTo: "",
        from: "session-b",
        relaunchClosed: true,
        to: "session-a"
      })
    );
  });

  // T4 反向：已结束的是**被右键的**那条时不重开（后端接受这种配对），
  // 也不加特殊文案。按 from 判就会在这里错报 true。
  it("does not ask for a reopen when only the right-clicked conversation is closed", async () => {
    const host = pairingHostStub();
    renderPairingRail({ bStatus: "completed", host });

    await mark("session-a");
    await openMenu("session-b");
    const entry = await screen.findByRole("menuitem", {
      name: /Pair with “Session A”/
    });
    // 反向断言：不能带「会先重开」的文案。
    expect(entry.textContent).not.toContain("reopens it first");
    fireEvent.pointerUp(entry, { button: 0 });

    await waitFor(() =>
      expect(host.createPeerPair).toHaveBeenCalledWith(
        expect.objectContaining({ relaunchClosed: false })
      )
    );
  });

  // T3：真机上条目 title 是整段 prompt；菜单必须显示剥过的短标题。
  it("shows the stripped short title instead of the whole prompt", async () => {
    renderPairingRail({
      aTitle:
        "请根据下面的「任务元数据」和「用户填写」完成本次任务。\n\n## 用户填写\n把 peer_list 的返回贴出来"
    });

    await mark("session-a");
    await openMenu("session-b");

    const entry = await screen.findByRole("menuitem", {
      name: /Pair with “把 peer_list 的返回贴出来”/
    });
    // 副标题（provider · 别名预览）要真的渲染出来，点之前能确认是它。
    expect(entry.textContent).toContain("claude");
  });

  // T6：解除成功要有 toast 点名解的是谁，光靠徽标数字变小真机上看不出来。
  it("toasts the unpaired peer title after a successful unpair", async () => {
    const toast = { error: vi.fn(), success: vi.fn() };
    setAgentHostApiForTests({ toast } as never);
    try {
      const host = pairingHostStub([PAIR_AB]);
      renderPairingRail({ host });
      await waitFor(() => expect(host.listPeerPairs).toHaveBeenCalledTimes(1));

      await openMenu("session-a");
      // 子菜单要先点开（Radix 的 SubTrigger 靠 click / hover 展开）。
      fireEvent.click(
        await screen.findByRole("menuitem", { name: /Unpair \(1\)/ })
      );
      fireEvent.pointerUp(
        await screen.findByRole("menuitem", { name: /Session B prompt/ }),
        { button: 0 }
      );

      await waitFor(() =>
        expect(host.deletePeerPair).toHaveBeenCalledWith({
          pairId: "pair-ab",
          taskId: "task-a"
        })
      );
      await waitFor(() =>
        expect(toast.success).toHaveBeenCalledWith("Unpaired: “Session B prompt”")
      );
    } finally {
      resetAgentHostApiForTests();
    }
  });
});

// T7 真机走查说右键菜单 Escape / 点外面都关不掉。菜单外壳（ContextMenu /
// Trigger / Content）在补丁 0103 里逐字未改，这两条钉住「补丁引入的配对组与
// 子菜单没有拦掉关闭手势」；真机若仍关不掉，就是 Tutti 既有菜单在嵌入宿主里的
// 行为，不在本补丁范围内。
describe("conversation rail peer pairing menu dismissal", () => {
  it("still closes on Escape and on an outside pointer down while the unpair submenu exists", async () => {
    const host = pairingHostStub([PAIR_AB]);
    renderPairingRail({ host });
    await screen.findByTestId("agent-gui-conversation-peer-pair-badge-session-a");

    await openMenu("session-a");
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menuitem")).toBeNull());

    await openMenu("session-a");
    fireEvent.pointerDown(document.body, { button: 0, pointerType: "mouse" });
    await waitFor(() => expect(screen.queryByRole("menuitem")).toBeNull());
  });
});

describe("conversation rail peer pairing item presentation", () => {
  // T1：徽标占自己的槽位，不再绝对定位压在相对时间上。
  it("renders the pair-count badge in the row's own slot", async () => {
    const host = pairingHostStub([PAIR_AB]);
    renderPairingRail({ host });

    const badge = await screen.findByTestId(
      "agent-gui-conversation-peer-pair-badge-session-a"
    );
    expect(badge).toHaveTextContent("1");
    expect(
      screen.getByTestId("agent-gui-conversation-item-session-a")
    ).toHaveAttribute("data-peer-pair-slot", "badge");
  });

  // T2：标记态 = 虚线描边 + 「待配对」chip，chip 优先占槽位。
  it("marks the pending conversation with a chip that wins the slot", async () => {
    const host = pairingHostStub([PAIR_AB]);
    renderPairingRail({ host });
    await screen.findByTestId("agent-gui-conversation-peer-pair-badge-session-a");

    await mark("session-a");

    expect(
      screen.getByTestId("agent-gui-conversation-peer-pair-chip-session-a")
    ).toHaveTextContent("Pending pair");
    expect(
      screen.queryByTestId("agent-gui-conversation-peer-pair-badge-session-a")
    ).toBeNull();
    const row = screen.getByTestId("agent-gui-conversation-item-session-a");
    expect(row).toHaveAttribute("data-peer-pair-marked", "true");
    expect(row).toHaveAttribute("data-peer-pair-slot", "chip");
  });
});

const PAIR_AB: ConversationRailPeerPair = {
  a: {
    alias: "claude-sess",
    cwd: "/workspace",
    provider: "claude",
    sessionId: "session-a",
    status: "ready",
    taskId: "task-a",
    title: "Session A"
  },
  b: {
    alias: "codex-sess",
    cwd: "/workspace",
    provider: "codex",
    sessionId: "session-b",
    status: "ready",
    taskId: "task-b",
    title:
      "请根据下面的「任务元数据」和「用户填写」完成本次任务。\n\n## 用户填写\nSession B prompt"
  },
  pairId: "pair-ab"
};

async function mark(sessionId: string): Promise<void> {
  await openMenu(sessionId);
  fireEvent.pointerUp(
    await screen.findByRole("menuitem", { name: "Mark for pairing" }),
    { button: 0 }
  );
  await waitFor(() => expect(screen.queryByRole("menuitem")).toBeNull());
}

async function openMenu(sessionId: string): Promise<void> {
  fireEvent.contextMenu(
    screen.getByTestId(`agent-gui-conversation-item-${sessionId}`)
  );
  await screen.findAllByRole("menuitem");
}

function pairingHostStub(
  pairs: ConversationRailPeerPair[] = []
): {
  createPeerPair: ReturnType<typeof vi.fn>;
  deletePeerPair: ReturnType<typeof vi.fn>;
  listPeerPairs: ReturnType<typeof vi.fn>;
} & ConversationRailPeerPairingHost {
  return {
    createPeerPair: vi.fn(async () => ({ pairId: "pair-1" })),
    deletePeerPair: vi.fn(async () => ({ ok: true })),
    listPeerPairs: vi.fn(async () => ({ pairs }))
  } as never;
}

function PairingRail({
  aStatus,
  aTitle,
  bImported,
  bStatus,
  host
}: {
  aStatus?: AgentGUIConversationStatus;
  aTitle?: string;
  bImported?: boolean;
  bStatus?: AgentGUIConversationStatus;
  host: ConversationRailPeerPairingHost;
}): React.JSX.Element {
  const pairing = useAgentGUIConversationRailPeerPairingState({
    activeConversationId: "session-a",
    host,
    labels: PAIRING_LABELS
  });
  return (
    <AgentGUIConversationRailPeerPairingProvider value={pairing}>
      {railItem({
        id: "session-a",
        provider: "claude",
        status: aStatus,
        title: aTitle ?? "Session A"
      })}
      {railItem({
        id: "session-b",
        isImported: bImported,
        provider: "codex",
        status: bStatus,
        title: "Session B"
      })}
    </AgentGUIConversationRailPeerPairingProvider>
  );
}

function railItem(overrides: Partial<AgentGUIConversationSummary> & { id: string }) {
  return (
    <AgentGUIConversationRailItem
      key={overrides.id}
      active={false}
      isDeletingConversation={false}
      isPendingDeleteConversation={false}
      isRailInteractionLocked={() => false}
      item={{
        cwd: "/workspace",
        provider: "codex",
        title: overrides.id,
        updatedAtUnixMs: 1,
        ...overrides,
        // 显式覆盖优先，但 `status: undefined` 不该把默认值抹掉。
        status: overrides.status ?? "ready"
      }}
      labels={RAIL_ITEM_LABELS}
      registerItemElement={() => {}}
      uiLanguage="en"
      workspaceId="workspace-1"
      onCancelDeleteConversation={() => {}}
      onConfirmDeleteConversation={() => {}}
      onRequestDeleteConversation={() => {}}
      onRequestRenameConversation={() => {}}
      onSelectConversation={() => {}}
      onToggleConversationPinned={() => {}}
      onMarkConversationUnread={() => {}}
    />
  );
}

function renderPairingRail(input: {
  aStatus?: AgentGUIConversationStatus;
  aTitle?: string;
  bImported?: boolean;
  bStatus?: AgentGUIConversationStatus;
  host?: ConversationRailPeerPairingHost;
}) {
  return render(
    <TooltipProvider>
      <PairingRail
        aStatus={input.aStatus}
        aTitle={input.aTitle}
        bImported={input.bImported}
        bStatus={input.bStatus}
        host={input.host ?? pairingHostStub()}
      />
    </TooltipProvider>
  );
}

const PAIRING_LABELS = {
  peerPairFailed: "Pairing failed",
  peerPairPaired: (from: string, to: string) => `Paired: ${from} ↔ ${to}`,
  peerUnpaired: (title: string) => `Unpaired: “${title}”`,
  peerUnpairFailed: "Unpairing failed"
};

const RAIL_ITEM_LABELS = {
  copiedToClipboard: "Copied",
  copyAsMarkdown: "Copy as Markdown",
  copyAsReference: "Copy as reference",
  copyFailed: "Copy failed",
  deleteSession: "Delete",
  deleteSessionConfirm: "Confirm delete",
  markSessionUnread: "Mark as unread",
  moreSessionActions: "More actions",
  openConversationWindow: "Open in window",
  peerPairMark: "Mark for pairing",
  peerPairPending: "Pending pair",
  peerPairUnmanaged: "Unmanaged sessions cannot be paired",
  peerPairUnmark: "Clear pairing mark",
  peerPairUnpair: (count: number) => `Unpair (${count})`,
  peerPairWith: (title: string) => `Pair with “${title}”`,
  peerPairWithRelaunch: (title: string) =>
    `Pair with “${title}” (reopens it first)`,
  peerUnpaired: (title: string) => `Unpaired: “${title}”`,
  pinSession: "Pin",
  relativeTimeDays: (value: number) => `${value} days`,
  relativeTimeHours: (value: number) => `${value} hours`,
  relativeTimeJustNow: "just now",
  relativeTimeMinutes: (value: number) => `${value} minutes`,
  relativeTimeMonths: (value: number) => `${value} months`,
  relativeTimeYears: (value: number) => `${value} years`,
  renameSession: "Rename",
  unpinSession: "Unpin"
} as unknown as AgentGUIViewLabels;
