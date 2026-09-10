import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  registerSessionLivenessHost,
  type SessionLivenessHost
} from "./sessionLivenessHost";
import {
  sessionsNeedingHostAttach,
  useHostSessionAttach,
  type HostSessionAttachCandidate
} from "./useHostSessionAttach";

let unregister: (() => void) | null = null;

function installHost(host: SessionLivenessHost): void {
  unregister?.();
  unregister = registerSessionLivenessHost(host);
}

function hostWithAttach(
  attach: SessionLivenessHost["attachSession"]
): SessionLivenessHost {
  return {
    querySessionLiveness: async () => ({ sessions: {} }),
    attachSession: attach
  };
}

function Probe({
  candidates
}: {
  candidates: HostSessionAttachCandidate[];
}): null {
  useHostSessionAttach(candidates);
  return null;
}

// 每次渲染都传新数组：真实调用方（会话栏）就是 flatMap 出来的新数组。
function renderProbe(candidates: HostSessionAttachCandidate[]): {
  rerender: (next: HostSessionAttachCandidate[]) => void;
} {
  const view = render(<Probe candidates={candidates.map((c) => ({ ...c }))} />);
  return {
    rerender: (next) =>
      view.rerender(<Probe candidates={next.map((c) => ({ ...c }))} />)
  };
}

afterEach(() => {
  unregister?.();
  unregister = null;
});

describe("sessionsNeedingHostAttach（补丁 0124）", () => {
  it("只挑「这一轮在跑、宿主却说不活」的那些", () => {
    expect(
      sessionsNeedingHostAttach([
        { id: "closed", working: true, hostLiveness: "closed" },
        { id: "unknown", working: true, hostLiveness: "unknown" },
        // 宿主说还活着：账挂着呢，什么都不用做。
        { id: "live", working: true, hostLiveness: "live" },
        // 闲着：没人在说话，补挂无从谈起。
        { id: "idle", working: false, hostLiveness: "closed" },
        // 第一拍还没回来：什么都不知道的时候不写状态。
        { id: "nodata", working: true, hostLiveness: undefined }
      ])
    ).toEqual(["closed", "unknown"]);
  });

  it("空 id 与重复 id 都不出现在结果里", () => {
    expect(
      sessionsNeedingHostAttach([
        { id: "  ", working: true, hostLiveness: "closed" },
        { id: "a", working: true, hostLiveness: "closed" },
        { id: "a", working: true, hostLiveness: "unknown" }
      ])
    ).toEqual(["a"]);
  });
});

describe("useHostSessionAttach（补丁 0124）", () => {
  it("看见 working + 不活就补挂一次，宿主报 live 之后不再补", async () => {
    const attach = vi
      .fn()
      .mockResolvedValue({ state: "live", taskId: "t-1", status: "queued" });
    installHost(hostWithAttach(attach));

    const view = renderProbe([
      { id: "s1", working: true, hostLiveness: "closed" }
    ]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
    expect(attach).toHaveBeenCalledWith({ agentSessionId: "s1" });

    // 宿主那边新挂的行要过几秒才起跑，这几秒里会话栏看到的还是同样的
    // 「working + closed」。补第二次就会把跟随协程堆起来。
    view.rerender([{ id: "s1", working: true, hostLiveness: "closed" }]);
    view.rerender([{ id: "s1", working: true, hostLiveness: "closed" }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));

    // 起跑之后宿主报 live，记忆解除；下一次再崩掉才允许重新补。
    view.rerender([{ id: "s1", working: true, hostLiveness: "live" }]);
    view.rerender([{ id: "s1", working: true, hostLiveness: "closed" }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(2));
  });

  it("宿主回 unsupported 之后永久停手", async () => {
    const attach = vi.fn().mockRejectedValue(new Error("unsupported"));
    installHost(hostWithAttach(attach));

    const view = renderProbe([
      { id: "s1", working: true, hostLiveness: "closed" }
    ]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
    view.rerender([{ id: "s2", working: true, hostLiveness: "closed" }]);
    view.rerender([{ id: "s3", working: true, hostLiveness: "closed" }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
  });

  it("其余错误允许下一次重试（宿主可能只是正在重启）", async () => {
    const attach = vi.fn().mockRejectedValue(new Error("backend restarting"));
    installHost(hostWithAttach(attach));

    const view = renderProbe([
      { id: "s1", working: true, hostLiveness: "closed" }
    ]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
    view.rerender([{ id: "s1", working: true, hostLiveness: "closed" }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(2));
  });

  it("老宿主只实现了 querySessionLiveness 时整档不启用", async () => {
    installHost({ querySessionLiveness: async () => ({ sessions: {} }) });
    // 没有 attachSession 就一句都不发；渲染不报错即可。
    renderProbe([{ id: "s1", working: true, hostLiveness: "closed" }]);
    await Promise.resolve();
  });
});
