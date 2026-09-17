import { afterEach, describe, expect, it } from "vitest";
import {
  HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES,
  hostSessionLivenessCacheSizeForTests,
  readHostSessionLivenessCache,
  resetHostSessionLivenessCacheForTests,
  writeHostSessionLivenessCache
} from "./hostSessionLivenessCache";

afterEach(() => {
  resetHostSessionLivenessCacheForTests();
});

describe("hostSessionLivenessCache", () => {
  it("按工作区隔离：同一会话号在不同工作区互不影响", () => {
    writeHostSessionLivenessCache(
      "ws-a",
      [["sess-1", { state: "closed", attached: false }]],
      new Set(["sess-1"])
    );
    writeHostSessionLivenessCache(
      "ws-b",
      [["sess-1", { state: "live", attached: true }]],
      new Set(["sess-1"])
    );

    expect(readHostSessionLivenessCache("ws-a", "sess-1")).toEqual({
      state: "closed",
      attached: false
    });
    expect(readHostSessionLivenessCache("ws-b", "sess-1")).toEqual({
      state: "live",
      attached: true
    });
  });

  it("权威回包只更新对应会话，不把别的条目清掉", () => {
    writeHostSessionLivenessCache(
      "ws-a",
      [
        ["sess-1", { state: "closed", attached: false }],
        ["sess-2", { state: "live", attached: true }]
      ],
      new Set(["sess-1", "sess-2"])
    );
    writeHostSessionLivenessCache(
      "ws-a",
      [["sess-2", { state: "closed", attached: false }]],
      new Set(["sess-2"])
    );

    expect(readHostSessionLivenessCache("ws-a", "sess-1")?.state).toBe(
      "closed"
    );
    expect(readHostSessionLivenessCache("ws-a", "sess-2")).toEqual({
      state: "closed",
      attached: false
    });
  });

  it("超出上限时按写入顺序丢掉最旧的，当前可见的不丢", () => {
    const firstBatch: Array<
      readonly [string, { state: "closed"; attached: false }]
    > = [];
    for (
      let index = 0;
      index < HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES;
      index += 1
    ) {
      firstBatch.push([`old-${index}`, { state: "closed", attached: false }]);
    }
    writeHostSessionLivenessCache(
      "ws-a",
      firstBatch,
      new Set(firstBatch.map(([id]) => id))
    );
    expect(hostSessionLivenessCacheSizeForTests()).toBe(
      HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES
    );

    writeHostSessionLivenessCache(
      "ws-a",
      [["fresh", { state: "live", attached: true }]],
      new Set(["fresh"])
    );

    expect(readHostSessionLivenessCache("ws-a", "old-0")).toBeUndefined();
    expect(readHostSessionLivenessCache("ws-a", "fresh")).toEqual({
      state: "live",
      attached: true
    });
    expect(hostSessionLivenessCacheSizeForTests()).toBe(
      HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES
    );
  });

  it("正在保护的可见 id 哪怕最旧也不驱逐", () => {
    writeHostSessionLivenessCache(
      "ws-a",
      [["keep", { state: "closed", attached: false }]],
      new Set(["keep"])
    );
    const filler: Array<readonly [string, { state: "live"; attached: true }]> =
      [];
    for (
      let index = 0;
      index < HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES;
      index += 1
    ) {
      filler.push([`n-${index}`, { state: "live", attached: true }]);
    }
    writeHostSessionLivenessCache(
      "ws-a",
      filler,
      new Set(["keep", ...filler.map(([id]) => id)])
    );

    expect(readHostSessionLivenessCache("ws-a", "keep")?.state).toBe("closed");
    expect(hostSessionLivenessCacheSizeForTests()).toBeGreaterThan(
      HOST_SESSION_LIVENESS_CACHE_MAX_ENTRIES
    );
  });
});
