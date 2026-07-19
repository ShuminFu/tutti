package claudesidecar

import (
	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

// SidecarClaudeOptions mirrors options.ts: the Claude query option overrides
// the daemon passes with the start request.
type SidecarClaudeOptions struct {
	SystemPromptAppend   string
	PlanModeInstructions string
	AllowedTools         []string
	DisallowedTools      []string
	Plugins              []claudecli.PluginConfig
	ExtraArgs            map[string]*string
	Tools                claudecli.ToolsOption
}

func sidecarClaudeOptionsFromPayload(payload map[string]any) SidecarClaudeOptions {
	tools := toolsValue(payload["tools"])
	if tools == nil {
		tools = claudecli.ToolsPreset{Preset: "claude_code"}
	}
	return SidecarClaudeOptions{
		SystemPromptAppend:   stringValue(payload["systemPromptAppend"]),
		PlanModeInstructions: stringValue(payload["planModeInstructions"]),
		AllowedTools:         stringArrayValue(payload["allowedTools"]),
		DisallowedTools:      stringArrayValue(payload["disallowedTools"]),
		Plugins:              pluginListValue(payload["plugins"]),
		ExtraArgs:            stringRecordValue(payload["extraArgs"]),
		Tools:                tools,
	}
}

// applyClaudeQueryOptionOverrides mirrors claudeQueryOptionOverrides: it
// stamps the sidecar's option overrides onto the query options.
func applyClaudeQueryOptionOverrides(options *claudecli.Options, overrides SidecarClaudeOptions) {
	options.SystemPromptPreset = "claude_code"
	options.SystemPromptAppend = overrides.SystemPromptAppend
	options.Tools = overrides.Tools
	if len(overrides.AllowedTools) > 0 {
		options.AllowedTools = overrides.AllowedTools
	}
	if overrides.PlanModeInstructions != "" {
		options.PlanModeInstructions = overrides.PlanModeInstructions
	}
	if len(overrides.DisallowedTools) > 0 {
		options.DisallowedTools = overrides.DisallowedTools
	}
	if len(overrides.Plugins) > 0 {
		options.Plugins = overrides.Plugins
	}
	if len(overrides.ExtraArgs) > 0 {
		options.ExtraArgs = overrides.ExtraArgs
	}
}

func claudeOptionOverrideKeys(overrides SidecarClaudeOptions) []string {
	keys := []string{"systemPrompt", "tools"}
	if len(overrides.AllowedTools) > 0 {
		keys = append(keys, "allowedTools")
	}
	if overrides.PlanModeInstructions != "" {
		keys = append(keys, "planModeInstructions")
	}
	if len(overrides.DisallowedTools) > 0 {
		keys = append(keys, "disallowedTools")
	}
	if len(overrides.Plugins) > 0 {
		keys = append(keys, "plugins")
	}
	if len(overrides.ExtraArgs) > 0 {
		keys = append(keys, "extraArgs")
	}
	return keys
}

func stringRecordValue(value any) map[string]*string {
	record := recordValue(value)
	if record == nil {
		return map[string]*string{}
	}
	out := map[string]*string{}
	for key, item := range record {
		name := normalizeTitle(key)
		if name == "" {
			continue
		}
		if item == nil {
			out[name] = nil
			continue
		}
		if text := stringValue(item); text != "" {
			value := text
			out[name] = &value
		}
	}
	return out
}

func pluginListValue(value any) []claudecli.PluginConfig {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var plugins []claudecli.PluginConfig
	for _, item := range items {
		record := recordValue(item)
		if record == nil || record["type"] != "local" {
			continue
		}
		path := stringValue(record["path"])
		if path == "" {
			continue
		}
		plugins = append(plugins, claudecli.PluginConfig{
			Type:             "local",
			Path:             path,
			SkipMcpDiscovery: record["skipMcpDiscovery"] == true,
		})
	}
	return plugins
}

func toolsValue(value any) claudecli.ToolsOption {
	if items, ok := value.([]any); ok {
		list := make(claudecli.ToolsList, 0, len(items))
		for _, item := range items {
			if text := stringValue(item); text != "" {
				list = append(list, text)
			}
		}
		return list
	}
	record := recordValue(value)
	if record != nil && record["type"] == "preset" && record["preset"] == "claude_code" {
		return claudecli.ToolsPreset{Preset: "claude_code"}
	}
	return nil
}
