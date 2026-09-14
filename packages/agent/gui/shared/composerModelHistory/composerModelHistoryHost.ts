/**
 * Optional, provider-neutral host port for composer model history.
 *
 * The composer model menus keep two small chrome lists per agent target:
 * favorite model ids and recent model picks. They used to live in
 * `window.localStorage` only. That is fine for a normal desktop window, but the
 * DinTalDock web build runs inside a host iframe whose origin is a random
 * loopback port; the port changes on every host restart or reinstall, so
 * `localStorage` silently belongs to a brand new origin each time and the user's
 * favorites look lost.
 *
 * The embedding host therefore registers an implementation here. The desktop
 * adapter (`apps/desktop`, embedded web build) maps these methods onto
 * `requestHostCapability`:
 *
 * - `readComposerModelHistory` -> capability `getComposerModelHistory`
 *   `args[0] = { targetId, legacyFavorites, legacyRecents }`
 * - `updateComposerModelHistory` -> capability `updateComposerModelHistory`
 *   `args[0] = { targetId, kind: "favorite" | "recent", modelId, favorite? }`
 *
 * Both answer with the authoritative snapshot
 * `{ favorites: string[], recents: string[] }` for that target.
 *
 * Contract the host owns:
 *
 * - data is per target and instance-local; it is independent of session, app
 *   version, and the iframe port;
 * - `targetId` is the trimmed target key, falling back to `"default"` (the same
 *   suffix the localStorage keys use);
 * - a read seeds the passed legacy lists only when no durable record exists for
 *   that target yet (an explicit empty record counts as existing); an existing
 *   record always wins over the passed legacy lists;
 * - `favorite` is the desired membership (a set), not a toggle, and writes are
 *   serialized read-modify-write with atomic file writes, so the returned
 *   snapshot can be trusted as the new state; `recent` keeps at most 5 ids.
 *
 * When no host is registered (plain web build, plain desktop window) callers
 * keep the localStorage behavior. When the host reports the capability as
 * unsupported, callers fall back to localStorage for the rest of the realm.
 *
 * Values from origins the user no longer runs cannot be recovered
 * automatically: an iframe cannot read another origin's `localStorage`, so only
 * the origin currently in use can seed a migration.
 */

export interface ComposerModelHistorySnapshot {
  favorites: string[];
  recents: string[];
}

export interface ComposerModelHistoryReadInput {
  /** Trimmed agent target key; `"default"` when the caller has no target. */
  targetId: string;
  /** Values found in this origin's localStorage, offered as a migration seed. */
  legacyFavorites: string[];
  legacyRecents: string[];
}

export interface ComposerModelHistoryUpdateInput {
  targetId: string;
  kind: "favorite" | "recent";
  modelId: string;
  /** Only for `kind: "favorite"`: desired membership, not a toggle. */
  favorite?: boolean;
}

export interface ComposerModelHistoryHost {
  readComposerModelHistory(
    input: ComposerModelHistoryReadInput
  ): Promise<ComposerModelHistorySnapshot>;
  updateComposerModelHistory(
    input: ComposerModelHistoryUpdateInput
  ): Promise<ComposerModelHistorySnapshot>;
}

/**
 * Error message the host adapter throws when the host cannot serve this
 * capability at all (not embedded, or an older host without it), as opposed to
 * a call that merely failed and may be retried. Callers treat it as "stop asking
 * this host" and fall back to localStorage.
 */
export const COMPOSER_MODEL_HISTORY_HOST_UNSUPPORTED_MESSAGE = "unsupported";

// Registration and capability negotiation share one lifecycle. Replacing a host
// resets its unsupported result; an old disposer cannot unregister the new host.
function createHostRegistry() {
  let registered: ComposerModelHistoryHost | null = null;
  let unsupported: ComposerModelHistoryHost | null = null;
  return {
    register(host: ComposerModelHistoryHost): () => void {
      registered = host;
      unsupported = null;
      return () => {
        if (registered === host) {
          registered = null;
          unsupported = null;
        }
      };
    },
    current(): ComposerModelHistoryHost | null {
      return registered === unsupported ? null : registered;
    },
    markUnsupported(host: ComposerModelHistoryHost): void {
      if (registered === host) unsupported = host;
    }
  };
}

const registry = createHostRegistry();
export const registerComposerModelHistoryHost = registry.register;
export const composerModelHistoryHost = registry.current;
export const markComposerModelHistoryHostUnsupported = registry.markUnsupported;
