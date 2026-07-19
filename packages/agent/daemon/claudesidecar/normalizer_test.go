package claudesidecar

import (
	"reflect"
	"testing"
)

func TestCommandEntriesFiltersUnsupportedAndPreservesHints(t *testing.T) {
	entries := commandEntries([]any{
		map[string]any{"name": "context", "description": "Show context", "argumentHint": "scope"},
		map[string]any{"name": "review", "description": "Review changes", "argumentHint": []any{"target", "range"}},
		map[string]any{"name": "github (MCP)", "description": "MCP command"},
		map[string]any{"name": "login", "description": "Unsupported"},
		"usage",
		"  ",
	})
	expected := []map[string]any{
		{"name": "context", "description": "Show context", "input": map[string]any{"hint": "scope"}},
		{"name": "review", "description": "Review changes", "input": map[string]any{"hint": "target range"}},
		{"name": "mcp:github", "description": "MCP command"},
		{"name": "usage"},
	}
	if !reflect.DeepEqual(entries, expected) {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestSpeedFromFastModeStateNormalizesDefinitiveStatesOnly(t *testing.T) {
	if speedFromFastModeState("on") != "fast" || speedFromFastModeState("off") != "standard" {
		t.Fatal("definitive states mismapped")
	}
	for _, state := range []any{"cooldown", "unsupported", nil} {
		if speedFromFastModeState(state) != "" {
			t.Fatalf("state %v mapped unexpectedly", state)
		}
	}
}

func TestSDKContentFromPromptBlocksEmbedsImages(t *testing.T) {
	content := sdkContentFromPromptBlocks([]any{
		map[string]any{"type": "text", "text": "What's in this image?"},
		map[string]any{"type": "image", "mimeType": "image/png", "data": "aW1hZ2U="},
	}, "fallback")
	expected := []map[string]any{
		{"type": "text", "text": "What's in this image?"},
		{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "aW1hZ2U="}},
	}
	if !reflect.DeepEqual(content, expected) {
		t.Fatalf("content = %#v", content)
	}
}

func TestSDKContentFromPromptBlocksMapsHTTPSImageURLs(t *testing.T) {
	url := "https://bucket.example/image.webp?token=secret"
	content := sdkContentFromPromptBlocks([]any{
		map[string]any{"type": "image", "mimeType": "image/webp", "url": url},
	}, "fallback")
	expected := []map[string]any{
		{"type": "image", "source": map[string]any{"type": "url", "url": url}},
	}
	if !reflect.DeepEqual(content, expected) {
		t.Fatalf("content = %#v", content)
	}
	insecure := sdkContentFromPromptBlocks([]any{
		map[string]any{"type": "image", "mimeType": "image/webp", "url": "http://example.com/image.webp"},
	}, "fallback")
	if !reflect.DeepEqual(insecure, []map[string]any{{"type": "text", "text": "fallback"}}) {
		t.Fatalf("insecure content = %#v", insecure)
	}
	ambiguous := sdkContentFromPromptBlocks([]any{
		map[string]any{"type": "image", "mimeType": "image/webp", "url": url, "data": "ambiguous"},
	}, "fallback")
	if !reflect.DeepEqual(ambiguous, []map[string]any{{"type": "text", "text": "fallback"}}) {
		t.Fatalf("ambiguous content = %#v", ambiguous)
	}
}

func TestSDKContentFromPromptBlocksFallsBackToLegacyPrompt(t *testing.T) {
	content := sdkContentFromPromptBlocks(nil, "hello")
	if !reflect.DeepEqual(content, []map[string]any{{"type": "text", "text": "hello"}}) {
		t.Fatalf("content = %#v", content)
	}
}

func TestToolPayloadConvertsStructuredPatchHunksIntoChanges(t *testing.T) {
	payload := toolPayload("turn-1", &ToolState{
		ID:   "toolu-edit",
		Name: "Edit",
		Input: map[string]any{
			"file_path":  "/repo/src/app.ts",
			"old_string": "old line",
			"new_string": "new line",
		},
		Started: true,
	}, "completed", map[string]any{
		"_meta": map[string]any{
			"claudeCode": map[string]any{
				"toolResponse": map[string]any{
					"type":     "update",
					"filePath": "/repo/src/app.ts",
					"structuredPatch": []any{
						map[string]any{
							"oldStart": float64(5), "oldLines": float64(3),
							"newStart": float64(5), "newLines": float64(3),
							"lines": []any{" context", "-old line", "+new line"},
						},
						map[string]any{
							"oldStart": float64(20), "oldLines": float64(1),
							"newStart": float64(20), "newLines": float64(1),
							"lines": []any{"-foo", "+bar"},
						},
					},
				},
			},
		},
	})

	output := recordValue(payload["output"])
	diff := "@@ -5,3 +5,3 @@\n context\n-old line\n+new line\n@@ -20,1 +20,1 @@\n-foo\n+bar"
	expectedPatch := []map[string]any{
		{
			"path":     "/repo/src/app.ts",
			"filePath": "/repo/src/app.ts",
			"kind":     "update",
			"change":   "update",
			"diff":     diff,
			"patch":    diff,
		},
	}
	if !reflect.DeepEqual(output["structuredPatch"], expectedPatch) {
		t.Fatalf("structuredPatch = %#v", output["structuredPatch"])
	}
	expectedChanges := []map[string]any{
		{"path": "/repo/src/app.ts", "type": "update", "diff": diff},
	}
	if !reflect.DeepEqual(output["changes"], expectedChanges) {
		t.Fatalf("changes = %#v", output["changes"])
	}
}

func TestToolPayloadPreservesWriteCreateSemantics(t *testing.T) {
	payload := toolPayload("turn-1", &ToolState{
		ID:   "toolu-write",
		Name: "Write",
		Input: map[string]any{
			"file_path": "/repo/new.ts",
			"content":   "first\nsecond",
		},
		Started: true,
	}, "completed", map[string]any{
		"tool_response": map[string]any{
			"type":     "create",
			"filePath": "/repo/new.ts",
			"structuredPatch": []any{
				map[string]any{
					"oldStart": float64(0), "oldLines": float64(0),
					"newStart": float64(1), "newLines": float64(2),
					"lines": []any{"+first", "+second"},
				},
			},
		},
	})
	output := recordValue(payload["output"])
	expectedChanges := []map[string]any{
		{"path": "/repo/new.ts", "type": "create", "diff": "@@ -0,0 +1,2 @@\n+first\n+second"},
	}
	if !reflect.DeepEqual(output["changes"], expectedChanges) {
		t.Fatalf("changes = %#v", output["changes"])
	}
}

func TestToolPayloadPreservesSubagentParentIDAndSteps(t *testing.T) {
	step := map[string]any{
		"id":         "toolu-read",
		"toolUseId":  "toolu-read",
		"toolName":   "Read",
		"status":     "completed",
		"toolInput":  map[string]any{"file_path": "/repo/README.md"},
		"toolResult": map[string]any{"text": "Read README"},
	}
	payload := toolPayload("turn-1", &ToolState{
		ID:   "toolu-task",
		Name: "Task",
		Input: map[string]any{
			"description": "Inspect workspace",
			"prompt":      "Find relevant files",
		},
		Started: true,
		Steps:   []map[string]any{step},
	}, "completed", map[string]any{"content": "Task complete"})
	metadata := recordValue(payload["metadata"])
	steps, _ := metadata["steps"].([]map[string]any)
	if len(steps) != 1 || !reflect.DeepEqual(steps[0], step) {
		t.Fatalf("steps = %#v", metadata["steps"])
	}

	childPayload := toolPayload("turn-1", &ToolState{
		ID:              "toolu-read",
		Name:            "Read",
		Input:           map[string]any{"file_path": "/repo/README.md"},
		Started:         true,
		ParentToolUseID: "toolu-task",
	}, "completed", map[string]any{"content": "Read README"})
	if recordValue(childPayload["metadata"])["parentToolUseId"] != "toolu-task" {
		t.Fatalf("child metadata = %#v", childPayload["metadata"])
	}
}

func TestToolPayloadExtractsAsyncSubagentLaunchMetadata(t *testing.T) {
	payload := toolPayload("turn-1", &ToolState{
		ID:      "toolu-agent",
		Name:    "Agent",
		Input:   map[string]any{"prompt": "Inspect workspace"},
		Started: true,
	}, "completed", map[string]any{
		"content": "Async agent launched successfully\nagentId: a33f4e9013dedffe8\noutput_file: /private/tmp/claude/tasks/a33f4e9013dedffe8.output",
	})
	expected := map[string]any{
		"adapter":            "claude-agent-sdk",
		"toolName":           "Agent",
		"async":              true,
		"subagentAsync":      true,
		"taskStatus":         "running",
		"subagentStatus":     "running",
		"subagentAgentId":    "a33f4e9013dedffe8",
		"agentId":            "a33f4e9013dedffe8",
		"outputFile":         "/private/tmp/claude/tasks/a33f4e9013dedffe8.output",
		"subagentOutputFile": "/private/tmp/claude/tasks/a33f4e9013dedffe8.output",
	}
	if !reflect.DeepEqual(payload["metadata"], expected) {
		t.Fatalf("metadata = %#v", payload["metadata"])
	}
}

func TestToolPayloadIgnoresAsyncSubagentAgentIDSuffix(t *testing.T) {
	payload := toolPayload("turn-1", &ToolState{
		ID:      "toolu-agent",
		Name:    "Agent",
		Input:   map[string]any{"prompt": "Inspect workspace"},
		Started: true,
	}, "completed", map[string]any{
		"content": "Async agent launched successfully\nagentId: a33f4e9013dedffe8 (internal ID: keep out)\noutput_file: /tmp/a33f4e9013dedffe8.output",
	})
	if recordValue(payload["metadata"])["agentId"] != "a33f4e9013dedffe8" {
		t.Fatalf("metadata = %#v", payload["metadata"])
	}
}

func TestToolPayloadClassifiesInteractiveTools(t *testing.T) {
	for _, toolName := range []string{"AskUserQuestion", "EnterPlanMode", "ExitPlanMode"} {
		payload := toolPayload("turn-1", &ToolState{
			ID:      "toolu-" + toolName,
			Name:    toolName,
			Input:   map[string]any{},
			Started: true,
		}, "completed", nil)
		if payload["callType"] != "interactive" {
			t.Fatalf("%s callType = %#v", toolName, payload["callType"])
		}
		expected := map[string]any{"adapter": "claude-agent-sdk", "toolName": toolName}
		if !reflect.DeepEqual(payload["metadata"], expected) {
			t.Fatalf("%s metadata = %#v", toolName, payload["metadata"])
		}
	}
}

func TestAnswersFromInteractivePayloadKeysAnswersByQuestionText(t *testing.T) {
	answers := answersFromInteractivePayload(map[string]any{
		"answers": []any{"React", "UI, daemon"},
		"answersByQuestionId": map[string]any{
			"framework-id": "React",
			"question-2":   []any{"UI", "daemon"},
		},
	}, map[string]any{
		"questions": []any{
			map[string]any{
				"id":       "framework-id",
				"question": "Which framework?",
				"options":  []any{map[string]any{"label": "React"}},
			},
			map[string]any{
				"question":    "Which areas?",
				"multiSelect": true,
				"options":     []any{map[string]any{"label": "UI"}, map[string]any{"label": "daemon"}},
			},
		},
	})
	expected := map[string]any{
		"Which framework?": "React",
		"Which areas?":     "UI, daemon",
	}
	if !reflect.DeepEqual(answers, expected) {
		t.Fatalf("answers = %#v", answers)
	}
}

func TestGoalStateFromContentBlocksMapsGoalStatusAttachments(t *testing.T) {
	active := goalStateFromContentBlocks([]map[string]any{
		{
			"type": "attachment",
			"attachment": map[string]any{
				"type":      "goal_status",
				"met":       false,
				"sentinel":  true,
				"condition": "ship native goal",
			},
		},
	})
	expectedActive := map[string]any{
		"objective": "ship native goal",
		"status":    "active",
		"sentinel":  true,
	}
	if !reflect.DeepEqual(active, expectedActive) {
		t.Fatalf("active goal = %#v", active)
	}
	complete := goalStateFromContentBlocks([]map[string]any{
		{
			"type":       "goal_status",
			"met":        true,
			"condition":  "ship native goal",
			"reason":     "done",
			"iterations": float64(1),
		},
	})
	expectedComplete := map[string]any{
		"objective":  "ship native goal",
		"status":     "complete",
		"reason":     "done",
		"iterations": float64(1),
	}
	if !reflect.DeepEqual(complete, expectedComplete) {
		t.Fatalf("complete goal = %#v", complete)
	}
}
