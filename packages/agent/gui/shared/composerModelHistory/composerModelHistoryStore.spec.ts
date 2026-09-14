import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ComposerModelHistoryHost,
  ComposerModelHistorySnapshot
} from "./composerModelHistoryHost";

const target = "local:extension:deepseek-harness";
const favoritesKey = `agent-gui:composer-model-favorites:${target}`;

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function flush() {
  for (let i = 0; i < 20; i += 1) await Promise.resolve();
}

async function loadStore(host?: ComposerModelHistoryHost) {
  const port = await import("./composerModelHistoryHost");
  if (host) port.registerComposerModelHistoryHost(host);
  return import("./composerModelHistoryStore");
}

describe("composer model history persistence", () => {
  let storage: Map<string, string>;

  beforeEach(() => {
    vi.resetModules();
    storage = new Map();
    vi.stubGlobal("window", {
      localStorage: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => storage.set(key, value)
      }
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("restores saved favorites into a new origin and preserves an empty saved record", async () => {
    let durable: ComposerModelHistorySnapshot | undefined;
    const host: ComposerModelHistoryHost = {
      readComposerModelHistory: vi.fn(async (input) => {
        durable ??= {
          favorites: input.legacyFavorites,
          recents: input.legacyRecents
        };
        return durable;
      }),
      updateComposerModelHistory: vi.fn(async (input) => {
        durable = {
          favorites: input.favorite ? [input.modelId] : [],
          recents: []
        };
        return durable;
      })
    };
    storage.set(favoritesKey, '["deepseek-flash"]');
    let store = await loadStore(host);
    store.hydrateComposerModelHistory(target);
    await flush();
    expect(durable?.favorites).toEqual(["deepseek-flash"]);

    // A new realm with empty localStorage models a changed loopback origin.
    vi.resetModules();
    storage.clear();
    store = await loadStore(host);
    store.hydrateComposerModelHistory(target);
    await flush();
    expect(store.composerModelHistoryView(target).favoriteModelIds).toEqual([
      "deepseek-flash"
    ]);
    store.toggleComposerModelFavorite(target, "deepseek-flash");
    await flush();

    vi.resetModules();
    storage.set(favoritesKey, '["deepseek-flash"]');
    store = await loadStore(host);
    store.hydrateComposerModelHistory(target);
    await flush();
    expect(store.composerModelHistoryView(target).favoriteModelIds).toEqual([]);
    expect(storage.get(favoritesKey)).toBe("[]");
  });

  it("orders hydration before rapid edits without overwriting newer intent or another target", async () => {
    const read = deferred<ComposerModelHistorySnapshot>();
    const firstWrite = deferred<ComposerModelHistorySnapshot>();
    const update = vi
      .fn<ComposerModelHistoryHost["updateComposerModelHistory"]>()
      .mockImplementationOnce(() => firstWrite.promise)
      .mockResolvedValueOnce({ favorites: [], recents: [] });
    const store = await loadStore({
      readComposerModelHistory: () => read.promise,
      updateComposerModelHistory: update
    });
    store.hydrateComposerModelHistory(target);
    store.toggleComposerModelFavorite(target, "deepseek-flash");
    store.toggleComposerModelFavorite(target, "deepseek-flash");
    await flush();
    expect(update).not.toHaveBeenCalled();
    read.resolve({ favorites: ["old-model"], recents: [] });
    await flush();
    expect(store.composerModelHistoryView(target).favoriteModelIds).toEqual([]);
    expect(
      store.composerModelHistoryView("other-target").favoriteModelIds
    ).toEqual([]);
    firstWrite.resolve({ favorites: ["deepseek-flash"], recents: [] });
    await flush();
    expect(update.mock.calls.map(([input]) => input.favorite)).toEqual([
      true,
      false
    ]);
    expect(store.composerModelHistoryView(target)).toMatchObject({
      favoriteModelIds: [],
      pending: false
    });
  });

  it("keeps clicks made while an older host negotiates unsupported", async () => {
    const read = deferred<ComposerModelHistorySnapshot>();
    const update = vi.fn();
    const store = await loadStore({
      readComposerModelHistory: () => read.promise,
      updateComposerModelHistory: update
    });
    store.hydrateComposerModelHistory(target);
    store.toggleComposerModelFavorite(target, "deepseek-flash");
    await flush();
    read.reject(new Error("unsupported"));
    await flush();
    expect(update).not.toHaveBeenCalled();
    expect(store.composerModelHistoryView(target).favoriteModelIds).toEqual([
      "deepseek-flash"
    ]);
    expect(storage.get(favoritesKey)).toBe('["deepseek-flash"]');
  });

  it("rolls back failed writes and reconciles on reopening without changing another target", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const read = vi
      .fn<ComposerModelHistoryHost["readComposerModelHistory"]>()
      .mockResolvedValueOnce({ favorites: ["saved"], recents: [] })
      .mockResolvedValueOnce({
        favorites: ["saved", "other-window"],
        recents: []
      });
    const store = await loadStore({
      readComposerModelHistory: read,
      updateComposerModelHistory: async () => {
        throw new Error("disk full");
      }
    });
    store.hydrateComposerModelHistory(target);
    await flush();
    store.toggleComposerModelFavorite(target, "new-model");
    await flush();
    expect(store.composerModelHistoryView(target).favoriteModelIds).toEqual([
      "saved"
    ]);
    expect(warn).toHaveBeenCalledOnce();
    store.hydrateComposerModelHistory(target, { force: true });
    await flush();
    expect(store.composerModelHistoryView(target).favoriteModelIds).toEqual([
      "saved",
      "other-window"
    ]);
    expect(
      store.composerModelHistoryView("other-target").favoriteModelIds
    ).toEqual([]);
  });

  it("merges local edits with another window's latest favorites and recents", async () => {
    const store = await loadStore();
    store.hydrateComposerModelHistory(target);
    storage.set(favoritesKey, '["another-window"]');
    store.recordComposerModelRecent(target, "new-recent");
    expect(JSON.parse(storage.get(favoritesKey)!)).toEqual(["another-window"]);
    storage.set(favoritesKey, '["another-window","second-window"]');
    store.toggleComposerModelFavorite(target, "my-model");
    expect(JSON.parse(storage.get(favoritesKey)!)).toEqual([
      "another-window",
      "second-window",
      "my-model"
    ]);
    expect(store.composerModelHistoryView(target).recentModelIds).toEqual([
      "new-recent"
    ]);
  });

  it("migrates the legacy production default bucket without reviving explicit empty target data", async () => {
    storage.set("agent-gui:composer-model-favorites:default", '["legacy"]');
    const read = vi
      .fn<ComposerModelHistoryHost["readComposerModelHistory"]>()
      .mockImplementation(async (input) => ({
        favorites: input.legacyFavorites,
        recents: input.legacyRecents
      }));
    const store = await loadStore({
      readComposerModelHistory: read,
      updateComposerModelHistory: vi.fn()
    });
    store.hydrateComposerModelHistory(target);
    await flush();
    expect(read.mock.calls[0]?.[0]).toMatchObject({
      targetId: target,
      legacyFavorites: ["legacy"]
    });
    storage.set("agent-gui:composer-model-favorites:other-target", "[]");
    store.hydrateComposerModelHistory("other-target");
    await flush();
    expect(
      store.composerModelHistoryView("other-target").favoriteModelIds
    ).toEqual([]);
    expect(storage.get("agent-gui:composer-model-favorites:default")).toBe(
      '["legacy"]'
    );
  });

  it("shares local favorites and limits recent picks without a host", async () => {
    const store = await loadStore();
    const changed = vi.fn();
    const unsubscribe = store
      .composerModelHistorySource(target)
      .subscribe(changed);
    store.toggleComposerModelFavorite(target, "deepseek-flash");
    for (let i = 0; i < 7; i += 1)
      store.recordComposerModelRecent(target, `model-${i}`);
    expect(changed).toHaveBeenCalled();
    expect(store.composerModelHistoryView(target).recentModelIds).toEqual([
      "model-6",
      "model-5",
      "model-4",
      "model-3",
      "model-2"
    ]);
    expect(storage.get(favoritesKey)).toBe('["deepseek-flash"]');
    unsubscribe();
  });
});
