package claudesidecar

import (
	"strings"
	"sync"
)

// QueryGeneration mirrors queryGeneration.ts: one SDK Query execution
// generation, its prompt/abort resources, and the canceled-tail quarantine.
type QueryGeneration struct {
	promptQueue *asyncPromptQueue
	id          int

	mu      sync.Mutex
	query   SessionQuery
	revoked bool
	// consumptionDone closes when the consume loop for this generation ends.
	consumptionDone chan struct{}
	consumeStarted  bool
	cancelCh        chan struct{}

	quarantineCanceledTail   bool
	expectedPromptUUID       string
	currentPromptObserved    bool
	canceledTaskTailObserved bool
}

func NewQueryGeneration(id int, quarantineCanceledTail bool) *QueryGeneration {
	return &QueryGeneration{
		promptQueue:            newAsyncPromptQueue(),
		id:                     id,
		cancelCh:               make(chan struct{}),
		quarantineCanceledTail: quarantineCanceledTail,
	}
}

func (g *QueryGeneration) Query() SessionQuery {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.query
}

func (g *QueryGeneration) setQuery(query SessionQuery) {
	g.mu.Lock()
	g.query = query
	g.mu.Unlock()
}

func (g *QueryGeneration) Revoked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.revoked
}

// ExpectPromptEcho arms the canceled-tail quarantine for one prompt uuid.
// Caller holds the runtime lock.
func (g *QueryGeneration) ExpectPromptEcho(promptUUID string) {
	if g.quarantineCanceledTail {
		g.expectedPromptUUID = normalizeTitle(promptUUID)
	}
}

// ShouldRouteMessage decides whether a resumed generation routes a message or
// quarantines it as the canceled generation's replayed tail. Caller holds the
// runtime lock.
func (g *QueryGeneration) ShouldRouteMessage(message map[string]any) bool {
	if !g.quarantineCanceledTail {
		return true
	}
	messageType := stringValue(message["type"])
	parentToolUseID := stringValue(message["parent_tool_use_id"])
	if g.isCurrentPromptEcho(message, messageType, parentToolUseID) {
		g.currentPromptObserved = true
		return true
	}
	if isTaskLifecycleTail(message, messageType) {
		g.canceledTaskTailObserved = true
		return false
	}
	if messageType == "result" && parentToolUseID == "" {
		if g.canceledTaskTailObserved || !g.currentPromptObserved {
			g.canceledTaskTailObserved = false
			return false
		}
		g.clearCanceledTailQuarantine()
		return true
	}
	if messageType == "assistant" && parentToolUseID == "" {
		g.clearCanceledTailQuarantine()
	}
	return true
}

// Revoke fences the generation: the prompt queue closes and callbacks stop.
func (g *QueryGeneration) Revoke() {
	g.mu.Lock()
	if g.revoked {
		g.mu.Unlock()
		return
	}
	g.revoked = true
	close(g.cancelCh)
	g.mu.Unlock()
	g.promptQueue.close()
}

// CloseQuery closes the underlying claude process.
func (g *QueryGeneration) CloseQuery() {
	if query := g.Query(); query != nil {
		query.Close()
	}
}

// Shutdown mirrors queryGeneration.ts: revoke first so callbacks are fenced,
// optionally await the SDK interrupt ack, then close, then await consumption.
func (g *QueryGeneration) Shutdown(interrupt bool) error {
	g.Revoke()
	var failure error
	if interrupt {
		if query := g.Query(); query != nil {
			failure = query.Interrupt()
		}
	}
	// Revocation fences callbacks while interrupt waits for its SDK ACK.
	// Closing earlier rejects that pending ACK as a transport failure.
	g.CloseQuery()
	g.awaitConsumption()
	return failure
}

func (g *QueryGeneration) awaitConsumption() {
	g.mu.Lock()
	done := g.consumptionDone
	g.mu.Unlock()
	if done != nil {
		<-done
	}
}

// beginConsumption marks the consume loop started; it reports false when a
// loop already ran for this generation.
func (g *QueryGeneration) beginConsumption() (chan struct{}, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.consumeStarted || g.query == nil {
		return nil, false
	}
	g.consumeStarted = true
	g.consumptionDone = make(chan struct{})
	return g.consumptionDone, true
}

func (g *QueryGeneration) isCurrentPromptEcho(message map[string]any, messageType string, parentToolUseID string) bool {
	if messageType != "user" || parentToolUseID != "" {
		return false
	}
	notificationText := readUserMessageNotificationText(message)
	if containsTaskNotification(notificationText) {
		return false
	}
	return readSDKMessageUUID(message) == g.expectedPromptUUID || notificationText != ""
}

func (g *QueryGeneration) clearCanceledTailQuarantine() {
	g.quarantineCanceledTail = false
	g.expectedPromptUUID = ""
	g.currentPromptObserved = false
	g.canceledTaskTailObserved = false
}

func containsTaskNotification(text string) bool {
	return strings.Contains(text, "<task-notification>")
}

func isTaskLifecycleTail(message map[string]any, messageType string) bool {
	if messageType == "system" {
		switch stringValue(message["subtype"]) {
		case "task_started", "task_progress", "task_notification", "task_updated":
			return true
		default:
			return false
		}
	}
	if messageType == "attachment" {
		return strings.Contains(readQueuedTaskNotificationPrompt(message), "<task-notification>")
	}
	if messageType != "user" {
		return false
	}
	return strings.Contains(readUserMessageNotificationText(message), "<task-notification>")
}
