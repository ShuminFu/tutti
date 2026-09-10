// 分栏配对（补丁 0108）gui 包半边的规格：
// 1) 侧栏条目的指针拖动手势只在宿主注册后生效，并按 6px 死区上报生命周期；
// 2) 在任一栏里显示的会话在侧栏画成 active。
import { fireEvent, render, screen } from "@testing-library/react";
import { TooltipProvider } from "@tutti-os/ui-system";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationModel";
import {
  registerConversationRailSplitHost,
  type ConversationRailSplitHost
} from "../model/conversationRailSplitHost";
import type { AgentGUIViewLabels } from "./AgentGUINodeView.types";
import { AgentGUIConversationRailItem } from "./AgentGUIConversationRailItem";
import { AgentGUIConversationRailSection } from "./AgentGUIConversationRailSection";
import { AgentGUIConversationRailSectionPresentationProvider } from "./agentGUIConversationRailSectionPresentationContext";

let unregister: (() => void) | null = null;

afterEach(() => {
  unregister?.();
  unregister = null;
});

describe("conversation rail split drag gesture", () => {
  it("does nothing without a host: a 20px move emits nothing and click still selects", () => {
    const onSelectConversation = vi.fn();
    renderItem({ onSelectConversation });
    const row = screen.getByTestId("agent-gui-conversation-item-session-a");

    expect(row).not.toHaveAttribute("data-split-draggable");
    pointer(row, "pointerDown", 10, 10);
    pointer(row, "pointerMove", 30, 10);
    pointer(row, "pointerUp", 30, 10);
    expect(row).not.toHaveAttribute("data-split-dragging");
    fireEvent.click(screen.getByRole("button", { name: /Session A/ }));

    expect(onSelectConversation).toHaveBeenCalledWith("session-a");
  });

  it("emits exactly one dragStart with the session summary after the 6px deadzone, then dragMove", () => {
    const host = splitHostStub();
    unregister = registerConversationRailSplitHost(host);
    renderItem({ status: "completed" });
    const row = screen.getByTestId("agent-gui-conversation-item-session-a");

    expect(row).toHaveAttribute("data-split-draggable", "true");
    pointer(row, "pointerDown", 10, 10);
    // 4px：死区之内，什么都不报。
    pointer(row, "pointerMove", 14, 10);
    expect(host.onDragStart).not.toHaveBeenCalled();
    expect(row).not.toHaveAttribute("data-split-dragging");
    // 8px：越过死区，报一次 dragStart。
    pointer(row, "pointerMove", 18, 10);
    expect(host.onDragStart).toHaveBeenCalledTimes(1);
    expect(host.onDragStart).toHaveBeenCalledWith(
      {
        // 行自己解析出来的 provider 平面图标，宿主拖影照用。
        iconUrl: "/app/renderer/assets/icons/agents/claudecode-flat-filled.svg",
        id: "session-a",
        isImported: false,
        provider: "claude",
        status: "completed",
        title: "Session A"
      },
      { x: 18, y: 10 }
    );
    expect(row).toHaveAttribute("data-split-dragging", "true");
    // 之后每次移动只报 dragMove，不再重复 dragStart。
    pointer(row, "pointerMove", 40, 12);
    expect(host.onDragStart).toHaveBeenCalledTimes(1);
    expect(host.onDragMove).toHaveBeenCalledWith({ x: 40, y: 12 });
    expect(host.onDragCancel).not.toHaveBeenCalled();
    expect(host.onDragEnd).not.toHaveBeenCalled();
  });

  it("emits dragEnd on pointerup and swallows the drop's click so the node does not select", () => {
    const host = splitHostStub();
    unregister = registerConversationRailSplitHost(host);
    const onSelectConversation = vi.fn();
    renderItem({ onSelectConversation });
    const row = screen.getByTestId("agent-gui-conversation-item-session-a");
    const select = screen.getByRole("button", { name: /Session A/ });

    pointer(select, "pointerDown", 10, 10);
    pointer(select, "pointerMove", 40, 10);
    pointer(select, "pointerUp", 60, 20);
    fireEvent.click(select);

    expect(host.onDragEnd).toHaveBeenCalledWith({ x: 60, y: 20 });
    expect(host.onDragCancel).not.toHaveBeenCalled();
    expect(onSelectConversation).not.toHaveBeenCalled();
    expect(row).not.toHaveAttribute("data-split-dragging");
    // 只吞一次：下一次普通点击照常选中。
    pointer(select, "pointerDown", 10, 10);
    pointer(select, "pointerUp", 10, 10);
    fireEvent.click(select);
    expect(onSelectConversation).toHaveBeenCalledWith("session-a");
  });

  it("emits dragCancel on Escape while dragging and clears the dragging marker", () => {
    const host = splitHostStub();
    unregister = registerConversationRailSplitHost(host);
    renderItem({});
    const row = screen.getByTestId("agent-gui-conversation-item-session-a");

    pointer(row, "pointerDown", 10, 10);
    pointer(row, "pointerMove", 40, 10);
    expect(row).toHaveAttribute("data-split-dragging", "true");
    fireEvent.keyDown(window, { key: "Escape" });

    expect(host.onDragCancel).toHaveBeenCalledTimes(1);
    expect(host.onDragEnd).not.toHaveBeenCalled();
    expect(row).not.toHaveAttribute("data-split-dragging");
    // 取消之后再动指针不再上报。
    pointer(row, "pointerMove", 80, 10);
    expect(host.onDragMove).not.toHaveBeenCalled();
  });

  it("does not start a drag from a pending-delete row", () => {
    const host = splitHostStub();
    unregister = registerConversationRailSplitHost(host);
    renderItem({ isPendingDeleteConversation: true });
    const row = screen.getByTestId("agent-gui-conversation-item-session-a");

    expect(row).not.toHaveAttribute("data-split-draggable");
    pointer(row, "pointerDown", 10, 10);
    pointer(row, "pointerMove", 40, 10);
    expect(host.onDragStart).not.toHaveBeenCalled();
  });
});

describe("conversation rail split highlight", () => {
  it("marks a row active when its session is shown in any pane", () => {
    const host = splitHostStub(new Set(["session-b"]));
    unregister = registerConversationRailSplitHost(host);
    renderSection({ activeConversationId: "session-a" });

    expect(
      screen.getByTestId("agent-gui-conversation-item-session-a")
    ).toHaveAttribute("data-active", "true");
    expect(
      screen.getByTestId("agent-gui-conversation-item-session-b")
    ).toHaveAttribute("data-active", "true");
    expect(
      screen.getByTestId("agent-gui-conversation-item-session-c")
    ).toHaveAttribute("data-active", "false");
  });

  it("keeps the single-value highlight when no host is registered", () => {
    renderSection({ activeConversationId: "session-a" });

    expect(
      screen.getByTestId("agent-gui-conversation-item-session-a")
    ).toHaveAttribute("data-active", "true");
    expect(
      screen.getByTestId("agent-gui-conversation-item-session-b")
    ).toHaveAttribute("data-active", "false");
  });
});

function pointer(
  target: Element,
  type: "pointerDown" | "pointerMove" | "pointerUp",
  clientX: number,
  clientY: number
): void {
  fireEvent[type](target, {
    button: 0,
    buttons: type === "pointerUp" ? 0 : 1,
    clientX,
    clientY,
    isPrimary: true,
    pointerId: 1,
    pointerType: "mouse"
  });
}

function splitHostStub(
  shown: ReadonlySet<string> = new Set()
): ConversationRailSplitHost & {
  onDragCancel: ReturnType<typeof vi.fn>;
  onDragEnd: ReturnType<typeof vi.fn>;
  onDragMove: ReturnType<typeof vi.fn>;
  onDragStart: ReturnType<typeof vi.fn>;
} {
  return {
    getShownSessionIds: () => shown,
    onDragCancel: vi.fn(),
    onDragEnd: vi.fn(),
    onDragMove: vi.fn(),
    onDragStart: vi.fn(),
    subscribe: () => () => {}
  } as never;
}

function renderItem(input: {
  isPendingDeleteConversation?: boolean;
  onSelectConversation?: (id: string) => void;
  status?: AgentGUIConversationSummary["status"];
}) {
  return render(
    <TooltipProvider>
      <AgentGUIConversationRailItem
        active={false}
        isDeletingConversation={false}
        isPendingDeleteConversation={input.isPendingDeleteConversation ?? false}
        isRailInteractionLocked={() => false}
        item={{
          cwd: "/workspace",
          id: "session-a",
          provider: "claude",
          status: input.status ?? "ready",
          title: "Session A",
          updatedAtUnixMs: 1
        }}
        labels={LABELS}
        registerItemElement={() => {}}
        uiLanguage="en"
        workspaceId="workspace-1"
        onCancelDeleteConversation={() => {}}
        onConfirmDeleteConversation={() => {}}
        onRequestDeleteConversation={() => {}}
        onRequestRenameConversation={() => {}}
        onSelectConversation={input.onSelectConversation ?? (() => {})}
        onToggleConversationPinned={() => {}}
        onMarkConversationUnread={() => {}}
      />
    </TooltipProvider>
  );
}

function renderSection(input: { activeConversationId: string | null }) {
  const items: AgentGUIConversationSummary[] = ["a", "b", "c"].map((key) => ({
    cwd: "/workspace",
    id: `session-${key}`,
    provider: "claude",
    status: "ready",
    title: `Session ${key.toUpperCase()}`,
    updatedAtUnixMs: 1
  }));
  return render(
    <TooltipProvider>
      <AgentGUIConversationRailSectionPresentationProvider
        batchDeletionDisabled
        projectActionLocked={false}
        projectDragDisabled={false}
      >
        <AgentGUIConversationRailSection
          activeConversation={null}
          activeConversationCountsTowardTotal={false}
          activeConversationId={input.activeConversationId}
          createConversationDisabled={false}
          isDeletingConversation={false}
          isLoadingMoreConversations={false}
          isProjectActionLocked={() => false}
          isRailInteractionLocked={() => false}
          isSectionCollapsed={false}
          labels={LABELS}
          pendingDeleteConversationId={null}
          projectDragging={false}
          projectDropIndicator={null}
          projectLabel=""
          projectPath=""
          registerItemElement={() => {}}
          section={{
            id: "conversations",
            items,
            kind: "conversations",
            label: "Conversations",
            project: null
          }}
          sectionHasMore={false}
          sectionTotalCount={items.length}
          uiLanguage="en"
          visibleItemLimit={5}
          workspaceId="workspace-1"
          onCancelDeleteConversation={() => {}}
          onConfirmDeleteConversation={() => {}}
          onCreateConversation={() => {}}
          onLoadMoreConversations={() => {}}
          onMarkConversationUnread={() => {}}
          onOpenProjectFiles={() => {}}
          onProjectDragEnd={() => {}}
          onProjectDragOver={() => {}}
          onProjectDragStart={() => {}}
          onProjectMenuOpenChange={() => {}}
          onRequestDeleteConversation={() => {}}
          onRequestRenameConversation={() => {}}
          onRequestSectionBatchDeletion={() => {}}
          onSelectConversation={() => {}}
          onToggleConversationPinned={() => {}}
          onToggleProjectPinned={() => Promise.resolve()}
          onToggleProjectSectionCollapsed={() => {}}
          onVisibleItemLimitChange={() => {}}
          setPendingProjectAction={() => {}}
        />
      </AgentGUIConversationRailSectionPresentationProvider>
    </TooltipProvider>
  );
}

const LABELS = {
  batchDeleteProjectSessions: "Delete sessions",
  copiedToClipboard: "Copied",
  copyAsMarkdown: "Copy as Markdown",
  copyAsReference: "Copy as reference",
  copyFailed: "Copy failed",
  deleteSession: "Delete",
  deleteSessionConfirm: "Confirm delete",
  emptyProjectConversations: "No sessions",
  markSessionUnread: "Mark as unread",
  moreSessionActions: "More actions",
  newConversation: "New session",
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
  pinProject: "Pin project",
  pinSession: "Pin",
  pinnedProjectAccessibleName: (label: string) => `Pinned project: ${label}`,
  projectSectionEdit: "New session",
  projectSectionMoreActions: "Project actions",
  projectSectionViewFiles: "Open folder",
  relativeTimeDays: (value: number) => `${value} days`,
  relativeTimeHours: (value: number) => `${value} hours`,
  relativeTimeJustNow: "just now",
  relativeTimeMinutes: (value: number) => `${value} min`,
  relativeTimeMonths: (value: number) => `${value} months`,
  relativeTimeYears: (value: number) => `${value} years`,
  removeProject: "Remove project",
  renameSession: "Rename",
  showLessConversations: "Show less",
  showMoreConversations: "Show more",
  unpinProject: "Unpin project",
  unpinSession: "Unpin"
} as unknown as AgentGUIViewLabels;
