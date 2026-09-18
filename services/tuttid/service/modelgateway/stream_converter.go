package modelgateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

type chatStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []chatStreamChoice `json:"choices"`
	Usage   *chatUsage         `json:"usage"`
	Error   json.RawMessage    `json:"error"`
}

type chatStreamChoice struct {
	Index        int             `json:"index"`
	Delta        chatStreamDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type chatStreamDelta struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ReasoningContent json.RawMessage `json:"reasoning_content"`
	Reasoning        json.RawMessage `json:"reasoning"`
	ToolCalls        []chatToolCall  `json:"tool_calls"`
	FunctionCall     *struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function_call"`
}

type streamItem interface {
	outputIndex() int
	finish(*responsesSSEWriter) (map[string]any, error)
}

type streamedTextMode uint8

const (
	streamedTextModeUnknown streamedTextMode = iota
	streamedTextModeDelta
	streamedTextModeSnapshot
)

// streamedText keeps the mode unknown until a chunk proves whether an upstream
// sends incremental tokens or cumulative snapshots.
type streamedText struct {
	text strings.Builder
	base string
	mode streamedTextMode
}

func (s *streamedText) String() string {
	return s.text.String()
}

func (s *streamedText) append(next string) string {
	if next == "" {
		return ""
	}
	previous := s.text.String()
	if previous == "" {
		_, _ = s.text.WriteString(next)
		s.base = next
		return next
	}

	switch s.mode {
	case streamedTextModeDelta:
		return s.appendDelta(next)
	case streamedTextModeSnapshot:
		return s.appendSnapshot(next)
	}

	if strings.TrimSpace(next) == "" {
		_, _ = s.text.WriteString(next)
		return next
	}
	suffix, extends := streamedTextSuffix(s.base, next)
	if extends && strings.TrimSpace(suffix) != "" {
		s.mode = streamedTextModeSnapshot
		s.base = next
		return s.appendDelta(suffix)
	}
	if !extends {
		if _, retreats := streamedTextSuffix(next, s.base); retreats {
			return ""
		}
	}
	if extends {
		s.base = next
		return ""
	}
	s.mode = streamedTextModeDelta
	return s.appendDelta(next)
}

func (s *streamedText) appendSnapshot(next string) string {
	if strings.TrimSpace(next) == "" {
		return s.appendDelta(next)
	}
	suffix, extends := streamedTextSuffix(s.base, next)
	if extends && strings.TrimSpace(suffix) != "" {
		s.base = next
		return s.appendDelta(suffix)
	}
	if !extends {
		if _, retreats := streamedTextSuffix(next, s.base); retreats {
			return ""
		}
	}
	if extends {
		s.base = next
		return ""
	}
	s.mode = streamedTextModeDelta
	s.base = next
	return s.appendDelta(next)
}

func streamedTextSuffix(base string, next string) (string, bool) {
	baseRunes := []rune(base)
	nextRunes := []rune(next)
	baseIndex := 0
	nextIndex := 0
	for baseIndex < len(baseRunes) {
		if unicode.IsSpace(baseRunes[baseIndex]) {
			baseIndex++
			continue
		}
		for nextIndex < len(nextRunes) && unicode.IsSpace(nextRunes[nextIndex]) {
			nextIndex++
		}
		if nextIndex == len(nextRunes) || nextRunes[nextIndex] != baseRunes[baseIndex] {
			return "", false
		}
		baseIndex++
		nextIndex++
	}
	return string(nextRunes[nextIndex:]), true
}

func (s *streamedText) appendDelta(next string) string {
	_, _ = s.text.WriteString(next)
	return next
}

type reasoningStreamItem struct {
	index int
	id    string
	text  streamedText
}

func (i *reasoningStreamItem) outputIndex() int { return i.index }

func (i *reasoningStreamItem) finish(writer *responsesSSEWriter) (map[string]any, error) {
	part := map[string]any{"type": "reasoning_text", "text": i.text.String()}
	if err := writer.Event("response.content_part.done", map[string]any{
		"item_id": i.id, "output_index": i.index, "content_index": 0, "part": part,
	}); err != nil {
		return nil, err
	}
	item := map[string]any{
		"id":                i.id,
		"type":              "reasoning",
		"summary":           []any{},
		"content":           []any{part},
		"encrypted_content": nil,
		"status":            "completed",
	}
	if err := writer.Event("response.output_item.done", map[string]any{
		"output_index": i.index, "item": item,
	}); err != nil {
		return nil, err
	}
	return item, nil
}

type messageStreamItem struct {
	index int
	id    string
	text  streamedText
}

func (i *messageStreamItem) outputIndex() int { return i.index }

func (i *messageStreamItem) finish(writer *responsesSSEWriter) (map[string]any, error) {
	part := map[string]any{
		"type": "output_text", "text": i.text.String(), "annotations": []any{},
	}
	if err := writer.Event("response.output_text.done", map[string]any{
		"item_id": i.id, "output_index": i.index, "content_index": 0, "text": i.text.String(),
	}); err != nil {
		return nil, err
	}
	if err := writer.Event("response.content_part.done", map[string]any{
		"item_id": i.id, "output_index": i.index, "content_index": 0, "part": part,
	}); err != nil {
		return nil, err
	}
	item := map[string]any{
		"id": i.id, "type": "message", "role": "assistant", "status": "completed",
		"content": []any{part},
	}
	if err := writer.Event("response.output_item.done", map[string]any{
		"output_index": i.index, "item": item,
	}); err != nil {
		return nil, err
	}
	return item, nil
}

type toolStreamItem struct {
	index     int
	chatIndex int
	id        string
	callID    string
	name      string
	toolMap   responseToolMap
	// wrappedCustom marks a call whose registered tool is a Responses custom
	// tool that travels upstream as a synthesized function wrapper. Its raw
	// arguments are buffered and only the decoded custom input is streamed.
	wrappedCustom bool
	announced     bool
	arguments     strings.Builder
	flushed       int
}

func (i *toolStreamItem) outputIndex() int { return i.index }

func (i *toolStreamItem) itemType() string {
	if i.wrappedCustom {
		return "custom_tool_call"
	}
	return "function_call"
}

// announce emits output_item.added once the call shape is settled. The item id
// is allocated here because its Responses prefix depends on that shape.
func (i *toolStreamItem) announce(writer *responsesSSEWriter) error {
	if i.announced {
		return nil
	}
	i.announced = true
	if i.id == "" {
		prefix := "fc"
		if i.wrappedCustom {
			prefix = "ctc"
		}
		i.id = newResponseID(prefix)
	}
	inputKey := "arguments"
	if i.wrappedCustom {
		inputKey = "input"
	}
	identity := responseIdentityForChatTool(i.name, i.toolMap)
	item := map[string]any{
		"id": i.id, "type": i.itemType(), "status": "in_progress",
		"call_id": i.callID, "name": identity.Name, inputKey: "",
	}
	if identity.Namespace != "" {
		item["namespace"] = identity.Namespace
	}
	return writer.Event("response.output_item.added", map[string]any{
		"output_index": i.index, "item": item,
	})
}

// observeName classifies the call once its streamed name can no longer grow,
// then announces the settled shape. A name that is still a strict prefix of a
// registered tool name stays unclassified so a split name is never mistaken for
// an ordinary function.
func (i *toolStreamItem) observeName(writer *responsesSSEWriter) error {
	if i.announced {
		return nil
	}
	wrapped, settled := classifyToolStreamName(i.name, i.toolMap)
	if !settled || i.callID == "" {
		return nil
	}
	i.wrappedCustom = wrapped
	return i.announce(writer)
}

// settle commits the final call shape at the end of the call. A call whose
// function arguments were already streamed cannot be re-shaped into a custom
// tool call without emitting contradictory events, so that case fails instead
// of fabricating a custom call.
func (i *toolStreamItem) settle() error {
	if !i.announced {
		i.wrappedCustom = i.toolMap[i.name].WrappedCustomInput
		return nil
	}
	if i.wrappedCustom {
		return nil
	}
	if wrapped, settled := classifyToolStreamName(i.name, i.toolMap); settled && wrapped {
		return fmt.Errorf(
			"chat tool call %q was streamed as a function before its registered custom tool name was complete",
			i.name,
		)
	}
	return nil
}

// flushArguments streams buffered function arguments. Wrapped custom arguments
// are never streamed raw: only decodeCustomToolInput output may reach the
// caller as custom tool input.
func (i *toolStreamItem) flushArguments(writer *responsesSSEWriter) error {
	if i.wrappedCustom || !i.announced {
		return nil
	}
	buffered := i.arguments.String()
	if i.flushed >= len(buffered) {
		return nil
	}
	pending := buffered[i.flushed:]
	i.flushed = len(buffered)
	return writer.Event("response.function_call_arguments.delta", map[string]any{
		"item_id": i.id, "output_index": i.index, "delta": pending,
	})
}

func (i *toolStreamItem) finish(writer *responsesSSEWriter) (map[string]any, error) {
	if err := i.settle(); err != nil {
		return nil, err
	}
	if i.callID == "" {
		i.callID = newResponseID("call")
	}
	if err := i.announce(writer); err != nil {
		return nil, err
	}
	identity := responseIdentityForChatTool(i.name, i.toolMap)
	if i.wrappedCustom {
		input, err := decodeCustomToolInput(json.RawMessage(i.arguments.String()))
		if err != nil {
			return nil, fmt.Errorf("decode custom tool call %q: %w", i.name, err)
		}
		if err := writer.Event("response.custom_tool_call_input.delta", map[string]any{
			"item_id": i.id, "output_index": i.index, "delta": input,
		}); err != nil {
			return nil, err
		}
		if err := writer.Event("response.custom_tool_call_input.done", map[string]any{
			"item_id": i.id, "output_index": i.index, "input": input,
		}); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": i.id, "type": "custom_tool_call", "status": "completed",
			"call_id": i.callID, "name": identity.Name, "input": input,
		}
		if identity.Namespace != "" {
			item["namespace"] = identity.Namespace
		}
		if err := writer.Event("response.output_item.done", map[string]any{
			"output_index": i.index, "item": item,
		}); err != nil {
			return nil, err
		}
		return item, nil
	}
	if err := i.flushArguments(writer); err != nil {
		return nil, err
	}
	if err := writer.Event("response.function_call_arguments.done", map[string]any{
		"item_id": i.id, "output_index": i.index, "name": identity.Name, "arguments": i.arguments.String(),
	}); err != nil {
		return nil, err
	}
	item := map[string]any{
		"id": i.id, "type": "function_call", "status": "completed",
		"call_id": i.callID, "name": identity.Name, "arguments": i.arguments.String(),
	}
	if identity.Namespace != "" {
		item["namespace"] = identity.Namespace
	}
	if err := writer.Event("response.output_item.done", map[string]any{
		"output_index": i.index, "item": item,
	}); err != nil {
		return nil, err
	}
	return item, nil
}

// classifyToolStreamName resolves whether an accumulated Chat tool name is an
// ordinary function or a synthesized wrapper for a registered custom tool. A
// non-empty name that is still a strict prefix of a registered tool name is not
// settled, because the upstream may still be streaming that name.
func classifyToolStreamName(name string, toolMap responseToolMap) (wrapped bool, settled bool) {
	if name == "" {
		return false, false
	}
	for chatName := range toolMap {
		if chatName != name && strings.HasPrefix(chatName, name) {
			return false, false
		}
	}
	return toolMap[name].WrappedCustomInput, true
}

type chatStreamState struct {
	request        responsesRequest
	writer         *responsesSSEWriter
	responseID     string
	createdAt      int64
	model          string
	upstreamChatID string
	items          []streamItem
	reasoning      *reasoningStreamItem
	message        *messageStreamItem
	tools          map[int]*toolStreamItem
	usage          *chatUsage
	finishReason   *string
	sawFinish      bool
	toolMap        responseToolMap
}

func newChatStreamState(
	request responsesRequest,
	writer *responsesSSEWriter,
	toolMap responseToolMap,
) *chatStreamState {
	return &chatStreamState{
		request:    request,
		writer:     writer,
		responseID: newResponseID("resp"),
		createdAt:  time.Now().Unix(),
		model:      request.Model,
		tools:      make(map[int]*toolStreamItem),
		toolMap:    toolMap,
	}
}

func (s *chatStreamState) start() error {
	response := responseObject(
		s.request, s.responseID, s.createdAt, s.model, "in_progress",
		[]any{}, nil, nil, nil,
	)
	if err := s.writer.Event("response.created", map[string]any{"response": response}); err != nil {
		return err
	}
	return s.writer.Event("response.in_progress", map[string]any{"response": response})
}

func (s *chatStreamState) process(chunk chatStreamChunk) error {
	if id := strings.TrimSpace(chunk.ID); id != "" {
		s.upstreamChatID = id
	}
	if chunk.Created > 0 {
		s.createdAt = chunk.Created
	}
	if strings.TrimSpace(chunk.Model) != "" {
		s.model = chunk.Model
	}
	if chunk.Usage != nil {
		s.usage = chunk.Usage
	}
	if len(bytes.TrimSpace(chunk.Error)) > 0 && !bytes.Equal(bytes.TrimSpace(chunk.Error), []byte("null")) {
		return errors.New("upstream Chat stream returned an error")
	}
	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			continue
		}
		if err := s.processDelta(choice.Delta); err != nil {
			return err
		}
		// Only a reason that names something counts as the upstream saying it
		// stopped. Gateways that stamp an empty finish_reason on every chunk would
		// otherwise mark the turn finished one chunk in, and a later truncation
		// would then be indistinguishable from a clean ending.
		if hasTerminalFinishReason(choice.FinishReason) {
			s.finishReason = choice.FinishReason
			s.sawFinish = true
		}
	}
	return nil
}

func (s *chatStreamState) processDelta(delta chatStreamDelta) error {
	reasoning, err := chatText(delta.ReasoningContent)
	if err != nil {
		return fmt.Errorf("decode upstream reasoning_content delta: %w", err)
	}
	if reasoning == "" {
		reasoning, err = chatText(delta.Reasoning)
		if err != nil {
			return fmt.Errorf("decode upstream reasoning delta: %w", err)
		}
	}
	if reasoning != "" {
		if err := s.addReasoning(reasoning); err != nil {
			return err
		}
	}
	text, err := chatText(delta.Content)
	if err != nil {
		return fmt.Errorf("decode upstream content delta: %w", err)
	}
	if text != "" {
		if err := s.addText(text); err != nil {
			return err
		}
	}
	for _, toolCall := range delta.ToolCalls {
		if err := s.addToolDelta(toolCall); err != nil {
			return err
		}
	}
	if delta.FunctionCall != nil {
		legacy := chatToolCall{Index: 0}
		legacy.Type = "function"
		legacy.Function.Name = delta.FunctionCall.Name
		legacy.Function.Arguments = delta.FunctionCall.Arguments
		if err := s.addToolDelta(legacy); err != nil {
			return err
		}
	}
	return nil
}

func (s *chatStreamState) nextOutputIndex() int {
	return len(s.items)
}

func (s *chatStreamState) addReasoning(delta string) error {
	if s.reasoning == nil {
		s.reasoning = &reasoningStreamItem{
			index: s.nextOutputIndex(), id: newResponseID("rs"),
		}
		s.items = append(s.items, s.reasoning)
		item := map[string]any{
			"id": s.reasoning.id, "type": "reasoning", "summary": []any{},
			"content": []any{}, "encrypted_content": nil, "status": "in_progress",
		}
		if err := s.writer.Event("response.output_item.added", map[string]any{
			"output_index": s.reasoning.index, "item": item,
		}); err != nil {
			return err
		}
		if err := s.writer.Event("response.content_part.added", map[string]any{
			"item_id": s.reasoning.id, "output_index": s.reasoning.index, "content_index": 0,
			"part": map[string]any{"type": "reasoning_text", "text": ""},
		}); err != nil {
			return err
		}
	}
	delta = s.reasoning.text.append(delta)
	if delta == "" {
		return nil
	}
	return s.writer.Event("response.reasoning_text.delta", map[string]any{
		"item_id": s.reasoning.id, "output_index": s.reasoning.index, "content_index": 0,
		"delta": delta,
	})
}

func (s *chatStreamState) addText(delta string) error {
	if s.message == nil {
		s.message = &messageStreamItem{
			index: s.nextOutputIndex(), id: newResponseID("msg"),
		}
		s.items = append(s.items, s.message)
		item := map[string]any{
			"id": s.message.id, "type": "message", "role": "assistant",
			"status": "in_progress", "content": []any{},
		}
		if err := s.writer.Event("response.output_item.added", map[string]any{
			"output_index": s.message.index, "item": item,
		}); err != nil {
			return err
		}
		if err := s.writer.Event("response.content_part.added", map[string]any{
			"item_id": s.message.id, "output_index": s.message.index, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
		}); err != nil {
			return err
		}
	}
	delta = s.message.text.append(delta)
	if delta == "" {
		return nil
	}
	return s.writer.Event("response.output_text.delta", map[string]any{
		"item_id": s.message.id, "output_index": s.message.index, "content_index": 0,
		"delta": delta,
	})
}

// messageTextProgress reports how much assistant text has already been sent and
// the tail of it. That pair is what tells a truncated stream apart from a model
// that simply answered briefly; the tail is cut on a rune boundary so a log line
// never ends in half a character.
func (s *chatStreamState) messageTextProgress(byteLimit int) (int, string) {
	if s.message == nil {
		return 0, ""
	}
	text := s.message.text.String()
	if len(text) <= byteLimit {
		return len(text), text
	}
	tail := text[len(text)-byteLimit:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return len(text), tail
}

func (s *chatStreamState) addToolDelta(delta chatToolCall) error {
	if toolType := strings.TrimSpace(delta.Type); toolType != "" && toolType != "function" {
		return fmt.Errorf("upstream Chat tool call type %q is not a function call", toolType)
	}
	item := s.tools[delta.Index]
	if item == nil {
		callID := strings.TrimSpace(delta.ID)
		item = &toolStreamItem{
			index: s.nextOutputIndex(), chatIndex: delta.Index,
			callID: callID, toolMap: s.toolMap,
		}
		s.tools[delta.Index] = item
		s.items = append(s.items, item)
	}
	if strings.TrimSpace(delta.ID) != "" {
		if item.announced && item.callID != delta.ID {
			return fmt.Errorf("upstream changed an announced tool call ID")
		}
		item.callID = delta.ID
	}
	if delta.Function.Name != "" {
		item.name = mergeStreamedName(item.name, delta.Function.Name)
	}
	if err := item.observeName(s.writer); err != nil {
		return err
	}
	arguments := rawJSONString(delta.Function.Arguments)
	if arguments == "" {
		return nil
	}
	item.arguments.WriteString(arguments)
	return item.flushArguments(s.writer)
}

func mergeStreamedName(current string, delta string) string {
	if current == "" {
		return delta
	}
	if current == delta {
		return current
	}
	if strings.HasPrefix(delta, current) {
		return delta
	}
	return current + delta
}

func (s *chatStreamState) complete() error {
	sort.SliceStable(s.items, func(left int, right int) bool {
		return s.items[left].outputIndex() < s.items[right].outputIndex()
	})
	output := make([]any, 0, len(s.items))
	for _, item := range s.items {
		completed, err := item.finish(s.writer)
		if err != nil {
			return err
		}
		output = append(output, completed)
	}
	status, incompleteDetails, responseError := responseStatus(s.finishReason)
	response := responseObject(
		s.request,
		s.responseID,
		s.createdAt,
		s.model,
		status,
		output,
		responseUsage(s.usage),
		incompleteDetails,
		responseError,
	)
	eventType := "response.completed"
	if status == "incomplete" {
		eventType = "response.incomplete"
	}
	if status == "failed" {
		eventType = "response.failed"
	}
	return s.writer.Event(eventType, map[string]any{"response": response})
}

func (s *chatStreamState) fail(code string, message string) error {
	response := responseObject(
		s.request, s.responseID, s.createdAt, s.model, "failed", []any{},
		responseUsage(s.usage), nil,
		map[string]any{"code": code, "message": message},
	)
	return s.writer.Event("response.failed", map[string]any{"response": response})
}

func (g *Gateway) convertChatStream(
	writer http.ResponseWriter,
	clientRequest *http.Request,
	request responsesRequest,
	upstream *http.Response,
	toolMap responseToolMap,
	route Route,
) {
	eventWriter, ok := newResponsesSSEWriter(writer)
	if !ok {
		writeResponsesError(writer, http.StatusInternalServerError, "server_error", "streaming_unsupported", "", "HTTP streaming is unavailable")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	started := time.Now()
	state := newChatStreamState(request, eventWriter, toolMap)
	if err := state.start(); err != nil {
		return
	}
	var firstTokenTimedOut atomic.Bool
	var firstTokenSeen atomic.Bool
	timer := time.AfterFunc(g.firstTokenLimit, func() {
		if !firstTokenSeen.Load() {
			firstTokenTimedOut.Store(true)
			_ = upstream.Body.Close()
		}
	})
	defer timer.Stop()
	decoder := newSSEDecoder(upstream.Body)
	done := false
	for {
		event, err := decoder.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if clientRequest.Context().Err() != nil {
				return
			}
			if firstTokenTimedOut.Load() {
				_ = state.fail("upstream_timeout", "Timed out waiting for the first upstream token")
				return
			}
			_ = state.fail("upstream_stream_error", "Upstream Chat stream ended unexpectedly")
			return
		}
		data := bytes.TrimSpace(event.Data)
		if len(data) == 0 {
			continue
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			done = true
			break
		}
		if firstTokenSeen.CompareAndSwap(false, true) {
			timer.Stop()
		}
		var chunk chatStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			_ = state.fail("upstream_stream_error", "Upstream Chat stream emitted invalid JSON")
			return
		}
		if err := state.process(chunk); err != nil {
			_ = state.fail("upstream_error", err.Error())
			return
		}
	}
	if !state.sawFinish {
		// No chunk ever carried a usable finish_reason, so the upstream never said
		// why it stopped — whatever already reached the client is a fragment, not an
		// answer. Fail the turn instead of letting responseStatus fall back to
		// "completed": a half-streamed reply reported as a finished turn is what
		// makes a cut-off session look like a completed one.
		//
		// done (the Chat [DONE] marker) is deliberately not part of this decision.
		// Gateways are free to omit it — finish_reason is the authoritative signal —
		// and a gateway that emits [DONE] after the upstream connection dropped
		// would otherwise turn a truncation into a clean ending.
		textBytes, textTail := state.messageTextProgress(80)
		g.logger.WarnContext(
			clientRequest.Context(),
			"upstream Chat stream ended without a finish reason",
			"event", "model_gateway.stream.terminal_reason_missing",
			"workspace_id", route.WorkspaceID,
			"agent_session_id", route.AgentSessionID,
			"response_id", state.responseID,
			"upstream_chat_id", state.upstreamChatID,
			"elapsed_ms", time.Since(started).Milliseconds(),
			"model", strings.TrimSpace(state.model),
			"chat_done_marker_seen", done,
			"output_text_bytes", textBytes,
			"output_text_tail", textTail,
		)
		_ = state.fail("upstream_stream_error", "Upstream Chat stream closed before a finish reason")
		return
	}
	if err := state.complete(); err != nil {
		_ = state.fail("upstream_error", err.Error())
	}
}

func writeSyntheticStream(
	writer http.ResponseWriter,
	request responsesRequest,
	upstream chatCompletionResponse,
	toolMap responseToolMap,
) {
	eventWriter, ok := newResponsesSSEWriter(writer)
	if !ok {
		writeResponsesError(writer, http.StatusInternalServerError, "server_error", "streaming_unsupported", "", "HTTP streaming is unavailable")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("X-Accel-Buffering", "no")
	state := newChatStreamState(request, eventWriter, toolMap)
	if strings.TrimSpace(upstream.ID) != "" && strings.HasPrefix(upstream.ID, "resp_") {
		state.responseID = upstream.ID
	}
	if upstream.Created > 0 {
		state.createdAt = upstream.Created
	}
	if upstream.Model != "" {
		state.model = upstream.Model
	}
	if err := state.start(); err != nil {
		return
	}
	if len(upstream.Choices) == 0 {
		_ = state.fail("upstream_error", "Upstream Chat response contained no choices")
		return
	}
	choice := upstream.Choices[0]
	delta := chatStreamDelta{
		Role:             choice.Message.Role,
		Content:          choice.Message.Content,
		ReasoningContent: choice.Message.ReasoningContent,
		Reasoning:        choice.Message.Reasoning,
		ToolCalls:        choice.Message.ToolCalls,
		FunctionCall:     choice.Message.FunctionCall,
	}
	if err := state.processDelta(delta); err != nil {
		_ = state.fail("upstream_error", err.Error())
		return
	}
	state.finishReason = choice.FinishReason
	state.sawFinish = true
	state.usage = upstream.Usage
	if err := state.complete(); err != nil {
		_ = state.fail("upstream_error", err.Error())
	}
}
