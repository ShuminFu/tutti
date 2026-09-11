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

describe("sessionsNeedingHostAttach（补丁 0124；判据在 0128 换成 attached）", () => {
  it("只挑「这一轮在跑、宿主却没在盯」的那些", () => {
    expect(
      sessionsNeedingHostAttach([
        { id: "detached", working: true, hostAttached: false },
        // 宿主说在盯：账挂着呢，什么都不用做。
        { id: "attached", working: true, hostAttached: true },
        // 闲着：没人在说话，补挂无从谈起。
        { id: "idle", working: false, hostAttached: false },
        // 第一拍还没回来：什么都不知道的时候不写状态。
        { id: "nodata", working: true, hostAttached: undefined }
      ])
    ).toEqual(["detached"]);
  });

  // 钉死 2026-09-10 那次回归：宿主把 state 的数据源换成「tuttid 里有没有活的
  // ACP 进程」之后，用户一聊天 state 就恒为 "live"，旧判据（working && state
  // !== "live"）永远挑不出人，补挂整档成了死代码（坑144 的自愈路径失效）。
  it("state 是 live 但 attached 是 false 时必须挑中（今天坏掉的正是这一格）", () => {
    const hostSaysProcessAlive: HostSessionAttachCandidate & {
      state: "live";
    } = { id: "s1", working: true, hostAttached: false, state: "live" };
    expect(sessionsNeedingHostAttach([hostSaysProcessAlive])).toEqual(["s1"]);
  });

  it("空 id 与重复 id 都不出现在结果里", () => {
    expect(
      sessionsNeedingHostAttach([
        { id: "  ", working: true, hostAttached: false },
        { id: "a", working: true, hostAttached: false },
        { id: "a", working: true, hostAttached: false }
      ])
    ).toEqual(["a"]);
  });
});

describe("useHostSessionAttach（补丁 0124；判据在 0128 换成 attached）", () => {
  it("看见 working + 没在盯就补挂一次，宿主报 attached 之后不再补", async () => {
    const attach = vi.fn().mockResolvedValue({
      state: "live",
      taskId: "t-1",
      status: "queued",
      attached: true
    });
    installHost(hostWithAttach(attach));

    const view = renderProbe([
      { id: "s1", working: true, hostAttached: false }
    ]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
    expect(attach).toHaveBeenCalledWith({ agentSessionId: "s1" });

    // 宿主那边新挂的行要过几秒才起跑，这几秒里会话栏看到的还是同样的
    // 「working + 没在盯」。补第二次就会把跟随协程堆起来。
    view.rerender([{ id: "s1", working: true, hostAttached: false }]);
    view.rerender([{ id: "s1", working: true, hostAttached: false }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));

    // 起跑之后宿主报「在盯了」，记忆解除；下一次账再断才允许重新补。
    view.rerender([{ id: "s1", working: true, hostAttached: true }]);
    view.rerender([{ id: "s1", working: true, hostAttached: false }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(2));
  });

  it("宿主回 unsupported 之后永久停手", async () => {
    const attach = vi.fn().mockRejectedValue(new Error("unsupported"));
    installHost(hostWithAttach(attach));

    const view = renderProbe([
      { id: "s1", working: true, hostAttached: false }
    ]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
    view.rerender([{ id: "s2", working: true, hostAttached: false }]);
    view.rerender([{ id: "s3", working: true, hostAttached: false }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
  });

  it("其余错误允许下一次重试（宿主可能只是正在重启）", async () => {
    const attach = vi.fn().mockRejectedValue(new Error("backend restarting"));
    installHost(hostWithAttach(attach));

    const view = renderProbe([
      { id: "s1", working: true, hostAttached: false }
    ]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(1));
    view.rerender([{ id: "s1", working: true, hostAttached: false }]);
    await waitFor(() => expect(attach).toHaveBeenCalledTimes(2));
  });

  it("老宿主只实现了 querySessionLiveness 时整档不启用", async () => {
    installHost({ querySessionLiveness: async () => ({ sessions: {} }) });
    // 没有 attachSession 就一句都不发；渲染不报错即可。
    renderProbe([{ id: "s1", working: true, hostAttached: false }]);
    await Promise.resolve();
  });
});
