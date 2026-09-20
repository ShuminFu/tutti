import { afterEach, describe, expect, it } from "vitest";
import {
  clearNonEditableTextSelection,
  constrainNativeTextSelectionToOneMessage,
  shouldAllowNativeTextSelectStart,
  shouldPreventTranscriptSelectStart
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

  it("collapses a range that crosses two message bodies", () => {
    const first = document.createElement("p");
    first.dataset.agentTranscriptNativeSelect = "true";
    first.textContent = "one";
    const second = document.createElement("p");
    second.dataset.agentTranscriptNativeSelect = "true";
    second.textContent = "two";
    document.body.append(first, second);
    const firstText = first.firstChild;
    const secondText = second.firstChild;
    if (!firstText || !secondText) {
      throw new Error("expected text nodes");
    }

    const selection = selectAcross(firstText, secondText);
    expect(constrainNativeTextSelectionToOneMessage(document)).toBe(true);
    expect(selection.rangeCount).toBe(0);
  });

  it("keeps a range that stays inside one message body", () => {
    const message = document.createElement("p");
    message.dataset.agentTranscriptNativeSelect = "true";
    message.textContent = "keep me";
    document.body.append(message);
    const text = message.firstChild;
    if (!text) {
      throw new Error("expected text node");
    }

    const selection = selectAcross(text, text);
    expect(constrainNativeTextSelectionToOneMessage(document)).toBe(false);
    expect(selection.toString()).toBe("keep me");
  });

  it("blocks selectstart on transcript chrome and allows it in a message body", () => {
    const row = document.createElement("div");
    row.dataset.agentTranscriptRow = "row-1";
    const chrome = document.createElement("button");
    chrome.textContent = "Copy";
    const body = document.createElement("p");
    body.dataset.agentTranscriptNativeSelect = "true";
    body.textContent = "body";
    row.append(chrome, body);
    document.body.append(row);

    expect(shouldPreventTranscriptSelectStart(chrome)).toBe(true);
    expect(shouldAllowNativeTextSelectStart(body)).toBe(true);
    expect(shouldPreventTranscriptSelectStart(body)).toBe(false);
  });

  it("does not block selectstart in the composer", () => {
    const editor = document.createElement("div");
    editor.setAttribute("contenteditable", "true");
    editor.textContent = "prompt";
    document.body.append(editor);
    expect(shouldPreventTranscriptSelectStart(editor)).toBe(false);
    expect(shouldAllowNativeTextSelectStart(editor)).toBe(false);
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
});
