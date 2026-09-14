import type {
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText
} from "../model/agentGuiNodeTypes";

/**
 * Readiness summary for the attachments currently held by a composer draft.
 *
 * The submit projection already drops attachments that are still uploading or
 * that failed, so an unavailable attachment is not a reason to refuse the whole
 * send. Callers use this summary to send the ready remainder and to explain
 * what was left out, instead of silently ignoring the submit.
 */
export interface ComposerDraftUnavailableAttachments {
  /** At least one attachment is still uploading or has failed. */
  hasUnavailable: boolean;
  /** Attachments that finished with an error. */
  failedCount: number;
  /** Attachments whose upload has not settled yet. */
  uploadingCount: number;
}

/**
 * Counts unsettled and failed draft attachments across every attachment kind.
 *
 * This is a pure projection over draft membership: it reads no refs, owns no
 * state, and must stay consistent with the filters applied by
 * `projectAgentComposerDraftSubmission`.
 */
export function selectComposerDraftUnavailableAttachments(input: {
  images?: readonly AgentComposerDraftImage[];
  files?: readonly AgentComposerDraftFile[];
  largeTexts?: readonly AgentComposerDraftLargeText[];
}): ComposerDraftUnavailableAttachments {
  const all = [
    ...(input.images ?? []),
    ...(input.files ?? []),
    ...(input.largeTexts ?? [])
  ];
  let uploadingCount = 0;
  let failedCount = 0;
  for (const attachment of all) {
    if (attachment.uploadError) {
      failedCount += 1;
      continue;
    }
    if (attachment.uploading) {
      uploadingCount += 1;
    }
  }
  return {
    failedCount,
    hasUnavailable: uploadingCount > 0 || failedCount > 0,
    uploadingCount
  };
}
