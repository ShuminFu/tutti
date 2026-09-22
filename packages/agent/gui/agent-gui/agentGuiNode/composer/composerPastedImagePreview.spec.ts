import { describe, expect, it } from "vitest";
import { mergePastedComposerImages } from "./composerPastedImagePreview";

describe("mergePastedComposerImages", () => {
  it("shows a blob preview before any image bytes are stored", () => {
    const file = new File(["png"], "shot.png", { type: "image/png" });
    const merged = mergePastedComposerImages({
      current: [],
      incoming: [
        {
          id: "img-1",
          name: "shot.png",
          mimeType: "image/png",
          data: "",
          previewUrl: "blob:preview",
          sourceFile: file
        }
      ],
      remainingSlots: 4,
      uploadEnabled: true
    });

    expect(merged.images).toEqual([
      expect.objectContaining({
        id: "img-1",
        previewUrl: "blob:preview",
        uploading: true
      })
    ]);
    expect(merged.images[0]?.data).toBeUndefined();
    expect(merged.jobs).toEqual([
      expect.objectContaining({ id: "img-1", upload: true, sourceFile: file })
    ]);
  });

  it("updates the placeholder in place and ignores a removed chip", () => {
    const placeholder = mergePastedComposerImages({
      current: [],
      incoming: [
        {
          id: "img-1",
          name: "clipboard-image",
          mimeType: "image/png",
          data: "",
          previewUrl: ""
        }
      ],
      remainingSlots: 1,
      uploadEnabled: true
    });
    const resolved = mergePastedComposerImages({
      current: placeholder.images,
      incoming: [
        {
          id: "img-1",
          name: "shot.png",
          mimeType: "image/png",
          data: "",
          previewUrl: "blob:preview",
          sourceFile: new File(["png"], "shot.png", { type: "image/png" })
        }
      ],
      remainingSlots: 0,
      uploadEnabled: true
    });
    const removed = mergePastedComposerImages({
      current: [],
      incoming: [
        {
          id: "img-1",
          name: "shot.png",
          mimeType: "image/png",
          data: "",
          previewUrl: "blob:preview",
          sourceFile: new File(["png"], "shot.png", { type: "image/png" })
        }
      ],
      remainingSlots: 4,
      removedIds: new Set(["img-1"]),
      uploadEnabled: true
    });

    expect(resolved.images).toHaveLength(1);
    expect(resolved.images[0]).toMatchObject({
      id: "img-1",
      name: "shot.png",
      previewUrl: "blob:preview"
    });
    expect(resolved.addedCount).toBe(0);
    expect(removed.changed).toBe(false);
    expect(removed.images).toEqual([]);
  });
});
