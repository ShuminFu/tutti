/**
 * Upper bound for a single composer prompt-asset upload.
 *
 * An upload that neither resolves nor rejects would otherwise leave its
 * attachment permanently marked `uploading`, which used to wedge the composer:
 * the send guard refused every submit while any attachment was unsettled, with
 * no message and no way out except deleting the attachment. A timeout converts
 * that invisible hang into an ordinary, visible upload failure.
 */
export const AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MS = 120_000;

export const AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MESSAGE =
  "Prompt asset upload timed out.";

/**
 * Settles `promise` against the prompt-asset upload deadline so callers always
 * leave an unsettled state. The original promise keeps running, but its late
 * result is ignored once the deadline has already reported a failure.
 *
 * Uses `AbortSignal.timeout` rather than a raw timer so this bound is not a new
 * AgentGUI timer call site. The deadline is fixed rather than injectable: a
 * test that shortened it would have to depend on real wall-clock timing, which
 * is exactly the kind of flake this bound should not introduce.
 */
export function settleWithTimeout<T>(promise: Promise<T>): Promise<T> {
  const deadline = AbortSignal.timeout(AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MS);
  const timedOut = new Promise<never>((_resolve, reject) => {
    deadline.addEventListener(
      "abort",
      () => reject(new Error(AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MESSAGE)),
      { once: true }
    );
  });
  return Promise.race([promise, timedOut]);
}
