import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentModelReasoningDropdown } from "./AgentComposerModelReasoningDropdown";
import type { AgentGUIComposerSettingsVM } from "./model/agentGuiNodeTypes";
import type { AgentComposerSettingsMenuLabels } from "./model/composerSettingsMenuModel";
import { resolveAgentGUIComposerGate } from "./model/agentGuiComposerGate";

const labels = {
  modelLabel: "Model",
  modelSelectionLabel: "Model selection",
  modelContextWindowSuffix: "context",
  modelTooltipVersionLabel: "Version",
  defaultModel: "Default",
  loadingOptions: "Loading",
  reasoningLabel: "Reasoning",
  reasoningDegreeLabel: "Degree",
  reasoningOptionDefault: "Default",
  reasoningOptionMinimal: "Minimal",
  reasoningOptionLow: "Low",
  reasoningOptionMedium: "Medium",
  reasoningOptionHigh: "High",
  reasoningOptionXHigh: "XHigh",
  reasoningOptionMax: "Max",
  reasoningOptionUltra: "Ultra",
  speedLabel: "Speed",
  speedSelectionLabel: "Speed",
  speedOptionStandard: "Standard",
  speedOptionFast: "Fast",
  permissionLabel: "Permission",
  planModeLabel: "Plan",
  modelDescriptions: {
    frontierComplexCoding: "",
    everydayCoding: "",
    smallFastCostEfficient: "",
    codingOptimized: "",
    ultraFastCoding: "",
    professionalLongRunning: ""
  },
  inheritedUnavailable: "Unavailable"
} as AgentComposerSettingsMenuLabels;

function settings(
  patch: Partial<AgentGUIComposerSettingsVM>
): AgentGUIComposerSettingsVM {
  return {
    availableModels: [],
    availableReasoningEfforts: [],
    availableSpeeds: [],
    draftSettings: {
      model: null,
      reasoningEffort: null,
      speed: null,
      planMode: false
    },
    isSettingsLoading: false,
    modelUnavailable: false,
    reasoningUnavailable: true,
    sessionSettings: null,
    speedUnavailable: true,
    supportsModel: true,
    supportsPlanMode: false,
    supportsReasoningEffort: false,
    supportsSpeed: false,
    ...patch
  };
}

function openMenu(): void {
  fireEvent.pointerDown(
    screen.getByRole("button", { name: "Model / Reasoning" }),
    {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse"
    }
  );
}

describe("composer model list dropdown", () => {
  it("shows 点击刷新 for a missing list and does not block send", () => {
    const gate = resolveAgentGUIComposerGate({
      activeConversationBusy: false,
      activeConversationId: "session-1",
      activeConversationResumeUnavailable: false,
      activeEngineHasPendingInteractions: false,
      activeLiveState: "active",
      agentTargetsLoading: false,
      authBlocked: false,
      hasNonRetryableRecoveryFailure: false,
      isCollaboratorConversation: false,
      isCreatingConversation: false,
      isInterrupting: false,
      isSubmitting: false,
      pendingApproval: false,
      pendingInteractivePrompt: false,
      providerReadinessGate: null,
      selectedAgentTargetUnavailable: false,
      sessionRuntimeBlockedReason: null,
      settingsUpdatePending: false,
      targetConnectionBlocked: false
    });
    render(
      <>
        <AgentModelReasoningDropdown
          composerSettings={settings({ modelListState: "missing" })}
          labels={labels}
          onSettingsChange={vi.fn()}
        />
        <button type="button" disabled={gate.submission.status !== "ready"}>
          发送
        </button>
      </>
    );

    expect(screen.getByText("Refresh")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "发送" })).toBeEnabled();
  });

  it("probes once when the menu opens on a missing list", () => {
    const onRequestModelList = vi.fn();
    render(
      <AgentModelReasoningDropdown
        composerSettings={settings({ modelListState: "missing" })}
        labels={labels}
        onRequestModelList={onRequestModelList}
        onSettingsChange={vi.fn()}
      />
    );
    openMenu();
    expect(onRequestModelList).toHaveBeenCalledTimes(1);
    expect(onRequestModelList).toHaveBeenCalledWith({ force: false });
  });

  it("probes once when the menu opens on a stale list", () => {
    const onRequestModelList = vi.fn();
    render(
      <AgentModelReasoningDropdown
        composerSettings={settings({
          availableModels: [{ label: "Opus", value: "opus" }],
          draftSettings: {
            model: "opus",
            planMode: false,
            reasoningEffort: null,
            speed: null
          },
          modelListState: "stale",
          selectedModelValue: "opus"
        })}
        labels={labels}
        onRequestModelList={onRequestModelList}
        onSettingsChange={vi.fn()}
      />
    );
    openMenu();
    expect(onRequestModelList).toHaveBeenCalledTimes(1);
    expect(onRequestModelList).toHaveBeenCalledWith({ force: false });
  });

  it("does not probe when the menu opens on a fresh list", () => {
    const onRequestModelList = vi.fn();
    render(
      <AgentModelReasoningDropdown
        composerSettings={settings({
          availableModels: [{ label: "Opus", value: "opus" }],
          draftSettings: {
            model: "opus",
            planMode: false,
            reasoningEffort: null,
            speed: null
          },
          modelListState: "fresh",
          selectedModelValue: "opus"
        })}
        labels={labels}
        onRequestModelList={onRequestModelList}
        onSettingsChange={vi.fn()}
      />
    );
    openMenu();
    expect(onRequestModelList).not.toHaveBeenCalled();
  });

  it("forces one probe from 刷新 and keeps the previous list after a failed refresh", () => {
    const onRequestModelList = vi.fn();
    render(
      <AgentModelReasoningDropdown
        composerSettings={settings({
          availableModels: [{ label: "Opus", value: "opus" }],
          draftSettings: {
            model: "opus",
            planMode: false,
            reasoningEffort: null,
            speed: null
          },
          modelListLastError: "probe failed",
          modelListState: "fresh",
          selectedModelValue: "opus"
        })}
        labels={labels}
        onRequestModelList={onRequestModelList}
        onSettingsChange={vi.fn()}
      />
    );
    openMenu();
    expect(screen.getAllByText("Opus").length).toBeGreaterThan(0);
    expect(screen.getByText("上次刷新失败")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Refresh"));
    expect(onRequestModelList).toHaveBeenCalledTimes(1);
    expect(onRequestModelList).toHaveBeenCalledWith({ force: true });
  });
});
