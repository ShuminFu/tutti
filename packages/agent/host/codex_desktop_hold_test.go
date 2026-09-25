package agenthost_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	"golang.org/x/sys/unix"
)

func TestCodexDesktopHoldQueuesInOrderAndDeliversAfterRelease(t *testing.T) {
	_, store, runtime := newHostEditRetryFixture(t)
	codexHome := t.TempDir()
	runtime.userCodexHome = true
	runtime.codexHome = codexHome
	runtime.codexHeld = true
	dir := t.TempDir()
	newHost := func() *agenthost.Host {
		return agenthost.New(agenthost.Config{
			CanonicalStore:    sqliteCanonicalStore{Store: store},
			TurnSubmissions:   store,
			EffectiveHistory:  store,
			RuntimeOperations: store,
			Runtime:           runtime,
			HistoryRuntime:    runtime,
			GoalRuntime:       runtime,
			OperationOwner:    "worker-1",
			CodexHeldDir:      dir,
		})
	}
	host := newHost()
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}
	sends := []struct {
		id   string
		kind string
		text string
	}{
		{id: "submit-send", kind: "send", text: "first"},
		{id: "submit-handoff", kind: "handoff", text: "second"},
		{id: "submit-review", kind: "review", text: "third"},
	}
	for _, send := range sends {
		metadata := map[string]any{}
		if send.kind != "send" {
			metadata["codexOutboundKind"] = send.kind
		}
		result, err := host.SendInput(t.Context(), ref, agenthost.SendInput{
			Content:        []agenthost.PromptContentBlock{{Type: "text", Text: send.text}},
			ClientSubmitID: send.id,
			TurnID:         "turn-" + send.id,
			Metadata:       metadata,
		})
		if err != nil {
			t.Fatalf("SendInput(%s) error = %v", send.id, err)
		}
		if result.Kind != agenthost.SubmitKindCodexDesktopHeld {
			t.Fatalf("SendInput(%s) kind = %q", send.id, result.Kind)
		}
	}
	if runtime.execCalls != 3 {
		t.Fatalf("exec calls while held = %d, want 3 resume attempts", runtime.execCalls)
	}

	reloaded := newHost()
	state := reloaded.CodexDesktopHold("workspace-1", "session-1")
	if !state.Held || state.ReasonCode != agenthost.CodexThreadHeldExternallyReason || state.QueuedCount != 3 {
		t.Fatalf("reloaded hold = %+v", state)
	}
	if got, want := joinKinds(state.Kinds), "send,handoff,review"; got != want {
		t.Fatalf("queue order = %s, want %s", got, want)
	}
	if state.OpenURL != "codex://threads/thread-1" {
		t.Fatalf("open url = %q", state.OpenURL)
	}

	lock := holdWriterLock(t, codexHome, "thread-1")
	before := runtime.execCalls
	reloaded.DeliverReadyCodexDesktopHolds(t.Context())
	if runtime.execCalls != before {
		t.Fatalf("exec calls while the lock file is held = %d, want %d", runtime.execCalls, before)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	_ = lock.Close()
	if err := os.Remove(filepath.Join(codexHome, "thread-writer-locks", "thread-1.lock")); err != nil {
		t.Fatal(err)
	}

	runtime.codexHeld = false
	for _, turnID := range []string{"turn-submit-send", "turn-submit-handoff", "turn-submit-review"} {
		if err := reloaded.RetryCodexDesktopHold(t.Context(), ref); err != nil {
			t.Fatalf("retry %s: %v", turnID, err)
		}
		settleHostTurn(t, store, turnID)
	}
	state = reloaded.CodexDesktopHold("workspace-1", "session-1")
	if state.QueuedCount != 0 || state.Held {
		t.Fatalf("hold after release = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(dir, "workspace-1", "session-1.json")); !os.IsNotExist(err) {
		t.Fatalf("queue file still present: %v", err)
	}
}

func TestIsolatedCodexHoldStaysASendFailure(t *testing.T) {
	_, store, runtime := newHostEditRetryFixture(t)
	runtime.codexHeld = true
	host := agenthost.New(agenthost.Config{
		CanonicalStore:    sqliteCanonicalStore{Store: store},
		TurnSubmissions:   store,
		EffectiveHistory:  store,
		RuntimeOperations: store,
		Runtime:           runtime,
		HistoryRuntime:    runtime,
		GoalRuntime:       runtime,
		OperationOwner:    "worker-1",
		CodexHeldDir:      t.TempDir(),
	})
	_, err := host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "isolated"}},
		ClientSubmitID: "submit-isolated",
		TurnID:         "turn-isolated",
	})
	if err == nil {
		t.Fatal("isolated send succeeded, want the writer error")
	}
	state := host.CodexDesktopHold("workspace-1", "session-1")
	if state.QueuedCount != 0 {
		t.Fatalf("isolated queue = %+v", state)
	}
}

func TestCodexWriterLockProbe(t *testing.T) {
	home := t.TempDir()
	held, known := agenthost.CodexWriterLockHeld(home, "thread-1")
	if held || !known {
		t.Fatalf("missing lock = held %v known %v, want free", held, known)
	}
	lock := holdWriterLock(t, home, "thread-1")
	defer lock.Close()
	held, known = agenthost.CodexWriterLockHeld(home, "thread-1")
	if !held || !known {
		t.Fatalf("locked file = held %v known %v, want held", held, known)
	}
}

func settleHostTurn(t *testing.T, store *storesqlite.Store, turnID string) {
	t.Helper()
	now := time.Now().UnixMilli()
	if _, err := store.ReportActivityState(t.Context(), storesqlite.ActivityStateReport{
		Session: storesqlite.SessionStateReport{
			WorkspaceID: "workspace-1", AgentSessionID: "session-1",
			Kind: storesqlite.SessionKindRoot, Provider: "codex",
			ProviderSessionID: "thread-1", OccurredAtUnixMS: now,
		},
		Turn: &storesqlite.TurnTransition{
			WorkspaceID: "workspace-1", AgentSessionID: "session-1",
			TurnID: turnID, Phase: storesqlite.TurnPhaseSettled,
			Outcome: storesqlite.TurnOutcomeCompleted, Origin: storesqlite.TurnOriginUserPrompt,
			SettledAtUnixMS: now, OccurredAtUnixMS: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func holdWriterLock(t *testing.T, codexHome, threadID string) *os.File {
	t.Helper()
	dir := filepath.Join(codexHome, "thread-writer-locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(dir, threadID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	return file
}

func joinKinds(kinds []string) string {
	out := ""
	for index, kind := range kinds {
		if index > 0 {
			out += ","
		}
		out += kind
	}
	return out
}
