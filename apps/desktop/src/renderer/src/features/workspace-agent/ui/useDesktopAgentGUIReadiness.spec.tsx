import { act, cleanup, render } from "@testing-library/react";
import type { AgentGUIRuntime } from "@tutti-os/agent-gui";
import {
  resetHostPanelVisibilityForTests,
  setHostPanelVisible
} from "@tutti-os/agent-gui/host-panel-visibility";
import {
  InstantiationContext,
  InstantiationService,
  ServiceCollection
} from "@tutti-os/infra/di";
import type { DesktopComputerUseStatus } from "@shared/contracts/ipc";
import { afterEach, describe, expect, it, vi } from "vitest";
import { proxy } from "valtio";
import type { AgentProviderStatusSnapshot } from "../services/agentProviderStatusService.interface";
import { IAgentEnvService } from "../services/agentEnvService.interface.ts";
import { IAccountService } from "../../workspace-workbench/services/accountService.interface";
import {
  DESKTOP_COMPUTER_USE_STATUS_POLL_INTERVAL_MS,
  useDesktopAgentGUIReadiness
} from "./useDesktopAgentGUIReadiness.ts";

const POLL_MS = DESKTOP_COMPUTER_USE_STATUS_POLL_INTERVAL_MS;

const computerUseStatus: DesktopComputerUseStatus = {
  authorization: "unknown",
  installed: false,
  permissions: null,
  reason: "not-installed"
};

const providerStatusSnapshot = {
  capturedAt: "2026-01-01T00:00:00.000Z",
  defaultProvider: null,
  error: null,
  isLoading: false,
  pendingActions: [],
  statuses: []
} as AgentProviderStatusSnapshot;

function renderReadiness(input: {
  checkStatus: () => Promise<DesktopComputerUseStatus>;
  refresh: (providers?: string[]) => Promise<void>;
  sessionListener: { current: ((event: unknown) => void) | null };
}) {
  const services = new ServiceCollection();
  services.set(IAccountService, {
    _serviceBrand: undefined,
    dismissRegistrationCreditsReward: async () => undefined,
    logout: async () => undefined,
    refreshProductSummary: async () => undefined,
    refreshUserInfo: async () => undefined,
    startLogin: async () => undefined,
    store: proxy({
      error: null,
      loading: false,
      loginStatus: null,
      productSummary: null,
      productSummaryError: null,
      productSummaryLoading: false,
      signingIn: false,
      signingOut: false,
      user: null
    })
  });
  services.set(IAgentEnvService, {
    _serviceBrand: undefined
  } as never);
  const runtime = {
    subscribeSessionEvents(
      _workspaceId: string,
      listener: (event: unknown) => void
    ) {
      input.sessionListener.current = listener;
      return () => {
        input.sessionListener.current = null;
      };
    }
  } as AgentGUIRuntime;
  const computerUseApi = { checkStatus: input.checkStatus };
  const statusService = {
    ensureLoaded: vi.fn(async () => null),
    getSnapshot: () => providerStatusSnapshot,
    getStatus: () => null,
    refresh: input.refresh,
    subscribe: () => () => undefined
  };

  function Probe(): null {
    useDesktopAgentGUIReadiness({
      agentActivityRuntime: runtime,
      agentProviderStatusService: statusService as never,
      computerUseApi,
      host: undefined as never,
      provider: "codex",
      workspaceId: "ws-1"
    });
    return null;
  }

  return render(
    <InstantiationContext
      instantiationService={new InstantiationService(services)}
    >
      <Probe />
    </InstantiationContext>
  );
}

describe("useDesktopAgentGUIReadiness host panel visibility", () => {
  afterEach(() => {
    cleanup();
    resetHostPanelVisibilityForTests();
    vi.useRealTimers();
  });

  it("hides the 15s checkStatus interval and catches up when the panel is shown", async () => {
    vi.useFakeTimers();
    const checkStatus = vi.fn(async () => computerUseStatus);
    const refresh = vi.fn(async () => undefined);
    renderReadiness({
      checkStatus,
      refresh,
      sessionListener: { current: null }
    });
    await vi.advanceTimersByTimeAsync(0);
    expect(checkStatus).toHaveBeenCalledTimes(1);
    checkStatus.mockClear();

    await vi.advanceTimersByTimeAsync(3 * POLL_MS);
    expect(checkStatus).toHaveBeenCalledTimes(3);
    expect(document.visibilityState).toBe("visible");

    act(() => setHostPanelVisible(false));
    await vi.advanceTimersByTimeAsync(10 * POLL_MS);
    expect(checkStatus).toHaveBeenCalledTimes(3);
    expect(vi.getTimerCount()).toBe(0);

    act(() => setHostPanelVisible(true));
    expect(checkStatus).toHaveBeenCalledTimes(4);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    expect(checkStatus).toHaveBeenCalledTimes(5);
  });

  it("still refreshes provider status on an auth-failure event while hidden", async () => {
    vi.useFakeTimers();
    const checkStatus = vi.fn(async () => computerUseStatus);
    const refresh = vi.fn(async () => undefined);
    const sessionListener: { current: ((event: unknown) => void) | null } = {
      current: null
    };
    renderReadiness({ checkStatus, refresh, sessionListener });
    await vi.advanceTimersByTimeAsync(0);
    expect(sessionListener.current).toEqual(expect.any(Function));
    checkStatus.mockClear();
    refresh.mockClear();

    act(() => setHostPanelVisible(false));
    await vi.advanceTimersByTimeAsync(10 * POLL_MS);
    expect(checkStatus).not.toHaveBeenCalled();

    act(() => {
      sessionListener.current?.({
        data: { payload: { code: "auth_required" }, status: "failed" }
      });
    });
    await vi.advanceTimersByTimeAsync(0);
    expect(refresh).toHaveBeenCalledWith(["codex"]);
    expect(checkStatus).not.toHaveBeenCalled();
  });

  it("does not poll checkStatus when mounted while the host panel is already hidden", async () => {
    vi.useFakeTimers();
    act(() => setHostPanelVisible(false));
    const checkStatus = vi.fn(async () => computerUseStatus);
    renderReadiness({
      checkStatus,
      refresh: vi.fn(async () => undefined),
      sessionListener: { current: null }
    });
    await vi.advanceTimersByTimeAsync(10 * POLL_MS);
    expect(checkStatus).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);

    act(() => setHostPanelVisible(true));
    expect(checkStatus).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    expect(checkStatus).toHaveBeenCalledTimes(2);
  });
});
