export interface AgentRichTextPromptImage {
  id?: string;
  name: string;
  mimeType: "image/png" | "image/jpeg" | "image/webp";
  data: string;
  previewUrl?: string;
  /** Held only until the draft encodes bytes. Never stored on the draft. */
  sourceFile?: File;
  reject?: boolean;
  readError?: string;
}

const GENERIC_CLIPBOARD_IMAGE_NAME =
  /^(image|clipboard-image|blob|pasted-image)(\.[a-z0-9]+)?$/i;

export interface ClipboardPromptImageDelivery {
  externalFilesSupported: boolean;
  promptImagesSupported: boolean;
  onFiles: (files: readonly File[]) => void;
  onImages: (images: AgentRichTextPromptImage[]) => void;
  onUnsupported: () => void;
  onFileUnavailable: () => void;
}

interface ClipboardFileSource {
  type: string;
  file: File;
}

export function imageFilesFromDataTransfer(
  dataTransfer: DataTransfer | null
): File[] {
  if (!dataTransfer) {
    return [];
  }
  const items = fileItems(dataTransfer);
  if (!items) {
    return Array.from(dataTransfer.files ?? []).filter((file) =>
      isClipboardImageFile(file)
    );
  }
  const files: File[] = [];
  for (const item of items) {
    if (!isDeferredImageItemType(item.type)) {
      continue;
    }
    const file = item.getAsFile();
    if (file && isClipboardImageFile(file, item.type)) {
      files.push(file);
    }
  }
  if (files.length === 0) {
    return Array.from(dataTransfer.files ?? []).filter((file) =>
      isClipboardImageFile(file)
    );
  }
  return files;
}

export function nonImageFilesFromDataTransfer(
  dataTransfer: DataTransfer | null
): File[] {
  if (!dataTransfer) {
    return [];
  }
  const items = fileItems(dataTransfer);
  if (!items) {
    return Array.from(dataTransfer.files ?? []).filter(
      (file) => !isClipboardImageFile(file)
    );
  }
  const files: File[] = [];
  for (const item of items) {
    if (isPromptImageItemType(item.type)) {
      continue;
    }
    const file = item.getAsFile();
    if (!file || isClipboardImageFile(file, item.type)) {
      continue;
    }
    files.push(file);
  }
  if (files.length === 0) {
    return Array.from(dataTransfer.files ?? []).filter(
      (file) => !isClipboardImageFile(file)
    );
  }
  return files;
}

export function classifyAgentRichTextExternalFiles(
  dataTransfer: DataTransfer | null
): { imageFiles: File[]; regularFiles: File[] } {
  const imageFiles = imageFilesFromDataTransfer(dataTransfer);
  const imageFileSet = new Set(imageFiles);
  return {
    imageFiles,
    regularFiles: nonImageFilesFromDataTransfer(dataTransfer).filter(
      (file) => !imageFileSet.has(file)
    )
  };
}

export function routeAgentRichTextExternalFiles(
  dataTransfer: DataTransfer | null,
  options: {
    externalFilesSupported: boolean;
    promptImagesSupported: boolean;
  }
): {
  externalFiles: File[];
  imageFiles: File[];
  imagesHandledAsFiles: boolean;
} {
  const { imageFiles, regularFiles } =
    classifyAgentRichTextExternalFiles(dataTransfer);
  const imagesHandledAsFiles =
    imageFiles.length > 0 &&
    !options.promptImagesSupported &&
    options.externalFilesSupported;
  return {
    externalFiles: options.externalFilesSupported
      ? [...regularFiles, ...(imagesHandledAsFiles ? imageFiles : [])]
      : [],
    imageFiles,
    imagesHandledAsFiles
  };
}

export function systemFileDragInfoFromDataTransfer(
  dataTransfer: DataTransfer | null
): { hasImageFiles: boolean; hasRegularFiles: boolean } {
  if (!dataTransfer) {
    return { hasImageFiles: false, hasRegularFiles: false };
  }
  const items = fileItems(dataTransfer);
  if (items) {
    let hasImageFiles = false;
    let hasRegularFiles = false;
    for (const item of items) {
      if (isPromptImageItemType(item.type)) {
        hasImageFiles = true;
      } else {
        hasRegularFiles = true;
      }
    }
    return { hasImageFiles, hasRegularFiles };
  }
  let hasImageFiles = false;
  let hasRegularFiles = false;
  for (const file of Array.from(dataTransfer.files ?? [])) {
    if (isClipboardImageFile(file)) {
      hasImageFiles = true;
    } else {
      hasRegularFiles = true;
    }
  }
  return { hasImageFiles, hasRegularFiles };
}

export function supportedPromptImageMimeType(
  value: string
): value is AgentRichTextPromptImage["mimeType"] {
  return (
    value === "image/png" || value === "image/jpeg" || value === "image/webp"
  );
}

export function stageAgentRichTextPromptImages(
  files: readonly File[]
): AgentRichTextPromptImage[] {
  return files
    .filter((file) => isClipboardImageFile(file))
    .map((file) => stagePromptImage(file));
}

/**
 * Publishes a composer chip before clipboard bytes are read.
 *
 * Capture File handles while the trusted paste event is active. Placeholders
 * paint before preview creation, byte reads, and base64 encoding on later tasks.
 * Every File remains independent; clipboard metadata cannot prove that two
 * image representations describe the same OS file.
 */
export function deliverClipboardPromptImages(
  dataTransfer: DataTransfer | null,
  options: ClipboardPromptImageDelivery
): boolean {
  if (!dataTransfer) {
    return false;
  }
  const { sources, unavailable } = clipboardFileSources(dataTransfer);
  if (unavailable) {
    options.onFileUnavailable();
  }
  if (sources.length === 0) {
    return false;
  }
  const imageSources = sources.filter(sourceMightBeImage);
  const regularSources = sources.filter(
    (source) => !sourceMightBeImage(source)
  );
  if (!options.promptImagesSupported) {
    return deliverImagesAsFiles(sources, options);
  }
  const regularFiles = options.externalFilesSupported
    ? regularSources.map((source) => source.file)
    : [];
  if (!options.externalFilesSupported && regularSources.length > 0) {
    options.onFileUnavailable();
  }
  if (regularFiles.length > 0) {
    options.onFiles(regularFiles);
  }
  if (imageSources.length === 0) {
    return regularFiles.length > 0;
  }
  const placeholders = imageSources.map((source) =>
    placeholderPromptImage(source.type)
  );
  options.onImages(placeholders);
  deferUntilNextTask(() => {
    options.onImages(
      imageSources.map((source, index) =>
        stagePromptImage(source.file, placeholders[index]?.id)
      )
    );
  });
  return true;
}

export async function encodeAgentRichTextPromptImage(file: Blob): Promise<{
  mimeType: AgentRichTextPromptImage["mimeType"];
  data: string;
  previewUrl?: string;
} | null> {
  const declared = supportedPromptImageMimeType(file.type) ? file.type : null;
  if (declared) {
    const data = await blobToBase64(file);
    return data ? { mimeType: declared, data } : null;
  }
  const header = new Uint8Array(await file.slice(0, 16).arrayBuffer());
  const sniffed = sniffedProviderMime(header);
  if (sniffed) {
    const data = await blobToBase64(file);
    return data ? { mimeType: sniffed, data } : null;
  }
  return transcodeImageBlobToPng(file);
}

function fileItems(dataTransfer: DataTransfer): DataTransferItem[] | null {
  const items = (dataTransfer as { items?: DataTransferItemList | null }).items;
  if (!items) {
    return null;
  }
  return Array.from(items).filter((item) => item.kind === "file");
}

function clipboardFileSources(dataTransfer: DataTransfer): {
  sources: ClipboardFileSource[];
  unavailable: boolean;
} {
  const items = fileItems(dataTransfer);
  const transferFiles = Array.from(dataTransfer.files ?? []);
  if (items && items.length > 0) {
    let unavailable = false;
    const sources = items.flatMap((item, index) => {
      let file: File | null = null;
      try {
        file = item.getAsFile();
      } catch {
        file = transferFiles[index] ?? null;
      }
      file ??= transferFiles[index] ?? null;
      if (!file) unavailable = true;
      return file ? [{ type: item.type, file }] : [];
    });
    return { sources, unavailable };
  }
  return {
    sources: transferFiles.map((file) => ({ type: file.type, file })),
    unavailable: false
  };
}

function sourceMightBeImage(source: ClipboardFileSource): boolean {
  return isClipboardImageFile(source.file, source.type);
}

function deliverImagesAsFiles(
  sources: readonly ClipboardFileSource[],
  options: ClipboardPromptImageDelivery
): boolean {
  if (!options.externalFilesSupported) {
    if (sources.some(sourceMightBeImage)) {
      options.onUnsupported();
    }
    if (sources.some((source) => !sourceMightBeImage(source))) {
      options.onFileUnavailable();
    }
    return sources.length > 0;
  }
  const files = sources.map((source) => source.file);
  if (files.length > 0) {
    options.onFiles(files);
  }
  return files.length > 0;
}

function stagePromptImage(
  file: File,
  id = newPasteImageId()
): AgentRichTextPromptImage {
  return {
    id,
    name: file.name.trim() || "clipboard-image",
    mimeType: supportedPromptImageMimeType(file.type) ? file.type : "image/png",
    data: "",
    previewUrl: previewObjectUrl(file),
    sourceFile: file
  };
}

function placeholderPromptImage(type: string): AgentRichTextPromptImage {
  return {
    id: newPasteImageId(),
    name: "clipboard-image",
    mimeType: supportedPromptImageMimeType(type) ? type : "image/png",
    data: "",
    previewUrl: ""
  };
}

function previewObjectUrl(file: Blob): string {
  try {
    return URL.createObjectURL(file);
  } catch {
    return "";
  }
}

function newPasteImageId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function isPromptImageItemType(value: string): boolean {
  const type = value.trim().toLowerCase();
  return type.startsWith("image/") && type !== "image/svg+xml";
}

function isDeferredImageItemType(value: string): boolean {
  const type = value.trim().toLowerCase();
  return (
    isPromptImageItemType(type) ||
    type === "" ||
    type === "application/octet-stream"
  );
}

function isClipboardImageFile(file: File, itemType = file.type): boolean {
  const type = itemType.trim().toLowerCase();
  if (type === "image/svg+xml") {
    return false;
  }
  if (isPromptImageItemType(type)) {
    return true;
  }
  if (type && type !== "application/octet-stream") {
    return false;
  }
  const fileType = file.type.trim().toLowerCase();
  if (isPromptImageItemType(fileType)) {
    return true;
  }
  if (fileType && fileType !== "application/octet-stream") {
    return false;
  }
  const name = file.name.trim();
  return name === "" || GENERIC_CLIPBOARD_IMAGE_NAME.test(name);
}

function deferUntilNextTask(work: () => void): void {
  // The chip must paint before preview work. A timer would raise the AgentGUI
  // timer ratchet, so this queues one macrotask instead.
  if (typeof MessageChannel !== "function") {
    work();
    return;
  }
  const channel = new MessageChannel();
  channel.port1.onmessage = () => {
    channel.port1.onmessage = null;
    channel.port1.close();
    channel.port2.close();
    work();
  };
  channel.port2.postMessage(null);
}

function sniffedProviderMime(
  bytes: Uint8Array
): AgentRichTextPromptImage["mimeType"] | null {
  if (
    bytes.length >= 8 &&
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47
  ) {
    return "image/png";
  }
  if (
    bytes.length >= 3 &&
    bytes[0] === 0xff &&
    bytes[1] === 0xd8 &&
    bytes[2] === 0xff
  ) {
    return "image/jpeg";
  }
  if (
    bytes.length >= 12 &&
    bytes[0] === 0x52 &&
    bytes[1] === 0x49 &&
    bytes[2] === 0x46 &&
    bytes[3] === 0x46 &&
    bytes[8] === 0x57 &&
    bytes[9] === 0x45 &&
    bytes[10] === 0x42 &&
    bytes[11] === 0x50
  ) {
    return "image/webp";
  }
  return null;
}

async function blobToBase64(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await blob.arrayBuffer());
  if (bytes.byteLength === 0) {
    return "";
  }
  const chunkSize = 0x8000;
  const parts: string[] = [];
  for (let index = 0; index < bytes.length; index += chunkSize) {
    const slice = bytes.subarray(index, index + chunkSize);
    parts.push(String.fromCharCode(...(slice as unknown as number[])));
  }
  return btoa(parts.join(""));
}

async function transcodeImageBlobToPng(file: Blob): Promise<{
  mimeType: "image/png";
  data: string;
  previewUrl?: string;
} | null> {
  if (typeof createImageBitmap !== "function") {
    return null;
  }
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    return null;
  }
  try {
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    const context = canvas.getContext("2d");
    if (!context) {
      return null;
    }
    context.drawImage(bitmap, 0, 0);
    const blob = await new Promise<Blob | null>((resolve) => {
      canvas.toBlob((value) => resolve(value), "image/png");
    });
    if (!blob) {
      return null;
    }
    const data = await blobToBase64(blob);
    if (!data) {
      return null;
    }
    const previewUrl = previewObjectUrl(blob);
    return previewUrl
      ? { mimeType: "image/png", data, previewUrl }
      : { mimeType: "image/png", data };
  } finally {
    bitmap.close();
  }
}
