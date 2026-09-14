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
 * Settles `promise` against a deadline so callers always leave an unsettled
 * state. The original promise keeps running, but its late result is ignored
 * once the timeout has already reported a failure.
 */
export function settleWithTimeout<T>(
  promise: Promise<T>,
  options: {
    message?: string;
    timeoutMs?: number;
  } = {}
): Promise<T> {
  const timeoutMs = options.timeoutMs ?? AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MS;
  return new Promise<T>((resolve, reject) => {
    // timing: bound an unsettled upload so a stuck transfer fails visibly
    const timer = setTimeout(() => {
      reject(
        new Error(
          options.message ?? AGENT_COMPOSER_ASSET_UPLOAD_TIMEOUT_MESSAGE
        )
      );
    }, timeoutMs);
    promise.then(
      (value) => {
        clearTimeout(timer);
        resolve(value);
      },
      (error: unknown) => {
        clearTimeout(timer);
        reject(error);
      }
    );
  });
}
