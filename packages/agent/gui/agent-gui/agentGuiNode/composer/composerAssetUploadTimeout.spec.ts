import { describe, expect, it } from "vitest";
import {
  AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MESSAGE,
  settleWithTimeout
} from "./composerAssetUploadTimeout";

describe("settleWithTimeout", () => {
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
    await expect(
      settleWithTimeout(new Promise<never>(() => {}), { timeoutMs: 5 })
    ).rejects.toThrow(AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MESSAGE);
  });

  it("rejects with a caller-provided message", async () => {
    await expect(
      settleWithTimeout(new Promise<never>(() => {}), {
        message: "custom timeout",
        timeoutMs: 5
      })
    ).rejects.toThrow("custom timeout");
  });

  it("ignores a late result after the timeout already failed", async () => {
    let resolveLate!: (value: string) => void;
    const pending = settleWithTimeout(
      new Promise<string>((resolve) => {
        resolveLate = resolve;
      }),
      { timeoutMs: 5 }
    );
    await expect(pending).rejects.toThrow(
      AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MESSAGE
    );
    // The abandoned upload resolving afterwards must not resurrect the state.
    resolveLate("late");
  });
});
