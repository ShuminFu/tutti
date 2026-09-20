import { Component, useMemo, useRef, useState, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { AgentGUIBottomDockPane } from "../../packages/agent/gui/agent-gui/agentGuiNode/view/AgentGUIBottomDockPane";
import { useAgentGUIDetailScroll } from "../../packages/agent/gui/agent-gui/agentGuiNode/view/useAgentGUIDetailScroll";
import { useAgentGUIViewLabels } from "../../packages/agent/gui/agent-gui/agentGuiNode/AgentGUINode.labels";
import { buildAgentComposerDraft } from "../../packages/agent/gui/agent-gui/agentGuiNode/model/agentComposerDraft";
import { translate } from "../../packages/agent/gui/i18n/runtime";
import type { AgentComposerProps } from "../../packages/agent/gui/agent-gui/agentGuiNode/AgentComposer";
import type { AgentConversationPromptVM } from "../../packages/agent/gui/shared/agentConversation/contracts/agentConversationVM";
import "./style.css";

const noop = () => {};
const sessionId = "layout-acceptance-session";
const emptyChrome = {
  auth: null,
  approval: null,
  recovery: null,
  rawState: null
};
const settings = {
  availableModels: [],
  availablePermissionModes: [{ label: "Default", value: "default" }],
  availableReasoningEfforts: [],
  availableSpeeds: [],
  draftSettings: {
    browserUse: false,
    computerUse: false,
    model: null,
    permissionModeId: "default",
    planMode: false,
    reasoningEffort: null,
    speed: null
  },
  isSettingsLoading: false,
  modelUnavailable: true,
  permissionModeUnavailable: false,
  planExclusiveWithPermissionMode: true,
  reasoningUnavailable: true,
  selectedPermissionModeValue: "default",
  sessionSettings: null,
  speedUnavailable: true,
  supportsModel: false,
  supportsPermissionMode: true,
  supportsPlanMode: true,
  supportsReasoningEffort: false,
  supportsSpeed: false
};
const approval: AgentConversationPromptVM = {
  kind: "approval",
  id: "approval:fixture",
  agentSessionId: sessionId,
  turnId: "turn-1",
  requestId: "approval-fixture",
  callId: "tool-1",
  title: "Verify layout",
  status: "waiting_approval",
  toolName: "Bash",
  input: {
    command: "pnpm test --run composer-layout",
    description: "Verify approval controls without moving the reply above."
  },
  options: [
    { id: "allow_once", label: "Allow once", kind: "allow_once" },
    { id: "reject_once", label: "Reject", kind: "reject_once" }
  ],
  output: null,
  occurredAtUnixMs: 1
};
const question: AgentConversationPromptVM = {
  kind: "ask-user",
  agentSessionId: sessionId,
  turnId: "turn-1",
  requestId: "question-fixture",
  title: "Layout options",
  questions: [
    {
      id: "scope",
      header: "Scope",
      question: "Which verification scope should be used?",
      options: [
        {
          id: "focused",
          label: "Focused",
          description: "Only the affected components"
        },
        { id: "wide", label: "Wide", description: "More integration coverage" }
      ],
      multiSelect: false
    },
    {
      id: "details",
      header: "Details",
      question: "Add any details for this verification",
      options: [],
      multiSelect: false
    }
  ]
};

function Fixture() {
  const [kind, setKind] = useState("none");
  const [size, setSize] = useState("regular");
  const [notice, setNotice] = useState(false);
  const [queued, setQueued] = useState(false);
  const [draft, setDraft] = useState(() =>
    buildAgentComposerDraft({ prompt: "Retain this draft" })
  );
  const [receipt, setReceipt] = useState("No action submitted");
  const timelineRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const dockRef = useRef<HTMLDivElement>(null);
  const anchorRef = useRef(null);
  const submittedRef = useRef(null);
  const prependRef = useRef(null);
  const virtualRef = useRef(null);
  const actions = useMemo(() => ({ loadOlderConversationMessages: noop }), []);
  const viewModel = useMemo(
    () => ({
      rail: { activeConversationId: sessionId },
      detail: { hasOlderMessages: false, isLoadingOlderMessages: false }
    }),
    []
  );
  const scroll = useAgentGUIDetailScroll({
    actions,
    bottomDockRef: dockRef,
    conversation: null,
    isVisible: true,
    pendingPrependScrollAnchorRef: prependRef,
    showTimelineSkeleton: false,
    submittedPromptScrollConversationRef: submittedRef,
    timelineConversationId: sessionId,
    timelineContentRef: contentRef,
    timelineRef,
    timelineScrollAnchorRef: anchorRef,
    virtualScrollControllerRef: virtualRef,
    viewModel
  } as Parameters<typeof useAgentGUIDetailScroll>[0]);
  const labels = useAgentGUIViewLabels({
    displayProviderLabel: "Agent",
    fallbackAgentTitle: "Acceptance",
    t: translate,
    workspaceAppIcons: [],
    workspaceId: "fixture"
  });
  const promptLabels = {
    ...labels,
    approvalLead: "Waiting for your approval",
    fileChangeApprovalLead: "Permission to edit files"
  };
  const goalLabels = {
    titleActive: labels.goalTitleActive,
    titlePaused: labels.goalTitlePaused,
    titleBlocked: labels.goalTitleBlocked,
    titleUsageLimited: labels.goalTitleUsageLimited,
    titleBudgetLimited: labels.goalTitleBudgetLimited,
    titleComplete: labels.goalTitleComplete,
    budgetUsage: labels.goalBudgetUsage,
    clearHint: labels.goalClearHint,
    editAction: labels.goalEditAction,
    pauseAction: labels.goalPauseAction,
    resumeAction: labels.goalResumeAction,
    clearAction: labels.goalClearAction
  };
  const prompt =
    kind === "approval" || kind === "lifted"
      ? approval
      : kind === "ask-user"
        ? question
        : null;
  const submit = (input: unknown) => {
    setReceipt(JSON.stringify(input));
    setKind("none");
    return true;
  };
  const composerProps: AgentComposerProps = {
    workspaceId: "fixture",
    agentSessionId: sessionId,
    provider: "codex",
    draftContent: draft,
    draftScopeKey: sessionId,
    availableCommands: [],
    availableSkills: [],
    gate: {
      conversationBusy: false,
      runtime: { status: "ready", reason: null, sessionRuntimeReason: null },
      editor: { status: "editable", reason: null },
      submission: { status: "ready", reason: null }
    },
    placeholder: "Type a long draft",
    composerSettings: settings,
    queuedPrompts: queued
      ? ([
          {
            id: "queued-1",
            content: [{ type: "text", text: "A queued prompt to retain" }],
            displayPrompt: "A queued prompt to retain",
            createdAtUnixMs: 1
          }
        ] as AgentComposerProps["queuedPrompts"])
      : [],
    drainingQueuedPromptId: null,
    showStopButton: false,
    stopDisabled: false,
    activePrompt: kind === "lifted" ? null : prompt,
    isInterrupting: false,
    isSendingTurn: false,
    isSubmittingPrompt: false,
    projectMissingProbeEnabled: false,
    canGoalControl: false,
    canUploadAttachment: false,
    labels: promptLabels as AgentComposerProps["labels"],
    onDraftContentChange: setDraft,
    onSettingsChange: noop,
    onSubmit: (input) => setReceipt(JSON.stringify(input)),
    onSendQueuedPromptNext: () => setQueued(false),
    onRemoveQueuedPrompt: () => setQueued(false),
    onEditQueuedPrompt: noop,
    onInterruptCurrentTurn: noop,
    onSubmitInteractivePrompt: submit
  };
  return (
    <>
      <h1>DINTAL-5324 — production dock / composer / prompt components</h1>
      <p>
        Deterministic local fixture; no provider calls or persisted data.
        Transcript follow mode: <strong>{scroll.followEndMode}</strong>
      </p>
      <div className="fixture-controls">
        {["none", "approval", "ask-user", "lifted", "plan"].map((value) => (
          <button key={value} onClick={() => setKind(value)}>
            {value}
          </button>
        ))}
        <button onClick={() => setNotice((value) => !value)}>
          Toggle notice + goal
        </button>
        <button onClick={() => setQueued((value) => !value)}>
          Toggle queue
        </button>
        <button
          onClick={() =>
            setDraft(
              buildAgentComposerDraft({
                prompt: Array.from(
                  { length: 30 },
                  (_, i) => `Draft line ${i + 1}`
                ).join("\n")
              })
            )
          }
        >
          Long draft
        </button>
        {["regular", "short", "narrow"].map((value) => (
          <button key={value} onClick={() => setSize(value)}>
            {value}
          </button>
        ))}
      </div>
      <div className="fixture-frame agent-gui-node" data-size={size}>
        <main className="agent-gui-node__detail">
          <div
            ref={timelineRef}
            className="agent-gui-node__timeline agent-gui-node__timeline-with-composer"
            data-testid="acceptance-transcript"
          >
            <div ref={contentRef}>
              {Array.from({ length: 36 }, (_, i) => (
                <article
                  key={i}
                  className="fixture-message"
                  data-agent-transcript-row
                >
                  <h2>Reply {i + 1}</h2>
                  <p>
                    A long reply remains readable while the agent requests
                    approval or asks a question. This content must not move,
                    shrink, or become obscured when the dock changes.
                  </p>
                </article>
              ))}
            </div>
          </div>
          <AgentGUIBottomDockPane
            bottomDockRef={dockRef}
            showScrollToBottom={!scroll.isTimelineScrolledToBottom}
            scrollToBottomLabel="Latest reply"
            onScrollToBottom={scroll.scrollTimelineToBottom}
            bottomDockLiftedPrompt={kind === "lifted" ? approval : null}
            bottomDockReplacementPrompt={
              kind === "plan"
                ? {
                    kind: "plan-implementation",
                    requestId: "plan-fixture",
                    title: "Verify layout"
                  }
                : null
            }
            composerProps={composerProps}
            approvalDisabledReason={null}
            interactivePromptDisabledReason={null}
            inlineNoticeChrome={
              notice
                ? ({
                    ...emptyChrome,
                    recovery: {
                      kind: "recoverable",
                      message:
                        "An independent notice remains available alongside the request",
                      canRetry: true
                    }
                  } as never)
                : null
            }
            isRespondingApproval={false}
            sessionChrome={
              notice
                ? {
                    ...emptyChrome,
                    rawState: {
                      goal: {
                        objective:
                          "Verify stable layout with several simultaneous components",
                        status: "active"
                      }
                    }
                  }
                : emptyChrome
            }
            keyboardShortcutsEnabled
            chromeLabels={labels as never}
            goalBannerLabels={goalLabels}
            promptLabels={promptLabels}
            onSubmitApprovalOption={noop}
            onRetryActivation={noop}
            onRetryInlineNotice={noop}
            onContinueInNewConversation={noop}
            onSubmitBottomDockInteractivePrompt={submit}
            onGoalControl={noop}
            goalPauseSupported={false}
            tuttiWorkflowDock={{ phase: null } as never}
            tuttiWorkflowDockLabels={labels as never}
            tuttiPlanPanelLabels={labels as never}
            tuttiPlanIssuePanelLabels={labels as never}
          />
        </main>
      </div>
      <output aria-label="Response receipt">{receipt}</output>
    </>
  );
}
class FixtureBoundary extends Component<
  { children: ReactNode },
  { error: string | null }
> {
  state = { error: null as string | null };
  static getDerivedStateFromError(error: Error) {
    return { error: error.stack ?? error.message };
  }
  render() {
    return this.state.error ? (
      <pre role="alert">{this.state.error}</pre>
    ) : (
      this.props.children
    );
  }
}
createRoot(document.getElementById("root")!, {
  onUncaughtError(error) {
    const output = document.createElement("pre");
    output.setAttribute("role", "alert");
    output.textContent =
      error instanceof Error ? (error.stack ?? error.message) : String(error);
    document.body.appendChild(output);
  }
}).render(
  <FixtureBoundary>
    <Fixture />
  </FixtureBoundary>
);
