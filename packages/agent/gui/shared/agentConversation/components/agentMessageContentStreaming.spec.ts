import { describe, expect, it } from "vitest";
import { isAgentMessageContentStreaming } from "./agentMessageContentStreaming";

describe("isAgentMessageContentStreaming", () => {
  it("keeps a live Turn's content streaming regardless of message status", () => {
    expect(
      isAgentMessageContentStreaming({
        isActiveTurn: true,
        turnSettled: false,
        statusKind: "completed"
      })
    ).toBe(true);
  });

  it("stops streaming once the Turn is settled, even when the message row is stale", () => {
    // Provider died mid-stream: the Turn settled but the message never left
    // `streaming`, which projects to statusKind `working`.
    expect(
      isAgentMessageContentStreaming({
        isActiveTurn: false,
        turnSettled: true,
        statusKind: "working"
      })
    ).toBe(false);
    expect(
      isAgentMessageContentStreaming({
        isActiveTurn: false,
        turnSettled: true,
        statusKind: "waiting"
      })
    ).toBe(false);
  });

  it("falls back to the message status while the Turn lifecycle is unknown", () => {
    expect(
      isAgentMessageContentStreaming({
        isActiveTurn: false,
        turnSettled: false,
        statusKind: "working"
      })
    ).toBe(true);
    expect(
      isAgentMessageContentStreaming({
        isActiveTurn: false,
        turnSettled: false,
        statusKind: "waiting"
      })
    ).toBe(true);
    expect(
      isAgentMessageContentStreaming({
        isActiveTurn: false,
        turnSettled: false,
        statusKind: "completed"
      })
    ).toBe(false);
    expect(
      isAgentMessageContentStreaming({
        isActiveTurn: false,
        turnSettled: false
      })
    ).toBe(false);
  });
});
