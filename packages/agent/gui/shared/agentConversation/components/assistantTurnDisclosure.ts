import type { AgentMessageContentVM } from "../contracts/agentMessageRowVM";

/**
 * Provider-neutral Turn disclosure policy.
 *
 * Adapters may still persist purpose tags for copy/final identity, but folding
 * ordinary intermediate replies is decided here from Turn lifecycle + the
 * already-stamped final-text marker — not from a provider name.
 */
export interface AssistantTurnDisclosurePolicy {
  foldIntermediateReplies: boolean;
}

export function isAssistantProcessPresentation(
  message: AgentMessageContentVM
): boolean {
  return (
    message.presentationKind === "specific-progress" ||
    message.presentationKind === "turn-boundary"
  );
}

export function isProtectedAssistantContent(
  message: AgentMessageContentVM
): boolean {
  if (message.isTurnFinalText || message.isExplicitAssistantFinal) {
    return true;
  }
  if (message.visibleError) {
    return true;
  }
  if (message.systemNotice && message.presentationKind === "content") {
    return true;
  }
  switch (message.contentKind) {
    case "image-grid":
    case "plan":
    case "collaboration":
    case "tutti-checkpoint-wake":
    case "tutti-plan-issue-link":
      return true;
    default:
      return false;
  }
}

export function isIntermediateAssistantReply(
  message: AgentMessageContentVM
): boolean {
  if (isAssistantProcessPresentation(message)) {
    return false;
  }
  if (isProtectedAssistantContent(message)) {
    return false;
  }
  return message.body.trim().length > 0;
}

export function isAssistantWorkMessage(
  message: AgentMessageContentVM,
  policy: AssistantTurnDisclosurePolicy
): boolean {
  if (isAssistantProcessPresentation(message)) {
    return true;
  }
  return (
    policy.foldIntermediateReplies && isIntermediateAssistantReply(message)
  );
}
