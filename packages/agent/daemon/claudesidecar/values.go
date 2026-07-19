// Package claudesidecar is the Go port of the TypeScript
// @tutti-os/claude-sdk-sidecar package. It bridges the Tutti agent runtime to
// Claude Code over the same versioned NDJSON protocol the TypeScript sidecar
// speaks, but talks to the claude CLI's stream-json interface directly (see
// the claudecli subpackage) instead of going through the Node
// @anthropic-ai/claude-agent-sdk.
package claudesidecar

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

var whitespaceRun = regexp.MustCompile(`\s+`)

func normalizeTitle(value string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(value, " "))
}

func envObject(value any) map[string]string {
	record, ok := value.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	result := make(map[string]string, len(record))
	for key, item := range record {
		if text, ok := item.(string); ok {
			result[key] = text
		}
	}
	return result
}

func booleanValue(value any) bool {
	return value == true
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func recordValue(value any) map[string]any {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return record
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func errorPayload(err error) map[string]any {
	if err == nil {
		return map[string]any{"message": ""}
	}
	return map[string]any{
		"name":    "Error",
		"message": err.Error(),
	}
}

// randomUUID mirrors crypto.randomUUID(): a v4 UUID string.
func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("claude-sidecar-%x", b)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func cloneRecord(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func stringArrayValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringValue(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}
