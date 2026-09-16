import { describe, expect, it } from "vitest";
import { waitForAgentSubmitSettlement } from "./agentSubmitSettlement";

// 分栏结对模式（peer-pair-mode 票 05）：「发送被接受之后才给搭档投开工卡」靠这个判据。

type Status = "requested" | "accepted" | "confirmed" | "failed" | "uncertain";

function fakeEngine() {
  let submits: Record<string, { agentSessionId: string; clientSubmitId: string; status: Status }> = {};
  let queued: { clientSubmitId: string; visibleInQueue?: boolean }[] = [];
  const listeners = new Set<(state: unknown) => void>();
  const state = () => ({
    pendingIntents: { submitsByClientSubmitId: submits },
    promptQueue: {
      recordsBySessionId: { "session-a": { prompts: queued } }
    }
  });
  return {
    engine: {
      getSnapshot: () => state() as never,
      subscribe: (listener: (state: never) => void) => {
        listeners.add(listener as (state: unknown) => void);
        return () => listeners.delete(listener as (state: unknown) => void);
      }
    },
    listenerCount: () => listeners.size,
    set(status: Status | null, options: { queued?: boolean } = {}) {
      submits =
        status === null
          ? {}
          : { c1: { agentSessionId: "session-a", clientSubmitId: "c1", status } };
      queued = options.queued ? [{ clientSubmitId: "c1" }] : [];
      for (const listener of [...listeners]) listener(state());
    }
  };
}

describe("waitForAgentSubmitSettlement", () => {
  it("resolves true once the engine accepts, and unsubscribes", async () => {
    const fake = fakeEngine();
    fake.set("requested");
    const settled = waitForAgentSubmitSettlement(fake.engine as never, "session-a", "c1");
    fake.set("uncertain");
    fake.set("accepted");
    await expect(settled).resolves.toBe(true);
    expect(fake.listenerCount()).toBe(0);
  });

  it("resolves false when the submit fails and is not visibly queued", async () => {
    const fake = fakeEngine();
    fake.set("requested");
    const settled = waitForAgentSubmitSettlement(fake.engine as never, "session-a", "c1");
    fake.set("failed", { queued: true });
    fake.set("failed");
    await expect(settled).resolves.toBe(false);
  });

  it("resolves false when the record disappears before acceptance (dismissed / canceled)", async () => {
    const fake = fakeEngine();
    fake.set("requested");
    const settled = waitForAgentSubmitSettlement(fake.engine as never, "session-a", "c1");
    fake.set(null);
    await expect(settled).resolves.toBe(false);
  });

  it("gives up after the max wait", async () => {
    const fake = fakeEngine();
    fake.set("requested");
    await expect(
      waitForAgentSubmitSettlement(fake.engine as never, "session-a", "c1", 5)
    ).resolves.toBe(false);
    expect(fake.listenerCount()).toBe(0);
  });
});
