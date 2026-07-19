package claudesidecar

import (
	"context"
	"errors"
	"reflect"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

// InteractiveSubmission mirrors interactive.ts's InteractiveSubmission.
type InteractiveSubmission struct {
	RequestID string
	Action    string
	OptionID  string
	Payload   map[string]any
	TurnID    string
}

// InteractiveSubmitResult carries a submission's disposition.
type InteractiveSubmitResult struct {
	Disposition string // pending, answered, superseded, conflict, unknown
	Replayed    bool
	HasReplayed bool
}

func (r InteractiveSubmitResult) payload() map[string]any {
	payload := map[string]any{"disposition": r.Disposition}
	if r.HasReplayed {
		payload["replayed"] = r.Replayed
	}
	return payload
}

type pendingInteraction struct {
	turnID string
	done   chan interactionOutcome
}

type interactionOutcome struct {
	submission InteractiveSubmission
	err        error
}

type terminalInteraction struct {
	disposition string
	submission  *InteractiveSubmission
}

const terminalInteractionCapacity = 1024

//nolint:staticcheck // Mirrors the TypeScript sidecar's exact error text.
var errToolUseAborted = errors.New("Tool use aborted")

// InteractiveCoordinator mirrors interactive.ts: it owns pending interactive
// requests and the bounded terminal disposition registry that makes
// submit_interactive idempotent.
//
// The lock hook runs its callback holding the session runtime lock;
// HandleToolPermission is called from query callback goroutines without the
// lock held and blocks outside it.
type InteractiveCoordinator struct {
	pending               map[string]*pendingInteraction
	terminal              map[string]*terminalInteraction
	terminalOrder         []string
	settings              *SessionSettings
	resolveTurnID         func(request claudecli.ToolPermissionRequest) string
	activateSyntheticTurn func() string
	emit                  Emitter
	lock                  func(func())
}

// InteractiveCoordinatorOptions configures an InteractiveCoordinator.
type InteractiveCoordinatorOptions struct {
	Settings              *SessionSettings
	ResolveTurnID         func(request claudecli.ToolPermissionRequest) string
	ActivateSyntheticTurn func() string
	Emit                  Emitter
	Lock                  func(func())
}

func NewInteractiveCoordinator(options InteractiveCoordinatorOptions) *InteractiveCoordinator {
	lock := options.Lock
	if lock == nil {
		lock = func(callback func()) { callback() }
	}
	return &InteractiveCoordinator{
		pending:               map[string]*pendingInteraction{},
		terminal:              map[string]*terminalInteraction{},
		settings:              options.Settings,
		resolveTurnID:         options.ResolveTurnID,
		activateSyntheticTurn: options.ActivateSyntheticTurn,
		emit:                  options.Emit,
		lock:                  lock,
	}
}

// Submit resolves one pending interaction. Caller must hold the runtime lock.
func (c *InteractiveCoordinator) Submit(turnID string, requestID string, action string, optionID string, payload map[string]any) InteractiveSubmitResult {
	key := interactionKey(turnID, requestID)
	submission := InteractiveSubmission{
		RequestID: requestID,
		Action:    action,
		OptionID:  optionID,
		Payload:   payload,
		TurnID:    turnID,
	}
	if previous, exists := c.terminal[key]; exists {
		if previous.disposition == "superseded" {
			return InteractiveSubmitResult{Disposition: "superseded", Replayed: true, HasReplayed: true}
		}
		if previous.submission != nil && reflect.DeepEqual(*previous.submission, submission) {
			return InteractiveSubmitResult{Disposition: "answered", Replayed: true, HasReplayed: true}
		}
		return InteractiveSubmitResult{Disposition: "conflict"}
	}
	pending, exists := c.pending[key]
	if !exists {
		return InteractiveSubmitResult{Disposition: "unknown"}
	}
	delete(c.pending, key)
	c.recordTerminal(key, &terminalInteraction{disposition: "answered", submission: &submission})
	pending.done <- interactionOutcome{submission: submission}
	return InteractiveSubmitResult{Disposition: "answered", Replayed: false, HasReplayed: true}
}

// Disposition reports one interaction's terminal state. Caller must hold the
// runtime lock.
func (c *InteractiveCoordinator) Disposition(turnID string, requestID string, expected *InteractiveSubmission) InteractiveSubmitResult {
	key := interactionKey(turnID, requestID)
	if terminal, exists := c.terminal[key]; exists {
		if terminal.disposition == "answered" && expected != nil {
			expectedSubmission := *expected
			expectedSubmission.RequestID = requestID
			expectedSubmission.TurnID = turnID
			if terminal.submission == nil || !reflect.DeepEqual(*terminal.submission, expectedSubmission) {
				return InteractiveSubmitResult{Disposition: "conflict"}
			}
		}
		return InteractiveSubmitResult{Disposition: terminal.disposition, Replayed: true, HasReplayed: true}
	}
	if _, exists := c.pending[key]; exists {
		return InteractiveSubmitResult{Disposition: "pending"}
	}
	return InteractiveSubmitResult{Disposition: "unknown"}
}

// RejectAll fails every pending interaction. Caller must hold the runtime lock.
func (c *InteractiveCoordinator) RejectAll(err error) {
	for key, pending := range c.pending {
		delete(c.pending, key)
		c.recordTerminal(key, &terminalInteraction{disposition: "superseded"})
		pending.done <- interactionOutcome{err: err}
	}
}

// HandleToolPermission is the canUseTool callback. It is called without the
// runtime lock held and blocks until the daemon submits an answer.
func (c *InteractiveCoordinator) HandleToolPermission(
	toolName string,
	toolInput map[string]any,
	request claudecli.ToolPermissionRequest,
) (claudecli.PermissionResult, error) {
	if toolName == "AskUserQuestion" {
		return c.handleAskUserQuestion(toolInput, request)
	}
	if toolName == "ExitPlanMode" {
		return c.handleExitPlanMode(toolInput, request)
	}
	bypass := false
	c.lock(func() {
		bypass = effectivePermissionMode(c.settings) == "bypassPermissions"
	})
	if bypass {
		return claudecli.PermissionResult{Behavior: "allow", UpdatedInput: toolInput}, nil
	}
	submission, err := c.request("approval_requested", toolName, toolInput, approvalOptions(), request)
	if err != nil {
		return claudecli.PermissionResult{}, err
	}
	c.emitResolved("approval_resolved", submission)
	if isAllowOption(submission.OptionID) {
		result := claudecli.PermissionResult{Behavior: "allow", UpdatedInput: toolInput}
		if submission.OptionID == "allow_always" && request.HasSuggestions {
			result.UpdatedPermissions = append([]map[string]any{}, request.Suggestions...)
		}
		return result, nil
	}
	message := stringValue(submission.Payload["denyMessage"])
	if message == "" {
		message = "User refused permission to run tool"
	}
	return claudecli.PermissionResult{Behavior: "deny", Message: message}, nil
}

func (c *InteractiveCoordinator) handleAskUserQuestion(
	toolInput map[string]any,
	request claudecli.ToolPermissionRequest,
) (claudecli.PermissionResult, error) {
	submission, err := c.request("user_input_requested", "AskUserQuestion", toolInput, []map[string]any{}, request)
	if err != nil {
		return claudecli.PermissionResult{}, err
	}
	c.emitResolved("user_input_resolved", submission)
	return claudecli.PermissionResult{
		Behavior: "allow",
		UpdatedInput: map[string]any{
			"questions": toolInput["questions"],
			"answers":   answersFromInteractivePayload(submission.Payload, toolInput),
		},
	}, nil
}

func (c *InteractiveCoordinator) handleExitPlanMode(
	toolInput map[string]any,
	request claudecli.ToolPermissionRequest,
) (claudecli.PermissionResult, error) {
	submission, err := c.request("user_input_requested", "ExitPlanMode", toolInput, exitPlanOptions(), request)
	if err != nil {
		return claudecli.PermissionResult{}, err
	}
	c.emitResolved("user_input_resolved", submission)
	if !isExitPlanAllowOption(submission.OptionID) {
		return claudecli.PermissionResult{
			Behavior: "deny",
			Message:  "User rejected request to exit plan mode.",
		}, nil
	}
	result := claudecli.PermissionResult{Behavior: "allow", UpdatedInput: toolInput}
	if request.HasSuggestions {
		result.UpdatedPermissions = append([]map[string]any{}, request.Suggestions...)
	} else {
		result.UpdatedPermissions = []map[string]any{
			{
				"type":        "setMode",
				"mode":        submission.OptionID,
				"destination": "session",
			},
		}
	}
	return result, nil
}

func (c *InteractiveCoordinator) request(
	eventType string,
	toolName string,
	toolInput map[string]any,
	options []map[string]any,
	callbackOptions claudecli.ToolPermissionRequest,
) (InteractiveSubmission, error) {
	requestID := randomUUID()
	toolUseID := callbackOptions.ToolUseID
	if toolUseID == "" {
		toolUseID = requestID
	}
	agentID := normalizeTitle(callbackOptions.AgentID)
	pending := &pendingInteraction{done: make(chan interactionOutcome, 1)}
	var key string
	c.lock(func() {
		turnID := c.resolveTurnID(callbackOptions)
		if turnID == "" {
			turnID = c.activateSyntheticTurn()
		}
		pending.turnID = turnID
		key = interactionKey(turnID, requestID)
		c.pending[key] = pending
		payload := map[string]any{
			"turnId":     turnID,
			"requestId":  requestID,
			"toolCallId": toolUseID,
			"toolName":   toolName,
			"input":      toolInput,
			"options":    options,
			"toolCall": map[string]any{
				"toolCallId": toolUseID,
				"name":       toolName,
				"title":      toolName,
				"toolName":   toolName,
				"input":      toolInput,
			},
		}
		if agentID != "" {
			payload["agentId"] = agentID
		}
		c.emit(eventType, "", payload)
	})
	ctx := callbackOptions.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	stopWatch := context.AfterFunc(ctx, func() {
		c.lock(func() {
			if _, stillPending := c.pending[key]; !stillPending {
				return
			}
			delete(c.pending, key)
			c.recordTerminal(key, &terminalInteraction{disposition: "superseded"})
			pending.done <- interactionOutcome{err: errToolUseAborted}
		})
	})
	defer stopWatch()
	outcome := <-pending.done
	if outcome.err != nil {
		return InteractiveSubmission{}, outcome.err
	}
	return outcome.submission, nil
}

func (c *InteractiveCoordinator) emitResolved(eventType string, submission InteractiveSubmission) {
	c.lock(func() {
		c.emit(eventType, "", map[string]any{
			"turnId":    submission.TurnID,
			"requestId": submission.RequestID,
			"action":    submission.Action,
			"optionId":  submission.OptionID,
			"payload":   submission.Payload,
		})
	})
}

func (c *InteractiveCoordinator) recordTerminal(key string, terminal *terminalInteraction) {
	if _, exists := c.terminal[key]; exists {
		return
	}
	c.terminal[key] = terminal
	c.terminalOrder = append(c.terminalOrder, key)
	for len(c.terminalOrder) > terminalInteractionCapacity {
		oldest := c.terminalOrder[0]
		c.terminalOrder = c.terminalOrder[1:]
		delete(c.terminal, oldest)
	}
}

func interactionKey(turnID string, requestID string) string {
	return normalizeTitle(turnID) + "\x00" + normalizeTitle(requestID)
}
