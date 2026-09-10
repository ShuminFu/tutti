package agentstatus

import "testing"

func TestTerminalOperationStoreRejectsStaleCompletion(t *testing.T) {
	store := NewTerminalOperationStore()
	store.Begin("claude-code", 1)
	store.Begin("claude-code", 2)

	if store.Complete("claude-code", 1, RunActionResult{Status: RunActionFailed, Message: "stale"}) {
		t.Fatal("stale completion succeeded")
	}
	want := RunActionResult{Status: RunActionCompleted, Message: "latest"}
	if !store.Complete("claude-code", 2, want) {
		t.Fatal("current completion was rejected")
	}
	got := store.Latest("claude-code")
	if got == nil || got.Status != want.Status || got.Message != want.Message {
		t.Fatalf("Latest() = %#v, want %#v", got, want)
	}
}

func TestTerminalOperationStoreIsProviderScopedAndReturnsCopies(t *testing.T) {
	store := NewTerminalOperationStore()
	store.Begin("claude-code", 1)
	store.Begin("codex", 2)
	store.Complete("claude-code", 1, RunActionResult{Message: "claude"})
	store.Complete("codex", 2, RunActionResult{Message: "codex"})

	claude := store.Latest("claude-code")
	claude.Message = "mutated"
	if got := store.Latest("claude-code"); got == nil || got.Message != "claude" {
		t.Fatalf("claude Latest() = %#v, want independent copy", got)
	}
	if got := store.Latest("codex"); got == nil || got.Message != "codex" {
		t.Fatalf("codex Latest() = %#v", got)
	}
	if got := store.Latest("opencode"); got != nil {
		t.Fatalf("missing provider Latest() = %#v, want nil", got)
	}
}
