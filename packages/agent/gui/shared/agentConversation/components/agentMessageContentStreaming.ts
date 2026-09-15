import type { ToolCallStatusKind } from "../../workspaceAgentToolCallDisplay";

export interface AgentMessageContentStreamingInput {
  /** The row's Turn is the session's live Turn (never true for a settled Turn). */
  isActiveTurn: boolean;
  /**
   * The canonical Turn this row belongs to is settled. Unknown Turn identity
   * (turnless imported or session-level rows) passes `false`: without a
   * lifecycle fact to consult, the message-level signal stays the best evidence.
   */
  turnSettled: boolean;
  statusKind?: ToolCallStatusKind | null;
}

/**
 * Whether assistant content may still be growing, and therefore still needs the
 * streaming presentation (mermaid placeholder, tail-stabilized markdown).
 *
 * The Turn lifecycle is authoritative, not the message row: a settled Turn can
 * no longer stream, while a provider that dies mid-stream leaves its last
 * message at status `streaming` forever. Trusting that stale row would pin a
 * diagram in the Turn's final reply to a permanent loading placeholder and keep
 * the reply on the streaming markdown path — the "important output is never
 * hidden" failure that docs/architecture/agent-gui-node.md forbids.
 */
export function isAgentMessageContentStreaming(
  input: AgentMessageContentStreamingInput
): boolean {
  if (input.isActiveTurn) {
    return true;
  }
  if (input.turnSettled) {
    return false;
  }
  return input.statusKind === "working" || input.statusKind === "waiting";
}
