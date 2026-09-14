import { describe, expect, it } from "vitest";
import type {
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText
} from "../model/agentGuiNodeTypes";
import { selectComposerDraftUnavailableAttachments } from "./composerDraftAttachmentReadiness";

function image(
  overrides: Partial<AgentComposerDraftImage> = {}
): AgentComposerDraftImage {
  return {
    id: "image-1",
    mimeType: "image/png",
    name: "shot.png",
    previewUrl: "data:image/png;base64,aGVsbG8=",
    ...overrides
  };
}

function file(
  overrides: Partial<AgentComposerDraftFile> = {}
): AgentComposerDraftFile {
  return { id: "file-1", name: "a.txt", ...overrides };
}

function largeText(
  overrides: Partial<AgentComposerDraftLargeText> = {}
): AgentComposerDraftLargeText {
  return {
    id: "large-1",
    name: "pasted-text.txt",
    text: "hello",
    ...overrides
  };
}

describe("selectComposerDraftUnavailableAttachments", () => {
  it("reports nothing unavailable for an empty draft", () => {
    expect(selectComposerDraftUnavailableAttachments({})).toEqual({
      failedCount: 0,
      hasUnavailable: false,
      uploadingCount: 0
    });
  });

  it("counts an image that is still uploading", () => {
    expect(
      selectComposerDraftUnavailableAttachments({
        images: [image({ uploading: true })]
      })
    ).toEqual({
      failedCount: 0,
      hasUnavailable: true,
      uploadingCount: 1
    });
  });

  it("counts a failed image as failed rather than uploading", () => {
    expect(
      selectComposerDraftUnavailableAttachments({
        images: [image({ uploadError: "boom", uploading: false })]
      })
    ).toEqual({
      failedCount: 1,
      hasUnavailable: true,
      uploadingCount: 0
    });
  });

  it("does not treat a failed-and-uploading attachment as both", () => {
    expect(
      selectComposerDraftUnavailableAttachments({
        images: [image({ uploadError: "boom", uploading: true })]
      })
    ).toEqual({
      failedCount: 1,
      hasUnavailable: true,
      uploadingCount: 0
    });
  });

  it("aggregates unavailable attachments across every kind", () => {
    expect(
      selectComposerDraftUnavailableAttachments({
        images: [image({ uploading: true }), image({ id: "image-2" })],
        files: [file({ uploadError: "nope", uploading: false })],
        largeTexts: [largeText({ uploading: true })]
      })
    ).toEqual({
      failedCount: 1,
      hasUnavailable: true,
      uploadingCount: 2
    });
  });

  it("reports all-ready attachments as available", () => {
    expect(
      selectComposerDraftUnavailableAttachments({
        images: [image({ uploading: false })],
        files: [file({ path: "/tmp/a.txt", uploading: false })],
        largeTexts: [largeText({ path: "/tmp/a.txt", uploading: false })]
      })
    ).toEqual({
      failedCount: 0,
      hasUnavailable: false,
      uploadingCount: 0
    });
  });
});
