// Package claudecli is a Go client for the Claude Code CLI's stream-json
// interface. It replaces the Node @anthropic-ai/claude-agent-sdk for the
// sidecar's needs: it spawns the claude executable with
// `--input-format stream-json --output-format stream-json`, streams user
// messages over stdin, consumes SDK messages from stdout, and speaks the
// bidirectional control protocol (initialize, canUseTool callbacks, hook
// callbacks, interrupt, set_model, and friends).
package claudecli

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// ToolsOption mirrors the SDK Options["tools"] union.
type ToolsOption interface{ isToolsOption() }

// ToolsPreset selects a named tool preset.
type ToolsPreset struct{ Preset string }

func (ToolsPreset) isToolsOption() {}

// ToolsList selects an explicit tool list.
type ToolsList []string

func (ToolsList) isToolsOption() {}

// PluginConfig mirrors SdkPluginConfig.
type PluginConfig struct {
	Type             string
	Path             string
	SkipMcpDiscovery bool
}

// PermissionResult mirrors the SDK PermissionResult union.
type PermissionResult struct {
	Behavior           string // "allow" or "deny"
	UpdatedInput       map[string]any
	UpdatedPermissions []map[string]any
	Message            string
}

// ToolPermissionRequest carries the canUseTool callback options.
type ToolPermissionRequest struct {
	Ctx            context.Context
	Suggestions    []map[string]any
	HasSuggestions bool
	BlockedPath    string
	DecisionReason any
	Title          string
	ToolUseID      string
	AgentID        string
	RequestID      string
}

// CanUseToolFunc decides a tool permission. Returning suppress=true writes no
// control response (the SDK's null sentinel).
type CanUseToolFunc func(toolName string, input map[string]any, request ToolPermissionRequest) (result PermissionResult, suppress bool, err error)

// HookCallback handles one hook invocation and returns the hook output.
type HookCallback func(input map[string]any, toolUseID string) map[string]any

// HookMatcher mirrors the SDK hook matcher config.
type HookMatcher struct {
	Matcher string
	Hooks   []HookCallback
	Timeout float64
}

// Options mirrors the subset of SDK query options the sidecar uses.
type Options struct {
	CWD                             string
	Env                             map[string]string
	PathToClaudeCodeExecutable      string
	IncludePartialMessages          bool
	CanUseTool                      CanUseToolFunc
	Resume                          string
	SessionID                       string
	Model                           string
	PermissionMode                  string
	AllowDangerouslySkipPermissions bool
	Settings                        map[string]any
	SystemPromptPreset              string
	SystemPromptAppend              string
	PlanModeInstructions            string
	Tools                           ToolsOption
	AllowedTools                    []string
	DisallowedTools                 []string
	Plugins                         []PluginConfig
	ExtraArgs                       map[string]*string
	Hooks                           map[string][]HookMatcher
}

// cliArgs builds the claude CLI argument list the way the SDK transport does.
func (o *Options) cliArgs() []string {
	args := []string{"--output-format", "stream-json", "--verbose", "--input-format", "stream-json"}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.CanUseTool != nil {
		args = append(args, "--permission-prompt-tool", "stdio")
	}
	if o.Resume != "" {
		args = append(args, "--resume", o.Resume)
	}
	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(o.AllowedTools, ","))
	}
	if len(o.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(o.DisallowedTools, ","))
	}
	switch tools := o.Tools.(type) {
	case ToolsList:
		args = append(args, "--tools", strings.Join(tools, ","))
	case ToolsPreset:
		args = append(args, "--tools", "default")
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}
	if o.AllowDangerouslySkipPermissions {
		args = append(args, "--allow-dangerously-skip-permissions")
	}
	if o.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	for _, plugin := range o.Plugins {
		flag := "--plugin-dir"
		if plugin.SkipMcpDiscovery {
			flag = "--plugin-dir-no-mcp"
		}
		args = append(args, flag, plugin.Path)
	}
	if o.SessionID != "" {
		args = append(args, "--session-id", o.SessionID)
	}
	extraArgs := map[string]*string{}
	for key, value := range o.ExtraArgs {
		extraArgs[key] = value
	}
	if len(o.Settings) > 0 {
		serialized, err := json.Marshal(o.Settings)
		if err == nil {
			settings := string(serialized)
			extraArgs["settings"] = &settings
		}
	}
	keys := make([]string, 0, len(extraArgs))
	for key := range extraArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := extraArgs[key]
		if value == nil {
			args = append(args, "--"+key)
		} else {
			args = append(args, "--"+key, *value)
		}
	}
	return args
}

// initializeRequest builds the initialize control request payload, allocating
// hook callback ids into callbacks.
func (o *Options) initializeRequest(callbacks map[string]HookCallback) map[string]any {
	request := map[string]any{"subtype": "initialize"}
	if len(o.Hooks) > 0 {
		hooksPayload := map[string]any{}
		nextID := 0
		events := make([]string, 0, len(o.Hooks))
		for event := range o.Hooks {
			events = append(events, event)
		}
		sort.Strings(events)
		for _, event := range events {
			matchers := o.Hooks[event]
			if len(matchers) == 0 {
				continue
			}
			entries := make([]map[string]any, 0, len(matchers))
			for _, matcher := range matchers {
				ids := make([]any, 0, len(matcher.Hooks))
				for _, callback := range matcher.Hooks {
					id := "hook_" + itoa(nextID)
					nextID++
					callbacks[id] = callback
					ids = append(ids, id)
				}
				entry := map[string]any{"hookCallbackIds": ids}
				if matcher.Matcher != "" {
					entry["matcher"] = matcher.Matcher
				}
				if matcher.Timeout > 0 {
					entry["timeout"] = matcher.Timeout
				}
				entries = append(entries, entry)
			}
			hooksPayload[event] = entries
		}
		request["hooks"] = hooksPayload
	}
	if o.SystemPromptAppend != "" {
		request["appendSystemPrompt"] = o.SystemPromptAppend
	}
	if o.SystemPromptPreset == "" {
		// Mirrors the SDK: an unset systemPrompt initializes to [""], while a
		// preset omits the field so the CLI keeps its own system prompt.
		request["systemPrompt"] = []any{""}
	}
	if o.PlanModeInstructions != "" {
		request["planModeInstructions"] = o.PlanModeInstructions
	}
	return request
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
