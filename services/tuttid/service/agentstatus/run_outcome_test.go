package agentstatus

import (
	"testing"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

func TestRunOutcomeStoreIsProviderScoped(t *testing.T) {
	store := NewRunOutcomeStore()
	store.RecordAuthFailure(agentprovider.ClaudeCode)
	if !store.AuthInvalidated(agentprovider.ClaudeCode) {
		t.Fatal("claude-code should be invalidated")
	}
	if store.AuthInvalidated(agentprovider.Codex) {
		t.Fatal("codex must not be affected by a claude-code failure")
	}
	store.RecordSuccess(agentprovider.ClaudeCode)
	if store.AuthInvalidated(agentprovider.ClaudeCode) {
		t.Fatal("a success should clear the invalidation")
	}
}

func TestRunOutcomeStoreNilSafe(t *testing.T) {
	var store *RunOutcomeStore
	store.RecordAuthFailure(agentprovider.Codex) // must not panic
	if store.AuthInvalidated(agentprovider.Codex) {
		t.Fatal("nil store reports nothing invalidated")
	}
}

// A re-login rewrites the credential file after the failure was recorded; the
// probe must self-heal (clear the stale flag and detect normally) instead of
// sticking on "needs login" until the next successful run.
// A failure with no newer credential file (token genuinely still broken) must
// keep reporting "needs login".
