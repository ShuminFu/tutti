import { afterEach, describe, expect, it, vi } from "vitest";
import { settleWithTimeout } from "./composerAssetUploadTimeout";

describe("settleWithTimeout", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("resolves with the underlying value before the deadline", async () => {
    await expect(
      settleWithTimeout(Promise.resolve("ok"), { timeoutMs: 1_000 })
    ).resolves.toBe("ok");
  });

  it("propagates the underlying rejection", async () => {
    await expect(
      settleWithTimeout(Promise.reject(new Error("upload failed")), {
        timeoutMs: 1_000
      })
    ).rejects.toThrow("upload failed");
  });

  it("rejects when the upload never settles", async () => {
    vi.useFakeTimers();
    const pending = settleWithTimeout(new Promise<never>(() => {}), {
      timeoutMs: 50
    });
    const assertion = expect(pending).rejects.toThrow(
      "Prompt asset upload timed out."
    );
    await vi.advanceTimersByTimeAsync(60);
    await assertion;
  });

  it("ignores a late result after the timeout already failed", async () => {
    vi.useFakeTimers();
    let resolveLate!: (value: string) => void;
    const pending = settleWithTimeout(
      new Promise<string>((resolve) => {
        resolveLate = resolve;
      }),
      { timeoutMs: 50 }
    );
    const assertion = expect(pending).rejects.toThrow(
      "Prompt asset upload timed out."
    );
    await vi.advanceTimersByTimeAsync(60);
    await assertion;
    // The abandoned upload resolving afterwards must not resurrect the state.
    resolveLate("late");
    await vi.advanceTimersByTimeAsync(0);
  });
});
