import type { AgentGUIProviderReadinessGate } from "../../../types";
import type {
  AgentGUIComposerEditorBlockedReason,
  AgentGUIComposerGate,
  AgentGUIComposerSubmissionBlockedReason,
  AgentGUIRuntimeBlockedReason
} from "./agentGuiNodeTypes";

/**
 * Whether a settings update is still unsettled enough to hold the composer.
 *
 * Only the states where the request is genuinely outstanding count. The two
 * terminal ones do not, even though the daemon's durable settings may still
 * disagree with the composer:
 *
 * - `failed` is an explicit rejection. The request is over.
 * - `unknown` is a timeout or an invalid command result. The client already
 *   waited out its own `timeoutMs` for an answer and none came.
 *
 * Counting either as pending is what left the send button dead after a model
 * switch failed: nothing clears those statuses except another settings action,
 * so `canSubmit` stayed false for the rest of the conversation. The optimistic
 * value is already rolled back for both (see
 * SESSION_SETTINGS_STATUS_SHOWS_OPTIMISTIC_VALUE), so the composer shows the
 * durable settings and sending again is safe — a retry that races the daemon's
 * settings lock is serialized on the daemon side, not by disabling the user.
 *
 * The status is typed loosely because the two callers see it through different
 * shapes. An unrecognized status is treated as settled: holding the composer on
 * a value this function does not know is the failure mode being fixed here.
 */
export function composerSettingsUpdatePending(
  status: string | undefined
): boolean {
  return status === "inFlight" || status === "waitingForRuntime";
}

export interface ResolveAgentGUIComposerGateInput {
  activeConversationBusy: boolean;
  activeConversationId: string | null;
  activeEngineHasPendingInteractions: boolean;
  activeLiveState: "inactive" | "activating" | "active" | "failed";
  activeConversationResumeUnavailable: boolean;
  agentTargetsLoading: boolean;
  authBlocked: boolean;
  hasNonRetryableRecoveryFailure: boolean;
  isCollaboratorConversation: boolean;
  isCreatingConversation: boolean;
  isInterrupting: boolean;
  isSubmitting: boolean;
  pendingApproval: boolean;
  pendingInteractivePrompt: boolean;
  providerReadinessGate: AgentGUIProviderReadinessGate | null;
  selectedAgentTargetUnavailable: boolean;
  /**
   * True while a session settings update is unsettled (in flight, waiting for
   * runtime, timed out/unknown, or failed). Blocks submit until idle so send
   * cannot race UpdateSettings on the daemon settings lock.
   */
  settingsUpdatePending: boolean;
  sessionRuntimeBlockedReason: AgentGUIRuntimeBlockedReason | null;
  targetConnectionBlocked: boolean;
}

export function resolveAgentGUIComposerGate(
  input: ResolveAgentGUIComposerGateInput
): AgentGUIComposerGate {
  const conversationBusy = input.activeConversationBusy || input.isSubmitting;
  const sharingRevoked =
    input.sessionRuntimeBlockedReason === "agent_sharing_revoked";
  const runtime: AgentGUIComposerGate["runtime"] = sharingRevoked
    ? {
        status: "blocked",
        reason: "session_runtime",
        sessionRuntimeReason: "agent_sharing_revoked"
      }
    : input.targetConnectionBlocked
      ? {
          status: "blocked",
          reason: "target_connection",
          sessionRuntimeReason: null
        }
      : input.sessionRuntimeBlockedReason !== null
        ? {
            status: "blocked",
            reason: "session_runtime",
            sessionRuntimeReason: input.sessionRuntimeBlockedReason
          }
        : {
            status: "ready",
            reason: null,
            sessionRuntimeReason: null
          };
  const canQueue =
    input.activeConversationId !== null &&
    runtime.status === "ready" &&
    (conversationBusy || input.activeEngineHasPendingInteractions);
  const canSubmit =
    !input.agentTargetsLoading &&
    input.providerReadinessGate === null &&
    !input.selectedAgentTargetUnavailable &&
    runtime.status === "ready" &&
    input.activeLiveState !== "activating" &&
    input.activeLiveState !== "failed" &&
    !input.activeConversationResumeUnavailable &&
    !input.pendingApproval &&
    !input.pendingInteractivePrompt &&
    !input.authBlocked &&
    !input.settingsUpdatePending &&
    !conversationBusy &&
    !input.isCreatingConversation &&
    !input.isInterrupting;

  const hardBlockReason = resolveHardBlockReason(input, runtime);
  const editor = hardBlockReason
    ? {
        status: "blocked" as const,
        reason: hardBlockReason
      }
    : canQueue
      ? {
          status: "editable" as const,
          reason: null
        }
      : resolveEditorGate(input);
  const submission = hardBlockReason
    ? {
        status: "blocked" as const,
        reason: hardBlockReason
      }
    : canSubmit
      ? {
          status: "ready" as const,
          reason: null
        }
      : canQueue
        ? {
            status: "queue" as const,
            reason: "conversation_busy" as const
          }
        : {
            status: "blocked" as const,
            reason: resolveSubmissionBlockReason(input)
          };

  return {
    conversationBusy,
    runtime,
    editor,
    submission
  };
}

function resolveHardBlockReason(
  input: ResolveAgentGUIComposerGateInput,
  runtime: AgentGUIComposerGate["runtime"]
): AgentGUIComposerEditorBlockedReason | null {
  if (input.isCollaboratorConversation) {
    return "collaborator_read_only";
  }
  if (runtime.status === "blocked") {
    return "runtime_blocked";
  }
  if (input.hasNonRetryableRecoveryFailure) {
    return "non_retryable_recovery";
  }
  return null;
}

function resolveEditorGate(
  input: ResolveAgentGUIComposerGateInput
): AgentGUIComposerGate["editor"] {
  const reason: AgentGUIComposerEditorBlockedReason | null =
    input.providerReadinessGate !== null || input.selectedAgentTargetUnavailable
      ? "provider_readiness"
      : input.pendingApproval
        ? "pending_approval"
        : input.pendingInteractivePrompt
          ? "pending_interactive_prompt"
          : input.isSubmitting
            ? "submitting"
            : input.isInterrupting
              ? "interrupting"
              : input.isCreatingConversation
                ? "creating_conversation"
                : null;
  return reason
    ? { status: "blocked", reason }
    : { status: "editable", reason: null };
}

function resolveSubmissionBlockReason(
  input: ResolveAgentGUIComposerGateInput
): AgentGUIComposerSubmissionBlockedReason {
  if (input.agentTargetsLoading) return "agent_targets_loading";
  if (
    input.providerReadinessGate !== null ||
    input.selectedAgentTargetUnavailable
  ) {
    return "provider_readiness";
  }
  if (input.activeLiveState === "activating") return "activation_pending";
  if (input.activeLiveState === "failed") return "activation_failed";
  if (input.activeConversationResumeUnavailable) return "resume_unavailable";
  if (input.pendingApproval) return "pending_approval";
  if (input.pendingInteractivePrompt) return "pending_interactive_prompt";
  if (input.authBlocked) return "authentication_required";
  if (input.settingsUpdatePending) return "settings_update_pending";
  if (input.activeConversationBusy) return "conversation_busy";
  if (input.isCreatingConversation) return "creating_conversation";
  if (input.isSubmitting) return "submitting";
  if (input.isInterrupting) return "interrupting";
  return "conversation_busy";
}

export function isDifferentKnownConversationOwner(input: {
  conversationUserId?: string | null;
  currentUserId?: string | null;
}): boolean {
  const conversationUserId = input.conversationUserId?.trim() ?? "";
  const currentUserId = input.currentUserId?.trim() ?? "";
  if (
    !conversationUserId ||
    !currentUserId ||
    conversationUserId === "local" ||
    currentUserId === "local"
  ) {
    return false;
  }
  return conversationUserId !== currentUserId;
}

export function projectAgentGUIComposerGateControls(input: {
  gate: AgentGUIComposerGate;
  presentationEditorDisabled: boolean;
  presentationSubmitDisabled: boolean;
}): {
  canQueueWhileBusy: boolean;
  editorDisabled: boolean;
  submissionDisabled: boolean;
} {
  return {
    canQueueWhileBusy: input.gate.submission.status === "queue",
    editorDisabled:
      input.gate.editor.status === "blocked" ||
      input.presentationEditorDisabled,
    submissionDisabled:
      input.gate.submission.status === "blocked" ||
      input.presentationSubmitDisabled
  };
}
