// One message body. Cross-turn Ranges on transform-virtualized rows paint as a
// page-sized highlight in WKWebView and do not collapse on user-select:none chrome.
export const NATIVE_TEXT_SELECTION_ROOT_SELECTOR =
  '[data-agent-transcript-native-select="true"]';

const EDITABLE_SELECTOR = 'textarea, input, [contenteditable="true"]';

export function elementFromNode(node: Node | null | undefined): Element | null {
  if (!node) {
    return null;
  }
  return node instanceof Element ? node : node.parentElement;
}

export function closestNativeTextSelectionRoot(
  node: Node | null | undefined
): Element | null {
  return (
    elementFromNode(node)?.closest(NATIVE_TEXT_SELECTION_ROOT_SELECTOR) ?? null
  );
}

export function isInsideEditable(node: Node | null | undefined): boolean {
  return Boolean(elementFromNode(node)?.closest(EDITABLE_SELECTOR));
}

export function clearNonEditableTextSelection(documentRef: Document): boolean {
  const selection = documentRef.getSelection?.();
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) {
    return false;
  }
  if (
    isInsideEditable(selection.anchorNode) ||
    isInsideEditable(selection.focusNode)
  ) {
    return false;
  }
  selection.removeAllRanges();
  return true;
}

export function constrainNativeTextSelectionToOneMessage(
  documentRef: Document
): boolean {
  const selection = documentRef.getSelection?.();
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) {
    return false;
  }
  const start = closestNativeTextSelectionRoot(selection.anchorNode);
  const end = closestNativeTextSelectionRoot(selection.focusNode);
  if (start && start === end) {
    return false;
  }
  selection.removeAllRanges();
  return true;
}

export function shouldAllowNativeTextSelectStart(
  target: EventTarget | null
): boolean {
  return (
    closestNativeTextSelectionRoot(target instanceof Node ? target : null) !==
    null
  );
}

export function shouldPreventTranscriptSelectStart(
  target: EventTarget | null
): boolean {
  const node = target instanceof Node ? target : null;
  const element = elementFromNode(node);
  if (!element) {
    return false;
  }
  if (closestNativeTextSelectionRoot(element)) {
    return false;
  }
  if (isInsideEditable(element)) {
    return false;
  }
  return Boolean(element.closest("[data-agent-transcript-row]"));
}
