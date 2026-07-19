package claudesidecar

import (
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

// SessionQuery is the query capability surface the runtime needs; implemented
// by *claudecli.Query and by test fakes.
type SessionQuery interface {
	Messages() <-chan map[string]any
	ReadError() error
	InitializationResult() (map[string]any, error)
	WriteUserMessage(message map[string]any) error
	EndInput()
	Interrupt() error
	StopTask(taskID string) error
	SetPermissionMode(mode string) error
	SetModel(model string) error
	ApplyFlagSettings(settings map[string]any) error
	GetContextUsage() (map[string]any, error)
	Close()
}

// QueryFactory creates a SessionQuery; tests substitute scripted fakes.
type QueryFactory func(options *claudecli.Options) (SessionQuery, error)

// SessionRuntime is the Go port of sessionRuntime.ts: it coordinates one
// provider session through turn lifecycle, query generations, projection,
// interactions, compaction, and configuration.
//
// The TypeScript sidecar relies on the single-threaded event loop for
// ordering; the Go port serializes all state access under one mutex and
// releases it around blocking query round trips.
type SessionRuntime struct {
	mu sync.Mutex

	providerSessionID string
	cwd               string
	env               map[string]string
	restore           bool

	turns           *TurnLifecycle
	assistantStream *AssistantStreamProjector
	activities      *ToolActivityProjector
	compaction      *CompactionTracker
	projection      *MessageProjection
	configuration   *SessionConfiguration
	interactions    *InteractiveCoordinator
	router          *SDKMessageRouter
	driver          *SidecarTestDriver
	goalExecQueue   *goalExecQueue

	initialized              bool
	sessionClosed            bool
	executionEpoch           int
	nextQueryGenerationID    int
	queryGeneration          *QueryGeneration
	resumeQueries            bool
	canceledQueryTailPending bool
	lastTitle                string
	lastAssistantUUID        string
	resumeCursor             map[string]any

	claudeOptions SidecarClaudeOptions
	queryFactory  QueryFactory
	emit          Emitter
}

// SessionRuntimeConfig configures NewSessionRuntime.
type SessionRuntimeConfig struct {
	ProviderSessionID        string
	CWD                      string
	Env                      map[string]string
	Restore                  bool
	TestDriver               bool
	Settings                 *SessionSettings
	ClaudeOptions            SidecarClaudeOptions
	ResumeCursor             map[string]any
	Emit                     Emitter
	QueryFactory             QueryFactory
	ContinuationStartTimeout time.Duration
}

func NewSessionRuntime(config SessionRuntimeConfig) *SessionRuntime {
	s := &SessionRuntime{
		cwd:           config.CWD,
		env:           config.Env,
		claudeOptions: config.ClaudeOptions,
		queryFactory:  config.QueryFactory,
		emit:          config.Emit,
	}
	resumeSessionID := stringValue(config.ResumeCursor["resume"])
	s.providerSessionID = config.ProviderSessionID
	if resumeSessionID != "" {
		s.providerSessionID = resumeSessionID
	}
	s.restore = config.Restore || resumeSessionID != ""
	s.resumeQueries = s.restore

	s.turns = NewTurnLifecycle(TurnLifecycleOptions{
		Emit:       s.emit,
		OnActivate: s.resetTurnScratch,
		OnSettled:  s.emitSessionStateLocked,
		OnContinuationStartTimeout: func() {
			query := s.queryLocked()
			if query == nil {
				return
			}
			go func() {
				if err := query.Interrupt(); err != nil {
					s.emit("error", "", map[string]any{
						"error": "Claude SDK continuation interrupt failed: " + err.Error(),
					})
				}
			}()
		},
		ContinuationStartTimeout: config.ContinuationStartTimeout,
		LockTimerCallback:        s.withLock,
	})
	s.assistantStream = NewAssistantStreamProjector(s.turns.ActiveID, s.emit)
	s.activities = NewToolActivityProjector(
		s.turns.ActiveID,
		s.emit,
		func() { s.turns.ExpectSyntheticContinuation() },
		func() string { return s.turns.LastTurnID() },
	)
	s.compaction = NewCompactionTracker(CompactionTrackerOptions{
		ActiveTurnID:        s.turns.ActiveID,
		EnsureActive:        func(messageType string) { s.turns.EnsureActive(messageType) },
		ClearPendingOrphans: s.turns.ClearPendingOrphans,
		GetQuery: func() contextUsageQuery {
			return s.queryLocked()
		},
		Emit: s.emit,
		RunAsync: func(fetch func() (map[string]any, error), apply func(map[string]any, error)) {
			go func() {
				result, err := fetch()
				s.withLock(func() { apply(result, err) })
			}()
		},
	})
	s.projection = NewMessageProjection(MessageProjectionOptions{
		ProviderSessionID: func() string { return s.providerSessionID },
		Turns:             s.turns,
		Assistant:         s.assistantStream,
		Activities:        s.activities,
		Compaction:        s.compaction,
		Emit:              s.emit,
	})
	s.configuration = NewSessionConfiguration(SessionConfigurationOptions{
		Settings: config.Settings,
		GetQuery: func() configurableQuery {
			return s.queryLocked()
		},
		TestDriver:        config.TestDriver,
		IsInitialized:     func() bool { return s.initialized },
		MarkInitialized:   func() { s.initialized = true },
		EmitFastModeState: func(state string) { s.projection.EmitFastModeState(state) },
		RoundTrip:         s.withoutLock,
	})
	s.interactions = NewInteractiveCoordinator(InteractiveCoordinatorOptions{
		Settings: config.Settings,
		ResolveTurnID: func(request claudecli.ToolPermissionRequest) string {
			return resolveInteractiveTurnID(request, s.turns, s.activities)
		},
		ActivateSyntheticTurn: func() string { return s.turns.ActivateSynthetic().TurnID },
		Emit:                  s.emit,
		Lock:                  s.withLock,
	})
	if config.TestDriver {
		s.driver = NewSidecarTestDriver(s.turns, s.interactions, s.emit)
	}
	s.goalExecQueue = newGoalExecQueue(
		s.emit,
		func(callback func()) { go s.withLock(callback) },
		func(input goalExecInput) {
			s.dispatchExecLocked(input.turnID, input.prompt, input.content, input.turnOrigin, input.goal)
		},
	)
	s.router = NewSDKMessageRouter(SDKMessageRouterOptions{
		GetProviderSessionID: func() string { return s.providerSessionID },
		SetProviderSessionID: func(value string) { s.providerSessionID = value },
		OnAssistantUUID:      func(value string) { s.lastAssistantUUID = value },
		OnSessionState:       s.emitSessionStateLocked,
		OnMaybeTitle:         s.maybeEmitSessionTitleUpdated,
		Turns:                s.turns,
		Assistant:            s.assistantStream,
		Activities:           s.activities,
		Projection:           s.projection,
		Compaction:           s.compaction,
		Emit:                 s.emit,
	})
	s.resumeCursor = normalizeResumeCursor(config.ResumeCursor, s.providerSessionID)
	s.lastAssistantUUID = stringValue(s.resumeCursor["resumeSessionAt"])
	s.turns.RestoreTurnCount(numberValue(s.resumeCursor["turnCount"]))
	return s
}

// ProviderSessionID reports the current provider session id.
func (s *SessionRuntime) ProviderSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.providerSessionID
}

// ActiveTurnID reports the currently active turn id.
func (s *SessionRuntime) ActiveTurnID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns.ActiveID()
}

func (s *SessionRuntime) withLock(callback func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	callback()
}

// withoutLock releases the runtime lock around a blocking query round trip.
// It must only be called while holding the lock.
func (s *SessionRuntime) withoutLock(callback func() error) error {
	s.mu.Unlock()
	defer s.mu.Lock()
	return callback()
}

func (s *SessionRuntime) queryLocked() SessionQuery {
	if s.queryGeneration == nil {
		return nil
	}
	return s.queryGeneration.Query()
}

// Start mirrors sessionRuntime.start(): create and initialize the query,
// flush pending flags, then announce session_started.
func (s *SessionRuntime) Start() error {
	s.logAuthRefresh("session_start.begin", map[string]any{})
	if err := s.ensureQuery(true); err != nil {
		s.logAuthRefresh("session_start.failed", map[string]any{"error": errorPayload(err)})
		return err
	}
	if err := s.applyPendingFlags(); err != nil {
		return err
	}
	restore := s.restore
	if restore {
		s.awaitContextUsageSnapshot("")
	}
	s.withLock(func() {
		payload := map[string]any{
			"providerSessionId": s.providerSessionID,
			"resumeCursor":      s.currentResumeCursorLocked(),
		}
		for key, value := range s.configuration.SessionStatePayload() {
			payload[key] = value
		}
		s.emit("session_started", "", payload)
	})
	s.logAuthRefresh("session_start.succeeded", map[string]any{})
	return nil
}

func (s *SessionRuntime) awaitContextUsageSnapshot(turnID string) {
	done := make(chan struct{})
	s.withLock(func() {
		s.compaction.EmitContextUsageSnapshot(turnID, nil, nil, func(string) { close(done) })
	})
	<-done
}

func (s *SessionRuntime) applyPendingFlags() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configuration.ApplyPendingFlags()
}

// Close mirrors sessionRuntime.close(): permanent shutdown of the current
// generation without a resumable successor.
func (s *SessionRuntime) Close() error {
	s.mu.Lock()
	if s.sessionClosed {
		s.mu.Unlock()
		return nil
	}
	s.sessionClosed = true
	s.executionEpoch++
	s.interactions.RejectAll(errToolUseAborted)
	s.turns.Close()
	generation := s.queryGeneration
	s.queryGeneration = nil
	s.mu.Unlock()
	if generation != nil {
		return generation.Shutdown(false)
	}
	return nil
}

// SubmitInteractive resolves a pending interactive request.
func (s *SessionRuntime) SubmitInteractive(turnID string, requestID string, action string, optionID string, payload map[string]any) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interactions.Submit(turnID, requestID, action, optionID, payload).payload()
}

// InteractiveDisposition reports an interactive request's terminal state.
func (s *SessionRuntime) InteractiveDisposition(turnID string, requestID string, action string, optionID string, payload map[string]any) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	expected := &InteractiveSubmission{Action: action, OptionID: optionID, Payload: payload}
	return s.interactions.Disposition(turnID, requestID, expected).payload()
}

// ApplySettings mirrors sessionRuntime.applySettings.
func (s *SessionRuntime) ApplySettings(payload map[string]any) error {
	s.mu.Lock()
	err := s.configuration.Apply(payload)
	if err == nil {
		s.emitSessionStateLocked()
	}
	s.mu.Unlock()
	return err
}

// StopTask stops one delegated background task.
func (s *SessionRuntime) StopTask(taskID string, parentToolUseID string) (bool, error) {
	s.mu.Lock()
	if s.sessionClosed {
		s.mu.Unlock()
		return false, nil
	}
	resolvedTaskID := s.activities.ResolveDelegatedTaskIDForStop(taskID, parentToolUseID)
	query := s.queryLocked()
	s.mu.Unlock()
	if resolvedTaskID == "" || query == nil {
		return false, nil
	}
	// The stopped task_notification that follows settles the task's activity
	// state; no local bookkeeping happens here so a failed stop stays running.
	if err := query.StopTask(resolvedTaskID); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SessionRuntime) isQueryGenerationActive(generation *QueryGeneration) bool {
	return !s.sessionClosed && !generation.Revoked() && s.queryGeneration == generation
}

func (s *SessionRuntime) resetTurnScratch() {
	s.assistantStream.Reset()
	s.activities.ResetTurnScratch()
}

func (s *SessionRuntime) currentResumeCursorLocked() map[string]any {
	cursor := map[string]any{
		"kind":      "claude-agent-sdk",
		"version":   1,
		"resume":    s.providerSessionID,
		"turnCount": s.turns.TurnCount(),
	}
	if s.lastAssistantUUID != "" {
		cursor["resumeSessionAt"] = s.lastAssistantUUID
	}
	s.resumeCursor = cursor
	return cursor
}

func (s *SessionRuntime) emitSessionStateLocked() {
	payload := map[string]any{
		"providerSessionId": s.providerSessionID,
		"resumeCursor":      s.currentResumeCursorLocked(),
	}
	for key, value := range s.configuration.SessionStatePayload() {
		payload[key] = value
	}
	s.emit("session_state", "", payload)
}

// maybeEmitSessionTitleUpdated reads the CLI's session metadata off-lock and
// emits session_title_updated when a fresh title appears. Best effort: the
// turn result must not fail because Claude Code has not written session
// metadata yet.
func (s *SessionRuntime) maybeEmitSessionTitleUpdated(shouldEmit func() bool) {
	if s.driver != nil {
		return
	}
	providerSessionID := s.providerSessionID
	cwd := s.cwd
	if providerSessionID == "" {
		return
	}
	go func() {
		info := getClaudeSessionInfo(providerSessionID, cwd)
		if info == nil {
			return
		}
		title := normalizeTitle(firstNonEmptyText(info.customTitle, info.summary))
		s.withLock(func() {
			if shouldEmit != nil && !shouldEmit() {
				return
			}
			if title == "" || title == s.lastTitle {
				return
			}
			s.lastTitle = title
			s.emit("session_title_updated", "", map[string]any{
				"providerSessionId": s.providerSessionID,
				"title":             title,
			})
		})
	}()
}

func (s *SessionRuntime) logAuthRefresh(stage string, payload map[string]any) {
	if !claudeAuthRefreshDiagnosticsEnabled() {
		return
	}
	entry := map[string]any{
		"providerSessionId": s.providerSessionID,
		"credentials":       claudeCredentialSnapshot(),
	}
	for key, value := range payload {
		entry[key] = value
	}
	debugClaudeAuthRefreshLog(stage, entry)
}
