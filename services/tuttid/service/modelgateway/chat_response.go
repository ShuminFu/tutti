package modelgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   *chatUsage             `json:"usage"`
	Error   json.RawMessage        `json:"error"`
}

type chatCompletionChoice struct {
	Index        int                 `json:"index"`
	Message      chatResponseMessage `json:"message"`
	FinishReason *string             `json:"finish_reason"`
}

type chatResponseMessage struct {
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

type chatToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type chatUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens     int64 `json:"cached_tokens"`
		CacheWriteTokens int64 `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func convertChatResponse(
	request responsesRequest,
	upstream chatCompletionResponse,
	toolMap responseToolMap,
	scope reasoningScope,
) (map[string]any, error) {
	if len(bytes.TrimSpace(upstream.Error)) > 0 && !bytes.Equal(bytes.TrimSpace(upstream.Error), []byte("null")) {
		return nil, fmt.Errorf("upstream returned an error payload")
	}
	if len(upstream.Choices) == 0 {
		return nil, fmt.Errorf("upstream Chat response contained no choices")
	}
	choice := upstream.Choices[0]
	output := make([]any, 0, 2+len(choice.Message.ToolCalls))
	reasoning, err := chatText(choice.Message.ReasoningContent)
	if err != nil {
		return nil, fmt.Errorf("decode upstream reasoning_content: %w", err)
	}
	if reasoning == "" {
		reasoning, err = chatText(choice.Message.Reasoning)
		if err != nil {
			return nil, fmt.Errorf("decode upstream reasoning: %w", err)
		}
	}
	if reasoning != "" {
		output = append(output, completedReasoningItem(request, reasoning, scope))
	}
	text, err := chatText(choice.Message.Content)
	if err != nil {
		return nil, fmt.Errorf("decode upstream message content: %w", err)
	}
	// The non-streaming path must reach the same verdict as the streaming one,
	// or a request routed without streaming silently ships protocol residue as
	// an answer.
	if len(choice.Message.ToolCalls) == 0 && choice.Message.FunctionCall == nil && assistantTextIsProtocolResidue(text) {
		return nil, errModelProtocolResidue
	}
	if text != "" || (len(choice.Message.ToolCalls) == 0 && choice.Message.FunctionCall == nil) {
		output = append(output, completedMessageItem(text))
	}
	toolCalls := append([]chatToolCall(nil), choice.Message.ToolCalls...)
	if choice.Message.FunctionCall != nil {
		legacy := chatToolCall{ID: newResponseID("call")}
		legacy.Type = "function"
		legacy.Function.Name = choice.Message.FunctionCall.Name
		legacy.Function.Arguments = choice.Message.FunctionCall.Arguments
		toolCalls = append(toolCalls, legacy)
	}
	for _, toolCall := range toolCalls {
		item, err := completedToolCallItem(toolCall, toolMap)
		if err != nil {
			return nil, err
		}
		output = append(output, item)
	}
	responseID := responseIDFromUpstream(upstream.ID)
	createdAt := upstream.Created
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	status, incompleteDetails, responseError := responseStatus(choice.FinishReason)
	return responseObject(
		request,
		responseID,
		createdAt,
		upstream.Model,
		status,
		output,
		responseUsage(upstream.Usage),
		incompleteDetails,
		responseError,
	), nil
}

func chatText(encoded json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return "", err
	}
	var result strings.Builder
	for _, part := range parts {
		if part.Type == "" || part.Type == "text" || part.Type == "output_text" || part.Type == "reasoning_text" {
			result.WriteString(part.Text)
		}
	}
	return result.String(), nil
}

func completedReasoningItem(request responsesRequest, text string, scope reasoningScope) map[string]any {
	var encryptedContent any
	if requestIncludesReasoningEncryptedContent(request) {
		encryptedContent = scope.seal(text)
	}
	return map[string]any{
		"id":                newResponseID("rs"),
		"type":              "reasoning",
		"summary":           []any{},
		"content":           []any{map[string]any{"type": "reasoning_text", "text": text}},
		"encrypted_content": encryptedContent,
		"status":            "completed",
	}
}

func completedMessageItem(text string) map[string]any {
	return map[string]any{
		"id":     newResponseID("msg"),
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []any{map[string]any{
			"type": "output_text", "text": text, "annotations": []any{},
		}},
	}
}

// completedToolCallItem converts one upstream Chat tool call into its Responses
// output item. A call that resolves to a registered custom tool is restored to
// custom_tool_call so the caller keeps its own registered tool contract; the
// upstream function name alone never decides the shape, because a custom tool
// travels upstream as a synthesized function wrapper.
func completedToolCallItem(toolCall chatToolCall, toolMap responseToolMap) (map[string]any, error) {
	if toolType := strings.TrimSpace(toolCall.Type); toolType != "" && toolType != "function" {
		return nil, fmt.Errorf("upstream Chat tool call type %q is not a function call", toolType)
	}
	callID := strings.TrimSpace(toolCall.ID)
	if callID == "" {
		callID = newResponseID("call")
	}
	arguments := rawJSONString(toolCall.Function.Arguments)
	identity := responseIdentityForChatTool(toolCall.Function.Name, toolMap)
	if identity.WrappedCustomInput {
		input, err := decodeCustomToolInput(json.RawMessage(arguments))
		if err != nil {
			return nil, fmt.Errorf("decode custom tool call %q: %w", identity.Name, err)
		}
		item := map[string]any{
			"id":      newResponseID("ctc"),
			"type":    "custom_tool_call",
			"status":  "completed",
			"call_id": callID,
			"name":    identity.Name,
			"input":   input,
		}
		if identity.Namespace != "" {
			item["namespace"] = identity.Namespace
		}
		return item, nil
	}
	item := map[string]any{
		"id":        newResponseID("fc"),
		"type":      "function_call",
		"status":    "completed",
		"call_id":   callID,
		"name":      identity.Name,
		"arguments": arguments,
	}
	if identity.Namespace != "" {
		item["namespace"] = identity.Namespace
	}
	return item, nil
}

func responseIdentityForChatTool(name string, toolMap responseToolMap) responseToolIdentity {
	if identity, exists := toolMap[name]; exists {
		return identity
	}
	return responseToolIdentity{Name: name}
}

func rawJSONString(encoded json.RawMessage) string {
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err == nil {
		return value
	}
	return string(trimmed)
}

// hasTerminalFinishReason reports whether the upstream actually named a reason
// for stopping. An empty string is a placeholder some gateways stamp on every
// chunk rather than a statement that the answer is over, so a stream that only
// ever carried placeholders never announced an ending at all. Letting such a
// stream reach responseStatus is how a truncated answer gets reported as a
// finished turn.
func hasTerminalFinishReason(finishReason *string) bool {
	return finishReason != nil && strings.TrimSpace(*finishReason) != ""
}

// responseStatus maps the upstream's stop reason onto the Responses status.
//
// A nil reason means "the upstream never spoke about why it stopped", which is
// only benign on the non-streaming path: there the whole Chat body arrived as
// one JSON document, so there is no such thing as a fragment and a missing
// reason costs nothing. The streaming converter must never reach here with nil
// (it fails the turn first) — see convertChatStream.
func responseStatus(finishReason *string) (string, any, any) {
	if finishReason == nil {
		return "completed", nil, nil
	}
	switch strings.TrimSpace(*finishReason) {
	case "stop", "tool_calls", "function_call":
		return "completed", nil, nil
	case "length", "max_tokens":
		return "incomplete", map[string]any{"reason": "max_output_tokens"}, nil
	case "content_filter":
		return "failed", nil, map[string]any{
			"code": "content_filter", "message": "The upstream model blocked the response",
		}
	case "":
		// A named-but-empty reason is not a clean stop either; the upstream
		// declined to say why it ended, so the answer is not known to be whole.
		return "incomplete", map[string]any{"reason": "upstream_finish_reason"}, nil
	default:
		return "incomplete", map[string]any{"reason": "upstream_finish_reason"}, nil
	}
}

func responseObject(
	request responsesRequest,
	responseID string,
	createdAt int64,
	model string,
	status string,
	output []any,
	usage any,
	incompleteDetails any,
	responseError any,
) map[string]any {
	if strings.TrimSpace(model) == "" {
		model = request.Model
	}
	parallelToolCalls := true
	if request.ParallelToolCalls != nil {
		parallelToolCalls = *request.ParallelToolCalls
	}
	var reasoning any
	if request.Reasoning != nil {
		reasoning = map[string]any{
			"effort":  nullableStringValue(request.Reasoning.Effort),
			"summary": nullableStringValue(request.Reasoning.Summary),
		}
	}
	return map[string]any{
		"id":                   responseID,
		"object":               "response",
		"created_at":           createdAt,
		"completed_at":         completedAt(status),
		"status":               status,
		"error":                responseError,
		"incomplete_details":   incompleteDetails,
		"instructions":         nil,
		"max_output_tokens":    request.MaxOutputTokens,
		"model":                model,
		"output":               output,
		"parallel_tool_calls":  parallelToolCalls,
		"previous_response_id": nil,
		"reasoning":            reasoning,
		"store":                valueOrDefault(request.Store, false),
		"temperature":          request.Temperature,
		"text":                 responseTextEcho(request.Text),
		"tool_choice":          rawJSONValue(request.ToolChoice, "auto"),
		"tools":                rawJSONArray(request.Tools),
		"top_p":                request.TopP,
		"truncation":           "disabled",
		"usage":                usage,
		"metadata":             mergedMetadata(request),
	}
}

func responseUsage(usage *chatUsage) any {
	if usage == nil {
		return nil
	}
	cachedTokens := int64(0)
	cacheWriteTokens := int64(0)
	if usage.PromptTokensDetails != nil {
		cachedTokens = usage.PromptTokensDetails.CachedTokens
		cacheWriteTokens = usage.PromptTokensDetails.CacheWriteTokens
	}
	reasoningTokens := int64(0)
	if usage.CompletionTokensDetails != nil {
		reasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
	}
	return map[string]any{
		"input_tokens": usage.PromptTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens":      cachedTokens,
			"cache_write_tokens": cacheWriteTokens,
		},
		"output_tokens": usage.CompletionTokens,
		"output_tokens_details": map[string]any{
			"reasoning_tokens": reasoningTokens,
		},
		"total_tokens": usage.TotalTokens,
	}
}

func responseIDFromUpstream(upstreamID string) string {
	upstreamID = strings.TrimSpace(upstreamID)
	if strings.HasPrefix(upstreamID, "resp_") {
		return upstreamID
	}
	return newResponseID("resp")
}

func newResponseID(prefix string) string {
	token, err := randomToken()
	if err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	if len(token) > 22 {
		token = token[:22]
	}
	return prefix + "_" + token
}

func completedAt(status string) any {
	if status == "completed" || status == "failed" || status == "incomplete" {
		return time.Now().Unix()
	}
	return nil
}

func nullableStringValue(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func valueOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func rawJSONValue(encoded json.RawMessage, fallback any) any {
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fallback
	}
	var value any
	if json.Unmarshal(trimmed, &value) != nil {
		return fallback
	}
	return value
}

func rawJSONArray(values []json.RawMessage) []any {
	result := make([]any, 0, len(values))
	for _, encoded := range values {
		var value any
		if json.Unmarshal(encoded, &value) == nil {
			result = append(result, value)
		}
	}
	return result
}

func responseTextEcho(encoded json.RawMessage) any {
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return map[string]any{"format": map[string]any{"type": "text"}}
	}
	var value any
	if json.Unmarshal(trimmed, &value) != nil {
		return map[string]any{"format": map[string]any{"type": "text"}}
	}
	return value
}

func mergedMetadata(request responsesRequest) map[string]string {
	result := make(map[string]string, len(request.Metadata)+len(request.ClientMetadata))
	for key, value := range request.Metadata {
		result[key] = value
	}
	for key, value := range request.ClientMetadata {
		if _, exists := result[key]; !exists {
			result[key] = value
		}
	}
	return result
}
