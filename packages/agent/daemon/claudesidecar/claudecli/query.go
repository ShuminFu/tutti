package claudecli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ErrQueryClosed matches the SDK's "Query closed before response received"
// rejection; the daemon classifies restore failures by this exact text.
var ErrQueryClosed = errors.New("Query closed before response received")

const unmatchedControlResponsesMax = 1024

// Query is the Go equivalent of the SDK Query object: an SDK message stream
// plus control-protocol methods against one claude CLI process.
type Query struct {
	options   *Options
	transport *transport

	messages chan map[string]any

	readMu  sync.Mutex
	readErr error

	pendingMu      sync.Mutex
	pending        map[string]chan controlResponse
	unmatched      map[string]controlResponse
	unmatchedOrder []string
	closed         bool

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc

	hookCallbacks map[string]HookCallback

	initOnce   sync.Once
	initDone   chan struct{}
	initResult map[string]any
	initErr    error

	lastErrorResultMu  sync.Mutex
	lastErrorResultSet bool
	lastErrorResult    string

	closeOnce sync.Once
}

type controlResponse struct {
	subtype  string
	response map[string]any
	err      string
	raw      map[string]any
}

// Start spawns the claude CLI and begins reading messages. The initialize
// handshake is started immediately; await it with InitializationResult.
func Start(options *Options) (*Query, error) {
	callbacks := map[string]HookCallback{}
	initRequest := options.initializeRequest(callbacks)
	transport, err := startTransport(options)
	if err != nil {
		return nil, err
	}
	q := &Query{
		options:       options,
		transport:     transport,
		messages:      make(chan map[string]any, 64),
		pending:       map[string]chan controlResponse{},
		unmatched:     map[string]controlResponse{},
		cancels:       map[string]context.CancelFunc{},
		hookCallbacks: callbacks,
		initDone:      make(chan struct{}),
	}
	go q.readMessages()
	go q.initialize(initRequest)
	return q, nil
}

// Messages is the SDK message stream. It closes when the query ends; check
// ReadError afterwards.
func (q *Query) Messages() <-chan map[string]any {
	return q.messages
}

// ReadError reports the terminal stream failure, if any.
func (q *Query) ReadError() error {
	q.readMu.Lock()
	defer q.readMu.Unlock()
	return q.readErr
}

func (q *Query) initialize(request map[string]any) {
	response, err := q.request(request)
	q.initOnce.Do(func() {
		if err != nil {
			q.initErr = err
		} else {
			q.initResult = response.response
		}
		close(q.initDone)
	})
	if err == nil {
		q.processPendingPermissionRedelivery(response.raw)
	}
}

// InitializationResult awaits the initialize control response.
func (q *Query) InitializationResult() (map[string]any, error) {
	<-q.initDone
	return q.initResult, q.initErr
}

// WriteUserMessage streams one user message to the CLI.
func (q *Query) WriteUserMessage(message map[string]any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return q.transport.write(append(data, '\n'))
}

// EndInput closes the CLI's stdin stream.
func (q *Query) EndInput() {
	q.transport.endInput()
}

// Interrupt sends the interrupt control request and awaits its ack.
func (q *Query) Interrupt() error {
	_, err := q.request(map[string]any{"subtype": "interrupt"})
	return err
}

// StopTask stops one background task by SDK task id.
func (q *Query) StopTask(taskID string) error {
	_, err := q.request(map[string]any{"subtype": "stop_task", "task_id": taskID})
	return err
}

// SetPermissionMode switches the CLI permission mode.
func (q *Query) SetPermissionMode(mode string) error {
	_, err := q.request(map[string]any{"subtype": "set_permission_mode", "mode": mode})
	return err
}

// SetModel switches the model; an empty model selects the CLI default.
func (q *Query) SetModel(model string) error {
	request := map[string]any{"subtype": "set_model"}
	if model != "" {
		request["model"] = model
	}
	_, err := q.request(request)
	return err
}

// ApplyFlagSettings applies live flag settings (fastMode, effortLevel).
func (q *Query) ApplyFlagSettings(settings map[string]any) error {
	_, err := q.request(map[string]any{"subtype": "apply_flag_settings", "settings": settings})
	return err
}

// GetContextUsage fetches the CLI's context usage snapshot.
func (q *Query) GetContextUsage() (map[string]any, error) {
	response, err := q.request(map[string]any{"subtype": "get_context_usage"})
	if err != nil {
		return nil, err
	}
	return response.response, nil
}

// Close tears the query down: pending control requests are rejected, the
// process is asked to exit (SIGTERM then SIGKILL on the SDK timings).
func (q *Query) Close() {
	q.closeOnce.Do(func() {
		q.pendingMu.Lock()
		q.closed = true
		pending := q.pending
		q.pending = map[string]chan controlResponse{}
		q.unmatched = map[string]controlResponse{}
		q.unmatchedOrder = nil
		q.pendingMu.Unlock()
		for _, waiter := range pending {
			close(waiter)
		}
		q.cancelMu.Lock()
		for _, cancel := range q.cancels {
			cancel()
		}
		q.cancels = map[string]context.CancelFunc{}
		q.cancelMu.Unlock()
		q.initOnce.Do(func() {
			q.initErr = ErrQueryClosed
			close(q.initDone)
		})
		q.transport.close()
	})
}

func (q *Query) request(request map[string]any) (controlResponse, error) {
	requestID := newRequestID()
	waiter := make(chan controlResponse, 1)
	q.pendingMu.Lock()
	if q.closed {
		q.pendingMu.Unlock()
		return controlResponse{}, ErrQueryClosed
	}
	if unmatched, ok := q.unmatched[requestID]; ok {
		delete(q.unmatched, requestID)
		q.pendingMu.Unlock()
		return settleControlResponse(unmatched)
	}
	q.pending[requestID] = waiter
	q.pendingMu.Unlock()

	envelope := map[string]any{
		"request_id": requestID,
		"type":       "control_request",
		"request":    request,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		q.removePending(requestID)
		return controlResponse{}, err
	}
	if writeErr := q.transport.write(append(data, '\n')); writeErr != nil {
		q.removePending(requestID)
		return controlResponse{}, writeErr
	}
	response, ok := <-waiter
	if !ok {
		return controlResponse{}, ErrQueryClosed
	}
	return settleControlResponse(response)
}

func settleControlResponse(response controlResponse) (controlResponse, error) {
	if response.subtype != "success" {
		message := response.err
		if message == "" {
			message = "Claude Code control request failed"
		}
		return controlResponse{}, errors.New(message)
	}
	return response, nil
}

func (q *Query) removePending(requestID string) {
	q.pendingMu.Lock()
	delete(q.pending, requestID)
	q.pendingMu.Unlock()
}

func (q *Query) readMessages() {
	var terminal error
	for line := range q.transport.lines {
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		switch message["type"] {
		case "control_response":
			q.routeControlResponse(message)
		case "control_request":
			go q.handleControlRequest(message)
		case "control_cancel_request":
			q.handleControlCancelRequest(message)
		case "keep_alive", "transcript_mirror":
			// Neither carries session content the sidecar consumes.
		default:
			q.trackErrorResult(message)
			q.messages <- message
		}
	}
	terminal = q.transport.exitError()
	if terminal != nil {
		if text, ok := q.lastErrorResultText(); ok {
			//nolint:staticcheck // Mirrors the Node SDK's exact error text.
			terminal = fmt.Errorf("Claude Code returned an error result: %s", text)
		}
	}
	q.readMu.Lock()
	q.readErr = terminal
	q.readMu.Unlock()
	close(q.messages)
	q.Close()
}

func (q *Query) trackErrorResult(message map[string]any) {
	messageType, _ := message["type"].(string)
	subtype, _ := message["subtype"].(string)
	if messageType == "result" {
		text := ""
		isError := message["is_error"] == true
		if isError {
			if subtype == "success" {
				text, _ = message["result"].(string)
			} else if rawErrors, ok := message["errors"].([]any); ok {
				parts := make([]string, 0, len(rawErrors))
				for _, item := range rawErrors {
					if itemText, isString := item.(string); isString {
						parts = append(parts, itemText)
					}
				}
				text = strings.Join(parts, "; ")
			}
		}
		q.lastErrorResultMu.Lock()
		q.lastErrorResultSet = isError
		q.lastErrorResult = text
		q.lastErrorResultMu.Unlock()
		return
	}
	if messageType == "system" && subtype == "session_state_changed" {
		return
	}
	q.lastErrorResultMu.Lock()
	q.lastErrorResultSet = false
	q.lastErrorResult = ""
	q.lastErrorResultMu.Unlock()
}

func (q *Query) lastErrorResultText() (string, bool) {
	q.lastErrorResultMu.Lock()
	defer q.lastErrorResultMu.Unlock()
	return q.lastErrorResult, q.lastErrorResultSet
}

func (q *Query) routeControlResponse(message map[string]any) {
	response, _ := message["response"].(map[string]any)
	if response == nil {
		return
	}
	requestID, _ := response["request_id"].(string)
	if requestID == "" {
		return
	}
	parsed := controlResponse{raw: response}
	parsed.subtype, _ = response["subtype"].(string)
	parsed.response, _ = response["response"].(map[string]any)
	parsed.err, _ = response["error"].(string)

	q.pendingMu.Lock()
	waiter, ok := q.pending[requestID]
	if ok {
		delete(q.pending, requestID)
		q.pendingMu.Unlock()
		waiter <- parsed
		return
	}
	if q.closed {
		q.pendingMu.Unlock()
		return
	}
	if len(q.unmatchedOrder) >= unmatchedControlResponsesMax {
		oldest := q.unmatchedOrder[0]
		q.unmatchedOrder = q.unmatchedOrder[1:]
		delete(q.unmatched, oldest)
	}
	q.unmatched[requestID] = parsed
	q.unmatchedOrder = append(q.unmatchedOrder, requestID)
	q.pendingMu.Unlock()
}

// processPendingPermissionRedelivery replays permission and user-dialog
// requests attached to the initialize response, the way the SDK does.
func (q *Query) processPendingPermissionRedelivery(response map[string]any) {
	if response == nil {
		return
	}
	for _, key := range []string{"pending_permission_requests", "pending_user_dialog_requests"} {
		entries, ok := response[key].([]any)
		if !ok {
			continue
		}
		for _, entry := range entries {
			envelope, isRecord := entry.(map[string]any)
			if !isRecord {
				continue
			}
			request, _ := envelope["request"].(map[string]any)
			if request == nil {
				continue
			}
			subtype, _ := request["subtype"].(string)
			if subtype != "can_use_tool" && subtype != "request_user_dialog" {
				continue
			}
			go q.handleControlRequest(envelope)
		}
	}
}

func (q *Query) handleControlRequest(envelope map[string]any) {
	requestID, _ := envelope["request_id"].(string)
	request, _ := envelope["request"].(map[string]any)
	if requestID == "" || request == nil {
		return
	}
	q.cancelMu.Lock()
	if _, inFlight := q.cancels[requestID]; inFlight {
		q.cancelMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	q.cancels[requestID] = cancel
	q.cancelMu.Unlock()
	defer func() {
		q.cancelMu.Lock()
		delete(q.cancels, requestID)
		q.cancelMu.Unlock()
		cancel()
	}()

	response, suppress, err := q.processControlRequest(requestID, request, ctx)
	if suppress {
		return
	}
	var envelopeOut map[string]any
	if err != nil {
		envelopeOut = map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "error",
				"request_id": requestID,
				"error":      err.Error(),
			},
		}
	} else {
		envelopeOut = map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response":   response,
			},
		}
	}
	data, marshalErr := json.Marshal(envelopeOut)
	if marshalErr != nil {
		return
	}
	_ = q.transport.write(append(data, '\n'))
}

func (q *Query) processControlRequest(requestID string, request map[string]any, ctx context.Context) (map[string]any, bool, error) {
	subtype, _ := request["subtype"].(string)
	switch subtype {
	case "can_use_tool":
		if q.options.CanUseTool == nil {
			//nolint:revive,staticcheck // Mirrors the Node SDK's exact error text.
			return nil, false, errors.New("canUseTool callback is not provided.")
		}
		toolName, _ := request["tool_name"].(string)
		input, _ := request["input"].(map[string]any)
		toolUseID, _ := request["tool_use_id"].(string)
		permissionRequest := ToolPermissionRequest{
			Ctx:       ctx,
			ToolUseID: toolUseID,
			RequestID: requestID,
		}
		if suggestions, ok := request["permission_suggestions"].([]any); ok {
			permissionRequest.HasSuggestions = true
			for _, suggestion := range suggestions {
				if record, isRecord := suggestion.(map[string]any); isRecord {
					permissionRequest.Suggestions = append(permissionRequest.Suggestions, record)
				}
			}
		}
		permissionRequest.BlockedPath, _ = request["blocked_path"].(string)
		permissionRequest.DecisionReason = request["decision_reason"]
		permissionRequest.Title, _ = request["title"].(string)
		permissionRequest.AgentID, _ = request["agent_id"].(string)
		result, suppress, err := q.options.CanUseTool(toolName, input, permissionRequest)
		if err != nil {
			return nil, false, err
		}
		if suppress {
			return nil, true, nil
		}
		response := map[string]any{"behavior": result.Behavior, "toolUseID": toolUseID}
		if result.Behavior == "allow" {
			response["updatedInput"] = result.UpdatedInput
			if result.UpdatedPermissions != nil {
				response["updatedPermissions"] = result.UpdatedPermissions
			}
		} else {
			response["message"] = result.Message
		}
		return response, false, nil
	case "hook_callback":
		callbackID, _ := request["callback_id"].(string)
		callback := q.hookCallbacks[callbackID]
		if callback == nil {
			//nolint:staticcheck // Mirrors the Node SDK's exact error text.
			return nil, false, fmt.Errorf("No hook callback found for ID: %s", callbackID)
		}
		input, _ := request["input"].(map[string]any)
		toolUseID, _ := request["tool_use_id"].(string)
		return callback(input, toolUseID), false, nil
	case "request_user_dialog":
		// No onUserDialog handler: stay silent so a capable client (or the
		// worker's park deadline) settles it, matching the SDK.
		return nil, true, nil
	default:
		//nolint:staticcheck // Mirrors the Node SDK's exact error text.
		return nil, false, fmt.Errorf("Unsupported control request subtype: %s", subtype)
	}
}

func (q *Query) handleControlCancelRequest(message map[string]any) {
	requestID, _ := message["request_id"].(string)
	if requestID == "" {
		return
	}
	q.cancelMu.Lock()
	cancel, ok := q.cancels[requestID]
	if ok {
		delete(q.cancels, requestID)
	}
	q.cancelMu.Unlock()
	if ok {
		cancel()
	}
}

// newRequestID mirrors Math.random().toString(36).substring(2, 15).
func newRequestID() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	var raw [13]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "reqfallbackid"
	}
	id := make([]byte, len(raw))
	for i, b := range raw {
		id[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(id)
}
