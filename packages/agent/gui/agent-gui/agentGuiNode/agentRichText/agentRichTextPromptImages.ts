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

const IMAGE_FILE_NAME = /\.(png|jpe?g|webp|gif|tiff?|bmp|heic|heif|avif)$/i;
const GENERIC_CLIPBOARD_IMAGE_NAME =
  /^(image|clipboard-image|blob|pasted-image)(\.[a-z0-9]+)?$/i;

export interface ClipboardPromptImageDelivery {
  externalFilesSupported: boolean;
  promptImagesSupported: boolean;
  onFiles: (files: readonly File[]) => void;
  onImages: (images: AgentRichTextPromptImage[]) => void;
  onUnsupported: () => void;
}

interface ClipboardFileSource {
  type: string;
  file: File | null;
  materialize: () => File | null;
}

export function imageFilesFromDataTransfer(
  dataTransfer: DataTransfer | null
): File[] {
  if (!dataTransfer) {
    return [];
  }
  const items = fileItems(dataTransfer);
  if (!items) {
    return Array.from(dataTransfer.files ?? []).filter(isPromptImageFile);
  }
  const files: File[] = [];
  for (const item of items) {
    if (!isDeferredImageItemType(item.type)) {
      continue;
    }
    const file = item.getAsFile();
    if (file && isClipboardImageFile(file)) {
      files.push(file);
    }
  }
  if (files.length === 0) {
    return Array.from(dataTransfer.files ?? []).filter(isPromptImageFile);
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
    if (!file || isClipboardImageFile(file)) {
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
    .filter(isClipboardImageFile)
    .map((file) => stagePromptImage(file));
}

/**
 * Publishes a composer chip before clipboard bytes are read.
 *
 * Item kind and type are enough for the placeholder. `getAsFile()` and
 * base64 stay on a later task so a large or unlabeled paste can paint first.
 * When a provider image type is present, extra representations of the same
 * paste (often TIFF beside PNG) are ignored.
 */
export function deliverClipboardPromptImages(
  dataTransfer: DataTransfer | null,
  options: ClipboardPromptImageDelivery
): boolean {
  if (!dataTransfer) {
    return false;
  }
  const sources = clipboardFileSources(dataTransfer);
  if (sources.length === 0) {
    return false;
  }
  const imageSources = preferProviderImageSources(
    sources.filter(sourceMightBeImage)
  );
  const regularSources = sources.filter(
    (source) => !sourceMightBeImage(source)
  );
  if (!options.promptImagesSupported) {
    return deliverImagesAsFiles(imageSources, regularSources, options);
  }
  const regularFiles = options.externalFilesSupported
    ? materializeFiles(regularSources)
    : [];
  if (regularFiles.length > 0) {
    options.onFiles(regularFiles);
  }
  const immediate = imageSources.filter((source) => source.file);
  const deferred = imageSources.filter((source) => !source.file);
  const staged = immediate.map((source) => stagePromptImage(source.file!));
  if (deferred.length === 0) {
    if (staged.length > 0) {
      options.onImages(staged);
    }
    return staged.length > 0 || regularFiles.length > 0;
  }
  const placeholders = deferred.map((source) =>
    placeholderPromptImage(source.type)
  );
  options.onImages([...staged, ...placeholders]);
  deferUntilNextTask(() => {
    resolveDeferredPromptImages(deferred, placeholders, options);
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

function clipboardFileSources(
  dataTransfer: DataTransfer
): ClipboardFileSource[] {
  const items = fileItems(dataTransfer);
  if (items && items.length > 0) {
    return items.map((item) => ({
      type: item.type,
      file: null,
      materialize: () => item.getAsFile()
    }));
  }
  return Array.from(dataTransfer.files ?? []).map((file) => ({
    type: file.type,
    file,
    materialize: () => file
  }));
}

function sourceMightBeImage(source: ClipboardFileSource): boolean {
  if (isPromptImageItemType(source.type)) {
    return true;
  }
  if (source.file) {
    return isClipboardImageFile(source.file);
  }
  const type = source.type.trim().toLowerCase();
  return type === "" || type === "application/octet-stream";
}

function preferProviderImageSources(
  sources: ClipboardFileSource[]
): ClipboardFileSource[] {
  const providerSources = sources.filter(
    (source) =>
      supportedPromptImageMimeType(source.type) ||
      (source.file !== null && supportedPromptImageMimeType(source.file.type))
  );
  return providerSources.length > 0 ? providerSources : sources;
}

function deliverImagesAsFiles(
  imageSources: readonly ClipboardFileSource[],
  regularSources: readonly ClipboardFileSource[],
  options: ClipboardPromptImageDelivery
): boolean {
  if (!options.externalFilesSupported) {
    if (imageSources.length > 0) {
      options.onUnsupported();
    }
    return imageSources.length > 0;
  }
  const files = materializeFiles([...regularSources, ...imageSources]);
  if (files.length > 0) {
    options.onFiles(files);
  }
  return files.length > 0 || imageSources.length > 0;
}

function resolveDeferredPromptImages(
  sources: readonly ClipboardFileSource[],
  placeholders: readonly AgentRichTextPromptImage[],
  options: ClipboardPromptImageDelivery
): void {
  const images: AgentRichTextPromptImage[] = [];
  const rejectedFiles: File[] = [];
  sources.forEach((source, index) => {
    const id = placeholders[index]?.id ?? newPasteImageId();
    let file: File | null = null;
    try {
      file = source.materialize();
    } catch {
      images.push({
        id,
        name: "clipboard-image",
        mimeType: "image/png",
        data: "",
        readError: "This image could not be read."
      });
      return;
    }
    if (!file || !isClipboardImageFile(file)) {
      images.push({
        id,
        name: "",
        mimeType: "image/png",
        data: "",
        reject: true
      });
      if (file) {
        rejectedFiles.push(file);
      }
      return;
    }
    images.push(stagePromptImage(file, id));
  });
  if (images.length > 0) {
    options.onImages(images);
  }
  if (rejectedFiles.length > 0 && options.externalFilesSupported) {
    options.onFiles(rejectedFiles);
  }
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

function isPromptImageFile(file: File): boolean {
  return (
    isPromptImageItemType(file.type) || IMAGE_FILE_NAME.test(file.name.trim())
  );
}

function isClipboardImageFile(file: File): boolean {
  if (file.type.trim().toLowerCase() === "image/svg+xml") {
    return false;
  }
  if (isPromptImageFile(file)) {
    return true;
  }
  const type = file.type.trim().toLowerCase();
  if (type && type !== "application/octet-stream") {
    return false;
  }
  const name = file.name.trim();
  return name === "" || GENERIC_CLIPBOARD_IMAGE_NAME.test(name);
}

function materializeFiles(sources: readonly ClipboardFileSource[]): File[] {
  const files: File[] = [];
  for (const source of sources) {
    const file = source.materialize();
    if (file) {
      files.push(file);
    }
  }
  return files;
}

function deferUntilNextTask(work: () => void): void {
  // The chip must paint before clipboard bytes are read. A timer would raise
  // the AgentGUI timer ratchet, so this queues one macrotask instead.
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
