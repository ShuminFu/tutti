import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import type {
  AgentComposerDraft,
  AgentComposerDraftImage
} from "../model/agentGuiNodeTypes";
import {
  agentComposerDraftImages,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import { reportAgentComposerDiagnostic } from "./agentComposerDiagnostics";
import { settleWithTimeout } from "./composerAssetUploadTimeout";

/**
 * Applies the provider-readable locator returned by an image upload onto the
 * matching draft image, clearing its in-flight state.
 *
 * The uploaded block must carry at least one usable reference; an upload that
 * reports success without one is treated as a failure so the image cannot sit
 * in the draft claiming to be ready when it is not.
 */
function applyUploadedImage(
  currentDraft: AgentComposerDraft,
  imageId: string,
  uploadedImage: AgentGUIRuntimeUploadedImageBlock
): AgentComposerDraft {
  return updateAgentComposerDraft(currentDraft, {
    images: agentComposerDraftImages(currentDraft).map((image) =>
      image.id === imageId
        ? {
            id: image.id,
            name: image.name,
            mimeType: image.mimeType,
            ...(uploadedImage.attachmentId
              ? { attachmentId: uploadedImage.attachmentId }
              : {}),
            ...(uploadedImage.url
              ? { url: uploadedImage.url }
              : uploadedImage.data
                ? { data: uploadedImage.data }
                : {}),
            ...(uploadedImage.path ? { path: uploadedImage.path } : {}),
            previewUrl: image.previewUrl,
            uploading: false
          }
        : image
    )
  });
}

function markImageUploadFailed(
  currentDraft: AgentComposerDraft,
  imageId: string,
  message: string
): AgentComposerDraft {
  return updateAgentComposerDraft(currentDraft, {
    images: agentComposerDraftImages(currentDraft).map((image) =>
      image.id === imageId
        ? { ...image, uploading: false, uploadError: message }
        : image
    )
  });
}

type AgentGUIRuntimeUploadedImageBlock = {
  attachmentId?: string;
  data?: string;
  path?: string;
  url?: string;
};

/**
 * Runs one draft image through the host upload and projects the settled result
 * back onto the draft.
 *
 * Every image settles exactly once: the upload is bounded by a timeout, and
 * both the success and failure paths clear `uploading`. A result whose draft
 * scope no longer exists is dropped by `updateScopedDraft`, which reports it
 * rather than failing silently.
 */
export function uploadComposerDraftImage(input: {
  draftImage: AgentComposerDraftImage;
  runtime: AgentGUIRuntime | null;
  timeoutMs?: number;
  updateScopedDraft: (
    update: (current: AgentComposerDraft) => AgentComposerDraft
  ) => AgentComposerDraft | null;
  uploadPromptContent: NonNullable<AgentGUIRuntime["uploadPromptContent"]>;
  workspaceId: string;
}): void {
  const { draftImage, runtime, updateScopedDraft, uploadPromptContent } = input;
  const workspaceId = input.workspaceId;
  void settleWithTimeout(
    uploadPromptContent({
      workspaceId,
      content: [
        {
          type: "image",
          mimeType: draftImage.mimeType,
          data: draftImage.data,
          name: draftImage.name
        }
      ]
    }),
    input.timeoutMs === undefined ? {} : { timeoutMs: input.timeoutMs }
  )
    .then((result) => {
      const uploadedImage = result.content.find(
        (block) => block.type === "image"
      );
      const uploadedUrl = uploadedImage?.url?.trim();
      reportAgentComposerDiagnostic(runtime, {
        details: {
          foundImageBlock: Boolean(uploadedImage),
          hasAttachmentId: Boolean(uploadedImage?.attachmentId?.trim()),
          hasData: Boolean(uploadedImage?.data?.trim()),
          hasPath: Boolean(uploadedImage?.path?.trim()),
          hasUrl: Boolean(uploadedUrl),
          imageId: draftImage.id
        },
        event: "agent.gui.composer.image_upload.resolved",
        level: "info",
        source: "agent-gui",
        workspaceId
      });
      if (
        !uploadedImage ||
        (!uploadedUrl &&
          !uploadedImage.attachmentId &&
          !uploadedImage.path &&
          !uploadedImage.data)
      ) {
        throw new Error(
          "Prompt image upload completed without usable image reference."
        );
      }
      updateScopedDraft((currentDraft) =>
        applyUploadedImage(currentDraft, draftImage.id, {
          ...(uploadedImage.attachmentId
            ? { attachmentId: uploadedImage.attachmentId }
            : {}),
          ...(uploadedUrl ? { url: uploadedUrl } : {}),
          ...(uploadedImage.path ? { path: uploadedImage.path } : {}),
          ...(!uploadedUrl && uploadedImage.data
            ? { data: uploadedImage.data }
            : {})
        })
      );
    })
    .catch((error: unknown) => {
      const message = error instanceof Error ? error.message : String(error);
      reportAgentComposerDiagnostic(runtime, {
        details: {
          error: message.slice(0, 500),
          imageId: draftImage.id
        },
        event: "agent.gui.composer.image_upload.failed",
        level: "warn",
        source: "agent-gui",
        workspaceId
      });
      updateScopedDraft((currentDraft) =>
        markImageUploadFailed(currentDraft, draftImage.id, message)
      );
    });
}
