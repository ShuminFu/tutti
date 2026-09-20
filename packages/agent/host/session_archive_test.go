package agenthost_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func TestArchivePreservesLiveSessionGraphAndDiscovery(t *testing.T) {
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "archive.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	store := storesqlite.New(db, storesqlite.Options{})
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	report := storesqlite.SessionStateReport{
		WorkspaceID: "ws", AgentSessionID: "root", Kind: storesqlite.SessionKindRoot,
		Provider: "codex", ProviderSessionID: "provider-root", Cwd: "/isolated/root", Title: "Root",
		OccurredAtUnixMS: 10, CreatedAtUnixMS: 10,
		RailPlacement:  &storesqlite.RailSection{Kind: "project", Key: "project:/original", ProjectPath: "/original"},
		RuntimeContext: map[string]any{"worktreePath": "/isolated/root", "goal": map[string]any{"objective": "Keep working", "status": "active"}},
	}
	if _, err := store.ReportSessionState(ctx, report); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RecordTurnTransition(ctx, storesqlite.TurnTransition{WorkspaceID: "ws", AgentSessionID: "root", TurnID: "turn", Phase: storesqlite.TurnPhaseRunning, OccurredAtUnixMS: 11}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertInteraction(ctx, storesqlite.InteractionUpsert{WorkspaceID: "ws", AgentSessionID: "root", TurnID: "turn", RequestID: "question", Kind: storesqlite.InteractionKindQuestion, Status: storesqlite.InteractionStatusPending, OccurredAtUnixMS: 12}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReportSessionMessages(ctx, storesqlite.SessionMessageReport{WorkspaceID: "ws", AgentSessionID: "root", Messages: []storesqlite.MessageUpdate{{MessageID: "message", TurnID: "turn", Role: "assistant", Kind: "text", Status: "running", Payload: map[string]any{"text": "Keep this transcript"}, OccurredAtUnixMS: 13}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReportSessionState(ctx, storesqlite.SessionStateReport{WorkspaceID: "ws", AgentSessionID: "child", Kind: storesqlite.SessionKindChild, RootAgentSessionID: "root", RootTurnID: "turn", ParentAgentSessionID: "root", ParentTurnID: "turn", ParentToolCallID: "tool", Provider: "codex", Cwd: "/isolated/child", OccurredAtUnixMS: 14}); err != nil {
		t.Fatal(err)
	}
	before, _, err := store.UpdateSessionPinned(ctx, "ws", "root", true)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &deletedSessionLifecycleRuntime{sessions: map[string]agenthost.ProviderRuntimeSession{deletedSessionLifecycleRuntimeKey("ws", "root"): {ID: "root", WorkspaceID: "ws"}}}
	observer := &workspaceStoreObserver{}
	adapter := &agenthost.SQLiteWorkspaceStore{StoreForWorkspace: func(string) *storesqlite.Store { return store }, Observer: observer}
	host := agenthost.New(agenthost.Config{CanonicalStore: adapter, SessionManagement: adapter, SessionBatchManagement: adapter, Runtime: runtime, Clock: goalFenceTestClock{at: time.UnixMilli(1000)}})
	archive := func(workspace, id string, archived bool) (agenthost.GetSessionResult, error) {
		return host.UpdateArchive(ctx, agenthost.UpdateArchiveInput{WorkspaceID: workspace, AgentSessionID: id, Archived: archived})
	}
	for _, ref := range [][2]string{{"other", "root"}, {"ws", "child"}, {"ws", "missing"}} {
		if _, err := archive(ref[0], ref[1], true); !errors.Is(err, agenthost.ErrSessionNotFound) {
			t.Fatalf("mutation %v: %v", ref, err)
		}
	}
	archived, err := archive("ws", "root", true)
	if err != nil || archived.Canonical.Metadata.ArchivedAtUnixMS != 1000 || !archived.Live {
		t.Fatalf("archive: %#v %v", archived, err)
	}
	if !reflect.DeepEqual(archived.Canonical.Metadata.Goal, before.Metadata.Goal) {
		t.Fatal("archive changed goal")
	}
	retry, err := archive("ws", "root", true)
	if err != nil || retry.Canonical.UpdatedAtUnixMS != archived.Canonical.UpdatedAtUnixMS || retry.Canonical.Metadata.ArchivedAtUnixMS != 1000 {
		t.Fatalf("retry advanced archive: %#v %v", retry, err)
	}
	if len(observer.deltas) != 1 {
		t.Fatalf("expected one canonical archive commit, got %d", len(observer.deltas))
	}
	if observer.deltas[0].ProjectionDirty[0].Version != archived.Canonical.UpdatedAtUnixMS {
		t.Fatal("archive commit version differs from canonical session")
	}
	report.OccurredAtUnixMS = 20
	report.RuntimeContext["visible"] = true
	if _, err := store.ReportSessionState(ctx, report); err != nil {
		t.Fatal(err)
	}
	// Reopen the canonical store after the deadline; no archive maintenance runs.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	store = storesqlite.New(db, storesqlite.Options{})
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	host = agenthost.New(agenthost.Config{CanonicalStore: adapter, SessionManagement: adapter, SessionBatchManagement: adapter, Runtime: runtime, Clock: goalFenceTestClock{at: time.UnixMilli(1000 + 31*86400000)}})
	current, err := host.GetSession(ctx, agenthost.SessionRef{WorkspaceID: "ws", AgentSessionID: "root"})
	if err != nil || current.Canonical.Metadata.ArchivedAtUnixMS != 1000 || current.Canonical.PinnedAtUnixMS != before.PinnedAtUnixMS || current.Canonical.RailSectionKey != before.RailSectionKey || current.Canonical.Cwd != before.Cwd || !current.Live {
		t.Fatalf("archive preservation: %#v %v", current, err)
	}
	turn, found, err := store.GetTurn(ctx, "ws", "root", "turn")
	if err != nil || !found || turn.Phase != storesqlite.TurnPhaseRunning {
		t.Fatalf("turn changed: %#v %v", turn, err)
	}
	children, err := store.ListChildSessions(ctx, "ws", "root")
	if err != nil || len(children) != 1 || children[0].Cwd != "/isolated/child" {
		t.Fatalf("children: %#v %v", children, err)
	}
	canonical, _, err := store.ListSessions(ctx, "ws")
	if err != nil || len(canonical) != 1 {
		t.Fatalf("canonical inventory: %#v %v", canonical, err)
	}
	page, _, err := store.ListSessionMessages(ctx, storesqlite.ListSessionMessagesInput{WorkspaceID: "ws", AgentSessionID: "root", Limit: 10})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Payload["text"] != "Keep this transcript" {
		t.Fatalf("transcript: %#v %v", page, err)
	}
	var interactionStatus string
	if err := db.QueryRowContext(ctx, "SELECT status FROM workspace_agent_interactions WHERE request_id = 'question'").Scan(&interactionStatus); err != nil || interactionStatus != "pending" {
		t.Fatalf("interaction: %s %v", interactionStatus, err)
	}
	for _, section := range []string{"pinned", before.RailSectionKey} {
		page, ok, err := store.ListSessionSection(ctx, storesqlite.ListSessionSectionInput{WorkspaceID: "ws", SectionKey: section, Limit: 1})
		if err != nil || !ok || len(page.Sessions) != 0 || page.TotalCount != 0 {
			t.Fatalf("active section %s: %#v %v", section, page, err)
		}
	}
	sections, _, err := store.ListSessionSections(ctx, storesqlite.ListSessionSectionsInput{WorkspaceID: "ws", SectionKeys: []string{"pinned", before.RailSectionKey}, LimitPerSection: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range sections.Sections {
		if page.TotalCount != 0 || len(page.Sessions) != 0 {
			t.Fatalf("batch discovery: %#v", page)
		}
	}
	candidates, _, err := store.ListSessionSectionDeletionCandidates(ctx, storesqlite.ListSessionSectionDeletionCandidatesInput{WorkspaceID: "ws", SectionKey: before.RailSectionKey})
	if err != nil || len(candidates.SessionIDs) != 0 {
		t.Fatalf("normal deletion selected archive: %#v %v", candidates, err)
	}
	search, _, err := store.ListSessionsPage(ctx, storesqlite.ListSessionsPageInput{WorkspaceID: "ws", Limit: 1})
	if err != nil || len(search.Sessions) != 0 {
		t.Fatalf("search: %#v %v", search, err)
	}
	bootstrap, _, err := store.ListSessionsPage(ctx, storesqlite.ListSessionsPageInput{WorkspaceID: "ws", IncludeArchived: true, Limit: 1})
	if err != nil || len(bootstrap.Sessions) != 1 || bootstrap.Sessions[0].Metadata.ArchivedAtUnixMS != 1000 {
		t.Fatalf("bootstrap: %#v %v", bootstrap, err)
	}
	archivePage, _, err := store.ListSessionSection(ctx, storesqlite.ListSessionSectionInput{WorkspaceID: "ws", SectionKey: "archive", Limit: 1})
	if err != nil || archivePage.TotalCount != 1 || len(archivePage.Sessions) != 1 {
		t.Fatalf("archive page: %#v %v", archivePage, err)
	}
	if _, err := store.ReportSessionState(ctx, storesqlite.SessionStateReport{WorkspaceID: "ws", AgentSessionID: "second", Kind: storesqlite.SessionKindRoot, Provider: "codex", AgentTargetID: "target", Cwd: "/second", OccurredAtUnixMS: 30}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpdateSessionArchived(ctx, "ws", "second", true, 1001); err != nil {
		t.Fatal(err)
	}
	firstArchivePage, _, err := store.ListSessionSection(ctx, storesqlite.ListSessionSectionInput{WorkspaceID: "ws", SectionKey: "archive", Limit: 1})
	if err != nil || !firstArchivePage.HasMore || firstArchivePage.TotalCount != 2 || firstArchivePage.Sessions[0].ID != "second" || firstArchivePage.NextCursor != "1001|second" {
		t.Fatalf("bounded archive first page: %#v %v", firstArchivePage, err)
	}
	nextArchivePage, _, err := store.ListSessionSection(ctx, storesqlite.ListSessionSectionInput{WorkspaceID: "ws", SectionKey: "archive", Limit: 1, CursorSortTimeUnixMS: 1001, CursorSessionID: "second"})
	if err != nil || nextArchivePage.HasMore || len(nextArchivePage.Sessions) != 1 || nextArchivePage.Sessions[0].ID != "root" {
		t.Fatalf("bounded archive next page: %#v %v", nextArchivePage, err)
	}
	targetArchivePage, _, err := store.ListSessionSection(ctx, storesqlite.ListSessionSectionInput{WorkspaceID: "ws", SectionKey: "archive", AgentTargetID: "target", Limit: 1})
	if err != nil || targetArchivePage.TotalCount != 1 || len(targetArchivePage.Sessions) != 1 {
		t.Fatalf("target archive index: %#v %v", targetArchivePage, err)
	}
	restored, err := archive("ws", "root", false)
	if err != nil || restored.Canonical.Metadata.ArchivedAtUnixMS != 0 || restored.Canonical.RailSectionKey != before.RailSectionKey || restored.Canonical.PinnedAtUnixMS != before.PinnedAtUnixMS {
		t.Fatalf("restore: %#v %v", restored, err)
	}
	if !reflect.DeepEqual(restored.Canonical.Metadata.Goal, before.Metadata.Goal) {
		t.Fatal("goal changed")
	}
	rearchived, err := archive("ws", "root", true)
	if err != nil || rearchived.Canonical.Metadata.ArchivedAtUnixMS != 1000+31*86400000 {
		t.Fatalf("rearchive: %#v %v", rearchived, err)
	}
	deleted, err := host.DeleteSession(ctx, agenthost.SessionRef{WorkspaceID: "ws", AgentSessionID: "root"})
	if err != nil || !deleted.CanonicalRemoved || len(runtime.sessions) != 0 {
		t.Fatalf("explicit archive deletion: %#v %v", deleted, err)
	}
	if _, err := archive("ws", "root", false); !errors.Is(err, agenthost.ErrSessionNotFound) {
		t.Fatalf("deleted restore accepted: %v", err)
	}
}
