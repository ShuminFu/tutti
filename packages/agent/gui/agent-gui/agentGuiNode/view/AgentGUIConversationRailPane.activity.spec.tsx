import {
  act,
  fireEvent,
  render,
  screen,
  waitFor
} from "@testing-library/react";
import { useState } from "react";
import { toast, TooltipProvider } from "@tutti-os/ui-system";
import { describe, expect, it, vi } from "vitest";
import type { AgentActivitySnapshot } from "@tutti-os/agent-activity-core";
import { setAgentHostApiForTests } from "../../../agentActivityHost";
import {
  AgentGUIRuntimeProvider,
  type AgentGUIRuntime
} from "../../../agentActivityRuntime";
import type { AgentGUIConversationSummary } from "../model/agentGuiConversationTypes";
import type { useAgentGUIConversationRailQuery } from "../controller/useAgentGUIConversationRailQuery";
import type { AgentGUIConversationRailLabels } from "./agentGUIConversationRailLabels";
import { AgentGUIConversationRailPane } from "./AgentGUIConversationRailPane";
import { createAgentGUIConversationActivityController } from "../controller/agentGUIConversationActivityController";
import { createTestAgentSessionEngine } from "../../../shared/testing/createTestAgentSessionEngine";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { archiveRemainingDays } from "./AgentGUIConversationArchive";

describe("AgentGUIConversationRailPane Activity capability", () => {
  it("pages and opens expired archives, restores through the engine, and confirms menu-only deletion", async () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    const deadline = 30 * 86_400_000;
    const archivedAtUnixMs = Date.now() - deadline;
    expect(archiveRemainingDays(1000, 999)).toBe(30);
    expect(archiveRemainingDays(1000, 1000 + deadline - 1)).toBe(1);
    expect(archiveRemainingDays(1000, 1000 + deadline)).toBe(0);
    const archived = normalizeAgentActivitySession({
      activeTurnId: null,
      agentSessionId: "expired",
      archivedAtUnixMs,
      cwd: "/workspace",
      latestTurnInteractions: [],
      pendingInteractions: [],
      provider: "codex",
      railSectionKey: "conversations",
      title: "Expired conversation",
      updatedAtUnixMs: 1,
      workspaceId: "workspace-1"
    });
    const later = { ...archived, agentSessionId: "later", title: "Later page" };
    const onSelectConversation = vi.fn();
    const onConfirmDeleteConversation = vi.fn();
    let restored = false;
    const setSessionArchived = vi.fn(async () => {
      restored = true;
      const session = { ...archived, archivedAtUnixMs: 0, updatedAtUnixMs: 2 };
      engine.dispatch({ type: "session/upserted", session });
      return session;
    });
    const listSessionSectionPage = vi.fn<
      NonNullable<AgentGUIRuntime["listSessionSectionPage"]>
    >(async (input) => ({
      kind: "archive",
      sectionKey: "archive",
      hasMore: !input.cursor,
      nextCursor: input.cursor ? undefined : "next",
      totalCount: restored ? 1 : 2,
      sessions: input.cursor ? [later] : restored ? [] : [archived]
    }));
    renderPane({
      capability: false,
      runtimeOverrides: {
        getSessionEngine: () => engine,
        listSessionSectionPage,
        setSessionArchived
      },
      onSelectConversation,
      onConfirmDeleteConversation
    });
    fireEvent.click(await screen.findByRole("button", { name: "Archive" }));
    const row = await screen.findByTestId(
      "agent-gui-conversation-item-expired"
    );
    expect(screen.getByText("Expired")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show more" }));
    await screen.findByTestId("agent-gui-conversation-item-later");
    expect(listSessionSectionPage.mock.calls[1]?.[0].cursor).toBe("next");
    fireEvent.click(row.querySelector("button")!);
    expect(onSelectConversation).toHaveBeenCalledWith("expired");
    fireEvent.animationEnd(screen.getByRole("dialog"));
    fireEvent.click(await screen.findByRole("button", { name: "Archive" }));
    fireEvent.contextMenu(
      await screen.findByTestId("agent-gui-conversation-item-expired")
    );
    fireEvent.pointerUp(
      await screen.findByRole("menuitem", { name: "Delete session" }),
      { button: 0 }
    );
    await screen.findByRole("button", { name: "Confirm delete" });
    expect(onConfirmDeleteConversation).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      engine.getSnapshot().sessionLifecycle.sessionsById.expired
        ?.archivedAtUnixMs
    ).toBe(archivedAtUnixMs);
    for (const dialog of screen.queryAllByRole("dialog"))
      fireEvent.animationEnd(dialog);
    fireEvent.click(await screen.findByRole("button", { name: "Archive" }));
    fireEvent.pointerEnter(
      await screen.findByTestId("agent-gui-conversation-item-expired")
    );
    fireEvent.click(screen.getByRole("button", { name: "Restore session" }));
    await waitFor(() =>
      expect(
        screen.queryByTestId("agent-gui-conversation-item-expired")
      ).toBeNull()
    );
    expect(setSessionArchived).toHaveBeenCalledWith({
      workspaceId: "workspace-1",
      agentSessionId: "expired",
      archived: false
    });
    engine.dispose();
  });
  it("does not report an empty archive when loading failed", async () => {
    const engine = createTestAgentSessionEngine("workspace-1");
    renderPane({
      capability: false,
      runtimeOverrides: {
        getSessionEngine: () => engine,
        listSessionSectionPage: vi.fn(async () => {
          throw new Error("offline");
        }),
        setSessionArchived: vi.fn()
      }
    });
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    await screen.findByRole("button", { name: "Retry" });
    expect(screen.queryByText("No archived sessions")).toBeNull();
    engine.dispose();
  });

  it("fails closed when the host does not opt in", () => {
    renderPane({ capability: false });

    expect(screen.queryByTestId("agent-gui-activity-view-toggle")).toBeNull();
  });

  it("surfaces a rail load failure with a retry action", () => {
    const retryRuntimeRail = vi.fn(() => Promise.resolve());
    renderPane({
      activityConversations: [],
      capability: false,
      conversations: [],
      retryRuntimeRail,
      runtimeRailFailed: true,
      runtimeSectionsEnabled: true
    });

    expect(
      screen.getByTestId("agent-gui-conversation-rail-load-error")
    ).toHaveTextContent("Could not load sessions");

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(retryRuntimeRail).toHaveBeenCalledTimes(1);
  });

  it("retains existing sessions while reporting a rail refresh failure through a toast", () => {
    const toastError = vi.fn();
    setAgentHostApiForTests({
      clipboard: {},
      filesystem: {},
      toast: { error: toastError },
      workspace: {}
    } as never);
    renderPane({
      capability: false,
      conversations: [
        { ...conversationFixture(), railSectionKey: "conversations" }
      ],
      retryRuntimeRail: vi.fn(() => Promise.resolve()),
      runtimeRailFailed: true,
      runtimeRailMemberships: [
        {
          id: "conversations",
          kind: "conversations",
          project: null,
          sessionIds: ["session-1"]
        }
      ],
      runtimeSectionsEnabled: true
    });

    expect(
      screen.queryByTestId("agent-gui-conversation-rail-load-error")
    ).toBeNull();
    expect(toastError).toHaveBeenCalledTimes(1);
    expect(toastError).toHaveBeenCalledWith("Could not load sessions");
    expect(screen.getByText("Session 1")).toBeTruthy();
  });

  it("falls back to the UI toast when the host does not provide one", () => {
    const fallbackToastError = vi.spyOn(toast, "error");
    setAgentHostApiForTests({
      clipboard: {},
      filesystem: {},
      workspace: {}
    } as never);
    renderPane({
      capability: false,
      conversations: [
        { ...conversationFixture(), railSectionKey: "conversations" }
      ],
      retryRuntimeRail: vi.fn(() => Promise.resolve()),
      runtimeRailFailed: true,
      runtimeRailMemberships: [
        {
          id: "conversations",
          kind: "conversations",
          project: null,
          sessionIds: ["session-1"]
        }
      ],
      runtimeSectionsEnabled: true
    });

    expect(fallbackToastError).toHaveBeenCalledWith("Could not load sessions");
    fallbackToastError.mockRestore();
  });

  it("keeps Activity View visible while the rail list is loading", async () => {
    vi.useFakeTimers();
    try {
      renderPane({
        capability: true,
        runtimeRailSectionsPending: true,
        runtimeSectionsEnabled: true
      });
      fireEvent.click(screen.getByTestId("agent-gui-activity-view-toggle"));

      await act(async () => {
        await vi.advanceTimersByTimeAsync(300);
      });

      expect(screen.getByRole("heading", { name: "Priority" })).toBeTruthy();
      expect(screen.queryByText("Loading sessions")).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("toggles an in-memory Activity View without requesting another page", () => {
    const listPage = vi.fn();
    renderPane({ capability: true, listPage });

    const toggle = screen.getByTestId("agent-gui-activity-view-toggle");
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-pressed", "true");
    expect(toggle).toHaveAccessibleName("Turn off activity view");
    expect(screen.getByRole("heading", { name: "Priority" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Today" })).toBeTruthy();
    expect(screen.getByText("Nothing needs attention")).toBeTruthy();
    expect(listPage).not.toHaveBeenCalled();

    const search = screen.getByPlaceholderText("Search sessions");
    fireEvent.change(search, { target: { value: "Session" } });
    expect(screen.queryByRole("heading", { name: "Priority" })).toBeNull();
    fireEvent.change(search, { target: { value: "" } });
    expect(screen.getByRole("heading", { name: "Priority" })).toBeTruthy();
    expect(listPage).not.toHaveBeenCalled();

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("heading", { name: "Priority" })).toBeNull();
  });

  it("places the Activity toggle immediately after New session", () => {
    renderPane({ capability: true });

    const newConversation = screen.getByTestId("agent-gui-new-conversation");
    const activityToggle = screen.getByTestId("agent-gui-activity-view-toggle");

    expect(
      newConversation.compareDocumentPosition(activityToggle) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });

  it("announces attention on the inactive toggle", () => {
    renderPane({ capability: true, hasUnreadCompletion: true });

    expect(
      screen.getByRole("button", {
        name: "View activity, attention needed"
      })
    ).toBeTruthy();
  });

  it("uses only canonical in-memory conversations instead of stale rail projections", () => {
    renderPane({
      capability: true,
      runtimeRailConversations: [
        {
          cwd: "/workspace",
          id: "stale-session",
          provider: "codex",
          sortTimeUnixMs: Date.now(),
          status: "working",
          title: "Stale Session",
          updatedAtUnixMs: Date.now()
        }
      ]
    });

    fireEvent.click(screen.getByTestId("agent-gui-activity-view-toggle"));

    expect(screen.queryByText("Stale Session")).toBeNull();
    expect(
      screen.queryByTestId("agent-gui-conversation-item-stale-session")
    ).toBeNull();
    expect(
      screen.getByTestId("agent-gui-conversation-item-session-1")
    ).toBeTruthy();
  });

  it("does not admit a detail-only transient rail overlay", () => {
    const transient = {
      ...conversationFixture(),
      id: "selected-history",
      isTransient: true,
      status: "ready" as const,
      title: "Selected history"
    };
    renderPane({
      capability: true,
      activityConversations: [conversationFixture()],
      conversations: [conversationFixture(), transient]
    });

    fireEvent.click(screen.getByTestId("agent-gui-activity-view-toggle"));

    expect(screen.queryByText("Selected history")).toBeNull();
  });
});

function renderPane({
  capability,
  hasUnreadCompletion = false,
  listPage = vi.fn(),
  runtimeRailConversations = [],
  runtimeRailFailed = false,
  runtimeRailMemberships = null,
  runtimeRailSectionsPending = false,
  runtimeSectionsEnabled = false,
  retryRuntimeRail = vi.fn(() => Promise.resolve()),
  activityConversations: requestedActivityConversations,
  conversations: requestedConversations,
  runtimeOverrides = {},
  onSelectConversation = () => {},
  onConfirmDeleteConversation = () => {}
}: {
  capability: boolean;
  hasUnreadCompletion?: boolean;
  listPage?: ReturnType<typeof vi.fn>;
  runtimeRailConversations?: AgentGUIConversationSummary[];
  runtimeRailFailed?: boolean;
  runtimeRailMemberships?: ReturnType<
    typeof useAgentGUIConversationRailQuery
  >["runtimeRailMemberships"];
  runtimeRailSectionsPending?: boolean;
  runtimeSectionsEnabled?: boolean;
  retryRuntimeRail?: () => Promise<void>;
  activityConversations?: AgentGUIConversationSummary[];
  conversations?: AgentGUIConversationSummary[];
  runtimeOverrides?: Partial<AgentGUIRuntime>;
  onSelectConversation?: (id: string) => void;
  onConfirmDeleteConversation?: () => void;
}) {
  const conversation = conversationFixture({ hasUnreadCompletion });
  const activityConversations = requestedActivityConversations ?? [
    conversation
  ];
  const conversations = requestedConversations ?? activityConversations;
  const activityController = createAgentGUIConversationActivityController();
  activityController.configure({
    available: capability,
    conversations: activityConversations,
    identityKey: "workspace-1",
    scopeKey: "workspace-1"
  });
  const snapshot = {
    sessionMessagesById: {}
  } as unknown as AgentActivitySnapshot;
  const runtime = {
    conversationActivityViewEnabled: capability,
    getSnapshot: () => snapshot,
    listSessionsPage: listPage,
    subscribe: () => () => {},
    ...runtimeOverrides
  } as unknown as AgentGUIRuntime;
  function PaneHarness(): React.JSX.Element {
    const [conversationQuery, setConversationQuery] = useState("");
    const [pendingDeleteConversationId, setPendingDeleteConversationId] =
      useState<string | null>(null);
    return (
      <AgentGUIConversationRailPane
        activeConversation={null}
        activeConversationId={null}
        agentTargets={[]}
        agentTargetsLoading={false}
        conversationFilter={{ kind: "all" }}
        conversationQuery={conversationQuery}
        conversations={conversations}
        createConversationDisabled={false}
        isCollapsed={false}
        isDeletingConversation={false}
        isDeletingProjectConversations={false}
        isLoadingConversations={false}
        labels={LABELS}
        pendingDeleteConversationId={pendingDeleteConversationId}
        railQuery={{
          ...RAIL_QUERY,
          activityController,
          activityConversations,
          runtimeRailConversations,
          runtimeRailFailed,
          runtimeRailMemberships,
          runtimeRailSectionsPending,
          runtimeSectionsEnabled,
          retryRuntimeRail
        }}
        revealRequest={null}
        uiLanguage="en"
        userProjects={[]}
        workspaceId="workspace-1"
        workspaceUserProjectI18n={PROJECT_I18N}
        onCancelDeleteConversation={() => setPendingDeleteConversationId(null)}
        onConfirmDeleteConversation={onConfirmDeleteConversation}
        onConfirmDeleteConversations={() => {}}
        onConfirmDeleteProjectConversations={async () => []}
        onConversationQueryChange={setConversationQuery}
        onCreateConversation={() => {}}
        onMarkConversationUnread={() => {}}
        onMoveProject={async () => {}}
        onRemoveProject={() => {}}
        onRequestDeleteConversation={setPendingDeleteConversationId}
        onRequestRenameConversation={() => {}}
        onSelectConversation={onSelectConversation}
        onSelectConversationFilterTarget={() => {}}
        onToggleConversationPinned={() => {}}
        onToggleProjectPinned={async () => {}}
        onUpdateConversationFilter={() => {}}
      />
    );
  }
  return render(
    <AgentGUIRuntimeProvider runtime={runtime}>
      <TooltipProvider>
        <PaneHarness />
      </TooltipProvider>
    </AgentGUIRuntimeProvider>
  );
}

function conversationFixture(
  overrides: Pick<AgentGUIConversationSummary, "hasUnreadCompletion"> = {}
): AgentGUIConversationSummary {
  return {
    cwd: "/workspace",
    hasUnreadCompletion: overrides.hasUnreadCompletion,
    id: "session-1",
    provider: "codex",
    sortTimeUnixMs: Date.now(),
    status: "ready",
    title: "Session 1",
    updatedAtUnixMs: Date.now()
  };
}

const RAIL_QUERY = {
  activityConversations: [conversationFixture()],
  activityRootFacts: new Map(),
  batchDeletionAvailable: false,
  isInteractionLocked: () => false,
  loadMoreSectionConversations: () => {},
  railSearch: {
    enabled: false,
    failed: false,
    hasMore: false,
    loadMore: () => {},
    loadingMore: false,
    pending: false,
    retry: () => {},
    sessionIds: []
  },
  runtimeRailConversations: [],
  runtimeRailFailed: false,
  runtimeRailMemberships: null,
  runtimeRailReconcilingSessionIds: new Set<string>(),
  runtimeRailScopeResolved: true,
  runtimeRailSectionsPending: false,
  retryRuntimeRail: () => Promise.resolve(),
  runtimeSectionsEnabled: false,
  sectionPageStates: new Map()
} as unknown as ReturnType<typeof useAgentGUIConversationRailQuery>;

const PROJECT_I18N = {
  t: (key: string) => key,
  tFirst: () => "Projects"
} as never;

const LABELS = {
  cancel: "Cancel",
  deleteSession: "Delete session",
  deleteSessionConfirm: "Confirm delete",
  showMoreConversations: "Show more",
  activityConversationSource: "Conversation",
  activityNothingNeedsAttention: "Nothing needs attention",
  activityPriority: "Priority",
  activityStatusFailed: "Failed",
  activityStatusRecentlyActive: "Recently active",
  activityStatusUnread: "Unread result",
  activityStatusWaiting: "Waiting for you",
  activityStatusWorking: "Working",
  activityToday: "Today",
  activityYesterday: "Yesterday",
  conversationUnavailable: "Session unavailable",
  conversationsLoadFailed: "Could not load sessions",
  loadingConversations: "Loading sessions",
  newConversation: "New session",
  noConversations: "No sessions",
  projectRailCreateProject: "New project",
  projectRailLinkExistingProject: "Link project",
  retryConversations: "Retry",
  retrySearch: "Retry",
  searchFailed: "Search failed",
  searchNoConversations: "No results",
  searchPlaceholder: "Search sessions",
  sectionConversations: "Chats",
  sectionPinned: "Pinned",
  turnOffActivityView: "Turn off activity view",
  viewActivity: "View activity",
  viewActivityNeedsAttention: "View activity, attention needed"
} as unknown as AgentGUIConversationRailLabels;
