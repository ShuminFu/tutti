package claudesidecar

import (
	"strings"
	"testing"
)

func TestParseTaskNotificationExtractsFoldInFields(t *testing.T) {
	parsed := parseTaskNotification(`<task-notification>
<task-id>a12d5359ec75cda9d</task-id>
<tool-use-id>call_9c651fe8b89a49f9b4f56eba</tool-use-id>
<output-file>/tmp/agent.output</output-file>
<status>completed</status>
<summary>Agent "Generate random number 2" finished</summary>
<result>7</result>
<usage><subagent_tokens>0</subagent_tokens><tool_uses>0</tool_uses><duration_ms>1282</duration_ms></usage>
</task-notification>`)

	if parsed == nil {
		t.Fatal("parseTaskNotification returned nil")
	}
	if parsed.TaskID != "a12d5359ec75cda9d" {
		t.Fatalf("taskId = %q", parsed.TaskID)
	}
	if parsed.ToolUseID != "call_9c651fe8b89a49f9b4f56eba" {
		t.Fatalf("toolUseId = %q", parsed.ToolUseID)
	}
	if parsed.Status != "completed" {
		t.Fatalf("status = %q", parsed.Status)
	}
	if parsed.Result != "7" {
		t.Fatalf("result = %q", parsed.Result)
	}
	if numberValue(parsed.Usage["duration_ms"]) != 1282 {
		t.Fatalf("usage = %#v", parsed.Usage)
	}
}

func TestTaskNotificationToSystemMessageMapsToolUseID(t *testing.T) {
	parsed := parseTaskNotification(`<task-notification>
<task-id>agent-1</task-id>
<tool-use-id>toolu-agent</tool-use-id>
<status>completed</status>
<summary>Done</summary>
</task-notification>`)
	if parsed == nil {
		t.Fatal("parseTaskNotification returned nil")
	}
	message := taskNotificationToSystemMessage(parsed)
	if message["tool_use_id"] != "toolu-agent" || message["task_id"] != "agent-1" || message["status"] != "completed" {
		t.Fatalf("message = %#v", message)
	}
}

func TestReadUserMessageNotificationTextSupportsStringContent(t *testing.T) {
	text := readUserMessageNotificationText(map[string]any{
		"message": map[string]any{
			"content": "<task-notification><tool-use-id>toolu-agent</tool-use-id><status>completed</status></task-notification>",
		},
	})
	if !strings.Contains(text, "<task-notification>") {
		t.Fatalf("text = %q", text)
	}
}

func TestReadQueuedTaskNotificationPromptReadsFoldInAttachment(t *testing.T) {
	prompt := readQueuedTaskNotificationPrompt(map[string]any{
		"attachment": map[string]any{
			"type":        "queued_command",
			"commandMode": "task-notification",
			"prompt":      "<task-notification><tool-use-id>toolu-agent</tool-use-id><status>completed</status><summary>Done</summary></task-notification>",
		},
	})
	if !strings.Contains(prompt, "<tool-use-id>toolu-agent</tool-use-id>") {
		t.Fatalf("prompt = %q", prompt)
	}
}
