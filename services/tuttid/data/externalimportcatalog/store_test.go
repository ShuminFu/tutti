package externalimportcatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLookupUpsertAndCompleteRoot(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	key := Key{Provider: "codex", Root: "/tmp/root", RelPath: "sessions/a.jsonl"}
	entry := Entry{
		Key:        key,
		Signature:  Signature{Size: 12, MtimeNS: 100, FileID: "1:2", ParserVersion: ParserVersion, DescriptorSig: "sig"},
		Summary:    Summary{Valid: true, SessionID: "s1", Title: "hello", MessageCount: 2, UpdatedAtUnixMS: 50, TimeSource: TimeSourceMessage},
		Generation: 7,
	}
	if err := store.Upsert(ctx, []Entry{entry}); err != nil {
		t.Fatalf("Upsert error = %v", err)
	}
	got, err := store.Lookup(ctx, []Key{key})
	if err != nil {
		t.Fatalf("Lookup error = %v", err)
	}
	if !SignaturesEqual(got[key].Signature, entry.Signature) || got[key].Summary.Title != "hello" {
		t.Fatalf("lookup = %#v, want stored entry", got[key])
	}

	stale := Key{Provider: "codex", Root: "/tmp/root", RelPath: "sessions/gone.jsonl"}
	if err := store.Upsert(ctx, []Entry{{
		Key:        stale,
		Signature:  Signature{Size: 1, MtimeNS: 1, ParserVersion: ParserVersion, DescriptorSig: "sig"},
		Summary:    Summary{Valid: true, SessionID: "gone"},
		Generation: 7,
	}}); err != nil {
		t.Fatalf("Upsert stale error = %v", err)
	}
	if err := store.CompleteRoot(ctx, "codex", "/tmp/root", 8, []Key{key}); err != nil {
		t.Fatalf("CompleteRoot error = %v", err)
	}
	after, err := store.Lookup(ctx, []Key{key, stale})
	if err != nil {
		t.Fatalf("Lookup after complete error = %v", err)
	}
	if _, ok := after[stale]; ok {
		t.Fatalf("stale file remained after complete root: %#v", after)
	}
	if _, ok := after[key]; !ok {
		t.Fatalf("live file was deleted")
	}
}

func TestInspectPathRecordsSizeAndIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write error = %v", err)
	}
	sig, err := InspectPath(path)
	if err != nil {
		t.Fatalf("InspectPath error = %v", err)
	}
	if sig.Size != 3 || sig.MtimeNS == 0 || sig.ParserVersion != ParserVersion {
		t.Fatalf("signature = %#v", sig)
	}
}
