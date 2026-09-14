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

// functionOnlyUpstream fails requests whose tool declarations are not Chat
// function entries, reproducing the route that rejected a native custom tool
// with `tools[7].type: unknown variant custom, expected function`.
func functionOnlyUpstream(t *testing.T, output string) (*httptest.Server, *chatRequest) {
	t.Helper()
	captured := &chatRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, "read request", http.StatusInternalServerError)
			return
		}
		var raw struct {
			Tools []map[string]any `json:"tools"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		for _, tool := range raw.Tools {
			if tool["type"] != "function" {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(writer, fmt.Sprintf(
					`{"error":{"message":"tools.type: unknown variant %v, expected function","type":"invalid_request_error"}}`,
					tool["type"],
				))
				return
			}
		}
		if err := json.Unmarshal(body, captured); err != nil {
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, output)
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func TestGatewayDeclaresCustomToolsAsStrictFunctionWrappers(t *testing.T) {
	t.Parallel()

	server, upstreamRequest := functionOnlyUpstream(t, `{
		"id":"chat-declare","model":"model-a",
		"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
	}`)
	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"run",
		"tools":[
			{"type":"custom","name":"exec","description":"Run code","format":{"type":"text"}},
			{"type":"custom","name":"apply_patch"},
			{"type":"function","name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}
		],
		"tool_choice":"auto"
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	if len(upstreamRequest.Tools) != 3 {
		t.Fatalf("upstream tools = %#v", upstreamRequest.Tools)
	}
	for index, tool := range upstreamRequest.Tools {
		if tool["type"] != "function" {
			t.Fatalf("tools[%d] was not declared as a function: %#v", index, tool)
		}
	}
	for _, name := range []string{"exec", "apply_patch"} {
		wrapper := upstreamRequest.Tools[indexOfToolName(t, upstreamRequest.Tools, name)]["function"].(map[string]any)
		parameters := wrapper["parameters"].(map[string]any)
		properties := parameters["properties"].(map[string]any)
		input := properties["input"].(map[string]any)
		if parameters["type"] != "object" || input["type"] != "string" {
			t.Fatalf("wrapper %q parameters = %#v", name, parameters)
		}
		if parameters["additionalProperties"] != false {
			t.Fatalf("wrapper %q did not close its parameter object: %#v", name, parameters)
		}
		if fmt.Sprint(parameters["required"]) != "[input]" {
			t.Fatalf("wrapper %q required = %#v", name, parameters["required"])
		}
		description, _ := wrapper["description"].(string)
		if !strings.Contains(description, "not enforced by constrained decoding") {
			t.Fatalf("wrapper %q description = %q", name, description)
		}
	}
	readFile := upstreamRequest.Tools[indexOfToolName(t, upstreamRequest.Tools, "read_file")]["function"].(map[string]any)
	if _, wrapped := readFile["parameters"].(map[string]any)["properties"].(map[string]any)["input"]; wrapped {
		t.Fatalf("ordinary function was rewritten as a wrapper: %#v", readFile)
	}
}

func TestGatewayPreservesCustomGrammarDescriptionWithoutConstrainedDecodingClaim(t *testing.T) {
	t.Parallel()

	server, upstreamRequest := functionOnlyUpstream(t, `{
		"id":"chat-grammar","model":"model-a",
		"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
	}`)
	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"run",
		"tools":[{"type":"custom","name":"exec","description":"Run code","format":{
			"type":"grammar","syntax":"lark","definition":"start: /exec .+/"
		}}]
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	description, _ := upstreamRequest.Tools[0]["function"].(map[string]any)["description"].(string)
	for _, want := range []string{"Run code", "start: /exec .+/", "lark", "guidance only"} {
		if !strings.Contains(description, want) {
			t.Fatalf("wrapper description %q is missing %q", description, want)
		}
	}
}

func TestGatewayMapsCustomToolChoiceHistoryAndWrappedOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		upstreamBody string
		wantInput    string
	}{
		{
			name: "plain text input with quotes and newlines",
			upstreamBody: `{"id":"chat-1","model":"model-a","choices":[{"index":0,"message":{
				"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_exec","type":"function",
				"function":{"name":"exec","arguments":"{\"input\":\"line one\\nline \\\"two\\\"\\\\end\"}"}}]},
				"finish_reason":"tool_calls"}]}`,
			wantInput: "line one\nline \"two\"\\end",
		},
		{
			name: "unicode and unmatched braces",
			upstreamBody: `{"id":"chat-2","model":"model-a","choices":[{"index":0,"message":{
				"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_exec","type":"function",
				"function":{"name":"exec","arguments":"{\"input\":\"处理 { 括号\"}"}}]},
				"finish_reason":"tool_calls"}]}`,
			wantInput: "处理 { 括号",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, upstreamRequest := functionOnlyUpstream(t, test.upstreamBody)
			gateway := newTestGateway(t, Config{})
			endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", test.name)
			body := `{
				"model":"model-a",
				"input":[
					{"type":"custom_tool_call","call_id":"call_previous","name":"exec","input":"print 1"},
					{"type":"custom_tool_call_output","call_id":"call_previous","output":"1"},
					{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
				],
				"tools":[{"type":"custom","name":"exec"},{"type":"function","name":"read_file"}],
				"tool_choice":{"type":"custom","name":"exec"}
			}`
			response := postResponses(t, endpoint, body, nil)
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
			}
			historyCalls := upstreamRequest.Messages[0]["tool_calls"].([]any)
			historyCall := historyCalls[0].(map[string]any)
			historyFunction := historyCall["function"].(map[string]any)
			if historyCall["type"] != "function" || historyCall["id"] != "call_previous" ||
				historyFunction["arguments"] != `{"input":"print 1"}` {
				t.Fatalf("custom history = %#v", historyCall)
			}
			if upstreamRequest.Messages[1]["role"] != "tool" || upstreamRequest.Messages[1]["tool_call_id"] != "call_previous" {
				t.Fatalf("custom output message = %#v", upstreamRequest.Messages[1])
			}
			choice := upstreamRequest.ToolChoice.(map[string]any)
			if choice["type"] != "function" || choice["function"].(map[string]any)["name"] != "exec" {
				t.Fatalf("custom tool choice = %#v", upstreamRequest.ToolChoice)
			}

			var converted map[string]any
			if err := json.NewDecoder(response.Body).Decode(&converted); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			item := converted["output"].([]any)[0].(map[string]any)
			if item["type"] != "custom_tool_call" || item["call_id"] != "call_exec" ||
				item["name"] != "exec" || item["input"] != test.wantInput {
				t.Fatalf("custom output item = %#v, want input %q", item, test.wantInput)
			}
		})
	}
}

func TestGatewayPreservesParallelCustomToolCallIdentities(t *testing.T) {
	t.Parallel()

	server, _ := functionOnlyUpstream(t, `{"id":"chat-parallel","model":"model-a","choices":[{"index":0,"message":{
		"role":"assistant","content":null,"tool_calls":[
			{"index":0,"id":"call_one","type":"function","function":{"name":"exec","arguments":"{\"input\":\"first\"}"}},
			{"index":1,"id":"call_two","type":"function","function":{"name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\"}"}}
		]},"finish_reason":"tool_calls"}]}`)
	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"run both",
		"tools":[{"type":"custom","name":"exec"},{"type":"custom","name":"apply_patch"}],
		"parallel_tool_calls":true
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	var converted map[string]any
	if err := json.NewDecoder(response.Body).Decode(&converted); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	output := converted["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("output = %#v", output)
	}
	byCallID := map[string]map[string]any{}
	for _, entry := range output {
		item := entry.(map[string]any)
		byCallID[item["call_id"].(string)] = item
	}
	if byCallID["call_one"]["name"] != "exec" || byCallID["call_one"]["input"] != "first" {
		t.Fatalf("first custom call = %#v", byCallID["call_one"])
	}
	if byCallID["call_two"]["name"] != "apply_patch" || byCallID["call_two"]["input"] != "*** Begin Patch" {
		t.Fatalf("second custom call = %#v", byCallID["call_two"])
	}
}

func TestGatewayStreamsParallelCustomToolCallsAndSplitNames(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			// The second call's name arrives in two chunks, so the call may
			// only be classified after the full name is known.
			`{"id":"chat-split","model":"model-a","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"exec","arguments":"{\"input\":\"multi\\nline\"}"}},{"index":1,"id":"call_two","type":"function","function":{"name":"apply_"}}]}}]}`,
			`{"id":"chat-split","model":"model-a","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_one","function":{"name":"_code"}},{"index":1,"function":{"name":"patch","arguments":"{\"input\":\"*** Begin Patch\\n*** End Patch\"}"}}]}}]}`,
			`{"id":"chat-split","model":"model-a","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		} {
			_, _ = io.WriteString(writer, "data: "+event+"\n\n")
		}
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"run",
		"tools":[{"type":"function","name":"exec","parameters":{"type":"object"}},{"type":"custom","name":"exec_code"},{"type":"custom","name":"apply_patch"}],
		"parallel_tool_calls":true,
		"stream":true
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}

	inputsByCallID := map[string]string{}
	typesByCallID := map[string]string{}
	var leakedRawInput []string
	var failed map[string]any
	for _, event := range readSSEEvents(t, response.Body) {
		var payload map[string]any
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("decode %s: %v", event.Event, err)
		}
		switch event.Event {
		case "response.custom_tool_call_input.delta":
			inputsByCallID[payload["item_id"].(string)] += payload["delta"].(string)
		case "response.output_item.added":
			item, _ := payload["item"].(map[string]any)
			if item["call_id"] != "call_one" && item["call_id"] != "call_two" {
				t.Fatalf("unstable call ID: %#v", item)
			}
			typesByCallID[item["id"].(string)], _ = item["type"].(string)
		case "response.function_call_arguments.delta":
			leakedRawInput = append(leakedRawInput, payload["delta"].(string))
		case "response.failed":
			failed, _ = payload["response"].(map[string]any)
		}
	}
	if failed != nil {
		t.Fatalf("stream failed: %#v", failed["error"])
	}
	if len(inputsByCallID) != 2 {
		t.Fatalf("custom input items = %#v", inputsByCallID)
	}
	gotInputs := make([]string, 0, len(inputsByCallID))
	for _, input := range inputsByCallID {
		gotInputs = append(gotInputs, input)
	}
	if !containsString(gotInputs, "multi\nline") || !containsString(gotInputs, "*** Begin Patch\n*** End Patch") {
		t.Fatalf("custom inputs = %#v", gotInputs)
	}
	if len(leakedRawInput) != 0 {
		t.Fatalf("wrapper JSON leaked as function arguments: %#v", leakedRawInput)
	}
	if typesByCallID[firstMapKey(typesByCallID)] != "custom_tool_call" {
		t.Fatalf("custom call announced the wrong type: %#v", typesByCallID)
	}
}

func TestGatewaySynthesizesCustomToolStreamFromChatJSON(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"resp_synthetic","model":"model-a","choices":[{"index":0,"message":{
			"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_exec","type":"function",
			"function":{"name":"exec","arguments":"{\"input\":\"await tools.exec_command({cmd:\\\"pwd\\\"})\"}"}}]},
			"finish_reason":"tool_calls"}]}`)
	}))
	defer upstream.Close()

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"inspect",
		"tools":[{"type":"custom","name":"exec"}],
		"stream":true
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}

	var input string
	var completedItem map[string]any
	var completedResponse map[string]any
	for _, event := range readSSEEvents(t, response.Body) {
		var payload map[string]any
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("decode %s: %v", event.Event, err)
		}
		switch event.Event {
		case "response.custom_tool_call_input.done":
			input, _ = payload["input"].(string)
		case "response.output_item.done":
			item, _ := payload["item"].(map[string]any)
			if item["type"] == "custom_tool_call" {
				completedItem = item
			}
		case "response.completed":
			completedResponse, _ = payload["response"].(map[string]any)
		}
	}
	want := `await tools.exec_command({cmd:"pwd"})`
	if input != want || completedItem["input"] != want || completedItem["call_id"] != "call_exec" {
		t.Fatalf("synthetic custom input = %q, item = %#v", input, completedItem)
	}
	if completedResponse == nil || completedResponse["id"] != "resp_synthetic" {
		t.Fatalf("completed response = %#v", completedResponse)
	}
	output := completedResponse["output"].([]any)
	if len(output) != 1 || output[0].(map[string]any)["type"] != "custom_tool_call" {
		t.Fatalf("synthetic output = %#v", output)
	}
}

func TestGatewayRejectsMalformedCustomToolWrapperArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments string
		want      string
	}{
		{name: "missing input", arguments: `{}`, want: `missing the required "input" string`},
		{name: "null input", arguments: `{"input":null}`, want: `must be a string`},
		{name: "wrong input type", arguments: `{"input":42}`, want: `must be a string`},
		{name: "unexpected member", arguments: `{"input":"ok","extra":true}`, want: `unexpected member "extra"`},
		{name: "truncated json", arguments: `{"input":`, want: "must be a JSON object"},
		{name: "non object", arguments: `"freeform"`, want: "must be a JSON object"},
		{name: "empty arguments", arguments: "", want: `missing the required "input" string`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, _ := functionOnlyUpstream(t, fmt.Sprintf(
				`{"id":"chat-bad","model":"model-a","choices":[{"index":0,"message":{
					"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_exec","type":"function",
					"function":{"name":"exec","arguments":%q}}]},"finish_reason":"tool_calls"}]}`,
				test.arguments,
			))
			gateway := newTestGateway(t, Config{})
			endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", test.name)
			response := postResponses(t, endpoint, `{
				"model":"model-a",
				"input":"inspect",
				"tools":[{"type":"custom","name":"exec"}]
			}`, nil)
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadGateway {
				t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
			}
			var payload responsesErrorEnvelope
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if !strings.Contains(payload.Error.Message, test.want) {
				t.Fatalf("error = %#v, want %q", payload.Error, test.want)
			}
		})
	}
}

func TestGatewayFailsStreamingCustomToolWithMalformedWrapperArguments(t *testing.T) {
	t.Parallel()

	server, _ := functionOnlyUpstream(t, `{"id":"chat-bad-stream","model":"model-a","choices":[{"index":0,"message":{
		"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_exec","type":"function",
		"function":{"name":"exec","arguments":"{\"input\":12}"}}]},"finish_reason":"tool_calls"}]}`)
	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"inspect",
		"tools":[{"type":"custom","name":"exec"}],
		"stream":true
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	events := readSSEEvents(t, response.Body)
	last := events[len(events)-1]
	if last.Event != "response.failed" {
		t.Fatalf("last event = %#v", last)
	}
	var payload map[string]any
	if err := json.Unmarshal(last.Data, &payload); err != nil {
		t.Fatalf("decode failed event: %v", err)
	}
	responseError := payload["response"].(map[string]any)["error"].(map[string]any)
	message, _ := responseError["message"].(string)
	if responseError["code"] != "upstream_error" || !strings.Contains(message, `"input" must be a string`) {
		t.Fatalf("stream error = %#v", responseError)
	}
	if strings.Contains(message, `{"input":12}`) {
		t.Fatalf("stream error leaked wrapper payload: %q", message)
	}
}

func TestGatewayRejectsNativeCustomChatToolCalls(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"chat-native","model":"model-a","choices":[{"index":0,"message":{
			"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_exec","type":"custom",
			"custom":{"name":"exec","input":"print 1"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer upstream.Close()

	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, upstream.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"inspect",
		"tools":[{"type":"custom","name":"exec"}]
	}`, nil)
	defer response.Body.Close()
	// A function-only upstream can only answer with the function wrapper, so a
	// native custom call must fail instead of being silently reinterpreted.
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	var payload responsesErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !strings.Contains(payload.Error.Message, "not a function call") {
		t.Fatalf("error = %#v", payload.Error)
	}
}

func TestGatewayRejectsDuplicateFunctionAndCustomToolNames(t *testing.T) {
	t.Parallel()

	server, _ := functionOnlyUpstream(t, `{"id":"chat-dup","model":"model-a","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"x",
		"tools":[
			{"type":"function","name":"exec","parameters":{"type":"object"}},
			{"type":"custom","name":"exec"}
		]
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	var payload responsesErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if payload.Error.Param == nil || *payload.Error.Param != "tools[1].name" {
		t.Fatalf("error = %#v", payload.Error)
	}
}

func TestGatewayRejectsUnsupportedCustomToolFormat(t *testing.T) {
	t.Parallel()

	server, _ := functionOnlyUpstream(t, `{"id":"chat-format","model":"model-a","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	gateway := newTestGateway(t, Config{})
	endpoint := registerTestRoute(t, gateway, server.URL, "secret", "model-a", "workspace", "session")
	response := postResponses(t, endpoint, `{
		"model":"model-a",
		"input":"x",
		"tools":[{"type":"custom","name":"exec","format":{"type":"grammar","definition":"start: /.+/"}}]
	}`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	var payload responsesErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if payload.Error.Param == nil || *payload.Error.Param != "tools[0].format" {
		t.Fatalf("error = %#v", payload.Error)
	}
}

func indexOfToolName(t *testing.T, tools []map[string]any, name string) int {
	t.Helper()
	for index, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		if function["name"] == name {
			return index
		}
	}
	t.Fatalf("tool %q not found in %#v", name, tools)
	return -1
}

func firstMapKey(values map[string]string) string {
	for key := range values {
		return key
	}
	return ""
}

func TestCustomToolNamesAvoidFunctionCollisions(t *testing.T) {
	tools, mapping, _, err := convertResponseTools([]json.RawMessage{
		json.RawMessage(`{"type":"custom","name":"a.b"}`),
		json.RawMessage(`{"type":"function","name":"custom__a_b","parameters":{"type":"object"}}`),
		json.RawMessage(`{"type":"custom","name":"a_b"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		name := tool["function"].(map[string]any)["name"].(string)
		if seen[name] || len(name) > 64 {
			t.Fatalf("invalid or duplicate name: %q", name)
		}
		seen[name] = true
	}
	name := tools[0]["function"].(map[string]any)["name"].(string)
	if mapping[name].Name != "a.b" || !mapping[name].WrappedCustomInput {
		t.Fatalf("lost identity: %#v", mapping)
	}
}
