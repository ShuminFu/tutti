import type { AgentRichTextPastedImage } from "../agentRichText/AgentRichTextEditor";
import type { AgentComposerDraftImage } from "../model/agentGuiNodeTypes";

export interface PastedComposerImageJob {
  id: string;
  name: string;
  mimeType: AgentComposerDraftImage["mimeType"];
  data: string;
  sourceFile?: File;
  upload: boolean;
}

export function mergePastedComposerImages(input: {
  current: readonly AgentComposerDraftImage[];
  incoming: readonly AgentRichTextPastedImage[];
  remainingSlots: number;
  removedIds?: ReadonlySet<string>;
  uploadEnabled: boolean;
}): {
  addedCount: number;
  changed: boolean;
  images: AgentComposerDraftImage[];
  jobs: PastedComposerImageJob[];
  revokedPreviewUrls: string[];
} {
  const images = input.current.slice();
  const jobs: PastedComposerImageJob[] = [];
  const revokedPreviewUrls: string[] = [];
  let addedCount = 0;
  let changed = false;
  let slots = input.remainingSlots;

  for (const image of input.incoming) {
    if (image.id && input.removedIds?.has(image.id)) {
      continue;
    }
    const index = image.id
      ? images.findIndex((item) => item.id === image.id)
      : -1;
    if (image.reject) {
      if (index >= 0) {
        revokedPreviewUrls.push(images[index]!.previewUrl);
        images.splice(index, 1);
        changed = true;
      }
      continue;
    }
    if (image.readError) {
      if (index >= 0) {
        images[index] = {
          ...images[index]!,
          uploading: false,
          uploadError: image.readError
        };
        changed = true;
      }
      continue;
    }
    const hasBytes = image.data.trim() !== "";
    const previewUrl = pastedPreviewUrl(image, hasBytes);
    const uploading =
      Boolean(image.sourceFile) || (hasBytes ? input.uploadEnabled : true);
    if (index >= 0) {
      const existing = images[index]!;
      const nextPreview = previewUrl || existing.previewUrl;
      if (
        existing.previewUrl.startsWith("blob:") &&
        nextPreview !== existing.previewUrl
      ) {
        revokedPreviewUrls.push(existing.previewUrl);
      }
      const next = draftImageFromPaste(existing.id, image, {
        attachmentId: existing.attachmentId,
        data: hasBytes ? image.data : existing.data,
        path: existing.path,
        previewUrl: nextPreview,
        uploading,
        url: existing.url
      });
      images[index] = next;
      changed = true;
      pushPastedImageJob(jobs, next, image, input.uploadEnabled);
      continue;
    }
    if (slots <= 0) {
      continue;
    }
    const draft = draftImageFromPaste(image.id ?? newDraftImageId(), image, {
      data: hasBytes ? image.data : undefined,
      previewUrl,
      uploading
    });
    images.push(draft);
    slots -= 1;
    addedCount += 1;
    changed = true;
    pushPastedImageJob(jobs, draft, image, input.uploadEnabled);
  }

  return { addedCount, changed, images, jobs, revokedPreviewUrls };
}

export function revokeComposerImagePreviewUrl(
  previewUrl: string | undefined
): void {
  if (!previewUrl?.startsWith("blob:")) {
    return;
  }
  try {
    URL.revokeObjectURL(previewUrl);
  } catch {
    return;
  }
}

function pastedPreviewUrl(
  image: AgentRichTextPastedImage,
  hasBytes: boolean
): string {
  if (image.previewUrl) {
    return image.previewUrl;
  }
  if (!hasBytes) {
    return "";
  }
  return image.data.startsWith("data:")
    ? image.data
    : `data:${image.mimeType};base64,${image.data}`;
}

function draftImageFromPaste(
  id: string,
  image: AgentRichTextPastedImage,
  fields: {
    attachmentId?: string;
    data?: string;
    path?: string;
    previewUrl: string;
    uploading: boolean;
    url?: string;
  }
): AgentComposerDraftImage {
  return {
    id,
    name: image.name.trim() || "clipboard-image",
    mimeType: image.mimeType,
    ...(fields.attachmentId ? { attachmentId: fields.attachmentId } : {}),
    ...(fields.data ? { data: fields.data } : {}),
    ...(fields.url ? { url: fields.url } : {}),
    ...(fields.path ? { path: fields.path } : {}),
    previewUrl: fields.previewUrl,
    uploading: fields.uploading
  };
}

function pushPastedImageJob(
  jobs: PastedComposerImageJob[],
  draft: AgentComposerDraftImage,
  image: AgentRichTextPastedImage,
  uploadEnabled: boolean
): void {
  if (!image.sourceFile && !(image.data.trim() && uploadEnabled)) {
    return;
  }
  jobs.push({
    id: draft.id,
    name: draft.name,
    mimeType: draft.mimeType,
    data: image.data,
    ...(image.sourceFile ? { sourceFile: image.sourceFile } : {}),
    upload: uploadEnabled
  });
}

function newDraftImageId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}
