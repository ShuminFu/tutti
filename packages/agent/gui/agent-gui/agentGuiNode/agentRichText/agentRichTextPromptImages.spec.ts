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
    const namedPng = new File(["file"], "diagram.png", { type: "" });
    const dataTransfer = {
      files: [image, notes, namedPng]
    } as unknown as DataTransfer;

    expect(classifyAgentRichTextExternalFiles(dataTransfer)).toEqual({
      imageFiles: [image],
      regularFiles: [notes, namedPng]
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
    const createObjectURL = vi.fn(() => "blob:preview");
    urlGlobal.createObjectURL = createObjectURL;
    urlGlobal.revokeObjectURL = () => undefined;
    const file = new File(["png"], "shot.png", { type: "image/png" });
    const readBytes = vi.spyOn(file, "arrayBuffer");
    const getAsFile = vi.fn(() => file);
    const calls: AgentRichTextPromptImage[][] = [];
    let resolveDeferred!: () => void;
    const deferred = new Promise<void>((resolve) => {
      resolveDeferred = resolve;
    });
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
          if (calls.length === 2) resolveDeferred();
        },
        onUnsupported: () => undefined,
        onFileUnavailable: () => undefined
      }
    );

    expect(handled).toBe(true);
    // The clipboard item is readable only during the trusted paste event.
    expect(getAsFile).toHaveBeenCalledOnce();
    expect(calls[0]?.[0]).toMatchObject({
      name: "clipboard-image",
      previewUrl: ""
    });
    expect(createObjectURL).not.toHaveBeenCalled();
    expect(readBytes).not.toHaveBeenCalled();

    await deferred;

    expect(getAsFile).toHaveBeenCalledOnce();
    expect(createObjectURL).toHaveBeenCalledOnce();
    expect(readBytes).not.toHaveBeenCalled();
    expect(calls[1]?.[0]).toMatchObject({
      name: "shot.png",
      mimeType: "image/png",
      previewUrl: "blob:preview",
      data: ""
    });
  });

  it("preserves both clipboard image representations when they are File objects", async () => {
    urlGlobal.createObjectURL = () => "blob:preview";
    const png = new File(["png"], "shot.png", { type: "image/png" });
    const tiff = new File(["tiff"], "shot.tiff", { type: "image/tiff" });
    const readPng = vi.fn(() => png);
    const readTiff = vi.fn(() => tiff);
    const images: AgentRichTextPromptImage[][] = [];
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
        onImages: (next) => images.push(next),
        onUnsupported: () => undefined,
        onFileUnavailable: () => undefined
      }
    );

    await vi.waitFor(() => expect(images[1]).toBeDefined());

    expect(readPng).toHaveBeenCalledOnce();
    expect(readTiff).toHaveBeenCalledOnce();
    expect(images[1]?.map((image) => image.sourceFile)).toEqual([png, tiff]);
  });

  it("keeps a distinct TIFF image beside a PNG", async () => {
    const png = new File(["png"], "screen.png", { type: "image/png" });
    const tiff = new File(["tiff"], "diagram.tiff", { type: "image/tiff" });
    const images: AgentRichTextPromptImage[][] = [];
    deliverClipboardPromptImages(
      {
        items: [png, tiff].map((file) => ({
          kind: "file",
          type: file.type,
          getAsFile: () => file
        }))
      } as unknown as DataTransfer,
      {
        externalFilesSupported: true,
        promptImagesSupported: true,
        onFiles: () => undefined,
        onImages: (next) => images.push(next),
        onUnsupported: () => undefined,
        onFileUnavailable: () => undefined
      }
    );
    await vi.waitFor(() => expect(images[1]).toBeDefined());
    expect(images[1]?.map((image) => image.sourceFile)).toEqual([png, tiff]);
  });

  it("routes a named non-image directly to the file path", () => {
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
        onUnsupported: () => undefined,
        onFileUnavailable: () => undefined
      }
    );

    expect(images).toEqual([]);
    expect(files[0]).toEqual([notes]);
  });

  it("routes an unknown-MIME named PNG as a file while keeping a generic screenshot as an image", async () => {
    const namedPng = new File(["opaque"], "diagram.png", { type: "" });
    const screenshot = new File(["png"], "clipboard-image.png", {
      type: ""
    });
    const files: File[][] = [];
    const images: AgentRichTextPromptImage[][] = [];
    deliverClipboardPromptImages(
      {
        files: [namedPng, screenshot]
      } as unknown as DataTransfer,
      {
        externalFilesSupported: true,
        promptImagesSupported: true,
        onFiles: (next) => files.push([...next]),
        onImages: (next) => images.push(next),
        onUnsupported: () => undefined,
        onFileUnavailable: () => undefined
      }
    );
    expect(files).toEqual([[namedPng]]);
    await vi.waitFor(() => expect(images[1]).toBeDefined());
    expect(images[1]?.[0]?.sourceFile).toBe(screenshot);
  });

  it("uses a clipboard item's image MIME when the File MIME is empty", async () => {
    const image = new File(["png"], "diagram.png", { type: "" });
    const images: AgentRichTextPromptImage[][] = [];
    const files: File[][] = [];
    deliverClipboardPromptImages(
      {
        items: [{ kind: "file", type: "image/png", getAsFile: () => image }]
      } as unknown as DataTransfer,
      {
        externalFilesSupported: true,
        promptImagesSupported: true,
        onFiles: (next) => files.push([...next]),
        onImages: (next) => images.push(next),
        onUnsupported: () => undefined,
        onFileUnavailable: () => undefined
      }
    );
    await vi.waitFor(() => expect(images[1]).toBeDefined());
    expect(files).toEqual([]);
    expect(images[1]?.[0]?.sourceFile).toBe(image);
  });

  it("keeps unlabeled files beside a preferred PNG and reads each item during paste", async () => {
    const png = new File(["png"], "shot.png", { type: "image/png" });
    const notes = new File(["notes"], "notes.md", { type: "" });
    const archive = new File(["archive"], "data.bin", {
      type: "application/octet-stream"
    });
    let inPasteEvent = true;
    const getAsFile = [png, notes, archive].map((file) =>
      vi.fn(() => (inPasteEvent ? file : null))
    );
    const images: AgentRichTextPromptImage[][] = [];
    const files: File[][] = [];
    const handled = deliverClipboardPromptImages(
      {
        items: getAsFile.map((read, index) => ({
          kind: "file",
          type: ["image/png", "", "application/octet-stream"][index],
          getAsFile: read
        }))
      } as unknown as DataTransfer,
      {
        externalFilesSupported: true,
        promptImagesSupported: true,
        onFiles: (next) => files.push([...next]),
        onImages: (next) => images.push(next),
        onUnsupported: () => undefined,
        onFileUnavailable: () => undefined
      }
    );
    inPasteEvent = false;

    expect(handled).toBe(true);
    expect(getAsFile.map((read) => read.mock.calls.length)).toEqual([1, 1, 1]);
    expect(files).toEqual([[notes, archive]]);
    expect(images[0]).toHaveLength(1);
    await vi.waitFor(() => expect(images[1]).toBeDefined());
    expect(images[1]?.[0]?.sourceFile).toBe(png);
  });

  it("routes all files through external preparation when prompt images are unsupported", () => {
    const files = [
      new File(["a"], "a.txt", { type: "" }),
      new File(["b"], "b.txt", { type: "application/octet-stream" }),
      new File(["png"], "shot.png", { type: "image/png" })
    ];
    const delivered: File[][] = [];
    expect(
      deliverClipboardPromptImages(
        {
          items: files.map((file) => ({
            kind: "file",
            type: file.type,
            getAsFile: () => file
          }))
        } as unknown as DataTransfer,
        {
          externalFilesSupported: true,
          promptImagesSupported: false,
          onFiles: (next) => delivered.push([...next]),
          onImages: () => undefined,
          onUnsupported: () => undefined,
          onFileUnavailable: () => undefined
        }
      )
    ).toBe(true);
    expect(delivered).toEqual([files]);
  });

  it("reports an unavailable item and still delivers its readable sibling", () => {
    const notes = new File(["notes"], "notes.md", { type: "" });
    const onUnsupported = vi.fn();
    const onFileUnavailable = vi.fn();
    const onFiles = vi.fn();
    expect(
      deliverClipboardPromptImages(
        {
          items: [
            { kind: "file", type: "", getAsFile: () => null },
            { kind: "file", type: "", getAsFile: () => notes }
          ]
        } as unknown as DataTransfer,
        {
          externalFilesSupported: true,
          promptImagesSupported: true,
          onFiles,
          onImages: () => undefined,
          onUnsupported,
          onFileUnavailable
        }
      )
    ).toBe(true);
    expect(onUnsupported).not.toHaveBeenCalled();
    expect(onFileUnavailable).toHaveBeenCalledOnce();
    expect(onFiles).toHaveBeenCalledExactlyOnceWith([notes]);
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
