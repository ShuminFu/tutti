package claudesidecar

import (
	"errors"
	"strings"
)

func mergeToolResult(result map[string]any, hookResult map[string]any) map[string]any {
	if hookResult == nil {
		return result
	}
	merged := cloneRecord(result)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range hookResult {
		merged[key] = value
	}
	meta := map[string]any{}
	for key, value := range recordValue(result["_meta"]) {
		meta[key] = value
	}
	for key, value := range recordValue(hookResult["_meta"]) {
		meta[key] = value
	}
	merged["_meta"] = meta
	return merged
}

func isDiffToolResponse(value map[string]any) bool {
	if value == nil {
		return false
	}
	if stringValue(value["filePath"]) == "" {
		return false
	}
	patch, ok := value["structuredPatch"].([]any)
	return ok && len(patch) > 0
}

func readSDKSessionID(message map[string]any) string {
	value, _ := message["session_id"].(string)
	return value
}

func readSDKMessageUUID(message map[string]any) string {
	value, _ := message["uuid"].(string)
	return value
}

func readSDKAssistantUUID(message map[string]any) string {
	if message["type"] != "assistant" {
		return ""
	}
	return readSDKMessageUUID(message)
}

func readSDKAssistantMessageID(message map[string]any) string {
	if message["type"] != "assistant" {
		return ""
	}
	return stringValue(recordValue(message["message"])["id"])
}

func readSDKParentToolUseID(message map[string]any) string {
	value, _ := message["parent_tool_use_id"].(string)
	return strings.TrimSpace(value)
}

func taskStepFromToolPayload(payload map[string]any) map[string]any {
	id := firstNonEmptyText(stringValue(payload["toolCallId"]), stringValue(payload["callId"]))
	step := map[string]any{
		"id":         id,
		"toolUseId":  id,
		"toolName":   stringValue(payload["toolName"]),
		"name":       firstNonEmptyText(stringValue(payload["name"]), stringValue(payload["toolName"])),
		"callType":   stringValue(payload["callType"]),
		"status":     stringValue(payload["status"]),
		"toolInput":  recordValue(payload["input"]),
		"toolResult": recordValue(payload["output"]),
		"toolError":  recordValue(payload["error"]),
		"payload": map[string]any{
			"input":     recordValue(payload["input"]),
			"output":    recordValue(payload["output"]),
			"error":     recordValue(payload["error"]),
			"content":   listValue(payload["content"]),
			"locations": listValue(payload["locations"]),
		},
		"metadata":  recordValue(payload["metadata"]),
		"content":   listValue(payload["content"]),
		"locations": listValue(payload["locations"]),
	}
	return step
}

func listValue(value any) any {
	if items, ok := value.([]any); ok {
		return items
	}
	if items, ok := value.([]map[string]any); ok {
		return items
	}
	return nil
}

func normalizeResumeCursor(value map[string]any, providerSessionID string) map[string]any {
	resume := stringValue(value["resume"])
	if resume == "" {
		resume = providerSessionID
	}
	if resume == "" {
		return nil
	}
	cursor := map[string]any{
		"kind":      "claude-agent-sdk",
		"version":   1,
		"resume":    resume,
		"turnCount": numberValue(value["turnCount"]),
	}
	if resumeSessionAt := stringValue(value["resumeSessionAt"]); resumeSessionAt != "" {
		cursor["resumeSessionAt"] = resumeSessionAt
	}
	return cursor
}

// errAbort mirrors the TS AbortError used to fence a canceled generation.
//
//nolint:staticcheck // Mirrors the TypeScript sidecar's exact error text.
var errAbort = errors.New("Claude SDK turn interrupted")

func isAbortError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errAbort) || strings.Contains(err.Error(), "interrupted")
}

var contextWindowTokenKeys = []string{
	"maxTokens",
	"max_tokens",
	"contextWindowTokens",
	"context_window_tokens",
	"contextWindow",
	"modelContextWindow",
	"model_context_window",
	"size",
	"limit",
	"max",
}

func contextWindowTokensFromModelUsage(value any) float64 {
	if items, ok := value.([]any); ok {
		for _, item := range items {
			if tokens := contextWindowTokensFromModelUsage(item); tokens > 0 {
				return tokens
			}
		}
		return 0
	}
	record := recordValue(value)
	if record == nil {
		return 0
	}
	for _, key := range contextWindowTokenKeys {
		if tokens := numberValue(record[key]); tokens > 0 {
			return tokens
		}
	}
	for _, nested := range record {
		switch nested.(type) {
		case map[string]any, []any:
			if tokens := contextWindowTokensFromModelUsage(nested); tokens > 0 {
				return tokens
			}
		}
	}
	return 0
}

func isCompactCommandPrompt(value string) bool {
	prompt := strings.ToLower(strings.TrimSpace(value))
	return prompt == "/compact" || strings.HasPrefix(prompt, "/compact ")
}
