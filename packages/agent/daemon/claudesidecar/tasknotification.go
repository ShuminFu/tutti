package claudesidecar

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedTaskNotification mirrors the sidecar's <task-notification> parsing.
type ParsedTaskNotification struct {
	TaskID     string
	ToolUseID  string
	OutputFile string
	Status     string
	Summary    string
	Result     string
	Usage      map[string]any
}

var xmlTagPatterns = map[string]*regexp.Regexp{}

func extractXMLTag(text string, tag string) string {
	pattern, ok := xmlTagPatterns[tag]
	if !ok {
		pattern = regexp.MustCompile(`(?is)<` + tag + `>([\s\S]*?)</` + tag + `>`)
		xmlTagPatterns[tag] = pattern
	}
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func parseTaskNotificationUsage(text string) map[string]any {
	usageBlock := extractXMLTag(text, "usage")
	if usageBlock == "" {
		return nil
	}
	totalTokens := extractXMLTag(usageBlock, "subagent_tokens")
	toolUses := extractXMLTag(usageBlock, "tool_uses")
	durationMS := extractXMLTag(usageBlock, "duration_ms")
	if totalTokens == "" && toolUses == "" && durationMS == "" {
		return nil
	}
	usage := map[string]any{}
	if totalTokens != "" {
		usage["total_tokens"] = parsedNumberOrZero(totalTokens)
	}
	if toolUses != "" {
		usage["tool_uses"] = parsedNumberOrZero(toolUses)
	}
	if durationMS != "" {
		usage["duration_ms"] = parsedNumberOrZero(durationMS)
	}
	return usage
}

func parsedNumberOrZero(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseTaskNotification(text string) *ParsedTaskNotification {
	trimmed := strings.TrimSpace(text)
	if !strings.Contains(trimmed, "<task-notification>") {
		return nil
	}
	toolUseID := extractXMLTag(trimmed, "tool-use-id")
	status := extractXMLTag(trimmed, "status")
	if toolUseID == "" || status == "" {
		return nil
	}
	summary := extractXMLTag(trimmed, "summary")
	if summary == "" {
		summary = extractXMLTag(trimmed, "result")
	}
	if summary == "" {
		summary = "Subagent task completed."
	}
	return &ParsedTaskNotification{
		TaskID:     extractXMLTag(trimmed, "task-id"),
		ToolUseID:  toolUseID,
		OutputFile: extractXMLTag(trimmed, "output-file"),
		Status:     status,
		Summary:    summary,
		Result:     extractXMLTag(trimmed, "result"),
		Usage:      parseTaskNotificationUsage(trimmed),
	}
}

func taskNotificationToSystemMessage(parsed *ParsedTaskNotification) map[string]any {
	message := map[string]any{
		"tool_use_id": parsed.ToolUseID,
		"status":      parsed.Status,
		"summary":     parsed.Summary,
	}
	if parsed.TaskID != "" {
		message["task_id"] = parsed.TaskID
	}
	if parsed.OutputFile != "" {
		message["output_file"] = parsed.OutputFile
	}
	if parsed.Result != "" {
		message["result"] = parsed.Result
	}
	if parsed.Usage != nil {
		message["usage"] = parsed.Usage
	}
	return message
}

func readUserMessageNotificationText(message map[string]any) string {
	inner := recordValue(message["message"])
	if inner == nil {
		return ""
	}
	if text, ok := inner["content"].(string); ok {
		return text
	}
	content, ok := inner["content"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, block := range content {
		record := recordValue(block)
		if record == nil || record["type"] != "text" {
			continue
		}
		text, isString := record["text"].(string)
		if !isString {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n")
}

func readQueuedTaskNotificationPrompt(message map[string]any) string {
	attachment := recordValue(message["attachment"])
	if attachment == nil {
		return ""
	}
	if attachment["type"] != "queued_command" {
		return ""
	}
	if attachment["commandMode"] != "task-notification" {
		return ""
	}
	prompt, _ := attachment["prompt"].(string)
	return prompt
}
