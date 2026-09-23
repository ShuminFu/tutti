package agentruntime

import (
	"context"
	"errors"
	"strings"
	"time"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

const claudeSDKCloseTimeout = 10 * time.Minute

func (a *ClaudeCodeSDKAdapter) Start(ctx context.Context, session Session) ([]activityshared.Event, error) {
	if a == nil || a.transport == nil {
		return nil, ErrSessionDisconnected
	}
	contract, err := rndmasterContractFromSession(session)
	if err != nil {
		return nil, err
	}
	session.Env = rndmasterEnvList(session.Env, contract.Env)
	restore := strings.TrimSpace(session.ProviderSessionID) != ""
	providerSessionID := firstNonEmpty(strings.TrimSpace(session.ProviderSessionID), newID())
	session.ProviderSessionID = providerSessionID
	spec, cleanup, err := prepareProviderLaunch(ctx, a.preparer, session, ProcessSpec{
		Provider:           ProviderClaudeCode,
		AgentSessionID:     session.AgentSessionID,
		RootAgentSessionID: session.RootAgentSessionID,
		RoomID:             session.RoomID,
		CWD:                session.CWD,
		Command:            claudeSDKSidecarCommand(session.Env),
		Env:                claudeSDKSidecarEnv(session),
		DirectStart:        true,
	})
	if err != nil {
		return nil, err
	}
	launchSession := session
	launchSession.CWD = spec.CWD
	launchSession.Env = append([]string(nil), spec.Env...)
	conn, err := a.transport.Start(ctx, spec)
	if err != nil {
		cleanupPreparedLaunch(cleanup)
		return nil, err
	}
	trackInputUnits := providerInputUnitsEnabled(conn)
	conn = wrapProviderLaunchCleanup(conn, cleanup)
	adapterSession := &claudeSDKAdapterSession{
		conn:              conn,
		reader:            newClaudeSDKLineReader(conn, trackInputUnits),
		session:           session,
		providerSessionID: providerSessionID,
		resumeCursor:      claudeSDKResumeCursorFromSession(session),
		assistantMessages: make(map[string]string),
		thinkingMessages:  make(map[string]string),
		compactMessages:   make(map[string]claudeSDKCompactMessage),
		pendingRequests:   make(map[string]*pendingInteractiveRequest),
		pendingResponses:  make(map[string]chan claudeSDKSidecarEvent),
		turns:             make(map[string]*claudeSDKTurnWaiter),
		liveState:         newClaudeSDKLiveState(),
	}
	a.storeSession(session.AgentSessionID, adapterSession)
	a.emitCommandSnapshot(claudeSDKCommandSnapshot(session.AgentSessionID, adapterSession.liveState))
	// Continue-session tapes attach after the provider connection is already
	// initialized, so their first outbound is exec. Skip the cold start
	// bootstrap on attached-live-connection replay exactly as Codex/ACP do.
	if processCassetteCaptureOrigin(conn) ==
		ProcessCassetteCaptureOriginAttachedLiveConnection {
		if !restore {
			_ = conn.Close()
			a.removeSession(session.AgentSessionID, adapterSession)
			return nil, errors.New(
				"attached live Claude replay requires a restored provider session id",
			)
		}
		return []activityshared.Event{newSessionActivityEvent(
			session,
			EventSessionStarted,
			SessionStatusReady,
			claudeSDKRuntimeContext(session, adapterSession),
		)}, nil
	}
	startPayload := map[string]any{
		"agentSessionId":    session.AgentSessionID,
		"providerSessionId": providerSessionID,
		"cwd":               launchSession.CWD,
		"env":               envListToMap(launchSession.Env),
		"restore":           restore,
		"permissionModeId":  session.PermissionModeID,
		"settings":          claudeSDKSessionSettingsPayload(session),
		"resumeCursor":      claudeSDKResumeCursorFromSession(session),
		"mcpServers":        claudeSDKMCPServers(session.MCPServers),
	}
	for key, value := range claudeCodeSDKStartOptions(session, contract) {
		startPayload[key] = value
	}
	if err := adapterSession.send(claudeSDKSidecarRequest{
		ID:      newID(),
		Type:    "start",
		Payload: startPayload,
	}); err != nil {
		_ = conn.Close()
		a.removeSession(session.AgentSessionID, adapterSession)
		return nil, err
	}

	for {
		event, err := adapterSession.reader.next(ctx)
		if err != nil {
			_ = conn.Close()
			a.removeSession(session.AgentSessionID, adapterSession)
			return nil, err
		}
		eventCtx := context.Background()
		if event.inputUnit != nil {
			eventCtx = contextWithProviderInputUnit(eventCtx, *event.inputUnit)
		}
		endInputUnit := a.inputUnits.begin(eventCtx, session.AgentSessionID)
		next := a.applySidecarSessionEvent(adapterSession, session, event)
		next = a.inputUnits.stamp(session.AgentSessionID, next)
		endInputUnit()
		if next != nil {
			a.mu.Lock()
			adapterSession.session = applySessionEvents(session, next)
			a.mu.Unlock()
			// Controller.Start applies and publishes the returned events. Complete
			// asynchronously so an exact replay barrier can wait for that publish
			// without deadlocking inside Adapter.Start.
			go func() {
				if completeErr := completeClaudeSDKProviderInputUnit(
					context.Background(),
					adapterSession.conn,
					event,
				); completeErr != nil {
					a.failClaudeSDKReader(
						session.AgentSessionID,
						adapterSession,
						completeErr,
					)
				}
			}()
			return next, nil
		}
		if err := completeClaudeSDKProviderInputUnit(
			ctx,
			adapterSession.conn,
			event,
		); err != nil {
			_ = conn.Close()
			a.removeSession(session.AgentSessionID, adapterSession)
			return nil, err
		}
		if event.Type == "error" {
			_ = conn.Close()
			a.removeSession(session.AgentSessionID, adapterSession)
			return nil, errors.New(payloadString(event.Payload, "error"))
		}
	}
}

func claudeSDKMCPServers(bindings []MCPServerBinding) map[string]any {
	result := make(map[string]any)
	for _, binding := range bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" || strings.TrimSpace(binding.Type) != "http" || strings.TrimSpace(binding.URL) == "" {
			continue
		}
		headers := make(map[string]string, len(binding.Headers))
		for key, value := range binding.Headers {
			headers[key] = value
		}
		result[name] = map[string]any{"type": "http", "url": binding.URL, "headers": headers}
	}
	return result
}

func (a *ClaudeCodeSDKAdapter) Resume(ctx context.Context, session Session) error {
	if strings.TrimSpace(session.ProviderSessionID) == "" {
		return ErrSessionDisconnected
	}
	previous := a.getSession(session.AgentSessionID)
	_, err := a.Start(ctx, session)
	if err != nil && previous != nil {
		a.restorePreviousSession(session.AgentSessionID, previous)
	}
	if err == nil && previous != nil {
		a.removeSession(session.AgentSessionID, previous)
		_ = previous.conn.Close()
	}
	return classifyClaudeSDKResumeError(session, err)
}

func (*ClaudeCodeSDKAdapter) CanResume(session Session) bool {
	return strings.TrimSpace(session.ProviderSessionID) != ""
}

func (a *ClaudeCodeSDKAdapter) Close(ctx context.Context, session Session) error {
	adapterSession := a.getSession(session.AgentSessionID)
	if adapterSession == nil {
		return nil
	}
	a.mu.Lock()
	readerStarted := adapterSession.readerStarted
	a.mu.Unlock()
	if !readerStarted {
		if err := a.startClaudeSDKReader(session.AgentSessionID, adapterSession); err != nil {
			a.removeSession(session.AgentSessionID, adapterSession)
			_ = adapterSession.conn.Close()
			return err
		}
	}
	closeBaseCtx := context.WithoutCancel(ctx)
	var closeCtx context.Context
	var cancel context.CancelFunc
	if deadline, ok := ctx.Deadline(); ok {
		closeCtx, cancel = context.WithDeadline(closeBaseCtx, deadline)
	} else {
		closeCtx, cancel = context.WithTimeout(closeBaseCtx, claudeSDKCloseTimeout)
	}
	defer cancel()
	if err := a.roundTripClaudeSDK(closeCtx, session.AgentSessionID, adapterSession, claudeSDKSidecarRequest{
		ID:   newID(),
		Type: "close",
		Payload: map[string]any{
			"agentSessionId": session.AgentSessionID,
		},
	}); err != nil {
		a.removeSession(session.AgentSessionID, adapterSession)
		_ = adapterSession.conn.Close()
		return err
	}
	a.removeSession(session.AgentSessionID, adapterSession)
	if graceful, ok := adapterSession.conn.(GracefulProcessConnection); ok {
		_ = graceful.CloseInput()
	}
	return adapterSession.conn.Close()
}

func (a *ClaudeCodeSDKAdapter) HasLiveSession(session Session) bool {
	adapterSession := a.getSession(session.AgentSessionID)
	return a.sessionIsUsable(session.AgentSessionID, adapterSession)
}

// ReleaseLiveSession 把一条闲置 Claude Code 会话的进程还给系统，供空闲回收器
// （Controller.ReleaseIdleLiveSessions）与人工释放（Controller.ReleaseLiveSessionNow）
// 调用。
//
// 这里刻意**不走 Close**（历史实现直接 `return a.Close(ctx, session)`，是错的）：
// Close 会向 sidecar 发一条协议级 `close`，那是在告诉对端「这条会话到此为止」，
// 对端据此丢掉会话状态，之后续聊未必接得回来。空闲回收的意图恰恰相反 —— 只想
// 省掉闲着的进程，用户下次续聊必须原样接上。所以只丢本地这一侧：adapter 的
// sessions 表项、进程连接（连带 sidecar 子进程）。会话记录与 providerSessionID
// 留在控制器里不动，CanResume 仍为真，下一次 Resume 会重新拉起进程。
// 这与 codex 的 closeLiveSession、standard ACP 的 ReleaseLiveSession 同构。
func (a *ClaudeCodeSDKAdapter) ReleaseLiveSession(_ context.Context, session Session) error {
	if a == nil {
		return nil
	}
	agentSessionID := strings.TrimSpace(session.AgentSessionID)
	if a.hasLiveSessionWork(agentSessionID) {
		return ErrLiveSessionBusy
	}
	adapterSession := a.getSession(agentSessionID)
	if adapterSession == nil {
		return nil
	}
	if !a.removeSession(agentSessionID, adapterSession) {
		// 并发下这条已经被别处换掉/摘掉：那条连接不归这次调用管。
		return nil
	}
	if adapterSession.conn == nil {
		return nil
	}
	return adapterSession.conn.Close()
}

// hasLiveSessionWork 判断这条 Claude 会话此刻是不是还在干活：有在飞的回合
// （claudeSDKTurnWaiter 还没收尾），或有挂起/正在解析的交互请求（审批、提问）。
// 忙就表态 ErrLiveSessionBusy —— 绝不把用户正在用的会话掐掉。判据形状与
// standardACPAdapter.hasLiveSessionWork / CodexAppServerAdapter.hasLiveSessionWork
// 同源。
func (a *ClaudeCodeSDKAdapter) hasLiveSessionWork(agentSessionID string) bool {
	if a == nil {
		return false
	}
	agentSessionID = strings.TrimSpace(agentSessionID)
	a.mu.Lock()
	adapterSession := a.sessions[agentSessionID]
	if adapterSession == nil {
		a.mu.Unlock()
		return false
	}
	inFlightTurns := len(adapterSession.turns)
	pending := make([]*pendingInteractiveRequest, 0, len(adapterSession.pendingRequests))
	for _, request := range adapterSession.pendingRequests {
		pending = append(pending, request)
	}
	a.mu.Unlock()
	if inFlightTurns > 0 {
		return true
	}
	for _, request := range pending {
		state := request.disposition()
		if state == pendingInteractiveRequestStatePending || state == pendingInteractiveRequestStateResolving {
			return true
		}
	}
	return false
}
