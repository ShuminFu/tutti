package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 释放（空闲回收）与关闭是两件事：释放只丢本地进程、保留会话身份，
// 所以释放后 HasLiveSession 为假、CanResume 仍为真，且绝不能发协议级
// session/close —— 这里刻意把连接配成「支持 session/close」，
// Close 走这条路一定会发，释放走这条路一定不能发。
func TestStandardACPAdapterReleaseLiveSessionDropsProcessWithoutProtocolClose(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-release")
	transport.conn.supportsCloseSession = true
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)
	session.ProviderSessionID = "hermes-session-release"

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !adapter.HasLiveSession(session) {
		t.Fatal("HasLiveSession = false right after Start")
	}

	if err := adapter.ReleaseLiveSession(context.Background(), session); err != nil {
		t.Fatalf("ReleaseLiveSession: %v", err)
	}

	if adapter.HasLiveSession(session) {
		t.Fatal("HasLiveSession = true after release, want the local client dropped")
	}
	if !adapter.CanResume(session) {
		t.Fatal("CanResume = false after release, want the session still resumable")
	}
	if session.ProviderSessionID != "hermes-session-release" {
		t.Fatalf("ProviderSessionID = %q after release, want it preserved", session.ProviderSessionID)
	}
	if params := transport.conn.closeSessionParams(); params != nil {
		t.Fatalf("session/close was sent during release (params=%#v); release must not end the provider session", params)
	}
	if !transport.conn.closed() {
		t.Fatal("transport was not closed by release, want the agent process released")
	}
}

// 核心用例：释放之后再 Resume，进程要能拉回来、会话要能继续用。
func TestStandardACPAdapterResumeAfterReleaseRestoresUsableSession(t *testing.T) {
	t.Parallel()

	transport := &multiProcStandardACPTransport{
		agentTitle:          "Hermes Agent",
		sessionID:           "hermes-session-resume-after-release",
		supportsLoadSession: true,
	}
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)

	events, err := adapter.Start(context.Background(), session)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.ProviderSessionID = events[0].ProviderSessionID
	if session.ProviderSessionID == "" {
		t.Fatal("Start did not report a provider session id")
	}
	if _, err := adapter.Exec(context.Background(), session, textPrompt("ping"), "", "turn-1", nil, nil); err != nil {
		t.Fatalf("Exec before release: %v", err)
	}

	if err := adapter.ReleaseLiveSession(context.Background(), session); err != nil {
		t.Fatalf("ReleaseLiveSession: %v", err)
	}
	if adapter.HasLiveSession(session) {
		t.Fatal("HasLiveSession = true after release")
	}
	spawned, live := transport.snapshot()
	if spawned != 1 || len(live) != 0 {
		t.Fatalf("spawned/live processes after release = %d/%d, want 1/0", spawned, len(live))
	}

	if err := adapter.Resume(context.Background(), session); err != nil {
		t.Fatalf("Resume after release: %v", err)
	}
	if !adapter.HasLiveSession(session) {
		t.Fatal("HasLiveSession = false after resume, want the process back")
	}
	spawned, live = transport.snapshot()
	if spawned != 2 || len(live) != 1 {
		t.Fatalf("spawned/live processes after resume = %d/%d, want 2/1", spawned, len(live))
	}
	if got := asString(live[0].lastLoadSessionParams["sessionId"]); got != session.ProviderSessionID {
		t.Fatalf("session/load sessionId = %q, want %q", got, session.ProviderSessionID)
	}

	// 会话真的还能用：释放后重新拉起的进程上再跑一个回合。
	if _, err := adapter.Exec(context.Background(), session, textPrompt("ping again"), "", "turn-2", nil, nil); err != nil {
		t.Fatalf("Exec after resume: %v", err)
	}
}

// 回合还在飞的时候不许释放。
func TestStandardACPAdapterReleaseLiveSessionRefusesInFlightTurn(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-busy-turn")
	transport.conn.pauseBeforePromptResult = make(chan struct{})
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)
	session.ProviderSessionID = "hermes-session-busy-turn"

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	execDone := make(chan error, 1)
	go func() {
		_, err := adapter.Exec(context.Background(), session, textPrompt("run a long tool"), "", "turn-1", nil, nil)
		execDone <- err
	}()
	// 等 session/prompt 真的挂在连接上，再试释放。
	waitForCondition(t, func() bool {
		return adapter.hasLiveSessionWork(session.AgentSessionID)
	})

	err := adapter.ReleaseLiveSession(context.Background(), session)
	if !errors.Is(err, ErrLiveSessionBusy) {
		t.Fatalf("ReleaseLiveSession error = %v, want ErrLiveSessionBusy", err)
	}
	if !adapter.HasLiveSession(session) {
		t.Fatal("live session was released while a turn was in flight")
	}
	if transport.conn.closed() {
		t.Fatal("transport was closed while a turn was in flight")
	}

	close(transport.conn.pauseBeforePromptResult)
	select {
	case err := <-execDone:
		if err != nil {
			t.Fatalf("Exec: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Exec did not finish after the prompt was unblocked")
	}
}

// 有挂起的权限请求时不许释放。上一个用例里「在飞的调用」和「挂起的审批」
// 同时成立，这里把连接放空（回合已结束）后单独注入一条挂起审批，
// 单独钉住审批这一支判据。
func TestStandardACPAdapterReleaseLiveSessionRefusesPendingApproval(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-busy-approval")
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)
	session.ProviderSessionID = "hermes-session-busy-approval"

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if adapter.hasLiveSessionWork(session.AgentSessionID) {
		t.Fatal("session reports work right after Start, want idle")
	}
	acpSession := adapter.getSession(session.AgentSessionID)
	if acpSession == nil {
		t.Fatal("adapter has no session after Start")
	}
	adapter.mu.Lock()
	acpSession.pendingApprovals["permission-1"] = &pendingACPApproval{
		agentSessionID: session.AgentSessionID,
		requestID:      "permission-1",
		turnID:         "turn-1",
	}
	adapter.mu.Unlock()

	err := adapter.ReleaseLiveSession(context.Background(), session)
	if !errors.Is(err, ErrLiveSessionBusy) {
		t.Fatalf("ReleaseLiveSession error = %v, want ErrLiveSessionBusy", err)
	}
	if !adapter.HasLiveSession(session) {
		t.Fatal("live session was released while an approval was pending")
	}
	if transport.conn.closed() {
		t.Fatal("transport was closed while an approval was pending")
	}
}

// 整条空闲回收链路：ReleaseIdleLiveSessions 原来把 standard ACP 记成
// skipped_unsupported（grok 的进程因此一直握到 tuttid 重启），补上释放实现后
// 应当真的释放，且会话记录与 providerSessionID 原样留下。
func TestControllerReleasesIdleStandardACPLiveSession(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-idle-reaper")
	adapter := newHermesExtensionTestAdapter(transport)
	controller := NewController([]Adapter{adapter}, nil)
	started, err := controller.Start(context.Background(), StartInput{
		RoomID:         "room-1",
		AgentSessionID: "agent-session-1",
		Provider:       hermesExtensionTestProvider,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	setSessionUpdatedAt(t, controller, started.Session, time.Now().Add(-time.Hour))

	result := controller.ReleaseIdleLiveSessions(context.Background(), ReleaseIdleLiveSessionsInput{
		IdleAfter: 30 * time.Minute,
		Now:       time.Now(),
	})
	if result.Released != 1 || result.SkippedUnsupported != 0 {
		t.Fatalf("release result = %#v, want the standard ACP session released, not skipped as unsupported", result)
	}
	if adapter.HasLiveSession(started.Session) {
		t.Fatal("adapter still holds a live session after the idle reaper ran")
	}
	stored, ok := controller.Session(started.Session.RoomID, started.Session.AgentSessionID)
	if !ok {
		t.Fatal("controller session was deleted by the idle release")
	}
	if stored.ProviderSessionID != "hermes-session-idle-reaper" {
		t.Fatalf("provider session id = %q, want it preserved for resume", stored.ProviderSessionID)
	}
	if stored.Status == SessionStatusCompleted {
		t.Fatal("session status = completed, want release to be non-destructive")
	}
}
