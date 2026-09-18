import { describe, expect, it } from "vitest";
import type { AgentMessageContentVM } from "../contracts/agentMessageRowVM";
import {
  isAssistantWorkMessage,
  isIntermediateAssistantReply
} from "./assistantTurnDisclosure";

describe("assistantTurnDisclosure", () => {
  it("treats unmarked ordinary text as intermediate once folding is enabled", () => {
    const message = content("Checking files.");
    expect(isIntermediateAssistantReply(message)).toBe(true);
    expect(
      isAssistantWorkMessage(message, { foldIntermediateReplies: false })
    ).toBe(false);
    expect(
      isAssistantWorkMessage(message, { foldIntermediateReplies: true })
    ).toBe(true);
  });

  it("never folds the Turn's final answer or special cards", () => {
    expect(
      isAssistantWorkMessage(
        { ...content("Done."), isTurnFinalText: true },
        { foldIntermediateReplies: true }
      )
    ).toBe(false);
    expect(
      isAssistantWorkMessage(
        { ...content("Done."), isExplicitAssistantFinal: true },
        { foldIntermediateReplies: true }
      )
    ).toBe(false);
    expect(
      isAssistantWorkMessage(
        { ...content("Plan"), contentKind: "plan" },
        { foldIntermediateReplies: true }
      )
    ).toBe(false);
  });

  it("always folds progress and turn-boundary rows", () => {
    const progress = {
      ...content("Compacting context."),
      presentationKind: "specific-progress" as const
    };
    expect(isIntermediateAssistantReply(progress)).toBe(false);
    expect(
      isAssistantWorkMessage(progress, { foldIntermediateReplies: false })
    ).toBe(true);
  });
});

function content(body: string): AgentMessageContentVM {
  return {
    kind: "message-content",
    id: `message:${body}`,
    turnId: "turn-1",
    body,
    presentationKind: "content",
    occurredAtUnixMs: 1
  };
}
