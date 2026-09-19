import { useEffect, useState, type RefObject } from "react";
import type { AgentMessageLocatorItem } from "./agentTranscriptModel";
import { escapeCssString } from "./agentTranscriptModel";

export interface AgentMessageLocatorVirtualSelectionSource {
  readonly scrollOffset: number | null;
  readonly scrollRect: { readonly height: number } | null;
  getVirtualItemForOffset(
    offset: number
  ): { readonly index: number } | undefined;
}

export function useAgentMessageLocatorSelection({
  items,
  isVisible,
  locatorRef,
  virtualSelectionSource
}: {
  items: readonly AgentMessageLocatorItem[];
  isVisible: boolean;
  locatorRef: RefObject<HTMLElement | null>;
  virtualSelectionSource?: AgentMessageLocatorVirtualSelectionSource;
}): { selectItem(itemKey: string): void; selectedKey: string | null } {
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const virtualSelectionReady =
    !virtualSelectionSource ||
    (virtualSelectionSource.scrollOffset !== null &&
      virtualSelectionSource.scrollRect !== null);

  useEffect(() => {
    if (!isVisible) return;
    const locator = locatorRef.current;
    const scrollParent = locator
      ? findMessageLocatorScrollParent(locator)
      : null;
    if (!scrollParent) return;

    let animationFrame: number | null = null;
    const updateSelectedFromScroll = (): void => {
      animationFrame = null;
      // Selection describes the measured viewport. Wheel/key intent belongs to
      // the follow-end controller and cannot veto where the viewport settled.
      setSelectedKey(
        virtualSelectionSource
          ? selectVirtualizedMessageLocatorItemAtViewportCenter(
              virtualSelectionSource,
              items,
              scrollParent.scrollTop
            )
          : selectMessageLocatorItemAtViewportCenter(scrollParent, items)
      );
    };
    const scheduleUpdate = (): void => {
      if (animationFrame === null) {
        animationFrame = window.requestAnimationFrame(updateSelectedFromScroll);
      }
    };
    scheduleUpdate();
    scrollParent.addEventListener("scroll", scheduleUpdate, { passive: true });
    return () => {
      scrollParent.removeEventListener("scroll", scheduleUpdate);
      if (animationFrame !== null) window.cancelAnimationFrame(animationFrame);
    };
  }, [
    isVisible,
    items,
    locatorRef,
    virtualSelectionReady,
    virtualSelectionSource
  ]);

  return { selectItem: setSelectedKey, selectedKey };
}

export function findMessageLocatorScrollParent(
  locator: HTMLElement
): HTMLElement | null {
  const timeline = locator.closest<HTMLElement>(
    '[data-testid="agent-gui-timeline"]'
  );
  if (timeline) {
    return timeline;
  }

  let current = locator.parentElement;
  while (current) {
    const style = window.getComputedStyle(current);
    const overflowY = style.overflowY;
    if (
      (overflowY === "auto" || overflowY === "scroll") &&
      current.scrollHeight > current.clientHeight
    ) {
      return current;
    }
    current = current.parentElement;
  }
  return null;
}

function selectMessageLocatorItemAtViewportCenter(
  scrollParent: HTMLElement,
  items: readonly AgentMessageLocatorItem[]
): string | null {
  const viewportRect = scrollParent.getBoundingClientRect();
  const viewportCenterY = viewportRect.top + viewportRect.height / 2;
  let nearest: { key: string; distance: number } | null = null;

  for (const item of items) {
    const row = scrollParent.querySelector<HTMLElement>(
      `[data-agent-transcript-row="${escapeCssString(item.rowKey)}"]`
    );
    if (!row) {
      continue;
    }
    const rowRect = row.getBoundingClientRect();
    const rowCenterY = rowRect.top + rowRect.height / 2;
    const distance = Math.abs(rowCenterY - viewportCenterY);
    if (!nearest || distance < nearest.distance) {
      nearest = { key: item.key, distance };
    }
  }

  return nearest?.key ?? null;
}

function selectVirtualizedMessageLocatorItemAtViewportCenter(
  source: AgentMessageLocatorVirtualSelectionSource,
  items: readonly AgentMessageLocatorItem[],
  observedScrollOffset = source.scrollOffset
): string | null {
  const viewportHeight = source.scrollRect?.height;
  if (observedScrollOffset === null || viewportHeight === undefined) {
    return null;
  }
  const virtualTurn = source.getVirtualItemForOffset(
    observedScrollOffset + viewportHeight / 2
  );
  if (!virtualTurn) {
    return null;
  }

  let start = 0;
  let end = items.length - 1;
  let matchedIndex = -1;
  while (start <= end) {
    const middle = Math.floor((start + end) / 2);
    const item = items[middle];
    if (!item) {
      break;
    }
    if (item.turnGroupIndex <= virtualTurn.index) {
      matchedIndex = middle;
      start = middle + 1;
    } else {
      end = middle - 1;
    }
  }
  return items[matchedIndex < 0 ? 0 : matchedIndex]?.key ?? null;
}
