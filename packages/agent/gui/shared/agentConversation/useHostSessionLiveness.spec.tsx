import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  resetHostPanelVisibilityForTests,
  setHostPanelVisible
} from "../hostPanelVisibility";
import {
  registerSessionLivenessHost,
  type HostSessionLivenessEntry,
  type SessionLivenessHost
} from "./sessionLivenessHost";
import { resetHostSessionLivenessCacheForTests } from "./hostSessionLivenessCache";
import {
  HOST_SESSION_LIVENESS_POLL_INTERVAL_MS,
  useHostSessionLiveness,
  type HostSessionLivenessMap
} from "./useHostSessionLiveness";

// 每个用例自己装一份宿主实现，跑完摘掉，免得互相串。
let unregister: (() => void) | null = null;

function installHost(host: SessionLivenessHost): void {
  unregister?.();
  unregister = registerSessionLivenessHost(host);
}

// attached 默认给 true：绝大多数用例只关心 state 那一维（补丁 0128 起 map 的
// 值是 { state, attached } 小对象）。
function entry(
  state: HostSessionLivenessEntry["state"],
  attached = true
): HostSessionLivenessEntry {
  return {
    state,
    status: state === "closed" ? "failed" : "running",
    taskId: "task-1",
    attached
  };
}

function Probe({
  ids,
  workspaceId,
  onRender
}: {
  ids: string[];
  workspaceId?: string;
  onRender: (liveness: HostSessionLivenessMap) => void;
}): null {
  onRender(useHostSessionLiveness(ids, workspaceId));
  return null;
}

// 每次渲染都传一个新数组：真实调用方（会话栏）就是 flatMap 出来的新数组，
// 钩子必须靠内容而不是引用去判「要不要重新起一轮轮询」。
function renderProbe(
  ids: string[],
  workspaceId?: string
): {
  latest: () => HostSessionLivenessMap;
  rerender: (nextIds: string[], nextWorkspaceId?: string) => void;
} {
  let latest: HostSessionLivenessMap = new Map();
  const view = render(
    <Probe
      ids={[...ids]}
      workspaceId={workspaceId}
      onRender={(value) => (latest = value)}
    />
  );
  return {
    latest: () => latest,
    rerender: (nextIds, nextWorkspaceId = workspaceId) =>
      view.rerender(
        <Probe
          ids={[...nextIds]}
          workspaceId={nextWorkspaceId}
          onRender={(value) => (latest = value)}
        />
      )
  };
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
});

afterEach(() => {
  cleanup();
  resetHostPanelVisibilityForTests();
  unregister?.();
  unregister = null;
  resetHostSessionLivenessCacheForTests();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("useHostSessionLiveness", () => {
  it("只问会话栏里现在画着的会话号，去重并排序", async () => {
    const query = vi.fn(async (_input: { agentSessionIds: string[] }) => ({
      sessions: { "sess-a": entry("closed"), "sess-b": entry("live") }
    }));
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-b", "sess-a", "sess-b", "", "  "]);

    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));
    expect(query.mock.calls[0]?.[0]).toEqual({
      agentSessionIds: ["sess-a", "sess-b"]
    });
    await waitFor(() =>
      expect(probe.latest().get("sess-a")).toEqual({
        state: "closed",
        attached: true
      })
    );
    expect(probe.latest().get("sess-b")).toEqual({
      state: "live",
      attached: true
    });
  });

  it("到点再拍一次；页面不可见那一拍跳过", async () => {
    const query = vi.fn(async () => ({
      sessions: { "sess-a": entry("live") }
    }));
    installHost({ querySessionLiveness: query });

    renderProbe(["sess-a"]);
    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));

    await vi.advanceTimersByTimeAsync(4000);
    expect(query).toHaveBeenCalledTimes(2);

    const visibility = vi
      .spyOn(document, "visibilityState", "get")
      .mockReturnValue("hidden");
    await vi.advanceTimersByTimeAsync(4000);
    expect(query).toHaveBeenCalledTimes(2);

    visibility.mockReturnValue("visible");
    await vi.advanceTimersByTimeAsync(4000);
    expect(query).toHaveBeenCalledTimes(3);
  });

  it("宿主回 unsupported 之后永久停拍", async () => {
    const query = vi.fn(async () => {
      throw new Error("unsupported");
    });
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"]);
    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));

    await vi.advanceTimersByTimeAsync(20_000);
    expect(query).toHaveBeenCalledTimes(1);
    // 不支持 ≠ 加载中：退回空表，presence 才能走 0119 绿点。
    expect(probe.latest().size).toBe(0);
  });

  it("上一拍没回来就不叠第二拍", async () => {
    // 收在数组里而不是 let 变量：TS 不跟踪回调里的赋值，用 let 会把类型收窄成 never。
    const releases: (() => void)[] = [];
    const query = vi.fn(
      () =>
        new Promise<{ sessions: Record<string, HostSessionLivenessEntry> }>(
          (resolve) => {
            releases.push(() =>
              resolve({ sessions: { "sess-a": entry("live") } })
            );
          }
        )
    );
    installHost({ querySessionLiveness: query });

    renderProbe(["sess-a"]);
    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));

    // 三拍过去，在途那一发还没回来 → 一发都不许补。
    await vi.advanceTimersByTimeAsync(12_000);
    expect(query).toHaveBeenCalledTimes(1);

    releases[0]?.();
    await vi.advanceTimersByTimeAsync(4000);
    expect(query).toHaveBeenCalledTimes(2);
  });

  it("宿主没装（没人注册端口）时一次都不问，返回空表", async () => {
    const probe = renderProbe(["sess-a"]);
    await vi.advanceTimersByTimeAsync(12_000);
    expect(probe.latest().size).toBe(0);
  });

  it("内容没变就不换 Map 对象，会话栏的 memo 才挡得住重渲染", async () => {
    const query = vi.fn(async () => ({
      sessions: { "sess-a": entry("live") }
    }));
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"]);
    await waitFor(() =>
      expect(probe.latest().get("sess-a")?.state).toBe("live")
    );
    const first = probe.latest();

    await vi.advanceTimersByTimeAsync(4000);
    await waitFor(() => expect(query).toHaveBeenCalledTimes(2));
    expect(probe.latest()).toBe(first);
  });

  it("宿主没回 attached 时兜成 false（老宿主：当它没在盯）", async () => {
    const query = vi.fn(async () => ({
      // 故意造一条老宿主的回包：只有 state，没有 attached。
      sessions: {
        "sess-a": { state: "live", status: "running", taskId: "t-1" }
      } as unknown as Record<string, HostSessionLivenessEntry>
    }));
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"]);
    await waitFor(() =>
      expect(probe.latest().get("sess-a")).toEqual({
        state: "live",
        attached: false
      })
    );
  });

  it("只有 attached 变了也要换新 Map（不然补挂那条路看不见变化）", async () => {
    let attached = false;
    const query = vi.fn(async () => ({
      sessions: { "sess-a": entry("live", attached) }
    }));
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"]);
    await waitFor(() =>
      expect(probe.latest().get("sess-a")?.attached).toBe(false)
    );
    const first = probe.latest();

    attached = true;
    await vi.advanceTimersByTimeAsync(4000);
    await waitFor(() =>
      expect(probe.latest().get("sess-a")?.attached).toBe(true)
    );
    expect(probe.latest()).not.toBe(first);
  });

  it("第一拍还没回来时可见 id 是 pending，不是空闲", async () => {
    const releases: (() => void)[] = [];
    const query = vi.fn(
      () =>
        new Promise<{ sessions: Record<string, HostSessionLivenessEntry> }>(
          (resolve) => {
            releases.push(() =>
              resolve({ sessions: { "sess-a": entry("closed") } })
            );
          }
        )
    );
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"]);
    expect(probe.latest().get("sess-a")).toEqual({
      state: "pending",
      attached: undefined
    });

    releases[0]?.();
    await waitFor(() =>
      expect(probe.latest().get("sess-a")?.state).toBe("closed")
    );
  });

  it("切走再切回来：缓存里的权威状态立刻在，不等下一拍", async () => {
    const query = vi.fn(async (input: { agentSessionIds: string[] }) => {
      const sessions: Record<string, HostSessionLivenessEntry> = {};
      for (const id of input.agentSessionIds) {
        sessions[id] = entry(id === "sess-a" ? "closed" : "live");
      }
      return { sessions };
    });
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"], "ws-1");
    await waitFor(() =>
      expect(probe.latest().get("sess-a")?.state).toBe("closed")
    );

    probe.rerender(["sess-b"], "ws-1");
    await waitFor(() =>
      expect(probe.latest().get("sess-b")?.state).toBe("live")
    );
    expect(probe.latest().has("sess-a")).toBe(false);

    probe.rerender(["sess-a"], "ws-1");
    expect(probe.latest().get("sess-a")).toEqual({
      state: "closed",
      attached: true
    });
  });

  it("工作区隔离：别的工作区缓存不能冒充当前会话状态", async () => {
    const query = vi.fn(async () => ({
      sessions: { "sess-a": entry("closed") }
    }));
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"], "ws-1");
    await waitFor(() =>
      expect(probe.latest().get("sess-a")?.state).toBe("closed")
    );

    probe.rerender(["sess-a"], "ws-2");
    expect(probe.latest().get("sess-a")).toEqual({
      state: "pending",
      attached: undefined
    });
  });

  it("被取消的旧回包不写缓存", async () => {
    const releases: Array<
      (value: { sessions: Record<string, HostSessionLivenessEntry> }) => void
    > = [];
    const query = vi.fn(
      (_input: { agentSessionIds: string[] }) =>
        new Promise<{ sessions: Record<string, HostSessionLivenessEntry> }>(
          (resolve) => {
            releases.push((value) => resolve(value));
          }
        )
    );
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"], "ws-1");
    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));

    probe.rerender(["sess-b"], "ws-1");
    await waitFor(() => expect(query).toHaveBeenCalledTimes(2));

    releases[0]?.({ sessions: { "sess-a": entry("live") } });
    releases[1]?.({ sessions: { "sess-b": entry("closed") } });
    await waitFor(() =>
      expect(probe.latest().get("sess-b")?.state).toBe("closed")
    );

    probe.rerender(["sess-a"], "ws-1");
    expect(probe.latest().get("sess-a")).toEqual({
      state: "pending",
      attached: undefined
    });
  });

  it("被取消的旧请求回 unsupported 不停拍、不清空当前视图", async () => {
    const releases: Array<{
      resolve: (value: {
        sessions: Record<string, HostSessionLivenessEntry>;
      }) => void;
      reject: (reason: unknown) => void;
    }> = [];
    const query = vi.fn(
      () =>
        new Promise<{ sessions: Record<string, HostSessionLivenessEntry> }>(
          (resolve, reject) => {
            releases.push({ resolve, reject });
          }
        )
    );
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a"], "ws-1");
    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));

    probe.rerender(["sess-b"], "ws-1");
    await waitFor(() => expect(query).toHaveBeenCalledTimes(2));

    releases[1]?.resolve({ sessions: { "sess-b": entry("closed") } });
    await waitFor(() =>
      expect(probe.latest().get("sess-b")?.state).toBe("closed")
    );

    releases[0]?.reject(new Error("unsupported"));
    await vi.advanceTimersByTimeAsync(0);
    expect(probe.latest().get("sess-b")?.state).toBe("closed");
    expect(probe.latest().size).toBe(1);

    await vi.advanceTimersByTimeAsync(4000);
    expect(query).toHaveBeenCalledTimes(3);
  });

  it("宿主面板隐藏时拆掉 4s 轮询，亮回来先补一拍再恢复", async () => {
    vi.useFakeTimers();
    const query = vi.fn(async () => ({
      sessions: { "sess-a": entry("live") }
    }));
    installHost({ querySessionLiveness: query });
    renderProbe(["sess-a"]);
    await vi.advanceTimersByTimeAsync(0);
    expect(query).toHaveBeenCalledTimes(1);
    query.mockClear();

    const intervalMs = HOST_SESSION_LIVENESS_POLL_INTERVAL_MS;
    await vi.advanceTimersByTimeAsync(3 * intervalMs);
    expect(query).toHaveBeenCalledTimes(3);
    expect(document.visibilityState).toBe("visible");

    act(() => setHostPanelVisible(false));
    await vi.advanceTimersByTimeAsync(10 * intervalMs);
    expect(query).toHaveBeenCalledTimes(3);
    expect(vi.getTimerCount()).toBe(0);

    act(() => setHostPanelVisible(true));
    expect(query).toHaveBeenCalledTimes(4);
    await vi.advanceTimersByTimeAsync(intervalMs);
    expect(query).toHaveBeenCalledTimes(5);
  });

  it("挂上时宿主面板已经隐藏：不轮询，第一次变可见才补一拍", async () => {
    vi.useFakeTimers();
    const query = vi.fn(async () => ({
      sessions: { "sess-a": entry("live") }
    }));
    installHost({ querySessionLiveness: query });
    act(() => setHostPanelVisible(false));
    renderProbe(["sess-a"]);
    await vi.advanceTimersByTimeAsync(
      10 * HOST_SESSION_LIVENESS_POLL_INTERVAL_MS
    );
    expect(query).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);

    act(() => setHostPanelVisible(true));
    expect(query).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(HOST_SESSION_LIVENESS_POLL_INTERVAL_MS);
    expect(query).toHaveBeenCalledTimes(2);
  });

  it("会话栏只是重排（同一批 id 换个顺序）不会多问一次", async () => {
    const query = vi.fn(async () => ({
      sessions: { "sess-a": entry("live") }
    }));
    installHost({ querySessionLiveness: query });

    const probe = renderProbe(["sess-a", "sess-b"]);
    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));

    probe.rerender(["sess-b", "sess-a"]);
    await vi.advanceTimersByTimeAsync(0);
    expect(query).toHaveBeenCalledTimes(1);
  });
});
