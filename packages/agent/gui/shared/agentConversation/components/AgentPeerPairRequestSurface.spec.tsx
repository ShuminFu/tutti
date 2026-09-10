import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  registerPeerPairRequestHost,
  type PeerPairRequestHost,
  type PeerPairRequestView
} from "../peerPairRequestHost";
import { AgentPeerPairRequestSurface } from "./AgentPeerPairRequestSurface";

function side(sessionId: string, extra: Partial<PeerPairRequestView["to"]> = {}) {
  return {
    taskId: `task-${sessionId}`,
    sessionId,
    provider: "codex",
    alias: "",
    title: "",
    cwd: "",
    status: "running",
    ...extra
  };
}

function request(id: string): PeerPairRequestView {
  return {
    id,
    status: "pending",
    reason: "一起看这段 diff",
    createdAt: "2026-09-10T00:00:00Z",
    from: side("sess-a", { provider: "claude" }),
    to: side("sess-b", { title: "乙会话" })
  };
}

let unregister: (() => void) | null = null;
afterEach(() => {
  unregister?.();
  unregister = null;
});

function installHost(overrides: Partial<PeerPairRequestHost> = {}) {
  const host: PeerPairRequestHost = {
    listPendingPeerPairRequests: vi.fn(async () => ({ requests: [request("req-1")] })),
    decidePeerPairRequest: vi.fn(async ({ requestId }) => ({
      request: { ...request(requestId), status: "approved" }
    })),
    ...overrides
  };
  unregister = registerPeerPairRequestHost(host);
  return host;
}

describe("AgentPeerPairRequestSurface", () => {
  it("renders nothing when no host is registered", () => {
    const { container } = render(
      <AgentPeerPairRequestSurface agentSessionId="sess-a" />
    );
    expect(container.firstChild).toBeNull();
  });

  it("lists the session's pending requests and approves in place", async () => {
    const host = installHost();
    const { findByTestId, queryByTestId, getByTestId } = render(
      <AgentPeerPairRequestSurface agentSessionId="sess-a" />
    );
    await findByTestId("agent-peer-pair-request-req-1");
    expect(host.listPendingPeerPairRequests).toHaveBeenCalledWith({
      agentSessionId: "sess-a"
    });
    // 抬头带对端名字，原因原样展示。
    expect(getByTestId("agent-peer-pair-request-surface").textContent).toContain("乙会话");
    expect(getByTestId("agent-peer-pair-request-reason").textContent).toBe("一起看这段 diff");

    fireEvent.click(getByTestId("agent-peer-pair-request-req-1-approve"));
    await waitFor(() => {
      expect(host.decidePeerPairRequest).toHaveBeenCalledWith({
        requestId: "req-1",
        decision: "approve"
      });
    });
    await waitFor(() => {
      expect(queryByTestId("agent-peer-pair-request-req-1")).toBeNull();
    });
  });

  it("rejects in place and surfaces a host error without losing the card", async () => {
    const host = installHost({
      decidePeerPairRequest: vi.fn(async () => {
        throw new Error("request already decided");
      })
    });
    const { findByTestId, getByTestId } = render(
      <AgentPeerPairRequestSurface agentSessionId="sess-a" />
    );
    await findByTestId("agent-peer-pair-request-req-1");
    fireEvent.click(getByTestId("agent-peer-pair-request-req-1-reject"));
    await waitFor(() => {
      expect(host.decidePeerPairRequest).toHaveBeenCalledWith({
        requestId: "req-1",
        decision: "reject"
      });
    });
    expect((await findByTestId("agent-peer-pair-request-error")).textContent).toBe(
      "request already decided"
    );
    // 出错后立刻重拉一遍（第一次是挂载时的那一拍）。
    await waitFor(() => {
      expect(host.listPendingPeerPairRequests).toHaveBeenCalledTimes(2);
    });
  });

  it("stays hidden while an agent prompt owns the floating slot, and without a session id", async () => {
    const host = installHost();
    const { container, rerender } = render(
      <AgentPeerPairRequestSurface agentSessionId="sess-a" suppressed />
    );
    await waitFor(() => {
      expect(host.listPendingPeerPairRequests).toHaveBeenCalled();
    });
    expect(container.firstChild).toBeNull();
    rerender(<AgentPeerPairRequestSurface agentSessionId={null} />);
    expect(container.firstChild).toBeNull();
  });
});
