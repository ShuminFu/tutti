# Provider-Native Subagents

Status: accepted architecture; Codex and Claude Code child routing implemented,
remaining work listed below. Product rules that are now current behavior live
in [Agent GUI Node §3.1](../architecture/agent-gui-node.md); this record keeps
the entity, ownership, and adapter contract that implementations must not drift
from.

Covers provider-native child agents only: Codex app-server multi-agent child
threads, Claude Code SDK `Task`/subagent sessions and turns, and ACP providers
exposing an equivalent nested agent concept. It does not cover Tutti launching
another top-level AgentGUI session, handoff mentions, another provider process,
or Claude Code SDK task-system processes. A detached `run_in_background` Bash
shell — including one that merely tracks a top-level session — stays a
root-owned tool launch: its task id is kept for explicit stop, but it creates no
child session, gates no root turn, and reserves no agent continuation.

## Entity Contract

A provider-native child agent is a subordinate `WorkspaceAgentSession`, not a
new entity. `WorkspaceAgentTurn` remains the top-level user-visible unit and is
never duplicated into a child-agent status field:

```text
Session (root, user visible)
  -> Turn (root, user visible)
    -> Session[] (child, hidden; own turns, messages, interactions, children)
```

- Every session carries an explicit `root` or `child` kind. A child adds durable
  `parentAgentSessionId`, `parentTurnId`, `parentToolCallId`, plus root session
  and root turn for nested lookup and cancellation, and provider handles/aliases
  (`threadId`, `taskId`, `agentId`, `parentToolUseId`).
- The parent relationship records which turn and delegation tool created the
  child. It does not move child messages or interactions under the root turn:
  their canonical owner stays the exact child session and child turn, and root
  projections join them through the recorded fields.
- There is no `WorkspaceAgentSubAgentRun` entity. Each child has exactly one
  immutable creator edge; it is never re-parented, reused by another parent
  turn, or split into independently settled delegations. Later guidance only
  adds turns inside that same child session.
- Child lifecycle reuses the existing turn phase/outcome vocabulary. The daemon
  creates the submitted child turn when delegation is accepted, before all
  provider aliases are known, and binds later identifiers to it.
- Child sessions are hidden from ordinary navigation. The session detail read
  returns the requested session plus its nested `childSessions` as a flat
  collection (clients rebuild the tree from parent fields; no recursive DTO and
  no inference from transcripts). Lists, sections, pins, and search return root
  sessions only; a child is reached through the detail read or its exact id.

## Turn Ownership And Lifecycle

Provider transports report provider facts; they do not decide whether a root
turn is complete.

- A provider adapter normalizes native events into the shared session/turn/
  message/interaction vocabulary, carrying child `agentSessionId`/`turnId` as
  the event owner plus parent session/turn, parent tool call id, root
  session/turn, provider aliases, and any transcript or interaction payload.
  There is no parallel `subagent_*` lifecycle.
- A root provider `turn_completed` becomes `root_provider_turn_completed` and
  must never be normalized into a canonical root `turn.completed` inside the
  adapter. Both root-provider start and completion events must reach
  `services/tuttid` as durable inputs; a stream-only diagnostic leaves the
  canonical root turn active forever.
- A persisted state patch mutates a canonical turn only when it carries an
  explicit structured `Turn` transition. Runtime `TurnLifecycle` /
  `SubmitAvailability` snapshots may enrich the live stream, but must never be
  read as fallback canonical turn facts — in particular an enriched root
  lifecycle event must not replay `root=running` after the durable root settled.
- `services/tuttid` owns the durable root/parent/child relationship and the root
  completion rule: it records the root provider outcome, checks every nested
  child turn, and emits canonical root completion only when the root can settle.
  One atomic SQLite write covers the root-provider outcome, child turn
  transition, root turn transition, active turn reference, and outbox events,
  and contains no provider-specific policy.
- Event order must not change the result. Root completion with active children
  leaves the root waiting until the last child is terminal; a child terminal
  before root completion leaves the root active until the root completes. A
  child that appears only after the root settled is a normalization or ordering
  bug and must not be fixed by resurrecting the root turn.
- Session deletion follows the same tree: deleting a root or child
  atomically tombstones it with all nested children and removes their turns and
  interactions, so no child can survive as an orphan.

## Commands

Sending while children run targets the root agent and the same root turn:
append/guide the active root provider turn, let the provider decide how the
root uses it, and never fan the text out to every child. Direct user targeting
of one child is not in the product contract and needs its own decision rather
than a provider shortcut. Adapters must declare whether root guidance works
while only children run; when it cannot be sent safely the command returns a
clear pending/unsupported result instead of silently starting a new root turn.

Cancel is a fan-out over one root turn: cancel the live root provider turn,
traverse every nested child session owned by that turn, cancel every
non-terminal child handle/turn, settle confirmed children canceled and
unconfirmed children interrupted after a timeout, then settle the root
canceled. `services/tuttid` records the exact `(agentSessionId, turnId)` targets
from the durable tree before entering the provider runtime; the runtime request
carries the root session id separately because it locates the live provider
runtime, while each target identifies a canonical root or child turn. The
controller must not register a second copy of child state.

## UI Projection

AgentGUI renders subordinate sessions under the parent delegation card: root
transcript rows stay root-only and nested children follow recorded parent/root
fields as lanes or nested progress instead of being flattened into the root
transcript. Spawn-tool success means delegation was accepted, not that the child
finished; child terminal state comes from the canonical child turn, never from
the parent tool call status. Display-only rows never become approval or prompt
authority — pending interactions remain daemon-owned canonical entities on the
exact child session and turn, and actions route by the exact
`(agentSessionId, turnId, requestId)` tuple while the recorded root session only
locates the shared live runtime. The detail reconcile loads root plus nested
children into the same workspace engine and reads messages through each
session's existing endpoint; there is no second child-session store.

Superseded shapes are removed, not dual-read: historical rows carrying only
`ownerThreadId`, `ownerCallId`, or `runtimeContext.backgroundAgents` are not
reconstructed, and the standard ACP `backgroundAgents` map and runtime-context
projection are gone. ACP adapters must not invent child identity from display
text, tool title, or message order; providers without real child identity and
cancellation handles emit no child-session state.

## Remaining Work

- Decide which ACP providers expose enough child identity and cancellation
  handles to opt in, then adapt them the same way.
- Finish removing the superseded `backgroundAgents`, `ownerThreadId`, and
  `ownerCallId` paths wherever a provider still emits them.
- Validation that still needs evidence: concurrent children, child events before
  spawn registration, root terminal before child terminal, cancellation with
  late child events, child creation racing a prepared root cancellation, a
  provider terminal arriving after canonical cancellation, unowned provider
  turns after cancel, child approvals, and user guidance while children run.
