package storesqlite

import (
	"context"
	"strings"
	"testing"
)

func TestMCPAppResourceSnapshotsDeduplicateByContent(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()

	input := PutMCPAppResourceSnapshotInput{
		URI:      "ui://workflow_report/widget",
		MimeType: "text/html;profile=mcp-app",
		HTML:     "<!doctype html><html><body>shell</body></html>",
		Meta: map[string]any{
			"prefersBorder": false,
			"csp":           map[string]any{"resourceDomains": []any{"https://lib.baomitu.com"}, "connectDomains": []any{}},
		},
	}
	first, err := store.PutMCPAppResourceSnapshot(ctx, input)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !ValidMCPAppResourceSHA256(first.SHA256) {
		t.Fatalf("sha = %q", first.SHA256)
	}
	second, err := store.PutMCPAppResourceSnapshot(ctx, input)
	if err != nil {
		t.Fatalf("second Put() error = %v", err)
	}
	if second != first {
		t.Fatalf("second put = %#v, want existing row %#v", second, first)
	}
	var rows int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_mcp_app_resources`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}

	got, ok, err := store.GetMCPAppResourceSnapshot(ctx, strings.ToUpper(first.SHA256))
	if err != nil || !ok {
		t.Fatalf("Get() ok=%v err=%v", ok, err)
	}
	if got.HTML != input.HTML || got.URI != input.URI || got.MimeType != input.MimeType {
		t.Fatalf("Get() = %#v", got)
	}
	if got.MetaJSON != `{"csp":{"connectDomains":[],"resourceDomains":["https://lib.baomitu.com"]},"prefersBorder":false}` {
		t.Fatalf("meta json = %s", got.MetaJSON)
	}

	// A changed CSP is a different snapshot; history keeps the old one.
	changed := input
	changed.Meta = map[string]any{"csp": map[string]any{"resourceDomains": []any{"https://cdn.bootcdn.net"}}}
	third, err := store.PutMCPAppResourceSnapshot(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if third.SHA256 == first.SHA256 {
		t.Fatal("changed meta reused the old snapshot address")
	}

	if _, ok, err := store.GetMCPAppResourceSnapshot(ctx, strings.Repeat("0", 64)); ok || err != nil {
		t.Fatalf("missing snapshot ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetMCPAppResourceSnapshot(ctx, "../etc"); ok || err != nil {
		t.Fatalf("invalid address ok=%v err=%v", ok, err)
	}
	if _, err := store.PutMCPAppResourceSnapshot(ctx, PutMCPAppResourceSnapshotInput{URI: "ui://x"}); err == nil {
		t.Fatal("empty html accepted")
	}
}

func TestMCPAppResourcesMigrationIsIdempotent(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
}
