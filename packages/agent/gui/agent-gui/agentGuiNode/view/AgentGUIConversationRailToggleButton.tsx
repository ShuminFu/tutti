import { BareIconButton } from "@tutti-os/ui-system/components";
import { CanvasNodePanelLinedIcon } from "../../shared/canvasNodeChromeIcons";
import type { AgentGUIConversationRailToggle } from "./AgentGUINodeView.types";
import styles from "../AgentGUINode.styles";

/**
 * Conversation-rail collapse toggle for surfaces where the surrounding product
 * owns the only top bar (`frame.hostProvidedTopBar`). Upstream this control
 * lives on the Agent window header; without that header it has to live where
 * the rail itself begins, and it must stay reachable once the rail is gone —
 * hence the two placements.
 *
 * - `placement="rail"`: first cell of the rail's own top row, next to the
 *   session search field and the new-session button.
 * - `placement="floating"`: pinned to the top-left of the layout while the rail
 *   is collapsed (or the narrow drawer is closed), because the collapsed rail
 *   is `inert` and cannot carry its own re-open affordance.
 *
 * Exactly one placement is mounted at a time, so the shared
 * `agent-gui-toggle-conversation-rail` test id stays unambiguous — the narrow
 * drawer's Escape handler relies on that to restore focus.
 */
export function AgentGUIConversationRailToggleButton({
  placement,
  toggle
}: {
  placement: "floating" | "rail";
  toggle: AgentGUIConversationRailToggle;
}): React.JSX.Element {
  const label = toggle.isCollapsed ? toggle.expandLabel : toggle.collapseLabel;
  return (
    <BareIconButton
      aria-label={label}
      aria-controls="agent-gui-conversation-rail"
      aria-expanded={!toggle.isCollapsed}
      className={
        placement === "floating"
          ? `${styles.railToggleButton} ${styles.railToggleButtonFloating} nodrag tsh-desktop-no-drag`
          : styles.railToggleButton
      }
      data-agent-gui-conversation-rail-auto-collapsed={
        toggle.isAutoCollapsed ? "true" : "false"
      }
      data-agent-gui-conversation-rail-collapsed={
        toggle.isCollapsed ? "true" : "false"
      }
      data-agent-gui-conversation-rail-toggle-placement={placement}
      data-testid="agent-gui-toggle-conversation-rail"
      size="md"
      title={label}
      type="button"
      onClick={(event) => {
        event.stopPropagation();
        toggle.onToggle();
      }}
    >
      <CanvasNodePanelLinedIcon width={18} height={18} aria-hidden="true" />
    </BareIconButton>
  );
}
