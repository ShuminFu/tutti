import { afterEach, describe, expect, it } from "vitest";
import {
  registerAgentCustomMentionKind,
  resetAgentCustomMentionKindsForTests
} from "../../../shared/agentCustomMentionKinds";
import {
  buildAgentComposerDraft,
  snapshotAgentComposerDraft
} from "../model/agentComposerDraft";
import type { SubmittedDraftSnapshot } from "../model/agentGuiNodeTypes";
import {
  areAgentComposerDraftsEqual,
  clearSubmittedDraftIfUnchanged,
  deleteSubmittedDraftSnapshotsForScopes,
  deleteUnacceptedSubmittedDraftSnapshot,
  toRuntimeSendContent
} from "./agentGuiController.draftMessageHelpers";

afterEach(() => {
  resetAgentCustomMentionKindsForTests();
});

describe("runtime prompt materialization", () => {
  it("expands a custom mention for the runtime without changing its display prompt", () => {
    registerAgentCustomMentionKind({
      kind: "browser-element",
      present: (mention) => ({ name: mention.label }),
      materializePromptText: (mention) => {
        const context = mention.scope?.context?.trim();
        return context ? `\n${context}\n` : null;
      }
    });
    const displayPrompt =
      "[@div](mention://browser-element/browser-element%3A1?context=DOM+Path%3A+%23app%0APosition%3A+top%3D0px%0AHTML+Element%3A+%3Cdiv%3EHello%3C%2Fdiv%3E&tag=div&workspaceId=workspace-1) explain this";

    expect(
      toRuntimeSendContent([{ type: "text", text: displayPrompt }])
    ).toEqual([
      {
        type: "text",
        text: [
          "DOM Path: #app",
          "Position: top=0px",
          "HTML Element: <div>Hello</div>",
          " explain this"
        ].join("\n")
      }
    ]);
    expect(displayPrompt).toContain("mention://browser-element/");
  });

  it("preserves a custom mention when its kind has no prompt materializer", () => {
    registerAgentCustomMentionKind({
      kind: "external-note",
      present: (mention) => ({ name: mention.label })
    });
    const prompt = "[@note](mention://external-note/note-1) review";

    expect(toRuntimeSendContent([{ type: "text", text: prompt }])).toEqual([
      { type: "text", text: prompt }
    ]);
  });
});

describe("submitted composer draft cleanup", () => {
  const sourceScopeKey = "session:session-1";
  const submittedDraft = buildAgentComposerDraft({
    prompt: "Review this",
    images: [
      {
        id: "image-1",
        name: "screen.png",
        mimeType: "image/png",
        attachmentId: "attachment-1",
        previewUrl: "blob:image-1"
      }
    ],
    files: [
      {
        id: "file-1",
        name: "notes.md",
        path: "/workspace/notes.md"
      }
    ],
    largeTexts: [
      {
        id: "paste-1",
        name: "pasted-text.txt",
        text: "large pasted body",
        path: "/archive/paste-1.txt"
      }
    ]
  });
  const snapshot: SubmittedDraftSnapshot = {
    sourceScopeKey,
    content: snapshotAgentComposerDraft(submittedDraft)
  };

  it("clears the entire source scope when its full content still matches", () => {
    const drafts = { [sourceScopeKey]: submittedDraft };
    const result = clearSubmittedDraftIfUnchanged({ drafts, snapshot });

    expect(result).not.toBe(drafts);
    expect(result[sourceScopeKey]).toEqual([{ type: "text", text: "" }]);
  });

  it("retains the entire current draft when text changes during submission", () => {
    const editedDraft = buildAgentComposerDraft({
      prompt: "Review this and the follow-up",
      images: [
        {
          id: "image-1",
          name: "screen.png",
          mimeType: "image/png",
          attachmentId: "attachment-1",
          previewUrl: "blob:image-1"
        }
      ]
    });
    const drafts = { [sourceScopeKey]: editedDraft };

    expect(clearSubmittedDraftIfUnchanged({ drafts, snapshot })).toBe(drafts);
  });

  it("treats attachment upload metadata as part of the atomic content", () => {
    const current = snapshotAgentComposerDraft(submittedDraft);
    const image = current.find((block) => block.type === "image");
    if (image) image.uploading = true;

    expect(areAgentComposerDraftsEqual(current, submittedDraft)).toBe(false);
  });

  // A send can be handed off while an attachment is still uploading; the upload
  // then settles in place. That settlement is not a user edit, so the submitted
  // text and attachment must still be cleared instead of lingering in the
  // composer and being resent by accident.
  it("clears the submitted draft when an attachment only finishes uploading", () => {
    const current = snapshotAgentComposerDraft(submittedDraft);
    const image = current.find((block) => block.type === "image");
    if (image) image.uploading = true;
    const drafts = { [sourceScopeKey]: current };

    const result = clearSubmittedDraftIfUnchanged({ drafts, snapshot });

    expect(result).not.toBe(drafts);
    expect(result[sourceScopeKey]).toEqual([{ type: "text", text: "" }]);
  });

  // The same normalization must not swallow a real edit that happens while the
  // upload is still in flight.
  it("retains the draft when text is edited while an attachment is uploading", () => {
    const current = snapshotAgentComposerDraft(submittedDraft);
    const image = current.find((block) => block.type === "image");
    if (image) image.uploading = true;
    const text = current.find((block) => block.type === "text");
    if (text) text.text = "edited while uploading";
    const drafts = { [sourceScopeKey]: current };

    expect(clearSubmittedDraftIfUnchanged({ drafts, snapshot })).toBe(drafts);
  });

  it("compares newly-added block metadata without maintaining a field list", () => {
    const current = snapshotAgentComposerDraft(submittedDraft);
    const image = current.find((block) => block.type === "image");
    Object.assign(image ?? {}, { futureMetadata: "edited" });

    expect(areAgentComposerDraftsEqual(current, submittedDraft)).toBe(false);
  });

  it("does not clear a different draft scope", () => {
    const otherScopeKey = "session:session-2";
    const drafts = {
      [sourceScopeKey]: submittedDraft,
      [otherScopeKey]: buildAgentComposerDraft({ prompt: "Keep me" })
    };

    const result = clearSubmittedDraftIfUnchanged({ drafts, snapshot });
    expect(result[otherScopeKey]).toBe(drafts[otherScopeKey]);
  });

  it("deletes unresolved snapshots owned by deleted scopes only", () => {
    const snapshots: Record<string, SubmittedDraftSnapshot> = {
      "submit-1": snapshot,
      "submit-2": {
        sourceScopeKey: "session:session-2",
        content: buildAgentComposerDraft({ prompt: "Keep me" })
      },
      "submit-3": {
        sourceScopeKey: "home",
        targetAgentSessionId: "session-1",
        content: buildAgentComposerDraft({ prompt: "Recovered draft" })
      }
    };

    deleteSubmittedDraftSnapshotsForScopes({
      snapshots,
      scopeKeys: new Set([sourceScopeKey]),
      targetAgentSessionIds: new Set(["session-1"])
    });

    expect(snapshots).toEqual({
      "submit-2": {
        sourceScopeKey: "session:session-2",
        content: buildAgentComposerDraft({ prompt: "Keep me" })
      }
    });
  });

  it("drops a snapshot immediately when the engine rejects the submit", () => {
    const snapshots = { "submit-1": snapshot };

    deleteUnacceptedSubmittedDraftSnapshot({
      snapshots,
      clientSubmitId: "submit-1",
      accepted: false,
      queued: false
    });

    expect(snapshots).toEqual({});
  });

  it("keeps a snapshot while an accepted submit is pending", () => {
    const snapshots = { "submit-1": snapshot };

    deleteUnacceptedSubmittedDraftSnapshot({
      snapshots,
      clientSubmitId: "submit-1",
      accepted: true,
      queued: false
    });

    expect(snapshots["submit-1"]).toBe(snapshot);
  });
});
