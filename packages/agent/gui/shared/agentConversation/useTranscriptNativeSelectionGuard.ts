import { useEffect, useState } from "react";
import {
  clearDisconnectedTextSelection,
  clearNonEditableTextSelection,
  closestNativeTextSelectionRoot
} from "./nativeTextSelection";

/**
 * WKWebView keeps a Range when the user clicks `user-select: none` chrome,
 * and transform-virtualized turns can stretch that Range into a page-sized
 * highlight. Collapse it on Esc / chrome pointerdown, and freeze follow-end
 * geometry while a real drag-select is in progress.
 */
export function useTranscriptNativeSelectionGuard({
  documentRef = typeof document === "undefined" ? undefined : document,
  enabled,
  onSelectingChange,
  sessionId
}: {
  documentRef?: Document;
  enabled: boolean;
  onSelectingChange?: (selecting: boolean) => void;
  sessionId: string;
}): { isSelecting: boolean } {
  const [isSelecting, setSelecting] = useState(false);
  useEffect(() => {
    if (!enabled || !documentRef) {
      return;
    }
    const view = documentRef.defaultView;
    const setSelectingState = (selecting: boolean): void => {
      setSelecting(selecting);
      onSelectingChange?.(selecting);
    };
    const endSelecting = (): void => {
      setSelectingState(false);
    };
    const onPointerDown = (event: PointerEvent): void => {
      if (event.button !== 0) {
        return;
      }
      if (
        closestNativeTextSelectionRoot(
          event.target instanceof Node ? event.target : null
        )
      ) {
        setSelectingState(true);
        return;
      }
      clearNonEditableTextSelection(documentRef);
    };
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") {
        return;
      }
      clearNonEditableTextSelection(documentRef);
    };
    const onSelectionChange = (): void => {
      if (clearDisconnectedTextSelection(documentRef)) {
        setSelectingState(false);
      }
    };

    documentRef.addEventListener("pointerdown", onPointerDown, true);
    documentRef.addEventListener("keydown", onKeyDown);
    documentRef.addEventListener("selectionchange", onSelectionChange);
    view?.addEventListener("pointerup", endSelecting);
    view?.addEventListener("pointercancel", endSelecting);
    return () => {
      documentRef.removeEventListener("pointerdown", onPointerDown, true);
      documentRef.removeEventListener("keydown", onKeyDown);
      documentRef.removeEventListener("selectionchange", onSelectionChange);
      view?.removeEventListener("pointerup", endSelecting);
      view?.removeEventListener("pointercancel", endSelecting);
      clearNonEditableTextSelection(documentRef);
      setSelectingState(false);
    };
  }, [documentRef, enabled, onSelectingChange, sessionId]);
  return { isSelecting };
}
