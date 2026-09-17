package agentruntime

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

var (
	claudeACPPromptEvidenceSilence  = 8 * time.Second
	claudeACPPromptEvidenceInterval = 5 * time.Second
)

type acpPromptCallResult struct {
	result json.RawMessage
	err    error
	source string
}

func (a *standardACPAdapter) recoversClaudeACPPromptResult() bool {
	return a != nil &&
		a.config.provider == ProviderClaudeCode &&
		a.config.adapterName == "claude-agent-acp"
}

func (a *standardACPAdapter) callSessionPrompt(
	ctx context.Context,
	acpSession *standardACPSession,
	session Session,
	turnID string,
	promptParams any,
	normalizer *acpTurnNormalizer,
	handler func(context.Context, acpMessage) error,
) (json.RawMessage, error) {
	params := map[string]any{
		"sessionId": acpSession.providerSessionID,
		"prompt":    promptParams,
	}
	if !a.recoversClaudeACPPromptResult() {
		return acpSession.client.Call(ctx, acpMethodPrompt, params, handler)
	}
	return a.callSessionPromptRecoveringRootEvidence(
		ctx,
		acpSession,
		session,
		turnID,
		params,
		normalizer,
		handler,
	)
}

func (a *standardACPAdapter) callSessionPromptRecoveringRootEvidence(
	ctx context.Context,
	acpSession *standardACPSession,
	session Session,
	turnID string,
	params map[string]any,
	normalizer *acpTurnNormalizer,
	handler func(context.Context, acpMessage) error,
) (json.RawMessage, error) {
	var activityMu sync.Mutex
	lastActivity := time.Now()
	wrapped := func(ctx context.Context, message acpMessage) error {
		activityMu.Lock()
		lastActivity = time.Now()
		activityMu.Unlock()
		return handler(ctx, message)
	}
	callCtx, cancelCall := context.WithCancel(ctx)
	defer cancelCall()
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	results := make(chan acpPromptCallResult, 2)
	go func() {
		result, err := acpSession.client.Call(callCtx, acpMethodPrompt, params, wrapped)
		results <- acpPromptCallResult{result: result, err: err, source: "acp"}
	}()
	go a.watchClaudeACPPromptEvidence(
		watchCtx,
		acpSession,
		session,
		turnID,
		normalizer,
		&activityMu,
		&lastActivity,
		results,
	)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case outcome := <-results:
		stopWatch()
		if outcome.source == "transcript" {
			cancelCall()
			slog.Info("agent session ACP prompt recovered from provider transcript",
				"event", "agent_session.acp.exec.prompt_result_recovered",
				"provider", a.config.provider,
				"adapter", a.config.adapterName,
				"room_id", session.RoomID,
				"agent_session_id", session.AgentSessionID,
				"provider_session_id", firstNonEmpty(session.ProviderSessionID, acpSession.providerSessionID),
				"turn_id", turnID,
				"source", outcome.source,
			)
		}
		return outcome.result, outcome.err
	}
}

func (a *standardACPAdapter) watchClaudeACPPromptEvidence(
	ctx context.Context,
	acpSession *standardACPSession,
	session Session,
	turnID string,
	normalizer *acpTurnNormalizer,
	activityMu *sync.Mutex,
	lastActivity *time.Time,
	results chan<- acpPromptCallResult,
) {
	silence := claudeACPPromptEvidenceSilence
	if silence <= 0 {
		silence = 8 * time.Second
	}
	interval := claudeACPPromptEvidenceInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	timer := time.NewTimer(silence)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			activityMu.Lock()
			idleFor := time.Since(*lastActivity)
			pendingTools := normalizer.PendingToolCallCount()
			streamed := normalizer.CurrentAssistantText()
			activityMu.Unlock()
			if idleFor < silence {
				timer.Reset(silence - idleFor)
				continue
			}
			pendingApprovals := a.hasPendingApprovals(session.AgentSessionID)
			if pendingTools > 0 || pendingApprovals {
				slog.Info("agent session ACP prompt still waiting on live work",
					"event", "agent_session.acp.exec.prompt_result_pending",
					"provider", a.config.provider,
					"adapter", a.config.adapterName,
					"room_id", session.RoomID,
					"agent_session_id", session.AgentSessionID,
					"provider_session_id", firstNonEmpty(session.ProviderSessionID, acpSession.providerSessionID),
					"turn_id", turnID,
					"idle_for", idleFor,
					"pending_tool_calls", pendingTools,
					"pending_approvals", pendingApprovals,
				)
				timer.Reset(interval)
				continue
			}
			path := claudeACPTranscriptPath(
				claudeACPConfigDir(),
				session.CWD,
				firstNonEmpty(session.ProviderSessionID, acpSession.providerSessionID),
			)
			evidence, reason, ok := claudeACPRootTurnEvidenceFromTranscript(path, streamed)
			if !ok {
				slog.Info("agent session ACP prompt result still missing",
					"event", "agent_session.acp.exec.prompt_result_missing",
					"provider", a.config.provider,
					"adapter", a.config.adapterName,
					"room_id", session.RoomID,
					"agent_session_id", session.AgentSessionID,
					"provider_session_id", firstNonEmpty(session.ProviderSessionID, acpSession.providerSessionID),
					"turn_id", turnID,
					"idle_for", idleFor,
					"reason", reason,
				)
				timer.Reset(interval)
				continue
			}
			payload, err := json.Marshal(map[string]any{"stopReason": evidence.StopReason})
			if err != nil {
				timer.Reset(interval)
				continue
			}
			select {
			case results <- acpPromptCallResult{result: payload, source: "transcript"}:
			case <-ctx.Done():
			}
			return
		}
	}
}

func (a *standardACPAdapter) hasPendingApprovals(agentSessionID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	acpSession := a.sessions[strings.TrimSpace(agentSessionID)]
	if acpSession == nil {
		return false
	}
	for _, approval := range acpSession.pendingApprovals {
		if approval == nil {
			continue
		}
		state := approval.disposition()
		if state == pendingInteractiveRequestStatePending || state == pendingInteractiveRequestStateResolving {
			return true
		}
	}
	return false
}
