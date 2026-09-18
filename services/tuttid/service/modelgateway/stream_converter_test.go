package modelgateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStreamedTextAppend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		chunks     []string
		wantText   string
		wantDeltas []string
	}{
		{
			name:       "true deltas append",
			chunks:     []string{"让我", "检查"},
			wantText:   "让我检查",
			wantDeltas: []string{"让我", "检查"},
		},
		{
			name:       "cumulative snapshots emit only suffixes",
			chunks:     []string{"用户要求", "用户要求", "用户要求检查", "用户要求"},
			wantText:   "用户要求检查",
			wantDeltas: []string{"用户要求", "", "检查", ""},
		},
		{
			name:       "duplicate keeps classification unknown",
			chunks:     []string{"思考", "思考", "思考更多"},
			wantText:   "思考更多",
			wantDeltas: []string{"思考", "", "更多"},
		},
		{
			name:       "stale snapshot before growth stays unclassified",
			chunks:     []string{"ABC", "AB", "ABCX"},
			wantText:   "ABCX",
			wantDeltas: []string{"ABC", "", "X"},
		},
		{
			name:       "whitespace-polished snapshot before growth stays unclassified",
			chunks:     []string{"\n思考", "思考更多"},
			wantText:   "\n思考更多",
			wantDeltas: []string{"\n思考", "更多"},
		},
		{
			name:       "leading whitespace in growing snapshot does not shift suffix",
			chunks:     []string{"思考", " 思考更多"},
			wantText:   "思考更多",
			wantDeltas: []string{"思考", "更多"},
		},
		{
			name:       "snapshot whitespace drift does not append later growth twice",
			chunks:     []string{"思考\n", " 思考更多", " 思考更多内容"},
			wantText:   "思考\n更多内容",
			wantDeltas: []string{"思考\n", "更多", "内容"},
		},
		{
			name:       "internal whitespace normalization does not duplicate snapshot growth",
			chunks:     []string{"A BC", "A  BCX", "A  BCXY"},
			wantText:   "A BCXY",
			wantDeltas: []string{"A BC", "X", "Y"},
		},
		{
			name:       "shorter whitespace-equivalent snapshot keeps classification unknown",
			chunks:     []string{"思考 ", "思考", "思考更多"},
			wantText:   "思考 更多",
			wantDeltas: []string{"思考 ", "", "更多"},
		},
		{
			name:       "incremental overlap is preserved",
			chunks:     []string{"abc", "bcde"},
			wantText:   "abcbcde",
			wantDeltas: []string{"abc", "bcde"},
		},
		{
			name:       "delta mode preserves repeated chunks",
			chunks:     []string{"a", "b", "b"},
			wantText:   "abb",
			wantDeltas: []string{"a", "b", "b"},
		},
		{
			name:       "single rune deltas append",
			chunks:     []string{"字段", "。"},
			wantText:   "字段。",
			wantDeltas: []string{"字段", "。"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stream streamedText
			deltas := make([]string, 0, len(test.chunks))
			for _, chunk := range test.chunks {
				deltas = append(deltas, stream.append(chunk))
			}
			joined := strings.Join(deltas, "")
			if stream.String() != test.wantText || joined != stream.String() ||
				!utf8.ValidString(joined) || !reflect.DeepEqual(deltas, test.wantDeltas) {
				t.Fatalf("streamedText(%q) = text %q, deltas %#v, joined %q valid=%v; want text %q, deltas %#v",
					test.chunks, stream.String(), deltas, joined, utf8.ValidString(joined),
					test.wantText, test.wantDeltas)
			}
		})
	}
}

func TestGatewayNormalizesCumulativeStreamSnapshots(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"reasoning_content":%q}}]}`, "思考\n"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"reasoning_content":%q}}]}`, "思"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"reasoning_content":%q}}]}`, " 思考更多"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"reasoning_content":%q}}]}`, " 思考更多"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"reasoning_content":%q}}]}`, " 思考更多内容"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"reasoning_content":%q}}]}`, "思考"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"content":%q}}]}`, "<think>\nGreat"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"content":%q}}]}`, "<think>\nGreat target"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"content":%q}}]}`, "<think>\nGreat target"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"content":%q}}]}`, "<think>\nGreat"),
			fmt.Sprintf(`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{"content":%q}}]}`, " again"),
			`{"id":"chat-snapshot","model":"model-a","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, event := range events {
			_, _ = io.WriteString(writer, "data: "+event+"\n\n")
		}
	}))
	defer upstream.Close()

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{"model":"model-a","input":"x","stream":true}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}

	var reasoningDeltas []string
	var textDeltas []string
	var reasoningDone string
	var textDone string
	completed := false
	for _, event := range readSSEEvents(t, response.Body) {
		var payload map[string]any
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("decode %s: %v", event.Event, err)
		}
		switch event.Event {
		case "response.reasoning_text.delta":
			reasoningDeltas = append(reasoningDeltas, payload["delta"].(string))
		case "response.output_text.delta":
			textDeltas = append(textDeltas, payload["delta"].(string))
		case "response.output_item.done":
			item, _ := payload["item"].(map[string]any)
			text := completedItemText(item)
			switch item["type"] {
			case "reasoning":
				reasoningDone = text
			case "message":
				textDone = text
			}
		case "response.completed":
			completed = true
		}
	}

	if want := []string{"思考\n", "更多", "内容"}; !reflect.DeepEqual(reasoningDeltas, want) {
		t.Fatalf("reasoning deltas = %#v, want %#v", reasoningDeltas, want)
	}
	if want := []string{"<think>\nGreat", " target", " again"}; !reflect.DeepEqual(textDeltas, want) {
		t.Fatalf("text deltas = %#v, want %#v", textDeltas, want)
	}
	if reasoningDone != "思考\n更多内容" {
		t.Fatalf("completed reasoning = %q", reasoningDone)
	}
	if textDone != "<think>\nGreat target again" {
		t.Fatalf("completed text = %q", textDone)
	}
	if !completed {
		t.Fatal("response.completed missing")
	}
}

func completedItemText(item map[string]any) string {
	content, _ := item["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	part, _ := content[0].(map[string]any)
	text, _ := part["text"].(string)
	return text
}
