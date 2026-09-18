import type { AgentMessageContentVM } from "../contracts/agentMessageRowVM";
import type { AgentTranscriptPresentationKind } from "../contracts/agentTranscriptPresentation";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";

type AgentSystemNoticeVM = NonNullable<AgentMessageContentVM["systemNotice"]>;

export const ASSISTANT_MESSAGE_KIND_COMMENTARY = "assistant-commentary";
export const ASSISTANT_MESSAGE_KIND_FINAL = "assistant-final";

export function resolveAgentTranscriptPresentationKind(
  notice: Pick<AgentSystemNoticeVM, "command" | "commandStatus"> | null,
  messageKind?: string | null
): AgentTranscriptPresentationKind {
  if (notice?.command === "compact") {
    if (notice.commandStatus === "running") {
      return "specific-progress";
    }
    if (
      notice.commandStatus === "completed" ||
      notice.commandStatus === "failed" ||
      notice.commandStatus === "canceled"
    ) {
      return "turn-boundary";
    }
  }
  if (messageKind === ASSISTANT_MESSAGE_KIND_COMMENTARY) {
    return "specific-progress";
  }
  return "content";
}

export function agentTranscriptRowHasPresentationKind(
  row: AgentTranscriptRowVM | undefined,
  presentationKind: AgentTranscriptPresentationKind
): boolean {
  return Boolean(
    row?.kind === "message" &&
    row.messages.some(
      (message) => message.presentationKind === presentationKind
    )
  );
}
