/**
 * Composer draft versions distinguish a local edit from a later render of an
 * older snapshot. Submit, queue, restore, and clear all bind to one revision
 * so a lagging parent echo cannot truncate or resurrect the live draft.
 */

export interface ComposerDraftLocalEcho {
  prompt: string;
  revision: number;
  scopeKey: string;
}

export interface ComposerDraftVersionTracker {
  consumedRevision: number;
  nextRevision: number;
  pendingLocalEchoes: ComposerDraftLocalEcho[];
  scopeKey: string;
}

export interface ComposerDraftChangeMeta {
  revision: number;
}

export function createComposerDraftVersionTracker(
  scopeKey: string
): ComposerDraftVersionTracker {
  return {
    consumedRevision: 0,
    nextRevision: 0,
    pendingLocalEchoes: [],
    scopeKey
  };
}

export function recordComposerLocalDraftEdit(
  tracker: ComposerDraftVersionTracker,
  prompt: string
): number {
  tracker.nextRevision += 1;
  tracker.pendingLocalEchoes.push({
    prompt,
    revision: tracker.nextRevision,
    scopeKey: tracker.scopeKey
  });
  return tracker.nextRevision;
}

export function consumeComposerSubmittedRevision(
  tracker: ComposerDraftVersionTracker,
  revision: number
): void {
  tracker.consumedRevision = Math.max(tracker.consumedRevision, revision);
  tracker.pendingLocalEchoes = tracker.pendingLocalEchoes.filter(
    (echo) => echo.revision > revision
  );
}

export function shouldApplyComposerDraftProp(input: {
  prompt: string;
  revision?: number;
  scopeKey: string;
  tracker: ComposerDraftVersionTracker;
}): boolean {
  const { prompt, revision, scopeKey, tracker } = input;
  if (tracker.scopeKey !== scopeKey) {
    tracker.scopeKey = scopeKey;
    tracker.consumedRevision = 0;
    tracker.nextRevision = revision ?? 0;
    tracker.pendingLocalEchoes = [];
    return true;
  }
  if (revision != null && revision <= tracker.consumedRevision) {
    return false;
  }
  const localEcho = tracker.pendingLocalEchoes.find(
    (candidate) =>
      candidate.scopeKey === scopeKey && candidate.prompt === prompt
  );
  if (localEcho) {
    tracker.pendingLocalEchoes = tracker.pendingLocalEchoes.filter(
      (candidate) =>
        candidate.scopeKey !== scopeKey ||
        candidate.revision > localEcho.revision
    );
    return false;
  }
  if (revision != null && revision < tracker.nextRevision) {
    return false;
  }
  // Submit-clear is an unversioned empty replacement. Keep any local edit that
  // landed after the consumed send, including a retype of the same text.
  if (
    revision == null &&
    prompt === "" &&
    tracker.nextRevision > tracker.consumedRevision
  ) {
    return false;
  }
  tracker.pendingLocalEchoes = [];
  if (revision != null && revision > tracker.nextRevision) {
    tracker.nextRevision = revision;
  } else if (revision == null) {
    tracker.nextRevision += 1;
  }
  return true;
}
