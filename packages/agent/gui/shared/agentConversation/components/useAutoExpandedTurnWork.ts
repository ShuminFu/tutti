import { useState } from "react";
import type { AgentActivityTurn } from "@tutti-os/agent-activity-core";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";

interface TurnWorkDisclosureSnapshot {
  liveTurnKey: string | null;
  autoExpandedTurnKeys: ReadonlySet<string>;
}

const EMPTY_TURN_KEYS: ReadonlySet<string> = new Set();

export function useAutoExpandedTurnWork(
  sessionId: string,
  activeTurnId: string | null | undefined,
  turnById: ReadonlyMap<string, AgentActivityTurn>,
  followEndMode: AgentConversationFollowEndMode | undefined
): ReadonlySet<string> {
  const [snapshot, setSnapshot] = useState<TurnWorkDisclosureSnapshot>({
    liveTurnKey: null,
    autoExpandedTurnKeys: EMPTY_TURN_KEYS
  });
  const activeTurn = activeTurnId ? turnById.get(activeTurnId) : null;
  const liveTurnKey =
    activeTurn && activeTurn.phase !== "settled"
      ? `${sessionId}:${activeTurn.turnId}`
      : null;
  const previouslyLiveTurn = snapshot.liveTurnKey?.startsWith(`${sessionId}:`)
    ? turnById.get(snapshot.liveTurnKey.slice(sessionId.length + 1))
    : null;
  const autoExpandedTurnKeys =
    followEndMode === "detached" &&
    previouslyLiveTurn?.phase === "settled" &&
    previouslyLiveTurn.outcome === "completed" &&
    snapshot.liveTurnKey
      ? new Set([...snapshot.autoExpandedTurnKeys, snapshot.liveTurnKey])
      : snapshot.autoExpandedTurnKeys;

  if (
    liveTurnKey !== snapshot.liveTurnKey ||
    autoExpandedTurnKeys !== snapshot.autoExpandedTurnKeys
  ) {
    setSnapshot({ liveTurnKey, autoExpandedTurnKeys });
  }

  return autoExpandedTurnKeys;
}
