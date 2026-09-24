package agentruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
	"github.com/tutti-os/tutti/packages/agent/daemon/runtime/codexproto"
)

// handleAppServerMessage routes codex app-server server->client traffic.
// Server requests (approvals, user-input questions) register pending resolver
// state and respond asynchronously; notifications are translated into activity
// events through the shared ACP turn normalizer so the rest of the daemon sees
// one event shape.
func (a *CodexAppServerAdapter) handleAppServerMessage(
	ctx context.Context,
	client *codexAppServerClient,
	session Session,
	turnID string,
	message acpMessage,
	normalizer *acpTurnNormalizer,
	emit EventSink,
	emitCommands CommandSnapshotSink,
) ([]activityshared.Event, error) {
	if message.Method == "" {
		return nil, nil
	}
	if len(message.ID) > 0 {
		switch message.Method {
		case appServerMethodCommandApproval,
			appServerMethodFileChangeApproval,
			appServerMethodPermissionsApproval,
			appServerMethodRequestUserInput,
			appServerMethodExecApprovalV1,
			appServerMethodPatchApprovalV1:
			return a.appServerServerRequest(ctx, client, session, turnID, message, normalizer, emit)
		default:
			err := fmt.Errorf("server request method %q is not supported", message.Method)
			if codexproto.IsKnownServerRequestMethod(message.Method) {
				// Schema-known background requests the daemon deliberately
				// declines (auth token refresh, attestation, sandbox setup)
				// get a silent -32601; a transcript failure card would show
				// users spurious red cards for background operations.
				slog.Debug(
					"agent session app-server declined known server request",
					"agent_session_id", session.AgentSessionID,
					"method", message.Method,
				)
			} else if emit != nil {
				emit(appServerUnsupportedServerRequestEvents(session, turnID, message, err))
			}
			_ = client.Respond(ctx, message.ID, nil, &acpError{Code: -32601, Message: err.Error()})
			return nil, nil
		}
	}
	var processingTurn *codexAppServerActiveTurn
	if appServerNotificationUsesNormalizer(message.Method) {
		processingTurn = a.activeTurnForNormalizer(session.AgentSessionID, normalizer)
		if processingTurn != nil {
			processingTurn.processMu.Lock()
			defer processingTurn.processMu.Unlock()
		}
	}
	reduction := newCodexAppServerReducer(a).ReduceNotification(client, session, turnID, message, normalizer, emitCommands)
	return reduction.Events, nil
}

func appServerNotificationUsesNormalizer(method string) bool {
	switch method {
	case appServerNotifyAgentMessageDelta, appServerNotifyReasoningDelta, appServerNotifyReasoningSummary,
		appServerNotifyItemStarted, appServerNotifyItemCompleted, appServerNotifyCommandOutputDelta,
		appServerNotifyPlanUpdated, appServerNotifyWarning:
		return true
	default:
		return false
	}
}

func (a *CodexAppServerAdapter) activeTurnForNormalizer(agentSessionID string, normalizer *acpTurnNormalizer) *codexAppServerActiveTurn {
	if a == nil || normalizer == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	session := a.sessions[agentSessionID]
	if session == nil || session.activeTurn == nil || session.activeTurn.normalizer != normalizer {
		return nil
	}
	return session.activeTurn
}

// appServerNoticeItems maps review thread items to a one-line system-notice
// banner. emitOnCompleted selects which lifecycle event carries the banner:
// enteredReviewMode rides item/started (it always fires), while
// exitedReviewMode rides the authoritative item/completed.
var appServerNoticeItems = map[string]struct {
	message         string
	emitOnCompleted bool
}{
	"enteredReviewMode": {message: "Code review started.", emitOnCompleted: false},
	"exitedReviewMode":  {message: "Code review finished.", emitOnCompleted: true},
}

const (
	appServerCompactingContextTitle     = "Compacting context."
	appServerContextCompactedTitle      = "Context compacted."
	appServerCompactionInterruptedTitle = "Context compaction interrupted."
	appServerCompactionAdvisoryMessage  = "Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted."

	// appServerSkillsBudgetNoticeKind marks codex's advisory that the skills
	// catalog overflowed its context budget. It is informational (every skill
	// is still usable), so the GUI renders it as a quiet localized note rather
	// than a red warning banner.
	appServerSkillsBudgetNoticeKind = "skills_budget"
)

// codex (ext/skills/src/render.rs) sends the skills-budget advisory as a plain
// `warning` with no machine-readable code, so the only stable handle is the
// wording. It has changed across releases (older builds said "the 2% skills
// context budget"), so match the stable openings plus the shared phrase rather
// than one exact sentence.
var appServerSkillsBudgetWarningPrefixes = []string{
	"skill descriptions were shortened to fit",
	"exceeded skills context budget",
}

func isAppServerSkillsBudgetWarning(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if !strings.Contains(normalized, "skills context budget") {
		return false
	}
	for _, prefix := range appServerSkillsBudgetWarningPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

// appServerCompactionNoticeEvent emits the compaction banner for both item
// lifecycle events. Both banners share one messageId keyed to the thread item
// so the "Context compacted." notice replaces the in-progress "Compacting
// context." notice in place instead of appending a second transcript row.
