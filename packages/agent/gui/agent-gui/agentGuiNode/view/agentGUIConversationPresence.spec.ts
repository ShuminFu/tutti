import { describe, expect, it } from "vitest";
import { agentGUIConversationPresence } from "./agentGUIConversationPresence";

describe("agentGUIConversationPresence", () => {
  it("画蓝点：这一轮在跑", () => {
    expect(agentGUIConversationPresence({ status: "working" })).toBe("working");
  });

  it("画蓝点：agent 在等用户答话，这一轮没收尾", () => {
    expect(agentGUIConversationPresence({ status: "waiting" })).toBe("working");
  });

  it("画蓝点：状态是闲的，但活跃轮次还在", () => {
    expect(
      agentGUIConversationPresence({
        status: "ready",
        activeTurn: { turnId: "turn-1" } as never
      })
    ).toBe("working");
  });

  it.each(["ready", "completed", "failed", "canceled"] as const)(
    "画绿点：%s 且没有活跃轮次",
    (status) => {
      expect(agentGUIConversationPresence({ status })).toBe("idle");
    }
  );

  it("画绿点：endedAtUnixMs 是 0 / null / undefined 都算没结束", () => {
    expect(agentGUIConversationPresence({ status: "ready" })).toBe("idle");
    expect(
      agentGUIConversationPresence({ status: "ready", endedAtUnixMs: null })
    ).toBe("idle");
    expect(
      agentGUIConversationPresence({ status: "ready", endedAtUnixMs: 0 })
    ).toBe("idle");
  });

  it("不画点：会话已结束", () => {
    expect(
      agentGUIConversationPresence({ status: "completed", endedAtUnixMs: 1 })
    ).toBeNull();
  });

  it("不画点：已结束一票否决，压过还没落地的 working 状态与活跃轮次", () => {
    expect(
      agentGUIConversationPresence({
        status: "working",
        activeTurn: { turnId: "turn-1" } as never,
        endedAtUnixMs: 1_700_000_000_000
      })
    ).toBeNull();
  });
});
