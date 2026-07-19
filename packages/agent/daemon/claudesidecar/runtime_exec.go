package claudesidecar

import (
	"errors"
	"os"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

//nolint:staticcheck // Mirrors the TypeScript sidecar's exact error text.
var errGenerationRetired = errors.New("Claude SDK query generation was retired")

// Exec mirrors sessionRuntime.exec(): enqueue a turn and drive it through the
// query asynchronously.
func (s *SessionRuntime) Exec(turnID string, prompt string, content any, turnOrigin string, goal *GoalCommandDispatch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driver != nil {
		s.driver.Exec(turnID, prompt)
		return
	}
	if s.sessionClosed {
		s.emit("turn_failed", "", map[string]any{
			"turnId": turnID,
			"error":  "Claude SDK query is closed",
		})
		return
	}
	if goal != nil && goal.OperationID != "" && goal.Revision > 0 {
		s.goalExecQueue.accept(goalExecInput{
			turnID:     turnID,
			prompt:     prompt,
			content:    content,
			turnOrigin: turnOrigin,
			goal:       goal,
		})
		return
	}
	s.dispatchExecLocked(turnID, prompt, content, turnOrigin, nil)
}

func (s *SessionRuntime) dispatchExecLocked(turnID string, prompt string, content any, turnOrigin string, goal *GoalCommandDispatch) {
	s.turns.CloseSyntheticBeforeUserTurn()
	turn := &RuntimeTurn{
		TurnID:     turnID,
		PromptUUID: randomUUID(),
		Origin:     turnOrigin,
	}
	if goal != nil {
		turn.GoalOperationID = goal.OperationID
		turn.GoalRevision = goal.Revision
		turn.GoalRepairEpoch = goal.RepairEpoch
		turn.GoalAction = goal.Action
	}
	executionEpoch := s.executionEpoch
	s.turns.Enqueue(turn)
	s.compaction.SelectCommand(turnID, isCompactCommandPrompt(prompt))
	go s.execAsync(turn, executionEpoch, prompt, content)
}

func (s *SessionRuntime) execAsync(turn *RuntimeTurn, executionEpoch int, prompt string, content any) {
	err := s.ensureQuery(false)
	if err == nil {
		err = s.applyPendingFlags()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if executionEpoch != s.executionEpoch || turn.Settled {
			return
		}
		s.logAuthRefresh("exec.ensure_query_failed", map[string]any{
			"turnId": turn.TurnID,
			"error":  errorPayload(err),
		})
		s.emit("turn_failed", "", map[string]any{
			"turnId": turn.TurnID,
			"error":  errorMessage(err),
		})
		return
	}
	generation := s.queryGeneration
	if generation == nil || !s.isQueryGenerationActive(generation) ||
		executionEpoch != s.executionEpoch || turn.Settled {
		return
	}
	generation.ExpectPromptEcho(turn.PromptUUID)
	_ = generation.promptQueue.push(map[string]any{
		"uuid":               turn.PromptUUID,
		"type":               "user",
		"session_id":         s.providerSessionID,
		"parent_tool_use_id": nil,
		"message": map[string]any{
			"role":    "user",
			"content": sdkContentFromPromptBlocks(content, prompt),
		},
	})
	s.consumeLocked(generation)
}

// Guide mirrors sessionRuntime.guide(): inject guidance into the active turn.
func (s *SessionRuntime) Guide(prompt string, content any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driver != nil {
		s.driver.Guide(prompt)
		return
	}
	if s.sessionClosed {
		s.emit("error", "", map[string]any{"error": "Claude SDK query is closed"})
		return
	}
	executionEpoch := s.executionEpoch
	go s.guideAsync(executionEpoch, prompt, content)
}

func (s *SessionRuntime) guideAsync(executionEpoch int, prompt string, content any) {
	err := s.ensureQuery(false)
	if err == nil {
		err = s.applyPendingFlags()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if executionEpoch != s.executionEpoch {
			return
		}
		s.logAuthRefresh("guide.ensure_query_failed", map[string]any{
			"error": errorPayload(err),
		})
		s.emit("error", "", map[string]any{"error": errorMessage(err)})
		return
	}
	generation := s.queryGeneration
	if generation == nil || !s.isQueryGenerationActive(generation) || executionEpoch != s.executionEpoch {
		return
	}
	_ = generation.promptQueue.push(map[string]any{
		"uuid":               randomUUID(),
		"type":               "user",
		"session_id":         s.providerSessionID,
		"parent_tool_use_id": nil,
		"message": map[string]any{
			"role":    "user",
			"content": sdkContentFromPromptBlocks(content, prompt),
		},
	})
	s.consumeLocked(generation)
}

// Cancel mirrors sessionRuntime.cancel(): revoke the current generation,
// await the SDK interrupt ack, then settle the canceled turns.
func (s *SessionRuntime) Cancel(expectedTurnID string) (bool, error) {
	s.mu.Lock()
	if s.sessionClosed {
		s.mu.Unlock()
		return false, nil
	}
	var hasActiveTurn bool
	if expectedTurnID != "" {
		hasActiveTurn = s.turns.CancelActiveExact(expectedTurnID)
		if !hasActiveTurn {
			s.mu.Unlock()
			return false, nil
		}
	} else {
		hasActiveTurn = s.turns.CancelQueued()
	}
	s.executionEpoch++
	s.canceledQueryTailPending = true
	s.interactions.RejectAll(errToolUseAborted)
	generation := s.queryGeneration
	s.queryGeneration = nil
	s.mu.Unlock()

	var shutdownErr error
	if generation != nil {
		shutdownErr = generation.Shutdown(true)
	}
	s.mu.Lock()
	if hasActiveTurn {
		s.turns.SettleActive("turn_canceled", nil)
	}
	s.turns.ClearCancelled()
	s.mu.Unlock()
	return hasActiveTurn, shutdownErr
}

// ensureQuery mirrors sessionRuntime.ensureQuery(): create one query
// generation and optionally await its initialization.
func (s *SessionRuntime) ensureQuery(initialize bool) error {
	s.mu.Lock()
	if s.queryGeneration != nil || s.driver != nil {
		s.mu.Unlock()
		return nil
	}
	if s.sessionClosed {
		s.mu.Unlock()
		//nolint:staticcheck // Mirrors the TypeScript sidecar's exact error text.
		return errors.New("Claude SDK session is closed")
	}
	executionEpoch := s.executionEpoch
	s.nextQueryGenerationID++
	generation := NewQueryGeneration(s.nextQueryGenerationID, s.canceledQueryTailPending)
	s.queryGeneration = generation
	permissionMode := effectivePermissionMode(s.configuration.Settings)
	allowBypassPermissions := canBypassPermissions()
	querySettings := querySettingsFromSessionSettings(s.configuration.Settings)
	model := modelOptionValue(s.configuration.Settings.Model)
	resume := s.resumeQueries
	providerSessionID := s.providerSessionID
	cwd := s.cwd
	sessionEnv := s.env
	claudeOptions := s.claudeOptions
	s.mu.Unlock()

	if cwd == "" {
		if workingDir, err := os.Getwd(); err == nil {
			cwd = workingDir
		}
	}
	// One settings snapshot feeds both the executable resolution and the SDK
	// env, so the two can never disagree (and the settings hierarchy is read
	// once per query creation).
	settingsEnv := claudeSettingsEnv(cwd)
	mergedEnv := map[string]string{}
	for _, item := range os.Environ() {
		if key, value, ok := cutEnvEntry(item); ok {
			mergedEnv[key] = value
		}
	}
	for key, value := range settingsEnv {
		mergedEnv[key] = value
	}
	for key, value := range sessionEnv {
		mergedEnv[key] = value
	}
	mergedEnv["CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS"] = "1"
	claudeExecutablePath := resolveClaudeCodeExecutablePath(mergedEnv, cwd)

	options := &claudecli.Options{
		CWD:                             cwd,
		Env:                             mergedEnv,
		PathToClaudeCodeExecutable:      claudeExecutablePath,
		IncludePartialMessages:          true,
		Model:                           model,
		PermissionMode:                  permissionMode,
		AllowDangerouslySkipPermissions: allowBypassPermissions,
		Hooks:                           s.queryGenerationHooks(generation),
		CanUseTool: func(toolName string, toolInput map[string]any, request claudecli.ToolPermissionRequest) (claudecli.PermissionResult, bool, error) {
			return s.handleToolPermission(generation, toolName, toolInput, request)
		},
	}
	if resume {
		options.Resume = providerSessionID
	} else {
		options.SessionID = providerSessionID
	}
	if len(querySettings) > 0 {
		options.Settings = querySettings
	}
	applyClaudeQueryOptionOverrides(options, claudeOptions)

	s.logAuthRefresh("query_create.begin", map[string]any{
		"initialize":                initialize,
		"restore":                   resume,
		"permissionMode":            permissionMode,
		"hasExecutablePathOverride": claudeExecutablePath != "",
		"hasModel":                  model != "",
		"claudeOptionKeys":          claudeOptionOverrideKeys(claudeOptions),
	})

	rollback := func(err error) error {
		s.mu.Lock()
		if s.queryGeneration == generation {
			s.queryGeneration = nil
		}
		s.mu.Unlock()
		generation.Revoke()
		generation.CloseQuery()
		return err
	}

	query, err := s.createQuery(options)
	if err != nil {
		return rollback(err)
	}
	generation.setQuery(query)
	go feedPromptQueue(generation.promptQueue, query)
	s.logAuthRefresh("query_create.succeeded", map[string]any{
		"initialize": initialize,
		"restore":    resume,
	})

	if initialize || resume {
		s.logAuthRefresh("query_initialization.begin", map[string]any{"restore": resume})
		initializationResult, initErr := query.InitializationResult()
		s.mu.Lock()
		if initErr == nil && (executionEpoch != s.executionEpoch || !s.isQueryGenerationActive(generation)) {
			initErr = errGenerationRetired
		}
		if initErr != nil {
			s.initialized = false
			s.mu.Unlock()
			s.logAuthRefresh("query_initialization.failed", map[string]any{
				"restore": resume,
				"error":   errorPayload(initErr),
			})
			return rollback(initErr)
		}
		s.configuration.ApplyInitializationResult(initializationResult)
		s.initialized = true
		s.resumeQueries = true
		s.canceledQueryTailPending = false
		s.mu.Unlock()
		s.logAuthRefresh("query_initialization.succeeded", map[string]any{"restore": true})
		return nil
	}
	s.mu.Lock()
	s.resumeQueries = true
	s.canceledQueryTailPending = false
	s.mu.Unlock()
	return nil
}

func (s *SessionRuntime) createQuery(options *claudecli.Options) (SessionQuery, error) {
	if s.queryFactory != nil {
		return s.queryFactory(options)
	}
	return claudecli.Start(options)
}

func cutEnvEntry(item string) (string, string, bool) {
	for index := 0; index < len(item); index++ {
		if item[index] == '=' {
			return item[:index], item[index+1:], true
		}
	}
	return "", "", false
}

func feedPromptQueue(queue *asyncPromptQueue, query SessionQuery) {
	for {
		message, ok := queue.next()
		if !ok {
			query.EndInput()
			return
		}
		if err := query.WriteUserMessage(message); err != nil {
			return
		}
	}
}

func (s *SessionRuntime) handleToolPermission(
	generation *QueryGeneration,
	toolName string,
	toolInput map[string]any,
	request claudecli.ToolPermissionRequest,
) (claudecli.PermissionResult, bool, error) {
	active := false
	s.withLock(func() { active = s.isQueryGenerationActive(generation) })
	if !active {
		return claudecli.PermissionResult{Behavior: "deny", Message: "Tool use aborted"}, false, nil
	}
	result, err := s.interactions.HandleToolPermission(toolName, toolInput, request)
	if err != nil {
		return claudecli.PermissionResult{}, false, err
	}
	return result, false, nil
}

// queryGenerationHooks mirrors queryHooks.ts: generation-scoped SDK hooks
// fenced by revocation.
func (s *SessionRuntime) queryGenerationHooks(generation *QueryGeneration) map[string][]claudecli.HookMatcher {
	guarded := func(callback func(input map[string]any, toolUseID string) map[string]any) claudecli.HookCallback {
		return func(input map[string]any, toolUseID string) map[string]any {
			result := map[string]any{"continue": false}
			s.withLock(func() {
				if !s.isQueryGenerationActive(generation) || generation.Revoked() {
					return
				}
				result = callback(input, toolUseID)
			})
			return result
		}
	}
	postToolUse := guarded(func(input map[string]any, toolUseID string) map[string]any {
		return s.activities.HandlePostToolUseHook(input, toolUseID)
	})
	taskLifecycle := guarded(func(input map[string]any, _ string) map[string]any {
		return s.activities.HandleTaskLifecycleHook(input)
	})
	return map[string][]claudecli.HookMatcher{
		"PostToolUse":   {{Hooks: []claudecli.HookCallback{postToolUse}}},
		"TaskCreated":   {{Hooks: []claudecli.HookCallback{taskLifecycle}}},
		"TaskCompleted": {{Hooks: []claudecli.HookCallback{taskLifecycle}}},
	}
}

// consumeLocked mirrors sessionRuntime.consume(): one consumption loop per
// generation routes SDK messages until the stream ends.
func (s *SessionRuntime) consumeLocked(generation *QueryGeneration) {
	done, ok := generation.beginConsumption()
	if !ok {
		return
	}
	query := generation.Query()
	go func() {
		defer close(done)
		streamErr := s.consumeMessages(generation, query)
		s.mu.Lock()
		defer s.mu.Unlock()
		if streamErr != nil && !isAbortError(streamErr) && !generation.Revoked() {
			s.logAuthRefresh("query_consume.failed", map[string]any{
				"activeTurnId": s.turns.ActiveID(),
				"error":        errorPayload(streamErr),
			})
			s.turns.FailLiveTurns(errorMessage(streamErr))
		}
		if s.queryGeneration != generation {
			return
		}
		s.queryGeneration = nil
		generation.Revoke()
		generation.CloseQuery()
		s.sessionClosed = true
		if s.turns.ActiveTurn() != nil {
			if s.turns.Cancelled() {
				s.turns.SettleActive("turn_canceled", nil)
			} else {
				s.turns.SettleActive("turn_failed", nil)
			}
		}
		s.turns.FailQueuedTurns("Claude SDK session ended")
		s.turns.ClearCancelled()
	}()
}

// consumeMessages drains the query's message stream, routing each message
// under the runtime lock. It returns the terminal stream error, if any.
func (s *SessionRuntime) consumeMessages(generation *QueryGeneration, query SessionQuery) error {
	messages := query.Messages()
	for {
		select {
		case <-generation.cancelCh:
			return errAbort
		case message, ok := <-messages:
			if !ok {
				if generation.Revoked() {
					return errAbort
				}
				return query.ReadError()
			}
			stop := false
			s.mu.Lock()
			if !s.isQueryGenerationActive(generation) {
				stop = true
			} else if generation.ShouldRouteMessage(message) {
				s.router.Handle(message)
			}
			s.mu.Unlock()
			if stop {
				return nil
			}
		}
	}
}
