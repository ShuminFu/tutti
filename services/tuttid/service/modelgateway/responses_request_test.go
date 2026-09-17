package modelgateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertResponseInputProjectsImageToolOutputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantToolText string
		wantCallID   string
		wantImageURL string
		wantDetail   string
		wantMessages int
	}{
		{
			name: "function output image only",
			input: `[
				{"type":"function_call","call_id":"call_shot","name":"screenshot","arguments":"{}"},
				{"type":"function_call_output","call_id":"call_shot","output":[
					{"type":"input_image","image_url":"data:image/png;base64,AA==","detail":"high"}
				]}
			]`,
			wantToolText: "",
			wantCallID:   "call_shot",
			wantImageURL: "data:image/png;base64,AA==",
			wantDetail:   "high",
			wantMessages: 3,
		},
		{
			name: "function output mixed text and image",
			input: `[
				{"type":"function_call","call_id":"call_read","name":"read_image","arguments":"{}"},
				{"type":"function_call_output","call_id":"call_read","output":[
					{"type":"input_text","text":"page 1"},
					{"type":"input_image","image_url":"https://example.test/shot.png"}
				]}
			]`,
			wantToolText: "page 1",
			wantCallID:   "call_read",
			wantImageURL: "https://example.test/shot.png",
			wantMessages: 3,
		},
		{
			name: "custom tool output image",
			input: `[
				{"type":"custom_tool_call","call_id":"call_exec","name":"exec","input":"screenshot"},
				{"type":"custom_tool_call_output","call_id":"call_exec","output":[
					{"type":"input_image","image_url":"data:image/jpeg;base64,/9j/"}
				]}
			]`,
			wantToolText: "",
			wantCallID:   "call_exec",
			wantImageURL: "data:image/jpeg;base64,/9j/",
			wantMessages: 3,
		},
		{
			name: "text-only function output stays a string",
			input: `[
				{"type":"function_call","call_id":"call_old","name":"read_file","arguments":"{}"},
				{"type":"function_call_output","call_id":"call_old","output":"ok"}
			]`,
			wantToolText: "ok",
			wantCallID:   "call_old",
			wantMessages: 2,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			messages, err := convertResponseInput(nil, json.RawMessage(test.input), nil)
			if err != nil {
				t.Fatalf("convertResponseInput() error = %v", err)
			}
			if len(messages) != test.wantMessages {
				t.Fatalf("messages = %#v, want %d", messages, test.wantMessages)
			}
			assistant := messages[0]
			if assistant["role"] != "assistant" {
				t.Fatalf("call history = %#v", assistant)
			}
			tool := messages[1]
			if tool["role"] != "tool" || tool["tool_call_id"] != test.wantCallID {
				t.Fatalf("tool message = %#v", tool)
			}
			if tool["content"] != test.wantToolText {
				t.Fatalf("tool content = %#v, want %q", tool["content"], test.wantToolText)
			}
			if test.wantImageURL == "" {
				return
			}
			user := messages[2]
			if user["role"] != "user" {
				t.Fatalf("image follow-up role = %#v", user)
			}
			parts, ok := user["content"].([]map[string]any)
			if !ok || len(parts) != 2 {
				t.Fatalf("image follow-up content = %#v", user["content"])
			}
			if parts[0]["type"] != "text" || !strings.Contains(parts[0]["text"].(string), test.wantCallID) {
				t.Fatalf("image caption = %#v", parts[0])
			}
			if parts[1]["type"] != "image_url" {
				t.Fatalf("image part = %#v", parts[1])
			}
			image, _ := parts[1]["image_url"].(map[string]any)
			if image["url"] != test.wantImageURL {
				t.Fatalf("image url = %#v", image)
			}
			if test.wantDetail != "" && image["detail"] != test.wantDetail {
				t.Fatalf("image detail = %#v, want %q", image, test.wantDetail)
			}
			if encoded, _ := json.Marshal(image["url"]); strings.Contains(string(encoded), `"content":"data:image`) {
				t.Fatal("image was stringified onto tool content")
			}
		})
	}
}

func TestConvertResponseInputRejectsEncryptedFunctionOutput(t *testing.T) {
	t.Parallel()

	_, err := convertResponseInput(nil, json.RawMessage(`[
		{"type":"function_call_output","call_id":"call_x","output":[
			{"type":"encrypted_content","data":"abc"}
		]}
	]`), nil)
	if err == nil {
		t.Fatal("expected encrypted function output to be rejected")
	}
	if !strings.Contains(err.Error(), "encrypted_content") {
		t.Fatalf("error = %v", err)
	}
}

func TestConvertResponsesRequestDropsOversizedChatMetadataValues(t *testing.T) {
	t.Parallel()

	exactLimit := strings.Repeat("a", maxChatMetadataValueBytes)
	overLimit := exactLimit + "a"
	request := responsesRequest{
		Model: "model-a",
		Input: json.RawMessage(`"hello"`),
		Metadata: map[string]string{
			"responses_exact_limit":               exactLimit,
			"responses_over_limit":                overLimit,
			"responses_over_limit_shadows_client": overLimit,
			"shared":                              "responses",
		},
		ClientMetadata: map[string]string{
			"client_exact_limit":                  exactLimit,
			"client_over_limit":                   overLimit,
			"responses_over_limit_shadows_client": "client",
			"shared":                              "client",
		},
	}

	converted, _, err := convertResponsesRequest(request)
	if err != nil {
		t.Fatalf("convertResponsesRequest() error = %v", err)
	}
	if len(converted.Metadata) != 3 {
		t.Fatalf("metadata = %#v, want three compatible values", converted.Metadata)
	}
	if converted.Metadata["responses_exact_limit"] != exactLimit {
		t.Fatal("Responses metadata at the Chat limit was dropped")
	}
	if converted.Metadata["client_exact_limit"] != exactLimit {
		t.Fatal("client metadata at the Chat limit was dropped")
	}
	if converted.Metadata["shared"] != "responses" {
		t.Fatalf("shared metadata = %q, want Responses value", converted.Metadata["shared"])
	}
	if _, exists := converted.Metadata["responses_over_limit"]; exists {
		t.Fatal("oversized Responses metadata was forwarded")
	}
	if _, exists := converted.Metadata["client_over_limit"]; exists {
		t.Fatal("oversized client metadata was forwarded")
	}
	if _, exists := converted.Metadata["responses_over_limit_shadows_client"]; exists {
		t.Fatal("lower-priority client metadata replaced dropped Responses metadata")
	}
}
