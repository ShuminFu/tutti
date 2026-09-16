import type { WorkspaceAgentSessionDetailViewModel } from "../../workspaceAgentSessionDetailViewModel";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";
import { stripLeadingPairKickoff } from "../components/pairKickoffEnvelope";

export function projectAgentMessageFinalText(
  rows: readonly AgentTranscriptRowVM[],
  detail: WorkspaceAgentSessionDetailViewModel
): AgentTranscriptRowVM[] {
  const finalTextTargetKeys = findLatestAssistantFinalTextTargetKeys(
    rows,
    buildAssistantFinalTextEligibleTurnIds(detail)
  );
  return rows.map((row) => {
    if (row.kind !== "message") {
      return row;
    }

    let changed = false;
    const messages = row.messages.map((message) => {
      const isTurnFinalText =
        row.speaker === "assistant" &&
        finalTextTargetKeys.has(messagePresentationTargetKey(row, message));
      const copyText =
        row.speaker === "user"
          ? copyTextForUserMessage(message)
          : isTurnFinalText
            ? message.body
            : null;
      if (
        (message.copyText ?? null) === copyText &&
        message.isTurnFinalText === (isTurnFinalText ? true : undefined)
      ) {
        return message;
      }
      changed = true;
      const nextMessage = isTurnFinalText
        ? { ...message, isTurnFinalText: true as const }
        : omitTurnFinalText(message);
      if (copyText) {
        return { ...nextMessage, copyText };
      }
      const { copyText: _copyText, ...withoutCopyText } = nextMessage;
      return withoutCopyText;
    });

    return changed ? { ...row, messages } : row;
  });
}

function omitTurnFinalText(
  message: AgentMessageContentVM
): AgentMessageContentVM {
  const { isTurnFinalText: _isTurnFinalText, ...withoutTurnFinalText } =
    message;
  return withoutTurnFinalText;
}

function buildAssistantFinalTextEligibleTurnIds(
  detail: WorkspaceAgentSessionDetailViewModel
): ReadonlySet<string> {
  const ids = new Set<string>();
  detail.turns.forEach((turn, index) => {
    if (
      index < detail.turns.length - 1 ||
      isLatestTranscriptTurnSettled(detail)
    ) {
      ids.add(turn.id);
    }
  });
  return ids;
}

function isLatestTranscriptTurnSettled(
  detail: WorkspaceAgentSessionDetailViewModel
): boolean {
  const latestTranscriptTurnId = detail.turns.at(-1)?.id;
  const canonicalTurn = detail.sessionTurns?.find(
    (turn) => turn.turnId === latestTranscriptTurnId
  );
  const activeTurn = detail.session.activeTurn;
  if (
    activeTurn &&
    activeTurn.turnId === latestTranscriptTurnId &&
    activeTurn.phase !== "settled"
  ) {
    return false;
  }
  if (canonicalTurn) {
    return (
      canonicalTurn.phase === "settled" &&
      detail.showProcessingIndicator !== true
    );
  }
  const activePhase = activeTurn?.phase ?? "";
  return (
    detail.showProcessingIndicator !== true &&
    !["submitted", "running", "waiting", "settling"].includes(activePhase)
  );
}

// Walks every eligible Turn backwards and stamps its latest visible assistant
// text as the Turn's final text. Eligibility is the Turn-level "this Turn is
// settled" signal (see buildAssistantFinalTextEligibleTurnIds), so a message's
// own stream state must not veto the stamp: a provider that dies mid-stream
// leaves its last assistant text at status "streaming" forever even though the
// Turn settled, and honouring that stale state would hand the Turn's final text
// to an earlier process line while the real reply collapsed into the work
// section — the "important output is never hidden" failure that
// docs/architecture/agent-gui-node.md forbids.
function findLatestAssistantFinalTextTargetKeys(
  rows: readonly AgentTranscriptRowVM[],
  eligibleTurnIds: ReadonlySet<string>
): ReadonlySet<string> {
  const targetKeys = new Set<string>();
  const coveredTurnIds = new Set<string>();
  for (let rowIndex = rows.length - 1; rowIndex >= 0; rowIndex -= 1) {
    const row = rows[rowIndex];
    if (
      row?.kind !== "message" ||
      row.speaker !== "assistant" ||
      coveredTurnIds.has(row.turnId) ||
      !eligibleTurnIds.has(row.turnId)
    ) {
      continue;
    }
    for (
      let messageIndex = row.messages.length - 1;
      messageIndex >= 0;
      messageIndex -= 1
    ) {
      const message = row.messages[messageIndex];
      if (message && isVisibleTextMessage(message)) {
        targetKeys.add(messagePresentationTargetKey(row, message));
        coveredTurnIds.add(row.turnId);
        break;
      }
    }
  }
  return targetKeys;
}

function messagePresentationTargetKey(
  row: AgentMessageRowVM,
  message: AgentMessageContentVM
): string {
  return `${row.id}\u0000${message.id}`;
}

function copyTextForUserMessage(message: AgentMessageContentVM): string | null {
  if (!isVisibleTextMessage(message)) return null;
  // 分栏结对模式的第一句：复制只拿用户原话，开工卡不进剪贴板（票 05 评审 E）。
  const text = stripLeadingPairKickoff(message.body);
  return text.trim() ? text : null;
}

function isVisibleTextMessage(message: AgentMessageContentVM): boolean {
  return (
    message.body.trim() !== "" &&
    message.contentKind !== "image-grid" &&
    message.contentKind !== "collaboration" &&
    !message.visibleError &&
    !message.systemNotice
  );
}
