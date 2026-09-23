package agentruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

// claudeCodeTestDescriptor returns the descriptor the daemon actually ships
// with, so the assertions cover the real permission-tier declaration instead of
// a hand-built copy that could drift from it.
func claudeCodeTestDescriptor(t *testing.T) providerregistry.ProviderDescriptor {
	t.Helper()
	for _, descriptor := range providerregistry.Migrated() {
		if descriptor.Identity.ID == providerregistry.ClaudeCodeProviderID {
			return descriptor
		}
	}
	t.Fatalf("provider %q is not registered", providerregistry.ClaudeCodeProviderID)
	return providerregistry.ProviderDescriptor{}
}

// TestClaudeCodeACPAutomaticPermissionDecision pins the tier behaviour of the
// Claude Code ACP target. Before this wiring the target had no tier at all:
// every session/request_permission became a durable user approval, so a user
// who picked 完全放行 was still asked for each Bash call — most visibly once the
// model put itself into plan mode, which outranks bypassPermissions inside
// Claude Code.
func TestClaudeCodeACPAutomaticPermissionDecision(t *testing.T) {
	t.Parallel()

	decisionFor := func(t *testing.T, permissionModeID string, planMode bool) string {
		t.Helper()
		adapter := newClaudeCodeACPAdapterFromProviderDescriptor(
			claudeCodeTestDescriptor(t),
			newStandardACPTransport("Claude Agent", "claude-automatic-permission"),
			LegacyHostMetadata(),
			nil,
		)
		session := standardTestSession(providerregistry.ClaudeCodeProviderID)
		session.PermissionModeID = permissionModeID
		session.ProviderSessionID = ""
		session.Settings = &SessionSettings{PlanMode: planMode}
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatalf("Start: %v", err)
		}
		// The controller applies the session's tier after Start; that is what
		// installs the live permission mode the decision is read from.
		if err := adapter.ApplyPermissionMode(context.Background(), session); err != nil {
			t.Fatalf("ApplyPermissionMode: %v", err)
		}
		return adapter.automaticPermissionDecisionFor(session)
	}

	// The incident case itself: the user picked 完全放行 and the model is not in
	// plan mode, so a plain tool ask is answered without them.
	if got := decisionFor(t, "bypassPermissions", false); got != "approved" {
		t.Fatalf("full-access decision = %q, want approved", got)
	}
	// Every other tier keeps today's behaviour: the user is asked.
	for _, mode := range []string{"default", "acceptEdits"} {
		if got := decisionFor(t, mode, false); got != "" {
			t.Fatalf("mode %q decision = %q, want prompt", mode, got)
		}
	}
	// A plan mode the host knows about is denied before the tier is consulted.
	// This covers the user-toggled case only: when the model puts itself into
	// plan mode, nothing reaches the host (`projectCurrentMode` is off for this
	// target), so those asks take the full-access branch above. What keeps that
	// from swallowing the plan review is the plan-exit rule in
	// standard_acp_stream.go, pinned separately below.
	if got := decisionFor(t, "bypassPermissions", true); got != "denied" {
		t.Fatalf("plan-mode decision = %q, want denied", got)
	}
}

// TestClaudeCodeACPPlanExitStaysInteractive pins the one request the tier must
// not answer: leaving plan mode. Claude Code's only allow-once exit option is
// "Yes, and manually approve edits", which the bridge maps to `setMode:
// default` — auto-approving it would skip the plan review AND silently move the
// CLI out of the 完全放行 tier the user picked.
func TestClaudeCodeACPPlanExitStaysInteractive(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Claude Agent", "claude-plan-exit")
	transport.conn.promptKind = "exit-plan"
	// The options claude-agent-acp offers on ExitPlanMode: the only allow-once
	// entry is the one that also moves the CLI to `default`.
	transport.conn.permissionOptions = []map[string]any{
		{"optionId": "exit-plan-default", "name": "Yes, and manually approve edits", "kind": "allow_once"},
		{"optionId": "exit-plan-bypass", "name": "Yes, and bypass permissions", "kind": "allow_always"},
		{"optionId": "exit-plan-keep", "name": "No, keep planning", "kind": "reject_once"},
	}
	transport.conn.permissionToolCall = map[string]any{
		// The bridge sends the tool name under `name` (the title is a
		// human-facing label and may be renamed), which is the branch
		// normalizedInteractiveToolName checks first.
		"toolCallId": "interactive-plan-1",
		"name":       "ExitPlanMode",
		"title":      "Approve Plan",
		"input":      map[string]any{"plan": "Implement the shared renderer"},
	}
	adapter := newClaudeCodeACPAdapterFromProviderDescriptor(
		claudeCodeTestDescriptor(t),
		transport,
		LegacyHostMetadata(),
		nil,
	)
	session := standardTestSession(providerregistry.ClaudeCodeProviderID)
	session.PermissionModeID = "bypassPermissions"
	session.ProviderSessionID = ""
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := adapter.ApplyPermissionMode(context.Background(), session); err != nil {
		t.Fatalf("ApplyPermissionMode: %v", err)
	}
	session.ProviderSessionID = "claude-plan-exit"

	execDone := make(chan error, 1)
	go func() {
		_, err := adapter.Exec(context.Background(), session, textPrompt("exit plan"), "", "turn-1", nil, nil)
		execDone <- err
	}()
	select {
	case err := <-execDone:
		t.Fatalf("Exec returned (err=%v); a plan-exit request must stay with the user", err)
	case <-time.After(500 * time.Millisecond):
	}
	if got := transport.conn.permissionOptionID(); got != "" {
		t.Fatalf("auto-selected option %q for a plan-exit request", got)
	}
}

func TestClaudeCodeACPRemapsRetiredDontAskAndRejectsUnknownModes(t *testing.T) {
	t.Parallel()

	adapter := newClaudeCodeACPAdapterFromProviderDescriptor(
		claudeCodeTestDescriptor(t),
		newStandardACPTransport("Claude Agent", "claude-permission-preflight"),
		LegacyHostMetadata(),
		nil,
	)
	if !adapter.config.failOnSetModeError {
		t.Fatal("claude-agent-acp must fail closed when set_mode is rejected")
	}
	if got := adapter.config.permissionModeID("dontAsk"); got != "default" {
		t.Fatalf("retired dontAsk maps to %q, want default", got)
	}
	session := standardTestSession(providerregistry.ClaudeCodeProviderID)
	session.PermissionModeID = "mysteryMode"
	if _, err := adapter.Start(context.Background(), session); !errors.Is(err, ErrPermissionModeUnavailable) {
		t.Fatalf("Start mysteryMode = %v, want ErrPermissionModeUnavailable", err)
	}
	session.PermissionModeID = "default"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.PermissionModeID = "mysteryMode"
	if err := adapter.ApplyPermissionMode(context.Background(), session); !errors.Is(err, ErrPermissionModeUnavailable) {
		t.Fatalf("ApplyPermissionMode mysteryMode = %v, want ErrPermissionModeUnavailable", err)
	}
}

func TestClaudeCodeACPAllowsDeclaredImagePromptWithoutLiveSession(t *testing.T) {
	t.Parallel()

	adapter := newClaudeCodeACPAdapterFromProviderDescriptor(
		claudeCodeTestDescriptor(t),
		newStandardACPTransport("Claude Agent", "claude-image-preflight"),
		LegacyHostMetadata(),
		nil,
	)
	session := standardTestSession(providerregistry.ClaudeCodeProviderID)
	if err := adapter.ValidatePromptContent(session, []PromptContentBlock{{
		Type:     "image",
		MimeType: "image/png",
		Path:     "/managed/agent-prompt-assets/screen.png",
	}}); err != nil {
		t.Fatalf("ValidatePromptContent without live session = %v, want nil", err)
	}
}

// TestClaudeCodeACPSessionMetaInjectsPreparedSurface pins the ACP half of the
// DinTalDock injection. claude-agent-acp builds its query options out of the
// session/new `_meta` it receives: `_meta.claudeCode.options` is spread straight
// into the SDK options, and `_meta.systemPrompt` replaces the default
// `{type:"preset", preset:"claude_code"}` while preserving every other preset
// key. So the plugin dir and the routing prompt that runtimeprep prepares only
// reach the model when the daemon puts them in that `_meta`. Before this wiring
// the ACP runtime dropped both, which is why no `tutti-cli:*` Skill and no
// `mention://` routing contract ever showed up in a session started under
// TUTTI_CLAUDE_CODE_RUNTIME=acp.
func TestClaudeCodeACPSessionMetaInjectsPreparedSurface(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pluginDir := filepath.Join(root, "claude-plugin", "tutti-cli")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	promptPath := filepath.Join(root, "claude-system-prompt.md")
	if err := os.WriteFile(promptPath, []byte("  routing table  \n"), 0o600); err != nil {
		t.Fatalf("write system prompt: %v", err)
	}

	meta := claudeACPSessionMeta(Session{Env: []string{
		claudePluginDirEnv + "=" + pluginDir,
		claudeSystemPromptFileEnv + "=" + promptPath,
	}})

	claudeCode, _ := meta["claudeCode"].(map[string]any)
	options, _ := claudeCode["options"].(map[string]any)
	plugins, _ := options["plugins"].([]map[string]any)
	if len(plugins) != 1 || plugins[0]["type"] != "local" || plugins[0]["path"] != pluginDir {
		t.Fatalf("plugins = %#v, want one local plugin at %q", options["plugins"], pluginDir)
	}

	systemPrompt, _ := meta["systemPrompt"].(map[string]any)
	if systemPrompt["type"] != "preset" || systemPrompt["preset"] != "claude_code" {
		t.Fatalf("systemPrompt = %#v, want the locked claude_code preset", systemPrompt)
	}
	if got := systemPrompt["append"]; got != "routing table" {
		t.Fatalf("systemPrompt append = %#v, want the trimmed prompt file body", got)
	}
}

// TestClaudeCodeACPSessionMetaDegradesWithoutPreparedEnv keeps the injection
// fail-open: a session without the DinTalDock surface is still usable, so a
// missing prompt file must drop only that half instead of failing session/new.
// The injected routing prompt itself tells the model to fall back to the
// materialized SKILL.md, so degrading is strictly better than refusing.
func TestClaudeCodeACPSessionMetaDegradesWithoutPreparedEnv(t *testing.T) {
	t.Parallel()

	if meta := claudeACPSessionMeta(Session{}); len(meta) != 0 {
		t.Fatalf("meta = %#v, want empty for a session with no prepared env", meta)
	}

	root := t.TempDir()
	meta := claudeACPSessionMeta(Session{Env: []string{
		claudePluginDirEnv + "=" + filepath.Join(root, "plugin"),
		claudeSystemPromptFileEnv + "=" + filepath.Join(root, "absent.md"),
	}})
	if _, ok := meta["systemPrompt"]; ok {
		t.Fatalf("meta = %#v, want no systemPrompt for a missing prompt file", meta)
	}
	if _, ok := meta["claudeCode"]; !ok {
		t.Fatalf("meta = %#v, want the plugin to survive a missing prompt file", meta)
	}
}

// TestClaudeCodeACPApplySessionMetaMergesIntoParams covers the adapter wiring:
// the claude ACP target must actually register the hook, and the hook must merge
// into an existing `_meta` (other callbacks put keys there) rather than replace
// it.
func TestClaudeCodeACPApplySessionMetaMergesIntoParams(t *testing.T) {
	t.Parallel()

	adapter := newClaudeCodeACPAdapterFromProviderDescriptor(
		claudeCodeTestDescriptor(t),
		newStandardACPTransport("Claude Agent", "claude-session-meta"),
		LegacyHostMetadata(),
		nil,
	)
	if adapter.config.applySessionMeta == nil {
		t.Fatal("claude ACP adapter must register applySessionMeta")
	}
	promptPath := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptPath, []byte("routing"), 0o600); err != nil {
		t.Fatalf("write system prompt: %v", err)
	}

	params := map[string]any{"_meta": map[string]any{"keep": "me"}}
	adapter.config.applySessionMeta(params, Session{Env: []string{
		claudeSystemPromptFileEnv + "=" + promptPath,
	}}, LegacyHostMetadata())

	meta, _ := params["_meta"].(map[string]any)
	if meta["keep"] != "me" {
		t.Fatalf("_meta = %#v, want the pre-existing key preserved", meta)
	}
	if _, ok := meta["systemPrompt"]; !ok {
		t.Fatalf("_meta = %#v, want the injected systemPrompt", meta)
	}
}
