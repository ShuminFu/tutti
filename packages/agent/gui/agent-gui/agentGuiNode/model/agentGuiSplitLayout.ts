/**
 * Split-view layout state machine for the agent conversation main area.
 *
 * Replaces the single "current conversation" value with two slots (left /
 * right), a focus side, and a width ratio. Pure functions only: no React, no
 * window / localStorage access inside the reducer. Persistence helpers take
 * the storage object explicitly so callers decide when to read / write
 * (PRD: write only after drop / select / close / resize; never on collapse).
 *
 * Invariant kept after every event: `right !== null` implies `left !== null`
 * (a single pane always lives in the left slot).
 */

export type SplitSide = "left" | "right";

export interface SplitLayoutState {
  panes: { left: string | null; right: string | null };
  focus: SplitSide;
  ratio: number;
}

export type SplitLayoutEvent =
  | { type: "drop"; id: string; side: SplitSide }
  | { type: "select"; id: string }
  | { type: "close"; side: SplitSide }
  | { type: "resize"; ratio: number }
  | {
      type: "restore";
      stored: SplitLayoutState | null;
      alive: ReadonlySet<string> | readonly string[];
    };

export interface SplitLayoutReduceOptions {
  /** Lower bound of the ratio; upper bound is `1 - minRatio`. Default 0.2. */
  minRatio?: number;
}

export const SPLIT_LAYOUT_DEFAULT_RATIO = 0.5;
export const SPLIT_LAYOUT_DEFAULT_MIN_RATIO = 0.2;

const SPLIT_LAYOUT_STORAGE_PREFIX = "agent-gui:split-layout:";

export function createEmptySplitLayout(): SplitLayoutState {
  return {
    panes: { left: null, right: null },
    focus: "left",
    ratio: SPLIT_LAYOUT_DEFAULT_RATIO
  };
}

export function isSplitLayoutSplit(state: SplitLayoutState): boolean {
  return state.panes.right !== null;
}

/**
 * The "current conversation" for code that does not care about split view:
 * the focused slot, falling back to the other slot when the focused one is
 * empty (so a single pane is always reported even if focus drifted).
 */
export function splitLayoutActiveConversationId(
  state: SplitLayoutState
): string | null {
  const other: SplitSide = state.focus === "left" ? "right" : "left";
  return state.panes[state.focus] ?? state.panes[other] ?? null;
}

function otherSide(side: SplitSide): SplitSide {
  return side === "left" ? "right" : "left";
}

function clampRatio(ratio: number, minRatio: number): number {
  const min = Number.isFinite(minRatio)
    ? Math.min(Math.max(minRatio, 0), 0.5)
    : SPLIT_LAYOUT_DEFAULT_MIN_RATIO;
  const value = Number.isFinite(ratio) ? ratio : SPLIT_LAYOUT_DEFAULT_RATIO;
  return Math.min(Math.max(value, min), 1 - min);
}

/**
 * Left-fill normalization: a lone pane never sits in the right slot. If left
 * is empty and right is filled, shift right into left and point focus at it.
 * Also guarantees focus never points at an empty slot while the other has a
 * conversation.
 */
function normalizeSplitLayout(state: SplitLayoutState): SplitLayoutState {
  let { left, right } = state.panes;
  let focus = state.focus;
  if (left === null && right !== null) {
    left = right;
    right = null;
    focus = "left";
  }
  if (right === null) {
    focus = "left";
  }
  if (
    left === state.panes.left &&
    right === state.panes.right &&
    focus === state.focus
  ) {
    return state;
  }
  return { panes: { left, right }, focus, ratio: state.ratio };
}

function sideOf(state: SplitLayoutState, id: string): SplitSide | null {
  if (state.panes.left === id) {
    return "left";
  }
  if (state.panes.right === id) {
    return "right";
  }
  return null;
}

export function reduceSplitLayout(
  state: SplitLayoutState,
  event: SplitLayoutEvent,
  options: SplitLayoutReduceOptions = {}
): SplitLayoutState {
  const minRatio = options.minRatio ?? SPLIT_LAYOUT_DEFAULT_MIN_RATIO;
  switch (event.type) {
    case "drop": {
      const existing = sideOf(state, event.id);
      // Decision: the same conversation never occupies both slots. Dropping an
      // id that already lives in the OTHER slot only moves focus there; the
      // slots stay untouched (no duplicate, no swap).
      if (existing !== null && existing !== event.side) {
        return normalizeSplitLayout({ ...state, focus: existing });
      }
      if (existing === event.side) {
        return normalizeSplitLayout({ ...state, focus: event.side });
      }
      // Dropping onto an occupied side while the other side is empty means
      // "split", not "replace": the sitting conversation moves across so the
      // dropped one lands on the half the user pointed at. Without this, a
      // drop on the left of a single pane overwrote the only pane and the
      // drop read as "the dragged session took over the whole window".
      // With both slots filled there is nowhere to move to, so that side is
      // replaced as before.
      const resident = state.panes[event.side];
      const vacant = otherSide(event.side);
      if (resident !== null && state.panes[vacant] === null) {
        return normalizeSplitLayout({
          ...state,
          panes:
            event.side === "left"
              ? { left: event.id, right: resident }
              : { left: resident, right: event.id },
          focus: event.side
        });
      }
      // Dropping on the right of an empty layout still lands in left
      // (left-fill invariant); normalize handles the shift.
      const panes = { ...state.panes, [event.side]: event.id };
      return normalizeSplitLayout({ ...state, panes, focus: event.side });
    }
    case "select": {
      const existing = sideOf(state, event.id);
      if (existing !== null) {
        return normalizeSplitLayout({ ...state, focus: existing });
      }
      // Unknown id replaces the focused slot only. In single-pane mode focus
      // is always left, so this is "replace the one pane".
      const panes = { ...state.panes, [state.focus]: event.id };
      return normalizeSplitLayout({ ...state, panes });
    }
    case "close": {
      const survivor = state.panes[otherSide(event.side)];
      // Whatever remains goes to left; closing the only pane yields empty.
      return normalizeSplitLayout({
        ...state,
        panes: { left: survivor, right: null },
        focus: "left"
      });
    }
    case "resize": {
      if (!isSplitLayoutSplit(state)) {
        return state;
      }
      const ratio = clampRatio(event.ratio, minRatio);
      return ratio === state.ratio ? state : { ...state, ratio };
    }
    case "restore": {
      if (event.stored === null) {
        return state;
      }
      const alive =
        event.alive instanceof Set
          ? event.alive
          : new Set(event.alive as readonly string[]);
      const isAlive = (id: string | null): id is string =>
        id !== null && alive.has(id);
      const { left, right } = event.stored.panes;
      const ratio = clampRatio(event.stored.ratio, minRatio);
      // Restore semantics: both alive -> stored as is; exactly one alive ->
      // single pane with that one in left (ratio kept for the next split);
      // none alive -> empty layout.
      if (isAlive(left) && isAlive(right)) {
        return normalizeSplitLayout({
          panes: { left, right },
          focus: event.stored.focus,
          ratio
        });
      }
      const survivor = isAlive(left) ? left : isAlive(right) ? right : null;
      if (survivor === null) {
        return createEmptySplitLayout();
      }
      return { panes: { left: survivor, right: null }, focus: "left", ratio };
    }
    default:
      return state;
  }
}

/**
 * Storage key mirrors the rail view state scope key
 * (`<workspaceId>:agentTarget:<targetId>` / `<workspaceId>:all`) so the
 * split layout follows the same workspace + target scoping.
 */
export function splitLayoutStorageKey(scope: {
  workspaceId: string;
  targetId: string | null;
}): string {
  const targetId = scope.targetId?.trim() ?? "";
  const filterScope = targetId ? `agentTarget:${targetId}` : "all";
  return `${SPLIT_LAYOUT_STORAGE_PREFIX}${scope.workspaceId.trim()}:${filterScope}`;
}

export function serializeSplitLayout(state: SplitLayoutState): string {
  return JSON.stringify({
    left: state.panes.left,
    right: state.panes.right,
    focus: state.focus,
    ratio: state.ratio
  });
}

function asIdOrNull(value: unknown): string | null | undefined {
  if (value === null) {
    return null;
  }
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed ? trimmed : null;
  }
  return undefined;
}

/** Tolerant parser: anything malformed yields null instead of throwing. */
export function parseSplitLayout(raw: string | null): SplitLayoutState | null {
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return null;
    }
    const candidate = parsed as {
      left?: unknown;
      right?: unknown;
      focus?: unknown;
      ratio?: unknown;
    };
    const left = asIdOrNull(candidate.left ?? null);
    const right = asIdOrNull(candidate.right ?? null);
    if (left === undefined || right === undefined) {
      return null;
    }
    const focus: SplitSide =
      candidate.focus === "right" ? "right" : "left";
    const ratio =
      typeof candidate.ratio === "number" && Number.isFinite(candidate.ratio)
        ? candidate.ratio
        : SPLIT_LAYOUT_DEFAULT_RATIO;
    return normalizeSplitLayout({ panes: { left, right }, focus, ratio });
  } catch {
    return null;
  }
}

export function loadSplitLayout(
  storage: Pick<Storage, "getItem">,
  key: string
): SplitLayoutState | null {
  try {
    return parseSplitLayout(storage.getItem(key));
  } catch {
    return null;
  }
}

export function saveSplitLayout(
  storage: Pick<Storage, "setItem">,
  key: string,
  state: SplitLayoutState
): void {
  try {
    storage.setItem(key, serializeSplitLayout(state));
  } catch {
    // Storage may be unavailable (private mode, quota); layout is best-effort.
  }
}
