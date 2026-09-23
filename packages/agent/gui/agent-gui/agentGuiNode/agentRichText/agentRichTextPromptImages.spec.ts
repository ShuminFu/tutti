import { afterEach, describe, expect, it, vi } from "vitest";
import {
  classifyAgentRichTextExternalFiles,
  deliverClipboardPromptImages,
  encodeAgentRichTextPromptImage,
  type AgentRichTextPromptImage
} from "./agentRichTextPromptImages";

describe("classifyAgentRichTextExternalFiles", () => {
  it("keeps supported images and regular files from one clipboard transfer", () => {
    const image = new File(["image"], "screen.png", { type: "image/png" });
    const document = new File(["document"], "notes.md", {
      type: "text/markdown"
    });
    const dataTransfer = {
      files: [image, document]
    } as unknown as DataTransfer;

    expect(classifyAgentRichTextExternalFiles(dataTransfer)).toEqual({
      imageFiles: [image],
      regularFiles: [document]
    });
  });

  it("treats an unlabeled image type as an image and leaves a named file alone", () => {
    const image = new File(["tiff"], "shot.tiff", { type: "image/tiff" });
    const notes = new File(["notes"], "notes.md", { type: "" });
    const dataTransfer = {
      files: [image, notes]
    } as unknown as DataTransfer;

    expect(classifyAgentRichTextExternalFiles(dataTransfer)).toEqual({
      imageFiles: [image],
      regularFiles: [notes]
    });
  });
});

describe("deliverClipboardPromptImages", () => {
  const urlGlobal = URL as unknown as {
    createObjectURL?: (blob: Blob) => string;
    revokeObjectURL?: (url: string) => void;
  };
  const previousCreate = urlGlobal.createObjectURL;
  const previousRevoke = urlGlobal.revokeObjectURL;

  afterEach(() => {
    if (previousCreate) {
      urlGlobal.createObjectURL = previousCreate;
    } else {
      delete urlGlobal.createObjectURL;
    }
    if (previousRevoke) {
      urlGlobal.revokeObjectURL = previousRevoke;
    } else {
      delete urlGlobal.revokeObjectURL;
    }
  });

  it("publishes a chip before reading clipboard bytes", async () => {
    urlGlobal.createObjectURL = () => "blob:preview";
    urlGlobal.revokeObjectURL = () => undefined;
    const file = new File(["png"], "shot.png", { type: "image/png" });
    const getAsFile = vi.fn(() => file);
    const calls: AgentRichTextPromptImage[][] = [];
    const handled = deliverClipboardPromptImages(
      {
        items: [{ kind: "file", type: "image/png", getAsFile }]
      } as unknown as DataTransfer,
      {
        externalFilesSupported: true,
        promptImagesSupported: true,
        onFiles: () => undefined,
        onImages: (images) => {
          calls.push(images);
        },
        onUnsupported: () => undefined
      }
    );

    expect(handled).toBe(true);
    expect(getAsFile).not.toHaveBeenCalled();
    expect(calls[0]?.[0]).toMatchObject({
      name: "clipboard-image",
      previewUrl: ""
    });

    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(getAsFile).toHaveBeenCalledOnce();
    expect(calls[1]?.[0]).toMatchObject({
      name: "shot.png",
      mimeType: "image/png",
      previewUrl: "blob:preview",
      data: ""
    });
  });

  it("ignores a tiff twin when the paste already has a png", async () => {
    urlGlobal.createObjectURL = () => "blob:preview";
    const png = new File(["png"], "shot.png", { type: "image/png" });
    const tiff = new File(["tiff"], "shot.tiff", { type: "image/tiff" });
    const readPng = vi.fn(() => png);
    const readTiff = vi.fn(() => tiff);
    deliverClipboardPromptImages(
      {
        items: [
          { kind: "file", type: "image/png", getAsFile: readPng },
          { kind: "file", type: "image/tiff", getAsFile: readTiff }
        ]
      } as unknown as DataTransfer,
      {
        externalFilesSupported: true,
        promptImagesSupported: true,
        onFiles: () => undefined,
        onImages: () => undefined,
        onUnsupported: () => undefined
      }
    );

    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(readPng).toHaveBeenCalledOnce();
    expect(readTiff).not.toHaveBeenCalled();
  });

  it("sends a named non-image back to the file path after the placeholder", async () => {
    const notes = new File(["hi"], "notes.md", { type: "" });
    const images: AgentRichTextPromptImage[][] = [];
    const files: File[][] = [];
    deliverClipboardPromptImages(
      {
        items: [{ kind: "file", type: "", getAsFile: () => notes }]
      } as unknown as DataTransfer,
      {
        externalFilesSupported: true,
        promptImagesSupported: true,
        onFiles: (next) => {
          files.push([...next]);
        },
        onImages: (next) => {
          images.push(next);
        },
        onUnsupported: () => undefined
      }
    );

    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(images[0]?.[0]?.previewUrl).toBe("");
    expect(images[1]?.[0]?.reject).toBe(true);
    expect(files[0]).toEqual([notes]);
  });
});

describe("encodeAgentRichTextPromptImage", () => {
  it("sniffs an unlabeled png without using the preview url as the payload", async () => {
    const bytes = Uint8Array.from([
      0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3
    ]);
    const encoded = await encodeAgentRichTextPromptImage(
      new File([bytes], "", { type: "" })
    );

    expect(encoded).toMatchObject({
      mimeType: "image/png",
      data: Buffer.from(bytes).toString("base64")
    });
    expect(encoded?.previewUrl).toBeUndefined();
  });
});
