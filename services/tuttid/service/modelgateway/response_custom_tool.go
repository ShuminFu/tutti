package modelgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Responses custom tools carry one free-form text input and allow the caller to
// describe that input with a grammar or plain-text format. Chat Completions has
// no custom tool type, so the gateway encodes a custom tool as a function with
// one required string parameter and restores the custom response shape on the
// way back. This file owns both directions of that encoding so the request,
// non-streaming response, and streaming conversion share one definition.

// customToolInputParameter is the single parameter of the synthesized wrapper
// function. The value is the original custom tool input as one opaque string.
const customToolInputParameter = "input"

// customToolWrapperFormatNote tells the model how the free-form input is
// transported. The wrapper is an encoding detail of this gateway: it does not
// give the upstream any grammar-constrained decoding guarantee, so the note
// states the requirement without promising enforcement.
const customToolWrapperFormatNote = "Provide free-form input for this tool as the required string parameter \"input\". The gateway forwards that string to the tool unchanged; the format description below is guidance only and is not enforced by constrained decoding."

func customToolParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			customToolInputParameter: map[string]any{"type": "string"},
		},
		"required":             []any{customToolInputParameter},
		"additionalProperties": false,
	}
}

// customToolArguments encodes one custom tool input as the wrapper function
// arguments document. Callers must pass the input unchanged: the gateway never
// inspects, rewrites, or re-encodes the tool payload itself.
func customToolArguments(input string) (string, error) {
	encoded, err := json.Marshal(map[string]any{customToolInputParameter: input})
	if err != nil {
		return "", fmt.Errorf("encode custom tool input as wrapper function arguments: %w", err)
	}
	return string(encoded), nil
}

// decodeCustomToolInput strictly unwraps wrapper function arguments back into
// the original custom tool input. A wrapper that is not a JSON object, omits
// "input", carries a non-string "input", or adds unexpected members is a
// conversion failure: silently substituting an empty string would fabricate a
// tool call the model never made.
func decodeCustomToolInput(encoded json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", fmt.Errorf("custom tool wrapper arguments are missing the required %q string", customToolInputParameter)
	}
	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &arguments); err != nil {
		return "", fmt.Errorf("custom tool wrapper arguments must be a JSON object: %w", err)
	}
	rawInput, exists := arguments[customToolInputParameter]
	if !exists {
		return "", fmt.Errorf("custom tool wrapper arguments are missing the required %q string", customToolInputParameter)
	}
	var input string
	if err := json.Unmarshal(rawInput, &input); err != nil || bytes.Equal(bytes.TrimSpace(rawInput), []byte("null")) {
		return "", fmt.Errorf("custom tool wrapper argument %q must be a string", customToolInputParameter)
	}
	if len(arguments) > 1 {
		for name := range arguments {
			if name != customToolInputParameter {
				return "", fmt.Errorf("custom tool wrapper arguments contain unexpected member %q", name)
			}
		}
	}
	return input, nil
}

// customToolFormatNote describes the declared custom tool format for the
// function description. Only text and grammar formats are translatable; an
// unsupported format stays a request error so a caller never loses the
// constraint it asked for.
func customToolFormatNote(encodedFormat json.RawMessage, param string) (string, error) {
	trimmed := bytes.TrimSpace(encodedFormat)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	var format struct {
		Type       string `json:"type"`
		Definition string `json:"definition"`
		Syntax     string `json:"syntax"`
	}
	if err := json.Unmarshal(trimmed, &format); err != nil {
		return "", invalidParam(param+".format", "custom tool format must be valid JSON")
	}
	switch format.Type {
	case "text":
		return "This custom tool accepts free-form text input.", nil
	case "grammar":
		if format.Definition == "" || (format.Syntax != "lark" && format.Syntax != "regex") {
			return "", invalidParam(param+".format", "custom tool grammar requires a definition and lark or regex syntax")
		}
		return fmt.Sprintf(
			"This custom tool input is documented as %s grammar:\n%s",
			format.Syntax, format.Definition,
		), nil
	default:
		return "", invalidParam(param+".format.type", fmt.Sprintf("unsupported custom tool format %q", format.Type))
	}
}

func customToolWrapperDescription(description string, formatNote string) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{strings.TrimSpace(description), customToolWrapperFormatNote, strings.TrimSpace(formatNote)} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}
