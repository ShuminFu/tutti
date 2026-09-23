package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func (c *Controller) ensureLiveAdapterSession(ctx context.Context, session Session, adapter Adapter) error {
	probe, ok := adapter.(LiveSessionProbeAdapter)
	if !ok || probe.HasLiveSession(session) {
		return c.applyRetainedGoalGenerationFencesOrClose(ctx, session, adapter)
	}
	if strings.TrimSpace(session.ProviderSessionID) == "" {
		return ErrSessionDisconnected
	}
	c.invalidateAppliedGoalGenerationFences(session)
	// Resume only reconnects provider context; it does not execute a Turn.
	// Host startup separately settles stale pre-restart Turns as interrupted.
	// Install retained admission fences before this Controller dispatches the
	// user operation that requested the connection.
	if err := adapter.Resume(ctx, session); err != nil {
		return err
	}
	if err := c.applyRetainedGoalGenerationFencesOrClose(ctx, session, adapter); err != nil {
		return err
	}
	session.Status = SessionStatusReady
	session.UpdatedAtUnixMS = unixMS(now())
	c.store(session)
	if !c.publishPendingCommandSnapshot(session) {
		c.publishAdapterCommandSnapshot(session, adapter)
	}
	return nil
}

func (c *Controller) ReleaseIdleLiveSessions(ctx context.Context, input ReleaseIdleLiveSessionsInput) ReleaseIdleLiveSessionsResult {
	var result ReleaseIdleLiveSessionsResult
	if c == nil || (input.IdleAfter <= 0 && input.IdleAfterFor == nil) {
		return result
	}
	nowTime := input.Now
	if nowTime.IsZero() {
		nowTime = now()
	}
	nowUnixMS := unixMS(nowTime)
	defaultIdleAfterMS := input.IdleAfter.Milliseconds()
	if defaultIdleAfterMS <= 0 && input.IdleAfterFor == nil {
		return result
	}
	type candidate struct {
		session Session
		adapter Adapter
	}
	candidates := make([]candidate, 0)
	c.mu.Lock()
	for key, session := range c.sessions {
		session = c.reconcileSessionStatusLocked(key, session)
		c.sessions[key] = session
		candidates = append(candidates, candidate{
			session: session,
			adapter: c.adapterForSessionLocked(session),
		})
	}
	c.mu.Unlock()
	for _, candidate := range candidates {
		if input.Limit > 0 && result.Scanned >= input.Limit {
			break
		}
		result.Scanned++
		idleAfterMS := defaultIdleAfterMS
		if input.IdleAfterFor != nil {
			// 判正负要在 Duration 上判，不能先转毫秒：-1ns 转成毫秒是 0，
			// 「不回收」会被悄悄读成「立刻回收」——正好反过来。
			idleAfter := input.IdleAfterFor(candidate.session)
			if idleAfter < 0 {
				// 策略说这条留着（用户开了常驻）。它不是「还没到点」，单独记一档，
				// 免得看日志的人以为是阈值没配对。
				result.SkippedRetained++
				continue
			}
			idleAfterMS = idleAfter.Milliseconds()
		}
		result.add(c.releaseIdleLiveSession(ctx, candidate.session, candidate.adapter, nowUnixMS, idleAfterMS, input.PolicyAfterLock, false, 0))
	}
	if input.MaxLiveSessions > 0 {
		result.add(c.evictLiveSessionsOverCap(ctx, input.MaxLiveSessions, input.EvictionGrace, nowUnixMS, input.PolicyAfterLock))
	}
	return result
}

// evictLiveSessionsOverCap 在常驻进程条数超过上限时，从最久没说话的那条开始挤掉。
//
// 为什么 TTL 之外还要这一道：半小时里开了几十条会话、每条都刚聊过，按 TTL 它们
// 全都「还新鲜」，可几十个 provider 进程已经把内存吃光了。TTL 管的是「这条闲了多久」，
// 上限管的是「一共留了多少条」，两个问题，两道闸。
//
// 「防止误杀」由三层保证：正在跑回合的不动（它根本不在候选里）、适配器说忙的不动
// （ReleaseLiveSession 回 ErrLiveSessionBusy）、刚说完话不到护身符时长的不动。
// 再加上释放本身是非破坏的——会话身份与续聊能力都留着，最坏结果只是下次说话冷启动一次。
func (c *Controller) evictLiveSessionsOverCap(
	ctx context.Context,
	maxLive int,
	grace time.Duration,
	nowUnixMS int64,
	policyAfterLock func(Session) (time.Duration, int),
) ReleaseIdleLiveSessionsResult {
	var result ReleaseIdleLiveSessionsResult
	if c == nil || maxLive <= 0 {
		return result
	}
	if grace <= 0 {
		grace = defaultLiveSessionEvictionGrace
	}
	graceMS := grace.Milliseconds()

	type candidate struct {
		session Session
		adapter Adapter
	}
	live := 0
	evictable := make([]candidate, 0)
	c.mu.Lock()
	for key, session := range c.sessions {
		adapter := c.adapterForSessionLocked(session)
		_, probe, ok := liveSessionReleaseAdapter(adapter)
		if !ok || strings.TrimSpace(session.ProviderSessionID) == "" || !probe.HasLiveSession(session) {
			continue
		}
		// 占着进程的都算进「一共留了多少条」，包括正在跑回合的那些 ——
		// 上限是内存口径，不是空闲口径。但它们不进候选。
		live++
		if _, hasActiveTurn := c.turns[key]; hasActiveTurn {
			continue
		}
		evictable = append(evictable, candidate{session: session, adapter: adapter})
	}
	c.mu.Unlock()

	overflow := live - maxLive
	if overflow <= 0 {
		return result
	}
	// 最久没说话的排前面：这就是 LRU。UpdatedAtUnixMS 是会话最后一次有动静的时刻。
	sort.SliceStable(evictable, func(i, j int) bool {
		return evictable[i].session.UpdatedAtUnixMS < evictable[j].session.UpdatedAtUnixMS
	})
	for _, item := range evictable {
		if overflow <= 0 {
			break
		}
		if !sessionIdleFor(item.session, nowUnixMS, graceMS) {
			// 刚说完话，用户很可能正要接着打字。宁可超限一会儿。
			result.SkippedOverCapProtected++
			continue
		}
		// 复用 TTL 那条路径的全部守卫（重新取一次会话、再确认没有在飞的回合、
		// 适配器喊忙就放弃）；阈值传 0，因为「该不该走」已经由上面的护身符判完了。
		single := c.releaseIdleLiveSession(ctx, item.session, item.adapter, nowUnixMS, 0, policyAfterLock, true, maxLive)
		if single.Released > 0 {
			result.EvictedOverCap += single.Released
			overflow -= single.Released
			continue
		}
		if single.SkippedRetained > 0 {
			result.add(single)
			break // the latest cap now permits the remaining live processes
		}
		// 没放成的原因（忙 / 已经不在了 / 不支持）照原样并进结果，别吞掉。
		single.Released = 0
		result.add(single)
	}
	return result
}

func (c *Controller) releaseIdleLiveSession(
	ctx context.Context,
	session Session,
	adapter Adapter,
	nowUnixMS int64,
	idleAfterMS int64,
	policyAfterLock func(Session) (time.Duration, int),
	overCap bool,
	capLimit int,
) ReleaseIdleLiveSessionsResult {
	var result ReleaseIdleLiveSessionsResult
	_, probe, ok := liveSessionReleaseAdapter(adapter)
	if !ok {
		result.SkippedUnsupported = 1
		return result
	}
	if strings.TrimSpace(session.ProviderSessionID) == "" || !probe.HasLiveSession(session) {
		result.SkippedNotLive = 1
		return result
	}
	key := sessionKey(session.RoomID, session.AgentSessionID)
	c.mu.Lock()
	_, hasActiveTurn := c.turns[key]
	c.mu.Unlock()
	if hasActiveTurn {
		result.SkippedActiveTurn = 1
		return result
	}
	if !sessionIdleFor(session, nowUnixMS, idleAfterMS) {
		result.SkippedFresh = 1
		return result
	}

	releaseLifecycleLock := c.acquireLifecycleLock(session.RoomID, session.AgentSessionID)
	defer releaseLifecycleLock()

	refreshed, adapter, err := c.sessionAndAdapter(session.RoomID, session.AgentSessionID)
	if err != nil {
		result.SkippedNotLive = 1
		return result
	}
	releaseAdapter, probe, ok := liveSessionReleaseAdapter(adapter)
	if !ok {
		result.SkippedUnsupported = 1
		return result
	}
	if strings.TrimSpace(refreshed.ProviderSessionID) == "" || !probe.HasLiveSession(refreshed) {
		result.SkippedNotLive = 1
		return result
	}
	if c.HasActiveTurn(refreshed.RoomID, refreshed.AgentSessionID) {
		result.SkippedActiveTurn = 1
		return result
	}
	liveCount := 0
	if overCap {
		// Other sessions may have started or exited since the candidate list was
		// built. Recount under the controller lock immediately before eviction.
		liveCount = c.countLiveSessions()
	}
	if policyAfterLock != nil {
		currentIdleAfter, currentMaxLive := policyAfterLock(refreshed)
		if overCap {
			capLimit = currentMaxLive
		} else {
			if currentIdleAfter < 0 {
				result.SkippedRetained = 1
				return result
			}
			idleAfterMS = currentIdleAfter.Milliseconds()
		}
	}
	if overCap && (capLimit <= 0 || liveCount <= capLimit) {
		result.SkippedRetained = 1
		return result
	}
	if !sessionIdleFor(refreshed, nowUnixMS, idleAfterMS) {
		result.SkippedFresh = 1
		return result
	}
	if err := releaseAdapter.ReleaseLiveSession(ctx, refreshed); err != nil {
		if errors.Is(err, ErrLiveSessionBusy) {
			result.SkippedBusy = 1
			return result
		}
		result.Failed = 1
		slog.Warn("agent live session release failed",
			"event", "agent_session.live_release.failed",
			"room_id", refreshed.RoomID,
			"agent_session_id", refreshed.AgentSessionID,
			"provider", refreshed.Provider,
			"provider_session_id", refreshed.ProviderSessionID,
			"error", err.Error(),
		)
		return result
	}
	c.invalidateAppliedGoalGenerationFences(refreshed)
	result.Released = 1
	return result
}

func (c *Controller) countLiveSessions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, session := range c.sessions {
		adapter := c.adapterForSessionLocked(session)
		_, probe, ok := liveSessionReleaseAdapter(adapter)
		if ok && strings.TrimSpace(session.ProviderSessionID) != "" && probe.HasLiveSession(session) {
			count++
		}
	}
	return count
}

func liveSessionReleaseAdapter(adapter Adapter) (LiveSessionReleaseAdapter, LiveSessionProbeAdapter, bool) {
	releaseAdapter, releaseOK := adapter.(LiveSessionReleaseAdapter)
	probe, probeOK := adapter.(LiveSessionProbeAdapter)
	return releaseAdapter, probe, releaseOK && probeOK
}

// CloseAllLiveSessions force-terminates every live provider process across
// all sessions, regardless of idle time, active turns, or pending approval
// requests. Unlike ReleaseIdleLiveSessions (the periodic reaper, which only
// reclaims idle, non-busy sessions so it never interrupts work in
// progress), this exists for daemon shutdown: an OS process is not killed
// automatically just because its parent (tuttid) exits — it is reparented
// and keeps running. A provider subprocess (e.g. a Codex app-server) left
// behind here would keep running unmanaged, still able to act on the
// session's working directory, until something else notices and kills it.
// Call this once, during shutdown, before the daemon process exits.
//
// This only closes the provider-side process; it deliberately does not
// mark sessions completed or delete their records, so providers that
// support live-session resume (see LiveSessionReleaseAdapter) reconnect
// normally the next time the daemon starts and the session resumes.
func (c *Controller) CloseAllLiveSessions(ctx context.Context) CloseAllLiveSessionsResult {
	var result CloseAllLiveSessionsResult
	if c == nil {
		return result
	}
	type candidate struct {
		session Session
		adapter Adapter
	}
	c.mu.Lock()
	candidates := make([]candidate, 0, len(c.sessions))
	for _, session := range c.sessions {
		candidates = append(candidates, candidate{
			session: session,
			adapter: c.adapterForSessionLocked(session),
		})
	}
	c.mu.Unlock()

	for _, cand := range candidates {
		probe, ok := cand.adapter.(LiveSessionProbeAdapter)
		if !ok || !probe.HasLiveSession(cand.session) {
			continue
		}
		result.Scanned++
		releaseLifecycleLock := c.acquireLifecycleLock(cand.session.RoomID, cand.session.AgentSessionID)
		err := cand.adapter.Close(ctx, cand.session)
		releaseLifecycleLock()
		if err != nil {
			result.Failed++
			slog.Warn("agent live session shutdown close failed",
				"event", "agent_session.shutdown_close.failed",
				"room_id", cand.session.RoomID,
				"agent_session_id", cand.session.AgentSessionID,
				"provider", cand.session.Provider,
				"error", err.Error(),
			)
			continue
		}
		result.Closed++
	}
	return result
}

func sessionIdleFor(session Session, nowUnixMS int64, idleAfterMS int64) bool {
	if session.UpdatedAtUnixMS <= 0 {
		return false
	}
	return nowUnixMS-session.UpdatedAtUnixMS >= idleAfterMS
}

func (r *ReleaseIdleLiveSessionsResult) add(next ReleaseIdleLiveSessionsResult) {
	r.Released += next.Released
	r.SkippedFresh += next.SkippedFresh
	r.SkippedActiveTurn += next.SkippedActiveTurn
	r.SkippedUnsupported += next.SkippedUnsupported
	r.SkippedNotLive += next.SkippedNotLive
	r.SkippedBusy += next.SkippedBusy
	r.SkippedRetained += next.SkippedRetained
	r.EvictedOverCap += next.EvictedOverCap
	r.SkippedOverCapProtected += next.SkippedOverCapProtected
	r.Failed += next.Failed
}

// isResumeRecreatableError reports whether a failed resume should fall back to
// creating a fresh provider session in place. These are the "the provider
// session is not available locally" cases — anything else is a genuine failure
// that should surface to the caller.
func isResumeRecreatableError(err error) bool {
	switch AppErrorCode(err) {
	case AppErrorProviderSessionNotFound, AppErrorResumeSessionNotLocal:
		return true
	default:
		return false
	}
}

// recreateAdapterSession starts a brand new provider session for an existing
// agent session, clearing the stale provider session id so the adapter mints a
// fresh one. The new provider session id is captured from the started events and
// persisted via the session report, keeping the conversation continuable.
//
// The freshly started provider session has no memory of anything said before
// this point (e.g. an externally-imported conversation whose rollout only
// ever existed on another device, or local history retention pruning it) even
// though the transcript keeps showing the old messages joined seamlessly with
// new ones. Without an explicit notice this looks to the user like the agent
// silently forgot the conversation, so a visible system notice is appended
// alongside the started events.
func (c *Controller) recreateAdapterSession(ctx context.Context, session Session, adapter Adapter) error {
	// 这是「同一条 agent 会话换一个全新 provider 会话」降级唯一可查的痕迹：界面上只有
	// 一条 system_notice，历史与后续回复在会话里连成一片，事后在日志里根本找不回来
	// （2026-09-17 排查收件箱导入会话续不上时，tuttid.log 里 grep 不到任何 recreate 痕迹）。
	// 有这一行才能统计这条降级到底发生了多少次、落在哪些 provider / 会话上。
	slog.Warn("agent provider session recreated without history",
		"room_id", strings.TrimSpace(session.RoomID),
		"agent_session_id", strings.TrimSpace(session.AgentSessionID),
		"provider", strings.TrimSpace(session.Provider),
		"provider_session_id", strings.TrimSpace(session.ProviderSessionID),
		"cwd", strings.TrimSpace(session.CWD),
	)
	fresh := session
	fresh.ProviderSessionID = ""
	fresh.Status = SessionStatusReady
	fresh.LastError = ""
	fresh.UpdatedAtUnixMS = unixMS(now())
	events, err := adapter.Start(ctx, fresh)
	if err != nil {
		return err
	}
	fresh = applySessionEvents(fresh, events)
	c.invalidateAppliedGoalGenerationFences(fresh)
	if err := c.applyRetainedGoalGenerationFencesOrClose(ctx, fresh, adapter); err != nil {
		return err
	}
	fresh.Status = SessionStatusReady
	fresh.UpdatedAtUnixMS = unixMS(now())
	if notice, ok := sessionRecreatedNoticeEvent(fresh); ok {
		events = append(events, notice)
	}
	c.store(fresh)
	c.publish(fresh, events)
	c.publishPendingConfigOptionsUpdates(fresh)
	if !c.publishPendingCommandSnapshot(fresh) {
		c.publishAdapterCommandSnapshot(fresh, adapter)
	}
	c.enqueueSessionReport(ctx, fresh, events)
	return nil
}

// sessionRecreatedNoticeEvent builds the visible system notice that
// accompanies a recreated provider session (see recreateAdapterSession). It
// reuses the same synthetic "agent_system_notice" message shape the ACP
// adapters already use for compaction/goal/transport notices
// (acpSystemNoticeEvent), so it renders through the existing generic notice
// card with no GUI changes required.
func sessionRecreatedNoticeEvent(session Session) (activityshared.Event, bool) {
	return acpSystemNoticeEvent(session, "", map[string]any{
		"sessionUpdate": "system_notice",
		"kind":          "agent_system_notice",
		"noticeKind":    "warning",
		"title":         "Conversation history could not be restored",
		"detail":        "The assistant could not resume this conversation's earlier messages locally (for example, if it was imported from another device or the local session data is no longer available), so this reply is starting fresh without that context.",
	}, "system_notice", true)
}

func (c *Controller) ValidatePromptContent(_ context.Context, input ExecInput) error {
	session, adapter, err := c.sessionAndAdapter(input.RoomID, input.AgentSessionID)
	if err != nil {
		return err
	}
	if err := validatePromptContentImagesForPreflight(input.Content); err != nil {
		return err
	}
	content := normalizeRuntimePromptContentForValidation(input.Content)
	if len(content) == 0 {
		return fmt.Errorf("prompt is required")
	}
	localContent, err := projectLocalSkillPrompt(session, content, false)
	if err != nil {
		return err
	}
	providerContent := projectRuntimePromptContent(localContent)
	if promptAdapter, ok := adapter.(PromptContentAdapter); ok {
		return promptAdapter.ValidatePromptContent(session, providerContent)
	}
	return nil
}
