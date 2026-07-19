package claudesidecar

import (
	"reflect"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

func TestSidecarClaudeOptionsFromPayloadMapsProviderMeta(t *testing.T) {
	pluginDir := "/tmp/tutti-plugin"
	model := "MiniMax-M2.7"
	options := sidecarClaudeOptionsFromPayload(map[string]any{
		"systemPromptAppend":   "Use Tutti CLI for issue context.",
		"planModeInstructions": "Inspect files, then produce a plan.",
		"allowedTools":         []any{"Grep", "Glob"},
		"disallowedTools":      []any{"Monitor"},
		"plugins": []any{
			map[string]any{"type": "local", "path": pluginDir},
			map[string]any{"type": "remote", "path": "/tmp/ignored"},
		},
		"extraArgs": map[string]any{
			"plugin-dir": pluginDir,
			"model":      model,
			"verbose":    nil,
		},
		"tools": map[string]any{"type": "preset", "preset": "claude_code"},
	})

	queryOptions := &claudecli.Options{}
	applyClaudeQueryOptionOverrides(queryOptions, options)

	if queryOptions.SystemPromptPreset != "claude_code" ||
		queryOptions.SystemPromptAppend != "Use Tutti CLI for issue context." {
		t.Fatalf("system prompt = %q append %q", queryOptions.SystemPromptPreset, queryOptions.SystemPromptAppend)
	}
	if preset, ok := queryOptions.Tools.(claudecli.ToolsPreset); !ok || preset.Preset != "claude_code" {
		t.Fatalf("tools = %#v", queryOptions.Tools)
	}
	if queryOptions.PlanModeInstructions != "Inspect files, then produce a plan." {
		t.Fatalf("planModeInstructions = %q", queryOptions.PlanModeInstructions)
	}
	if !reflect.DeepEqual(queryOptions.AllowedTools, []string{"Grep", "Glob"}) {
		t.Fatalf("allowedTools = %#v", queryOptions.AllowedTools)
	}
	if !reflect.DeepEqual(queryOptions.DisallowedTools, []string{"Monitor"}) {
		t.Fatalf("disallowedTools = %#v", queryOptions.DisallowedTools)
	}
	if len(queryOptions.Plugins) != 1 || queryOptions.Plugins[0].Path != pluginDir {
		t.Fatalf("plugins = %#v", queryOptions.Plugins)
	}
	if len(queryOptions.ExtraArgs) != 3 {
		t.Fatalf("extraArgs = %#v", queryOptions.ExtraArgs)
	}
	if value := queryOptions.ExtraArgs["plugin-dir"]; value == nil || *value != pluginDir {
		t.Fatalf("extraArgs plugin-dir = %#v", value)
	}
	if value := queryOptions.ExtraArgs["model"]; value == nil || *value != model {
		t.Fatalf("extraArgs model = %#v", value)
	}
	if value, present := queryOptions.ExtraArgs["verbose"]; !present || value != nil {
		t.Fatalf("extraArgs verbose = %#v present=%v", value, present)
	}
}

func TestSidecarClaudeOptionsFromPayloadDefaultsToClaudeCodePreset(t *testing.T) {
	options := sidecarClaudeOptionsFromPayload(map[string]any{})
	queryOptions := &claudecli.Options{}
	applyClaudeQueryOptionOverrides(queryOptions, options)

	if queryOptions.SystemPromptPreset != "claude_code" || queryOptions.SystemPromptAppend != "" {
		t.Fatalf("system prompt = %q append %q", queryOptions.SystemPromptPreset, queryOptions.SystemPromptAppend)
	}
	if preset, ok := queryOptions.Tools.(claudecli.ToolsPreset); !ok || preset.Preset != "claude_code" {
		t.Fatalf("tools = %#v", queryOptions.Tools)
	}
	if queryOptions.PlanModeInstructions != "" || queryOptions.AllowedTools != nil ||
		queryOptions.DisallowedTools != nil || queryOptions.Plugins != nil || queryOptions.ExtraArgs != nil {
		t.Fatalf("unexpected overrides: %#v", queryOptions)
	}
}
