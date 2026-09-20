import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useTranscriptNativeSelectionGuard } from "./useTranscriptNativeSelectionGuard";

function GuardHost({
  enabled = true,
  onSelectingChange = () => undefined,
  sessionId = "session-1"
}: {
  enabled?: boolean;
  onSelectingChange?: (selecting: boolean) => void;
  sessionId?: string;
}): null {
  useTranscriptNativeSelectionGuard({
    enabled,
    onSelectingChange,
    sessionId
  });
  return null;
}

function selectNode(node: Node): Selection {
  const selection = window.getSelection();
  if (!selection) {
    throw new Error("expected window.getSelection");
  }
  const range = document.createRange();
  range.selectNodeContents(node);
  selection.removeAllRanges();
  selection.addRange(range);
  return selection;
}

describe("useTranscriptNativeSelectionGuard", () => {
  afterEach(() => {
    cleanup();
    window.getSelection()?.removeAllRanges();
    document.body.replaceChildren();
  });

  it("clears a transcript range on Escape and on chrome pointerdown", () => {
    const row = document.createElement("div");
    row.dataset.agentTranscriptRow = "row-1";
    const body = document.createElement("p");
    body.dataset.agentTranscriptNativeSelect = "true";
    body.textContent = "stuck highlight";
    const chrome = document.createElement("button");
    chrome.type = "button";
    chrome.textContent = "Copy";
    row.append(body, chrome);
    document.body.append(row);

    render(<GuardHost />);
    const selection = selectNode(body);
    expect(selection.toString()).toBe("stuck highlight");

    fireEvent.keyDown(document, { key: "Escape" });
    expect(selection.rangeCount).toBe(0);

    selectNode(body);
    fireEvent.pointerDown(chrome, { button: 0 });
    expect(selection.rangeCount).toBe(0);
  });

  it("marks a drag-select in a message body and stops it on pointerup", () => {
    const onSelectingChange = vi.fn();
    const body = document.createElement("p");
    body.dataset.agentTranscriptNativeSelect = "true";
    body.textContent = "hello";
    document.body.append(body);
    render(<GuardHost onSelectingChange={onSelectingChange} />);

    fireEvent.pointerDown(body, { button: 0 });
    expect(onSelectingChange).toHaveBeenCalledWith(true);

    fireEvent.pointerUp(window);
    expect(onSelectingChange).toHaveBeenCalledWith(false);
  });

  it("clears the range when the guarded session unmounts", () => {
    const body = document.createElement("p");
    body.dataset.agentTranscriptNativeSelect = "true";
    body.textContent = "goodbye";
    document.body.append(body);
    const view = render(<GuardHost sessionId="session-a" />);
    const selection = selectNode(body);
    expect(selection.toString()).toBe("goodbye");
    view.unmount();
    expect(selection.rangeCount).toBe(0);
  });
});
