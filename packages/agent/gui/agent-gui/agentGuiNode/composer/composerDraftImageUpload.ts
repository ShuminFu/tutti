import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { encodeAgentRichTextPromptImage } from "../agentRichText/agentRichTextPromptImages";
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
import {
  revokeComposerImagePreviewUrl,
  type PastedComposerImageJob
} from "./composerPastedImagePreview";

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
    })
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

/**
 * Encodes a pasted image after its preview is already on screen, then uploads
 * when the host can archive it. The base64 payload is not copied onto the
 * draft while an upload is in flight.
 */
export function settlePastedComposerImage(input: {
  job: PastedComposerImageJob;
  runtime: AgentGUIRuntime | null;
  updateScopedDraft: (
    update: (current: AgentComposerDraft) => AgentComposerDraft
  ) => AgentComposerDraft | null;
  uploadPromptContent:
    | NonNullable<AgentGUIRuntime["uploadPromptContent"]>
    | undefined;
  workspaceId: string;
}): void {
  void encodePastedComposerImage(input.job)
    .then((encoded) => {
      if (!encoded) {
        input.updateScopedDraft((draft) =>
          markImageUploadFailed(
            draft,
            input.job.id,
            "This image could not be read."
          )
        );
        return;
      }
      input.updateScopedDraft((draft) => {
        const current = agentComposerDraftImages(draft).find(
          (image) => image.id === input.job.id
        );
        if (
          current &&
          encoded.previewUrl &&
          current.previewUrl !== encoded.previewUrl
        ) {
          revokeComposerImagePreviewUrl(current.previewUrl);
        }
        return updateAgentComposerDraft(draft, {
          images: agentComposerDraftImages(draft).map((image) =>
            image.id === input.job.id
              ? {
                  ...image,
                  mimeType: encoded.mimeType,
                  ...(encoded.previewUrl
                    ? { previewUrl: encoded.previewUrl }
                    : {}),
                  ...(input.job.upload ? {} : { data: encoded.data }),
                  uploading: input.job.upload
                }
              : image
          )
        });
      });
      if (!input.job.upload || !input.uploadPromptContent) {
        return;
      }
      uploadComposerDraftImage({
        draftImage: {
          id: input.job.id,
          name: input.job.name,
          mimeType: encoded.mimeType,
          data: encoded.data,
          previewUrl: "",
          uploading: true
        },
        runtime: input.runtime,
        updateScopedDraft: input.updateScopedDraft,
        uploadPromptContent: input.uploadPromptContent,
        workspaceId: input.workspaceId
      });
    })
    .catch((error: unknown) => {
      const message = error instanceof Error ? error.message : String(error);
      input.updateScopedDraft((draft) =>
        markImageUploadFailed(draft, input.job.id, message)
      );
    });
}

async function encodePastedComposerImage(job: PastedComposerImageJob): Promise<{
  mimeType: PastedComposerImageJob["mimeType"];
  data: string;
  previewUrl?: string;
} | null> {
  if (job.data.trim()) {
    return { mimeType: job.mimeType, data: job.data };
  }
  if (!job.sourceFile) {
    return null;
  }
  return encodeAgentRichTextPromptImage(job.sourceFile);
}
