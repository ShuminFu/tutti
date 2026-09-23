import {
  SESSION_LIVENESS_MAX_IDS,
  type HostSessionLivenessState
} from "./sessionLivenessHost";

/**
 * 两批可见会话（切一次 Agent）够用；再多就按写入顺序 LRU 丢掉非当前可见的。
 * 当前正在问宿主的那批 id 不驱逐，避免屏幕上的行突然变回 pending。
 */
export const HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES =
  SESSION_LIVENESS_MAX_IDS * 2;

export interface CachedHostSessionLiveness {
  state: HostSessionLivenessState;
  attached: boolean;
}

function cacheKey(workspaceId: string, sessionId: string): string {
  return `${workspaceId}\0${sessionId}`;
}

const cache = new Map<string, CachedHostSessionLiveness>();

export function readHostSessionLivenessCache(
  workspaceId: string,
  sessionId: string
): CachedHostSessionLiveness | undefined {
  return cache.get(cacheKey(workspaceId, sessionId));
}

export function invalidateHostSessionLivenessCache(
  workspaceId: string,
  sessionIds: readonly string[]
): void {
  for (const sessionId of sessionIds) {
    cache.delete(cacheKey(workspaceId, sessionId));
  }
}

/**
 * 权威回包写入。只覆盖对应 workspace+session，不整表替换。
 * `protectSessionIds` 是这一拍正在问的可见 id，驱逐时跳过它们。
 */
export function writeHostSessionLivenessCache(
  workspaceId: string,
  entries: Iterable<readonly [string, CachedHostSessionLiveness]>,
  protectSessionIds: ReadonlySet<string>
): void {
  for (const [sessionId, record] of entries) {
    if (typeof sessionId !== "string" || sessionId.trim() === "") continue;
    const key = cacheKey(workspaceId, sessionId);
    // delete + set：插到 Map 尾部，驱逐时从头部（最久没写过的）开始。
    cache.delete(key);
    cache.set(key, {
      state: record.state,
      attached: record.attached === true
    });
  }
  const protectKeys = new Set<string>();
  for (const sessionId of protectSessionIds) {
    protectKeys.add(cacheKey(workspaceId, sessionId));
  }
  for (const key of cache.keys()) {
    if (cache.size <= HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES) break;
    if (protectKeys.has(key)) continue;
    cache.delete(key);
  }
}

export function hostSessionLivenessCacheSizeForTests(): number {
  return cache.size;
}

export function resetHostSessionLivenessCacheForTests(): void {
  cache.clear();
}
