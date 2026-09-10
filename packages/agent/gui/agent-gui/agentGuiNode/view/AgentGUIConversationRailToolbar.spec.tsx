import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentGUIConversationRailToolbar } from "./AgentGUIConversationRailToolbar";
import { AgentGUIConversationRailToggleButton } from "./AgentGUIConversationRailToggleButton";

afterEach(cleanup);

const labels = {
  newConversation: "New session",
  searchPlaceholder: "Search sessions",
  turnOffActivityView: "Turn off activity",
  viewActivity: "View activity",
  viewActivityNeedsAttention: "View activity"
} as never;

const activityView = {
  available: false,
  conversationsById: new Map(),
  enabled: false,
  needsAttention: false,
  toggle: () => undefined
} as never;

// Issue 03: with the Agent header row gone in the embedded product, the rail's
// own top row is the single home of the three functional controls.
describe("AgentGUIConversationRailToolbar", () => {
  it("renders the rail toggle, session search and new session in one row", () => {
    const onToggle = vi.fn();
    const { container } = render(
      <AgentGUIConversationRailToolbar
        activityView={activityView}
        conversationQuery=""
        createConversationDisabled={false}
        labels={labels}
        leadingAccessory={
          <AgentGUIConversationRailToggleButton
            placement="rail"
            toggle={{
              collapseLabel: "Hide sidebar",
              expandLabel: "Show sidebar",
              isAutoCollapsed: false,
              isCollapsed: false,
              onToggle
            }}
          />
        }
        onConversationQueryChange={() => undefined}
        onCreateConversation={() => undefined}
      />
    );

    const row = container.querySelector(".agent-gui-node__rail-toolbar");
    expect(row).not.toBeNull();

    const toggle = screen.getByTestId("agent-gui-toggle-conversation-rail");
    expect(row?.contains(toggle)).toBe(true);
    expect(toggle).toHaveAttribute(
      "aria-controls",
      "agent-gui-conversation-rail"
    );
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    // The toggle leads the row, ahead of search and new session.
    expect(row?.firstElementChild).toBe(toggle);

    expect(row?.querySelector("input")).not.toBeNull();
    const newConversation = screen.getByTestId("agent-gui-new-conversation");
    expect(row?.contains(newConversation)).toBe(true);

    fireEvent.click(toggle);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it("keeps the row to search and new session without a toggle accessory", () => {
    const { container } = render(
      <AgentGUIConversationRailToolbar
        activityView={activityView}
        conversationQuery=""
        createConversationDisabled={false}
        labels={labels}
        onConversationQueryChange={() => undefined}
        onCreateConversation={() => undefined}
      />
    );

    expect(
      screen.queryByTestId("agent-gui-toggle-conversation-rail")
    ).toBeNull();
    expect(
      container.querySelector(".agent-gui-node__rail-toolbar input")
    ).not.toBeNull();
    expect(
      screen.getByTestId("agent-gui-new-conversation")
    ).toBeInTheDocument();
  });
});
