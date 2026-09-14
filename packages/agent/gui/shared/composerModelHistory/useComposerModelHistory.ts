import { useCallback, useMemo } from "react";
import { useEngineSelector } from "../engine/useEngineSelector";
import { normalizeComposerModelTargetId } from "../../agent-gui/agentGuiNode/model/composerModelChoiceHistory";
import {
  composerModelHistorySource,
  hydrateComposerModelHistory,
  recordComposerModelRecent,
  toggleComposerModelFavorite,
  type ComposerModelHistoryView
} from "./composerModelHistoryStore";

export interface ComposerModelHistoryController extends ComposerModelHistoryView {
  /**
   * Re-read history. Call it when the model menu opens: it picks up host-side
   * writes from another menu/window and retries a failed earlier read or write.
   */
  refreshModelHistory(): void;
  toggleFavorite(modelId: string): void;
  recordRecent(modelId: string): void;
}

/**
 * Composer model history (per-target favorites + recents) for one agent target.
 *
 * Both model menus use this hook so the two of them read and write the same
 * store: a favorite toggled in one menu is visible in the other immediately, and
 * the shared ordering rules (see `composerModelHistoryStore`) apply once instead
 * of per component.
 *
 * `targetId` is the menu's `modelHistoryTargetId`; an omitted/blank value maps
 * to the shared `"default"` target, exactly like the localStorage keys.
 */
export function useComposerModelHistory(
  targetId: string | null | undefined
): ComposerModelHistoryController {
  const key = normalizeComposerModelTargetId(targetId);
  const source = composerModelHistorySource(key);
  const view = useEngineSelector(source, (snapshot) => snapshot);
  const refreshModelHistory = useCallback((): void => {
    hydrateComposerModelHistory(key, { force: true });
  }, [key]);
  const toggleFavorite = useCallback(
    (modelId: string): void => {
      toggleComposerModelFavorite(key, modelId);
    },
    [key]
  );
  const recordRecent = useCallback(
    (modelId: string): void => {
      recordComposerModelRecent(key, modelId);
    },
    [key]
  );
  return useMemo(
    () => ({
      favoriteModelIds: view.favoriteModelIds,
      recentModelIds: view.recentModelIds,
      pending: view.pending,
      refreshModelHistory,
      toggleFavorite,
      recordRecent
    }),
    [view, refreshModelHistory, toggleFavorite, recordRecent]
  );
}
