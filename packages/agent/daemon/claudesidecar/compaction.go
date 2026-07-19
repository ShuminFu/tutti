package claudesidecar

// contextUsageQuery is the query capability the compaction tracker needs.
type contextUsageQuery interface {
	GetContextUsage() (map[string]any, error)
}

// CompactionTracker mirrors compaction.ts: compaction status projection and
// context-usage snapshots.
type CompactionTracker struct {
	inProgress                    bool
	commandTurnID                 string
	completedTurnIDs              map[string]struct{}
	activeTurnID                  func() string
	ensureActive                  func(messageType string)
	clearPendingOrphans           func()
	getQuery                      func() contextUsageQuery
	emit                          Emitter
	contextUsageRequestGeneration int
	// runAsync runs snapshot fetches off the runtime lock; the callback given
	// to it re-enters under the lock.
	runAsync func(fetch func() (map[string]any, error), apply func(map[string]any, error))
}

// CompactionTrackerOptions configures a CompactionTracker.
type CompactionTrackerOptions struct {
	ActiveTurnID        func() string
	EnsureActive        func(messageType string)
	ClearPendingOrphans func()
	GetQuery            func() contextUsageQuery
	Emit                Emitter
	RunAsync            func(fetch func() (map[string]any, error), apply func(map[string]any, error))
}

func NewCompactionTracker(options CompactionTrackerOptions) *CompactionTracker {
	runAsync := options.RunAsync
	if runAsync == nil {
		runAsync = func(fetch func() (map[string]any, error), apply func(map[string]any, error)) {
			apply(fetch())
		}
	}
	return &CompactionTracker{
		completedTurnIDs:    map[string]struct{}{},
		activeTurnID:        options.ActiveTurnID,
		ensureActive:        options.EnsureActive,
		clearPendingOrphans: options.ClearPendingOrphans,
		getQuery:            options.GetQuery,
		emit:                options.Emit,
		runAsync:            runAsync,
	}
}

func (c *CompactionTracker) SelectCommand(turnID string, isCompact bool) {
	if isCompact {
		c.commandTurnID = turnID
	} else {
		c.commandTurnID = ""
	}
}

func (c *CompactionTracker) HandleSystemMessage(subtype string, message map[string]any) bool {
	if subtype == "status" {
		return c.handleStatus(message)
	}
	if subtype != "compact_boundary" {
		return false
	}
	c.ensureActive("compact_boundary")
	turnID := c.eventTurnID()
	c.emitBoundaryUsage(message, turnID)
	c.emitBoundaryCompletion()
	return true
}

// EmitContextUsageSnapshot asynchronously fetches context usage and emits a
// usage_updated event; onResult (optional) receives "emitted", "unavailable",
// or "stale".
func (c *CompactionTracker) EmitContextUsageSnapshot(
	turnID string,
	modelUsage any,
	shouldEmit func() bool,
	onResult func(string),
) {
	if onResult == nil {
		onResult = func(string) {}
	}
	c.contextUsageRequestGeneration++
	requestGeneration := c.contextUsageRequestGeneration
	isCurrent := func() bool {
		if requestGeneration != c.contextUsageRequestGeneration {
			return false
		}
		return shouldEmit == nil || shouldEmit()
	}
	query := c.getQuery()
	if query == nil {
		onResult("unavailable")
		return
	}
	c.runAsync(query.GetContextUsage, func(rawUsage map[string]any, err error) {
		if err != nil {
			// Context usage is best-effort; result usage remains available.
			if isCurrent() {
				onResult("unavailable")
			} else {
				onResult("stale")
			}
			return
		}
		if !isCurrent() {
			onResult("stale")
			return
		}
		if rawUsage == nil {
			onResult("unavailable")
			return
		}
		usedTokens := numberValue(rawUsage["totalTokens"])
		contextWindowTokens := contextWindowTokensFromModelUsage(modelUsage)
		if contextWindowTokens == 0 {
			contextWindowTokens = numberValue(rawUsage["maxTokens"])
		}
		if usedTokens <= 0 && contextWindowTokens <= 0 {
			onResult("unavailable")
			return
		}
		contextWindow := map[string]any{
			"usedTokens":            usedTokens,
			"compactsAutomatically": rawUsage["isAutoCompactEnabled"] == true,
		}
		if contextWindowTokens > 0 {
			contextWindow["totalTokens"] = contextWindowTokens
		}
		emitUsageUpdated(c.emit, turnID, map[string]any{"contextWindow": contextWindow})
		onResult("emitted")
	})
}

func (c *CompactionTracker) handleStatus(message map[string]any) bool {
	if message["status"] == "compacting" {
		c.ensureActive("compact_status")
		c.clearPendingOrphans()
		c.inProgress = true
		c.emit("compact_started", "", map[string]any{
			"turnId":  c.activeTurnID(),
			"content": "Compacting...",
		})
		return true
	}
	compactResult := stringValue(message["compact_result"])
	if compactResult == "success" && c.inProgress {
		c.inProgress = false
		turnID := c.eventTurnID()
		c.emitCompleted(turnID, "Compacting completed.")
		c.EmitContextUsageSnapshot(turnID, nil, nil, nil)
		return true
	}
	if compactResult == "failed" && c.inProgress {
		c.inProgress = false
		turnID := c.activeTurnID()
		reason := collapseRepeatedText(stringValue(message["compact_error"]))
		content := "Compacting failed."
		if reason != "" {
			content = "Compacting failed: " + reason
		}
		c.emit("compact_failed", "", map[string]any{
			"turnId":  turnID,
			"reason":  reason,
			"content": content,
		})
		return true
	}
	return false
}

func (c *CompactionTracker) emitBoundaryUsage(message map[string]any, turnID string) {
	metadata := recordValue(message["compact_metadata"])
	postTokens := numberValue(metadata["post_tokens"])
	preTokens := numberValue(metadata["pre_tokens"])
	if postTokens > 0 && turnID != "" {
		contextWindow := map[string]any{"usedTokens": postTokens}
		if preTokens > 0 {
			contextWindow["lastUsedTokens"] = preTokens
		}
		emitUsageUpdated(c.emit, turnID, map[string]any{"contextWindow": contextWindow})
	}
	c.EmitContextUsageSnapshot(turnID, nil, nil, nil)
}

func (c *CompactionTracker) emitBoundaryCompletion() {
	turnID := c.eventTurnID()
	if turnID == "" {
		return
	}
	if !c.inProgress && turnID != c.commandTurnID {
		return
	}
	c.inProgress = false
	c.emitCompleted(turnID, "Compacting completed.")
}

func (c *CompactionTracker) emitCompleted(turnID string, content string) {
	if turnID == "" {
		return
	}
	if _, completed := c.completedTurnIDs[turnID]; completed {
		return
	}
	c.completedTurnIDs[turnID] = struct{}{}
	c.emit("compact_completed", "", map[string]any{"turnId": turnID, "content": content})
}

func (c *CompactionTracker) eventTurnID() string {
	if active := c.activeTurnID(); active != "" {
		return active
	}
	return c.commandTurnID
}

func collapseRepeatedText(value string) string {
	text := normalizeTitleKeepingNewlines(value)
	if len(text) < 2 {
		return text
	}
	for repetitions := 2; repetitions <= 4; repetitions++ {
		if len(text)%repetitions != 0 {
			continue
		}
		unit := text[:len(text)/repetitions]
		repeated := ""
		for i := 0; i < repetitions; i++ {
			repeated += unit
		}
		if repeated == text {
			return unit
		}
	}
	return text
}
