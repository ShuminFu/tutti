import type { AgentActivityTurn } from "@tutti-os/agent-activity-core";
import type {
  WorkspaceAgentSessionDetailMessage,
  WorkspaceAgentSessionDetailTurn
} from "./workspaceAgentSessionDetailViewModel";

/**
 * Enriches Turns that the hydrated transcript has already projected.
 *
 * `sessionTurns` can describe the full session while `turns` contains only the
 * current message window. Canonical lifecycle metadata therefore must not
 * create transcript membership or alter transcript order.
 */
export function enrichProjectedTurnsWithCanonicalErrors({
  turns,
  sessionTurns,
  provider,
  agentSessionId
}: {
  turns: ReadonlyMap<string, WorkspaceAgentSessionDetailTurn>;
  sessionTurns: readonly AgentActivityTurn[];
  provider: string;
  agentSessionId: string;
}): void {
  for (const canonicalTurn of sessionTurns) {
    if (
      canonicalTurn.outcome !== "failed" &&
      canonicalTurn.outcome !== "interrupted"
    ) {
      continue;
    }
    const detail = canonicalTurn.error?.message.trim() ?? "";
    if (!detail) {
      continue;
    }

    const turn = turns.get(canonicalTurn.turnId);
    if (!turn) {
      continue;
    }
    const existingErrorMessage = turn.agentMessages.find(
      (message) => message.visibleError
    );
    if (existingErrorMessage) {
      const explicitDetail = canonicalTurn.error?.detail ?? "";
      if (existingErrorMessage.visibleError) {
        const refinedCode = refinedCanonicalErrorCode(
          existingErrorMessage.visibleError.code,
          canonicalTurn.error?.code?.trim() ?? ""
        );
        const storedDetail = existingErrorMessage.visibleError.detail ?? "";
        const preserveFullDetail =
          canonicalTurn.error?.code === "provider_protocol_incompatible" &&
          explicitDetail.endsWith("...") &&
          storedDetail.length > explicitDetail.length &&
          storedDetail.startsWith(explicitDetail.slice(0, -3));
        if (explicitDetail.trim() || refinedCode) {
          existingErrorMessage.visibleError = {
            ...existingErrorMessage.visibleError,
            ...(refinedCode ? { code: refinedCode } : {}),
            ...(explicitDetail.trim()
              ? {
                  detail: preserveFullDetail ? storedDetail : explicitDetail,
                  detailAvailable: true
                }
              : {})
          };
        }
      }
      continue;
    }

    const matchingMessage = turn.agentMessages.find(
      (message) => message.body.trim() === detail
    );
    if (matchingMessage) {
      matchingMessage.status = "failed";
      matchingMessage.statusKind = "failed";
      matchingMessage.visibleError = visibleErrorFromCanonicalTurn(
        canonicalTurn,
        provider
      );
      continue;
    }

    const message: WorkspaceAgentSessionDetailMessage = {
      id: `turn-error:${agentSessionId}:${canonicalTurn.turnId}`,
      body: detail,
      status: "failed",
      statusKind: "failed",
      turnId: canonicalTurn.turnId,
      occurredAtUnixMs:
        canonicalTurn.settledAtUnixMs ?? canonicalTurn.updatedAtUnixMs,
      visibleError: visibleErrorFromCanonicalTurn(canonicalTurn, provider)
    };
    turn.agentMessages.push(message);
    turn.agentItems.push({ kind: "message", message });
  }
}

function visibleErrorFromCanonicalTurn(
  turn: AgentActivityTurn,
  provider: string
): NonNullable<WorkspaceAgentSessionDetailMessage["visibleError"]> {
  const explicitDetail = turn.error?.detail ?? "";
  return {
    code: turn.error?.code?.trim() || null,
    phase: "turn",
    provider: provider.trim() || null,
    detail: explicitDetail || turn.error?.message.trim() || null,
    ...(explicitDetail.trim() ? { detailAvailable: true } : {}),
    retryable: null
  };
}

/**
 * Codes the runtime emits when it cannot narrow the cause. A visible-error
 * payload written before the classifier learned a narrower code — or before the
 * runtime could classify at all — carries one of these (or no code), so the
 * canonical Turn error for the same Turn may be strictly more specific.
 */
const COARSE_VISIBLE_ERROR_CODES: ReadonlySet<string> = new Set([
  "provider_error",
  "unknown"
]);

// Only this protocol fix upgrades historical coarse cards. Other canonical
// codes keep the existing presentation behavior.
function refinedCanonicalErrorCode(
  storedCode: string | null | undefined,
  canonicalCode: string
): string | null {
  if (
    canonicalCode !== "provider_protocol_incompatible" ||
    canonicalCode === storedCode
  ) {
    return null;
  }
  const stored = (storedCode ?? "").trim();
  if (stored && !COARSE_VISIBLE_ERROR_CODES.has(stored)) {
    return null;
  }
  return canonicalCode;
}
