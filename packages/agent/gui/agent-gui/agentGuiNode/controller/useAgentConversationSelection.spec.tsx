import { act, renderHook } from "@testing-library/react";
import {
  createAgentSessionEngine,
  selectEngineSessionCanReload
} from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import type { AgentGUINodeData } from "../../../types";
import { createTestEngineCommandPort } from "../../../shared/testing/createTestAgentSessionEngine";
import { useAgentConversationSelection } from "./useAgentConversationSelection";

describe("useAgentConversationSelection", () => {
  it("reconciles detail when a selected rail session has no cached messages", () => {
    const active = { current: "recent-session" as string | null };
    const markPending = vi.fn();
    const ensureHydrated = vi.fn();
    const setLoading = vi.fn();
    const requestReveal = vi.fn();
    const hasConversationListQuery = vi.fn(() => true);
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending,
          setLoading
        },
        hasConversationListQuery,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() =>
      result.current.selectConversation("historical-session", {
        reveal: "external-open"
      })
    );

    expect(markPending).toHaveBeenCalledWith("historical-session");
    expect(setLoading).not.toHaveBeenCalled();
    expect(hasConversationListQuery).toHaveBeenCalledOnce();
    expect(ensureHydrated).toHaveBeenCalledWith("historical-session");
    expect(requestReveal).toHaveBeenCalledWith(
      "historical-session",
      "external-open"
    );
  });

  it("reuses cached detail when selecting another hydrated session", () => {
    const active = { current: "session-1" as string | null };
    const markPending = vi.fn();
    const ensureHydrated = vi.fn();
    const ensureStateHydrated = vi.fn();
    const setLoading = vi.fn();
    const data: AgentGUINodeData = {
      agentTargetId: null,
      lastActiveAgentSessionId: active.current,
      provider: "codex"
    };
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated,
          isHydrated: () => true,
          isStateHydrated: () => true,
          markPending,
          setLoading
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: {
          update: (updater) => {
            updater(data);
          }
        },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() => result.current.selectConversation("session-2"));

    expect(setLoading).toHaveBeenCalledWith(false);
    expect(markPending).not.toHaveBeenCalled();
    expect(ensureHydrated).not.toHaveBeenCalled();
    expect(ensureStateHydrated).not.toHaveBeenCalled();
  });

  it("hydrates authoritative state when cached messages came from a lightweight projection", () => {
    const active = { current: "session-1" as string | null };
    const ensureHydrated = vi.fn();
    const ensureStateHydrated = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated,
          isHydrated: () => true,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading: vi.fn()
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() => result.current.selectConversation("session-2"));

    expect(ensureHydrated).not.toHaveBeenCalled();
    expect(ensureStateHydrated).toHaveBeenCalledWith("session-2");
  });

  it("selects an optimistic pending session without reloading durable detail", () => {
    const active = { current: "session-b" as string | null };
    const ensureHydrated = vi.fn();
    const setLoading = vi.fn();
    const setIntent = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => true,
          forget: vi.fn(),
          isPending: (agentSessionId) => agentSessionId === "session-a"
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent
        }
      })
    );

    act(() => result.current.selectConversation("session-a"));

    expect(active.current).toBe("session-a");
    expect(setIntent).toHaveBeenCalledWith({
      tag: "active",
      id: "session-a",
      source: "user-selection"
    });
    expect(setLoading).toHaveBeenCalledWith(false);
    expect(ensureHydrated).not.toHaveBeenCalled();
  });

  it("does not reload Rail or detail for an activation that cannot reload", () => {
    const active = { current: "session-b" as string | null };
    const hasConversationListQuery = vi.fn(() => true);
    const ensureHydrated = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: () => false,
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading: vi.fn()
        },
        hasConversationListQuery,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (agentSessionId) => {
            active.current = agentSessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() => result.current.selectConversation("session-a"));

    expect(hasConversationListQuery).not.toHaveBeenCalled();
    expect(ensureHydrated).not.toHaveBeenCalled();
  });

  it("hydrates a historical session after its new activation failed", () => {
    const agentSessionId = "historical-session";
    const engine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({
        execute: async () => undefined
      }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    engine.dispatch({
      agentSessionId,
      agentTargetId: "target-1",
      clientSubmitId: "submit-1",
      content: [{ type: "text", text: "hello" }],
      cwd: "/workspace",
      expiresAtUnixMs: 45_001,
      mode: "new",
      requestedAtUnixMs: 1,
      requestId: "activation-1",
      type: "activation/requested",
      workspaceId: "workspace-1"
    });
    engine.dispatch({
      commandId: "activate:activation-1",
      commandType: "session/activate",
      correlationId: "activation-1",
      errorMessage: "Initial goal failed.",
      outcome: "failed",
      type: "engine/commandResult"
    });

    const active = { current: null as string | null };
    const ensureHydrated = vi.fn();
    const setIntent = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: (sessionId) =>
            selectEngineSessionCanReload(engine.getSnapshot(), sessionId),
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading: vi.fn()
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (sessionId) => {
            active.current = sessionId;
          },
          setComposerHome: vi.fn(),
          setIntent
        }
      })
    );

    act(() => result.current.selectConversation(agentSessionId));

    expect(setIntent).toHaveBeenCalledWith({
      id: agentSessionId,
      source: "user-selection",
      tag: "active"
    });
    expect(ensureHydrated).toHaveBeenCalledWith(agentSessionId);
  });

  // 宿主「打开这条会话」桥（收件箱 / 会话列表跳 DinTalDock）在同一次渲染里先
  // dispatch 一条 mode:"existing" 的 activation，再选中会话。Dock 已经挂着时
  // 选中紧接着就跑，activation 还停在 "requested" —— 旧判据会把装填历史整个跳过，
  // 而 existing 激活的结果从不带 messages，也没人再选一次，转录于是一直空白，
  // 直到用户发一句话才被实时事件补上（2026-09-16 真机）。
  it("hydrates while an existing-mode activation is still pending", () => {
    const agentSessionId = "dock-opened-session";
    const engine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({
        execute: async () => undefined
      }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    // 只请求、不给 commandResult：activation 停在 "requested"。
    engine.dispatch({
      agentSessionId,
      expiresAtUnixMs: 45_001,
      mode: "existing",
      requestedAtUnixMs: 1,
      requestId: "activation-existing-1",
      type: "activation/requested",
      workspaceId: "workspace-1"
    });
    expect(
      selectEngineSessionCanReload(engine.getSnapshot(), agentSessionId)
    ).toBe(true);

    const active = { current: null as string | null };
    const ensureHydrated = vi.fn();
    const { result } = renderHook(() =>
      useAgentConversationSelection({
        activation: {
          canReload: (sessionId) =>
            selectEngineSessionCanReload(engine.getSnapshot(), sessionId),
          forget: vi.fn(),
          isPending: () => false
        },
        conversations: {
          agentTargetIdFor: () => "local:codex",
          contains: () => true
        },
        detail: {
          ensureHydrated,
          ensureStateHydrated: vi.fn(),
          isHydrated: () => false,
          isStateHydrated: () => false,
          markPending: vi.fn(),
          setLoading: vi.fn()
        },
        hasConversationListQuery: () => true,
        isMounted: () => true,
        onMissingConversationListQuery: vi.fn(),
        persistence: { update: vi.fn() },
        rail: {
          clearRevealRequest: vi.fn(),
          requestReveal: vi.fn()
        },
        selection: {
          clearDetailError: vi.fn(),
          getActiveSessionId: () => active.current,
          setActiveSessionId: (sessionId) => {
            active.current = sessionId;
          },
          setComposerHome: vi.fn(),
          setIntent: vi.fn()
        }
      })
    );

    act(() => result.current.selectConversation(agentSessionId));

    expect(ensureHydrated).toHaveBeenCalledWith(agentSessionId);
  });

  // 反向：mode:"new" 的未决 activation 仍然要等 —— 它可能把临时会话号换成正式号，
  // 并带回权威 session 投影，抢先装填会读到错的那条。
  it("still defers hydration while a new-mode activation is pending", () => {
    const agentSessionId = "provisional-session";
    const engine = createAgentSessionEngine({
      clock: { nowUnixMs: () => 1 },
      commandPort: createTestEngineCommandPort({
        execute: async () => undefined
      }),
      identity: { origin: "test", workspaceId: "workspace-1" },
      scheduler: { schedule: () => ({ cancel() {} }) }
    });
    engine.dispatch({
      agentSessionId,
      agentTargetId: "target-1",
      clientSubmitId: "submit-1",
      content: [{ type: "text", text: "hello" }],
      cwd: "/workspace",
      expiresAtUnixMs: 45_001,
      mode: "new",
      requestedAtUnixMs: 1,
      requestId: "activation-new-1",
      type: "activation/requested",
      workspaceId: "workspace-1"
    });

    expect(
      selectEngineSessionCanReload(engine.getSnapshot(), agentSessionId)
    ).toBe(false);
  });
});
