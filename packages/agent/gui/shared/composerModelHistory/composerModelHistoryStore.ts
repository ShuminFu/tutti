import {
  composerModelFavoritesStorageKey,
  composerModelRecentsStorageKey,
  normalizeComposerModelTargetId,
  parseComposerModelIdList,
  recordRecentComposerModel,
  sanitizeComposerModelIdList,
  serializeComposerModelIdList
} from "../../agent-gui/agentGuiNode/model/composerModelChoiceHistory";
import type { EngineStateStore } from "../engine/useEngineSelector";
import {
  COMPOSER_MODEL_HISTORY_HOST_UNSUPPORTED_MESSAGE,
  composerModelHistoryHost,
  markComposerModelHistoryHostUnsupported,
  type ComposerModelHistorySnapshot,
  type ComposerModelHistoryUpdateInput
} from "./composerModelHistoryHost";

export interface ComposerModelHistoryView {
  favoriteModelIds: readonly string[];
  recentModelIds: readonly string[];
  pending: boolean;
}

interface HistoryRecord {
  key: string;
  snapshot: ComposerModelHistorySnapshot;
  durable: ComposerModelHistorySnapshot | null;
  hydrated: boolean;
  reading: boolean;
  readFailureLogged: boolean;
  editEpoch: number;
  pendingWrites: number;
  queue: Promise<void>;
  listeners: Set<() => void>;
  view: ComposerModelHistoryView;
  source: EngineStateStore<ComposerModelHistoryView>;
}

// One preference record per mounted target. Unmounted records are released once
// their pending I/O settles; this is not a second Session/Turn state store.
const records = new Map<string, HistoryRecord>();

export function composerModelHistorySource(
  targetId: string | null | undefined
) {
  return ensureRecord(normalizeComposerModelTargetId(targetId)).source;
}

export function composerModelHistoryView(targetId: string | null | undefined) {
  return ensureRecord(normalizeComposerModelTargetId(targetId)).view;
}

export function hydrateComposerModelHistory(
  targetId: string | null | undefined,
  options: { force?: boolean } = {}
): void {
  readHistory(
    ensureRecord(normalizeComposerModelTargetId(targetId)),
    options.force === true
  );
}

function readHistory(record: HistoryRecord, force: boolean): void {
  if (record.reading || (record.hydrated && !force)) return;
  const legacy = readLocalSnapshot(record.key);
  if (!composerModelHistoryHost()) {
    record.durable = legacy;
    publish(record, legacy);
    return;
  }
  record.reading = true;
  const epoch = record.editEpoch;
  enqueue(record, async () => {
    const host = composerModelHistoryHost();
    try {
      const snapshot = host
        ? await host.readComposerModelHistory({
            targetId: record.key,
            legacyFavorites: legacy.favorites,
            legacyRecents: legacy.recents
          })
        : readLocalSnapshot(record.key);
      record.durable = normalizeSnapshot(snapshot);
      record.hydrated = host !== null;
      if (record.editEpoch === epoch) {
        persistLocalSnapshot(record.key, record.durable);
        publish(record, record.durable);
      }
    } catch (cause) {
      if (host && isUnsupported(cause)) {
        markComposerModelHistoryHostUnsupported(host);
        // Queued edits will apply their semantic mutations to fresh local data.
        // Do not persist the optimistic whole snapshot during negotiation.
        if (record.pendingWrites === 0)
          publish(record, readLocalSnapshot(record.key));
      } else if (!record.readFailureLogged) {
        record.readFailureLogged = true;
        logFailure("read", record.key, cause);
      }
    } finally {
      record.reading = false;
    }
  });
}

export function toggleComposerModelFavorite(
  targetId: string | null | undefined,
  modelId: string
): void {
  const record = ensureRecord(normalizeComposerModelTargetId(targetId));
  const id = modelId.trim();
  if (!id) return;
  applyEdit(record, {
    targetId: record.key,
    kind: "favorite",
    modelId: id,
    favorite: !record.snapshot.favorites.includes(id)
  });
}

export function recordComposerModelRecent(
  targetId: string | null | undefined,
  modelId: string
): void {
  const record = ensureRecord(normalizeComposerModelTargetId(targetId));
  const id = modelId.trim();
  if (id)
    applyEdit(record, { targetId: record.key, kind: "recent", modelId: id });
}

function mutate(
  snapshot: ComposerModelHistorySnapshot,
  input: ComposerModelHistoryUpdateInput
): ComposerModelHistorySnapshot {
  if (input.kind === "recent") {
    return {
      ...snapshot,
      recents: [...recordRecentComposerModel(snapshot.recents, input.modelId)]
    };
  }
  return {
    ...snapshot,
    favorites: input.favorite
      ? [...sanitizeComposerModelIdList([...snapshot.favorites, input.modelId])]
      : snapshot.favorites.filter((id) => id !== input.modelId)
  };
}

function localMutation(
  record: HistoryRecord,
  input: ComposerModelHistoryUpdateInput
): ComposerModelHistorySnapshot {
  // Another window can change localStorage after this menu opens. Merge a single
  // membership/recent mutation into the latest snapshot, never a cached list.
  const snapshot = mutate(readLocalSnapshot(record.key), input);
  persistLocalSnapshot(record.key, snapshot);
  return snapshot;
}

function applyEdit(
  record: HistoryRecord,
  input: ComposerModelHistoryUpdateInput
): void {
  if (
    !composerModelHistoryHost() &&
    !record.reading &&
    record.pendingWrites === 0
  ) {
    record.editEpoch += 1;
    record.durable = localMutation(record, input);
    publish(record, record.durable);
    return;
  }
  if (composerModelHistoryHost() && !record.hydrated && !record.reading)
    readHistory(record, false);
  const epoch = ++record.editEpoch;
  record.pendingWrites += 1;
  publish(record, mutate(record.snapshot, input));
  enqueue(record, async () => {
    const host = composerModelHistoryHost();
    try {
      const snapshot = host
        ? await host.updateComposerModelHistory(input)
        : localMutation(record, input);
      confirm(record, snapshot, epoch);
    } catch (cause) {
      if (host && isUnsupported(cause)) {
        markComposerModelHistoryHostUnsupported(host);
        confirm(record, localMutation(record, input), epoch);
      } else {
        logFailure("update", record.key, cause);
        if (record.editEpoch === epoch)
          publish(record, record.durable ?? readLocalSnapshot(record.key));
      }
    } finally {
      record.pendingWrites -= 1;
      publish(record, record.snapshot);
    }
  });
}

function confirm(
  record: HistoryRecord,
  snapshot: ComposerModelHistorySnapshot,
  epoch: number
): void {
  record.durable = normalizeSnapshot(snapshot);
  record.hydrated = composerModelHistoryHost() !== null;
  // Older responses remain rollback baselines but cannot erase newer clicks.
  if (record.editEpoch !== epoch) return;
  persistLocalSnapshot(record.key, record.durable);
  publish(record, record.durable);
}

function enqueue(record: HistoryRecord, task: () => Promise<void>): void {
  record.queue = record.queue.then(task, task);
}

function ensureRecord(key: string): HistoryRecord {
  const existing = records.get(key);
  if (existing) return existing;
  const snapshot = readLocalSnapshot(key);
  const record: HistoryRecord = {
    key,
    snapshot,
    durable: null,
    hydrated: false,
    reading: false,
    readFailureLogged: false,
    editEpoch: 0,
    pendingWrites: 0,
    queue: Promise.resolve(),
    listeners: new Set(),
    view: {
      favoriteModelIds: snapshot.favorites,
      recentModelIds: snapshot.recents,
      pending: false
    },
    source: {
      getSnapshot: () => record.view,
      subscribe: (listener) => {
        record.listeners.add(listener);
        readHistory(record, false);
        return () => {
          record.listeners.delete(listener);
          // Delay eviction until all already-queued writes have finished. A new
          // subscriber in that interval keeps this same record and queue.
          releaseAfterPending(record);
        };
      }
    }
  };
  records.set(key, record);
  return record;
}

function releaseAfterPending(record: HistoryRecord): void {
  const pending = record.queue;
  const release = () => {
    if (record.listeners.size > 0 || records.get(record.key) !== record) return;
    if (record.queue !== pending) {
      releaseAfterPending(record);
      return;
    }
    records.delete(record.key);
  };
  void pending.then(release, release);
}

function publish(
  record: HistoryRecord,
  snapshot: ComposerModelHistorySnapshot
): void {
  const same = (left: readonly string[], right: readonly string[]) =>
    left.length === right.length &&
    left.every((id, index) => id === right[index]);
  const favorites = same(record.snapshot.favorites, snapshot.favorites)
    ? record.snapshot.favorites
    : snapshot.favorites;
  const recents = same(record.snapshot.recents, snapshot.recents)
    ? record.snapshot.recents
    : snapshot.recents;
  record.snapshot = { favorites, recents };
  const pending = record.pendingWrites > 0;
  if (
    record.view.favoriteModelIds === favorites &&
    record.view.recentModelIds === recents &&
    record.view.pending === pending
  )
    return;
  record.view = {
    favoriteModelIds: favorites,
    recentModelIds: recents,
    pending
  };
  for (const listener of record.listeners) listener();
}

function normalizeSnapshot(
  snapshot: ComposerModelHistorySnapshot
): ComposerModelHistorySnapshot {
  return {
    favorites: [...sanitizeComposerModelIdList(snapshot.favorites)],
    recents: [...sanitizeComposerModelIdList(snapshot.recents)].slice(0, 5)
  };
}

function readLocalSnapshot(key: string): ComposerModelHistorySnapshot {
  // Production menus historically omitted the target and used "default". Seed
  // each newly addressed target from that legacy bucket without deleting it:
  // its original target ownership cannot be inferred. Explicit [] suppresses it.
  return {
    favorites: [
      ...parseComposerModelIdList(
        readLocalStorage(composerModelFavoritesStorageKey(key)) ??
          readLocalStorage(composerModelFavoritesStorageKey("default"))
      )
    ],
    recents: [
      ...parseComposerModelIdList(
        readLocalStorage(composerModelRecentsStorageKey(key)) ??
          readLocalStorage(composerModelRecentsStorageKey("default"))
      )
    ].slice(0, 5)
  };
}

function persistLocalSnapshot(
  key: string,
  snapshot: ComposerModelHistorySnapshot
): void {
  writeLocalStorage(
    composerModelFavoritesStorageKey(key),
    serializeComposerModelIdList(snapshot.favorites)
  );
  writeLocalStorage(
    composerModelRecentsStorageKey(key),
    serializeComposerModelIdList(snapshot.recents)
  );
}

function readLocalStorage(key: string): string | null {
  try {
    return typeof window === "undefined"
      ? null
      : window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeLocalStorage(key: string, value: string): void {
  try {
    if (typeof window !== "undefined") window.localStorage.setItem(key, value);
  } catch {
    return;
  } // Browser storage remains best-effort in standalone clients.
}

function isUnsupported(cause: unknown): boolean {
  return (
    cause instanceof Error &&
    cause.message === COMPOSER_MODEL_HISTORY_HOST_UNSUPPORTED_MESSAGE
  );
}

function logFailure(
  operation: "read" | "update",
  targetId: string,
  cause: unknown
): void {
  console.warn("[agent-gui] composer model history request failed", {
    operation,
    targetId,
    error: cause instanceof Error ? cause.message : String(cause)
  });
}
