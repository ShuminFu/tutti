import { render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  registerSessionLivenessHost,
  type HostSessionLivenessEntry,
  type SessionLivenessHost
} from "./sessionLivenessHost";
import {
  useHostSessionLiveness,
  type HostSessionLivenessMap
} from "./useHostSessionLiveness";

// 每个用例自己装一份宿主实现，跑完摘掉，免得互相串。
let unregister: (() => void) | null = null;

function installHost(host: SessionLivenessHost): void {
  unregister?.();
  unregister = registerSessionLivenessHost(host);
}

function entry(
  state: HostSessionLivenessEntry["state"]
): HostSessionLivenessEntry {
  return {
    state,
    status: state === "closed" ? "failed" : "running",
    taskId: "task-1"
  };
}

function Probe({
  ids,
  onRender
}: {
  ids: string[];
  onRender: (liveness: HostSessionLivenessMap) => void;
}): null {
  onRender(useHostSessionLiveness(ids));
  return null;
}

// 每次渲染都传一个新数组：真实调用方（会话栏）就是 flatMap 出来的新数组，
// 钩子必须靠内容而不是引用去判「要不要重新起一轮轮询」。
function renderProbe(ids: string[]): {
  latest: () => HostSessionLivenessMap;
  rerender: (nextIds: string[]) => void;
} {
  let latest: HostSessionLivenessMap = new Map();
  const view = render(
    <Probe ids={[...ids]} onRender={(value) => (latest = value)} />
  );
  return {
    latest: () => latest,
    rerender: (nextIds) =>
      view.rerender(
        <Probe ids={[...nextIds]} onRender={(value) => (latest = value)} />
      )
  };
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
});

afterEach(() => {
  unregister?.();
  unregister = null;
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
    await waitFor(() => expect(probe.latest().get("sess-a")).toBe("closed"));
    expect(probe.latest().get("sess-b")).toBe("live");
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

    renderProbe(["sess-a"]);
    await waitFor(() => expect(query).toHaveBeenCalledTimes(1));

    await vi.advanceTimersByTimeAsync(20_000);
    expect(query).toHaveBeenCalledTimes(1);
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
    await waitFor(() => expect(probe.latest().get("sess-a")).toBe("live"));
    const first = probe.latest();

    await vi.advanceTimersByTimeAsync(4000);
    await waitFor(() => expect(query).toHaveBeenCalledTimes(2));
    expect(probe.latest()).toBe(first);
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
