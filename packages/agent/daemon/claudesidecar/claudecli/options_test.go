package claudecli

import (
	"reflect"
	"testing"
)

func TestCLIArgsMirrorSDKTransportConstruction(t *testing.T) {
	settingsJSON := `{"fastMode":true}`
	pluginDir := "/tmp/tutti-plugin"
	model := "MiniMax-M2.7"
	options := &Options{
		Model: "opus",
		CanUseTool: func(string, map[string]any, ToolPermissionRequest) (PermissionResult, bool, error) {
			return PermissionResult{}, false, nil
		},
		Resume:                          "session-1",
		AllowedTools:                    []string{"Grep", "Glob"},
		DisallowedTools:                 []string{"Monitor"},
		Tools:                           ToolsPreset{Preset: "claude_code"},
		PermissionMode:                  "acceptEdits",
		AllowDangerouslySkipPermissions: true,
		IncludePartialMessages:          true,
		Plugins:                         []PluginConfig{{Type: "local", Path: pluginDir}},
		Settings:                        map[string]any{"fastMode": true},
		ExtraArgs: map[string]*string{
			"model":   &model,
			"verbose": nil,
		},
	}

	args := options.cliArgs()
	expected := []string{
		"--output-format", "stream-json", "--verbose", "--input-format", "stream-json",
		"--model", "opus",
		"--permission-prompt-tool", "stdio",
		"--resume", "session-1",
		"--allowedTools", "Grep,Glob",
		"--disallowedTools", "Monitor",
		"--tools", "default",
		"--permission-mode", "acceptEdits",
		"--allow-dangerously-skip-permissions",
		"--include-partial-messages",
		"--plugin-dir", pluginDir,
		"--model", model,
		"--settings", settingsJSON,
		"--verbose",
	}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("args = %#v\nwant %#v", args, expected)
	}
}

func TestCLIArgsUseSessionIDWhenNotResuming(t *testing.T) {
	options := &Options{SessionID: "session-new"}
	args := options.cliArgs()
	found := false
	for index, arg := range args {
		if arg == "--session-id" && index+1 < len(args) && args[index+1] == "session-new" {
			found = true
		}
		if arg == "--resume" {
			t.Fatalf("unexpected --resume in %#v", args)
		}
	}
	if !found {
		t.Fatalf("missing --session-id in %#v", args)
	}
}

func TestResolveSpawnCommandUsesNodeForJSEntry(t *testing.T) {
	command, args := resolveSpawnCommand(&Options{PathToClaudeCodeExecutable: "/opt/sdk/cli.js"})
	if command != "node" || len(args) == 0 || args[0] != "/opt/sdk/cli.js" {
		t.Fatalf("command=%q args=%#v", command, args)
	}
	command, args = resolveSpawnCommand(&Options{PathToClaudeCodeExecutable: "/usr/local/bin/claude"})
	if command != "/usr/local/bin/claude" || len(args) == 0 || args[0] != "--output-format" {
		t.Fatalf("command=%q args=%#v", command, args)
	}
	command, _ = resolveSpawnCommand(&Options{})
	if command != "claude" {
		t.Fatalf("command=%q", command)
	}
}

func TestInitializeRequestRegistersHookCallbacks(t *testing.T) {
	called := map[string]bool{}
	hook := func(name string) HookCallback {
		return func(map[string]any, string) map[string]any {
			called[name] = true
			return map[string]any{"continue": true}
		}
	}
	options := &Options{
		SystemPromptPreset: "claude_code",
		SystemPromptAppend: "Extra guidance.",
		Hooks: map[string][]HookMatcher{
			"PostToolUse": {{Hooks: []HookCallback{hook("post")}}},
			"TaskCreated": {{Hooks: []HookCallback{hook("created")}}},
		},
	}
	callbacks := map[string]HookCallback{}
	request := options.initializeRequest(callbacks)

	if request["subtype"] != "initialize" {
		t.Fatalf("request = %#v", request)
	}
	if request["appendSystemPrompt"] != "Extra guidance." {
		t.Fatalf("appendSystemPrompt = %#v", request["appendSystemPrompt"])
	}
	if _, present := request["systemPrompt"]; present {
		t.Fatalf("preset system prompt must omit systemPrompt: %#v", request)
	}
	hooks, _ := request["hooks"].(map[string]any)
	if len(hooks) != 2 {
		t.Fatalf("hooks = %#v", hooks)
	}
	if len(callbacks) != 2 {
		t.Fatalf("callbacks = %#v", callbacks)
	}
	for id, callback := range callbacks {
		if callback(nil, "")["continue"] != true {
			t.Fatalf("callback %s result unexpected", id)
		}
	}
	if !called["post"] || !called["created"] {
		t.Fatalf("called = %#v", called)
	}
}

func TestInitializeRequestWithoutPresetSendsEmptySystemPrompt(t *testing.T) {
	options := &Options{}
	request := options.initializeRequest(map[string]HookCallback{})
	systemPrompt, ok := request["systemPrompt"].([]any)
	if !ok || len(systemPrompt) != 1 || systemPrompt[0] != "" {
		t.Fatalf("systemPrompt = %#v", request["systemPrompt"])
	}
}
