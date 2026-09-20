import { afterEach, describe, expect, it } from "vitest";
import {
  clearDisconnectedTextSelection,
  clearNonEditableTextSelection
} from "./nativeTextSelection";

function selectAcross(start: Node, end: Node): Selection {
  const selection = window.getSelection();
  if (!selection) {
    throw new Error("expected window.getSelection");
  }
  const range = document.createRange();
  range.setStart(start, 0);
  range.setEnd(end, end.textContent?.length ?? 0);
  selection.removeAllRanges();
  selection.addRange(range);
  return selection;
}

describe("nativeTextSelection", () => {
  afterEach(() => {
    window.getSelection()?.removeAllRanges();
    document.body.replaceChildren();
  });

  it("clears a non-editable range and leaves composer text alone", () => {
    const message = document.createElement("p");
    message.dataset.agentTranscriptNativeSelect = "true";
    message.textContent = "assistant reply";
    const editor = document.createElement("div");
    editor.setAttribute("contenteditable", "true");
    editor.textContent = "draft prompt";
    document.body.append(message, editor);

    const messageText = message.firstChild;
    const editorText = editor.firstChild;
    if (!messageText || !editorText) {
      throw new Error("expected text nodes");
    }

    const selection = selectAcross(messageText, messageText);
    expect(clearNonEditableTextSelection(document)).toBe(true);
    expect(selection.rangeCount).toBe(0);

    selectAcross(editorText, editorText);
    expect(clearNonEditableTextSelection(document)).toBe(false);
    expect(selection.toString()).toBe("draft prompt");
  });

  it("clears a large non-editable range so Escape can dismiss it", () => {
    const message = document.createElement("div");
    message.dataset.agentTranscriptNativeSelect = "true";
    message.textContent = "a".repeat(400);
    document.body.append(message);
    const text = message.firstChild;
    if (!text) {
      throw new Error("expected text node");
    }
    const selection = selectAcross(text, text);
    expect(selection.toString().length).toBe(400);
    expect(clearNonEditableTextSelection(document)).toBe(true);
    expect(selection.rangeCount).toBe(0);
  });

  it("does not clear a live range whose nodes are still connected", () => {
    const message = document.createElement("p");
    message.dataset.agentTranscriptNativeSelect = "true";
    message.textContent = "keep me";
    document.body.append(message);
    const text = message.firstChild;
    if (!text) {
      throw new Error("expected text node");
    }
    const selection = selectAcross(text, text);
    expect(clearDisconnectedTextSelection(document)).toBe(false);
    expect(selection.toString()).toBe("keep me");
  });
});
