package modelgateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The 2026-09-18 session ended with the model writing three closing-only DSML
// tags into message content and no structured tool_calls at all. Translated
// verbatim, the Responses client received that residue as the finished answer
// and the turn settled as completed, while the file work the session planned
// never ran. The refusal below is those tags, with the doubled sentinel bars
// and the space before each tag name exactly as recorded.
//
// The sentinel is spelled from runes so this file stays plain ASCII; the string
// built at runtime is the same byte sequence the session emitted.
const dsmlBar = "｜"

func dsmlClosingTag(name string) string {
	return "</" + dsmlBar + dsmlBar + "DSML" + dsmlBar + dsmlBar + " " + name + ">"
}

func dsmlOpeningTag(name string) string {
	return "<" + dsmlBar + dsmlBar + "DSML" + dsmlBar + dsmlBar + " " + name + ">"
}

var incidentResidue = strings.Join([]string{
	dsmlClosingTag("parameter"),
	dsmlClosingTag("invoke"),
	dsmlClosingTag("calls"),
}, "\n")

func TestAssistantTextIsProtocolResidue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty", text: "", want: false},
		{
			name: "the incident closing-only tags",
			text: incidentResidue,
			want: true,
		},
		{
			name: "one closing-only tag",
			text: dsmlClosingTag("parameter"),
			want: true,
		},
		{
			name: "single-bar sentinel",
			text: "<" + dsmlBar + "DSML" + dsmlBar + "tool_calls>",
			want: true,
		},
		{
			name: "bare protocol spelling",
			text: "<parameter name=\"cmd\">pwd</parameter>",
			want: true,
		},
		{
			name: "call with only its argument text left after stripping",
			text: dsmlOpeningTag("tool_calls") + dsmlOpeningTag("parameter") + "pwd" + dsmlClosingTag("parameter"),
			want: true,
		},
		{name: "normal answer", text: "已按工单要求完成修改。", want: false},
		{name: "comparison is not a tag", text: "use a < b when sorting", want: false},
		{name: "html mention is not DSML", text: "Wrap the label in a <div> please.", want: false},
		{
			name: "an answer that quotes the protocol stays an answer",
			text: "引擎用 " + dsmlOpeningTag("tool_calls") + " 作为工具调用标记，下面是格式说明。",
			want: false,
		},
		{
			name: "an answer followed by a leaked close tag stays an answer",
			text: "已经检查完仓库里的相关文件，结论见上。" + dsmlClosingTag("parameter"),
			want: false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := assistantTextIsProtocolResidue(test.text); got != test.want {
				t.Fatalf("assistantTextIsProtocolResidue(%q) = %v, want %v", test.text, got, test.want)
			}
		})
	}
}

func TestStreamingHoldbackStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "plain answer is fully safe", text: "已完成修改", want: len("已完成修改")},
		{name: "residue is held entirely", text: incidentResidue, want: 0},
		{
			name: "residue after an answer is held from the first tag",
			text: "已完成修改" + incidentResidue,
			want: len("已完成修改"),
		},
		{
			name: "a tag without its closing bracket is held",
			text: "已完成修改" + "</" + dsmlBar + dsmlBar + "DSML" + dsmlBar,
			want: len("已完成修改"),
		},
		{
			name: "an unclosed angle bracket is held",
			text: "已完成修改<",
			want: len("已完成修改"),
		},
		{
			name: "a long answer past the holdback window is fully safe",
			text: strings.Repeat("已完成修改", 4096),
			want: len(strings.Repeat("已完成修改", 4096)),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := streamingHoldbackStart(test.text); got != test.want {
				t.Fatalf("streamingHoldbackStart(%q) = %d, want %d", test.text, got, test.want)
			}
		})
	}
}

func chatContentDelta(t *testing.T, content string) string {
	t.Helper()
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	return fmt.Sprintf(
		`{"id":"chat-1","model":"model-a","choices":[{"index":0,"delta":{"content":%s}}]}`,
		encoded,
	)
}

func streamUpstream(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher := writer.(http.Flusher)
		for _, event := range events {
			_, _ = io.WriteString(writer, "data: "+event+"\n\n")
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func postStreamingResponses(t *testing.T, endpoint ClientEndpoint, body string) []sseEvent {
	t.Helper()
	response := postResponses(t, endpoint, body, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	return readSSEEvents(t, response.Body)
}

func sseEventTypes(events []sseEvent) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Event)
	}
	return types
}

func sseDeltas(t *testing.T, events []sseEvent) []string {
	t.Helper()
	deltas := make([]string, 0, len(events))
	for _, event := range events {
		if event.Event != "response.output_text.delta" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("decode %s: %v", event.Event, err)
		}
		delta, _ := payload["delta"].(string)
		deltas = append(deltas, delta)
	}
	return deltas
}

// The fragment arrives split across deltas, the way a real stream delivers it.
// None of it may reach the client, and the turn must end failed rather than
// completed: a closing-only fragment carries no call to promote, so the only
// honest outcome is a failure the caller can retry.
func TestGatewayFailsStreamWhoseAnswerIsOnlyProtocolResidue(t *testing.T) {
	t.Parallel()

	upstream := streamUpstream(t, []string{
		chatContentDelta(t, "</"),
		chatContentDelta(t, dsmlBar+dsmlBar+"DSML"+dsmlBar),
		chatContentDelta(t, dsmlBar+" parameter>"),
		chatContentDelta(t, "\n"+dsmlClosingTag("invoke")+"\n"+dsmlClosingTag("calls")),
		`{"id":"chat-1","model":"model-a","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	events := postStreamingResponses(t, endpoint, `{"model":"model-a","input":"继续","stream":true}`)

	types := sseEventTypes(events)
	if !containsString(types, "response.failed") {
		t.Fatalf("missing response.failed in %v", types)
	}
	for _, forbidden := range []string{"response.completed", "response.output_text.done"} {
		if containsString(types, forbidden) {
			t.Fatalf("unexpected %q in %v", forbidden, types)
		}
	}
	for _, delta := range sseDeltas(t, events) {
		if strings.Contains(delta, "DSML") || strings.Contains(delta, dsmlBar) {
			t.Fatalf("protocol residue reached the client as text: %q", delta)
		}
	}
}

// A model may legitimately write about the protocol it uses. That answer must
// still complete, and its text must still arrive intact.
func TestGatewayKeepsProseThatQuotesProtocolMarkup(t *testing.T) {
	t.Parallel()

	const answer = "引擎用 " + "<" + dsmlBar + "DSML" + dsmlBar + "tool_calls>" + " 作为工具调用标记，下面是格式说明。"
	upstream := streamUpstream(t, []string{
		chatContentDelta(t, answer),
		`{"id":"chat-1","model":"model-a","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	})

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	events := postStreamingResponses(t, endpoint, `{"model":"model-a","input":"解释一下","stream":true}`)

	if types := sseEventTypes(events); !containsString(types, "response.completed") {
		t.Fatalf("missing response.completed in %v", types)
	}
	if got := strings.Join(sseDeltas(t, events), ""); got != answer {
		t.Fatalf("streamed answer = %q, want %q", got, answer)
	}
}

// Markup in content does not change the verdict when the upstream also spelled
// a real structured call: the call is what the client needs.
func TestGatewayKeepsStructuredToolCallsBesideMarkupContent(t *testing.T) {
	t.Parallel()

	upstream := streamUpstream(t, []string{
		chatContentDelta(t, incidentResidue),
		`{"id":"chat-1","model":"model-a","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"alpha","arguments":"{\"a\":1}"}}]}}]}`,
		`{"id":"chat-1","model":"model-a","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	})

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	events := postStreamingResponses(t, endpoint, `{
		"model":"model-a",
		"input":"跑一下",
		"tools":[{"type":"function","name":"alpha","parameters":{"type":"object"}}],
		"tool_choice":"auto",
		"stream":true
	}`)

	if types := sseEventTypes(events); !containsString(types, "response.completed") {
		t.Fatalf("missing response.completed in %v", types)
	}
	for _, event := range events {
		// The deltas were withheld, so the authoritative done/item events must
		// not contradict them by carrying the residue.
		if strings.Contains(string(event.Data), "DSML") || strings.Contains(string(event.Data), dsmlBar) {
			t.Fatalf("protocol residue leaked through %s: %s", event.Event, event.Data)
		}
	}
	foundCall := false
	for _, event := range events {
		if event.Event != "response.output_item.done" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("decode %s: %v", event.Event, err)
		}
		if item, ok := payload["item"].(map[string]any); ok && item["type"] == "function_call" {
			foundCall = true
		}
	}
	if !foundCall {
		t.Fatal("structured tool call was dropped")
	}
}

// The request path without streaming must reach the same verdict, or a client
// that does not stream would still receive the residue as an answer.
func TestGatewayNonStreamingFailsProtocolResidue(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(incidentResidue)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, fmt.Sprintf(`{
			"id":"chatcmpl-upstream","object":"chat.completion","created":1,"model":"model-a",
			"choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]
		}`, encoded))
	}))
	defer upstream.Close()

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{"model":"model-a","input":"继续"}`, nil)
	defer response.Body.Close()

	body := readBody(t, response.Body)
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.StatusCode, body)
	}
	if !strings.Contains(body, "model_protocol_error") {
		t.Fatalf("body = %s, want model_protocol_error", body)
	}
}
