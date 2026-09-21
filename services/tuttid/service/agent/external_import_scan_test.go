package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
	"github.com/tutti-os/tutti/services/tuttid/data/externalimportcatalog"
)

func TestScanExternalImportsReusesCatalogWithoutReparsing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude-home"))
	t.Setenv("GROK_HOME", filepath.Join(root, "grok-home"))
	recent := time.Now().UTC().Add(-time.Hour)
	writeAgentServiceJSONL(t, filepath.Join(codexHome, "sessions", "one.jsonl"),
		map[string]any{
			"timestamp": recent.Format(time.RFC3339),
			"type":      "session_meta",
			"payload":   map[string]any{"id": "one", "cwd": project},
		},
		map[string]any{"timestamp": recent.Add(time.Second).Format(time.RFC3339), "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "m1", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "Hello catalog"}},
		}},
	)

	catalog, err := externalimportcatalog.Open(filepath.Join(root, "catalog.sqlite"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = catalog.Close() })

	service := newIsolatedAgentService(newFakeRuntime())
	service.ExternalImportCatalog = catalog

	first, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Providers: []string{"codex"}, Days: -1})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Diagnostics.ParsedFiles != 1 || first.ScannedSessions != 1 {
		t.Fatalf("first scan diagnostics = %#v result=%#v", first.Diagnostics, first)
	}
	if first.Complete != true || first.CutoffUnixMS != 0 {
		t.Fatalf("coverage = complete=%v cutoff=%d", first.Complete, first.CutoffUnixMS)
	}

	second, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Providers: []string{"codex"}, Days: -1})
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Diagnostics.ParsedFiles != 0 {
		t.Fatalf("hot catalog parsed %d files, want 0", second.Diagnostics.ParsedFiles)
	}
	if second.Diagnostics.CatalogHits != 1 || second.ScannedSessions != 1 {
		t.Fatalf("second scan diagnostics = %#v", second.Diagnostics)
	}
	if len(second.Sessions) != 1 || second.Sessions[0].Title != "Hello catalog" {
		t.Fatalf("second sessions = %#v", second.Sessions)
	}

	writeAgentServiceJSONL(t, filepath.Join(codexHome, "sessions", "one.jsonl"),
		map[string]any{
			"timestamp": recent.Format(time.RFC3339),
			"type":      "session_meta",
			"payload":   map[string]any{"id": "one", "cwd": project},
		},
		map[string]any{"timestamp": recent.Add(2 * time.Second).Format(time.RFC3339), "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "m1", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "Hello catalog"}},
		}},
		map[string]any{"timestamp": recent.Add(3 * time.Second).Format(time.RFC3339), "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "m2", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": "Updated"}},
		}},
	)
	third, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Providers: []string{"codex"}, Days: -1})
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if third.Diagnostics.ParsedFiles != 1 {
		t.Fatalf("changed file parsed %d, want 1", third.Diagnostics.ParsedFiles)
	}
	if third.ScannedSessions != 1 || third.Sessions[0].MessageCount != 2 {
		t.Fatalf("updated session = %#v", third.Sessions)
	}
}

func TestImportExternalSessionsParsesOnlySelectedBodies(t *testing.T) {
	ctx := context.Background()
	store := openAgentServiceSQLiteStore(t)
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	canonical, ok := canonicalExistingDir(project)
	if !ok {
		t.Fatalf("canonical project path")
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude-home"))
	t.Setenv("GROK_HOME", filepath.Join(root, "grok-home"))
	recent := time.Now().UTC().Add(-time.Hour)
	writeAgentServiceJSONL(t, filepath.Join(codexHome, "sessions", "keep.jsonl"),
		map[string]any{
			"timestamp": recent.Format(time.RFC3339),
			"type":      "session_meta",
			"payload":   map[string]any{"id": "keep", "cwd": canonical},
		},
		map[string]any{"timestamp": recent.Add(time.Second).Format(time.RFC3339), "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "k1", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "Keep me"}},
		}},
	)
	writeAgentServiceJSONL(t, filepath.Join(codexHome, "sessions", "skip.jsonl"),
		map[string]any{
			"timestamp": recent.Format(time.RFC3339),
			"type":      "session_meta",
			"payload":   map[string]any{"id": "skip", "cwd": canonical},
		},
		map[string]any{"timestamp": recent.Add(time.Second).Format(time.RFC3339), "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "s1", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "Skip me"}},
		}},
	)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "ws-1", Name: "Workspace"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	catalog, err := externalimportcatalog.Open(filepath.Join(root, "catalog.sqlite"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = catalog.Close() })
	service := newIsolatedAgentService(newFakeRuntime())
	service.ExternalImportStore = store
	service.ExternalImportCatalog = catalog

	scan, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Providers: []string{"codex"}, Days: -1})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	keepID := externalImportedSessionID("codex", "keep")
	result, err := service.ImportExternalSessions(ctx, "ws-1", ExternalImportInput{
		Projects: []ExternalImportProjectSelection{{
			Path:       canonical,
			Providers:  []string{"codex"},
			SessionIDs: []string{keepID},
		}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ImportedSessions != 1 {
		t.Fatalf("imported = %#v", result)
	}
	if _, ok, err := store.GetSession(ctx, "ws-1", keepID); err != nil || !ok {
		t.Fatalf("keep session missing ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetSession(ctx, "ws-1", externalImportedSessionID("codex", "skip")); err != nil {
		t.Fatalf("skip lookup err=%v", err)
	} else if ok {
		t.Fatalf("unselected session was imported")
	}
	_ = scan
}

func TestScanExternalImportsDropsBodiesFromScanResult(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude-home"))
	t.Setenv("GROK_HOME", filepath.Join(root, "grok-home"))
	recent := time.Now().UTC().Add(-time.Hour)
	writeAgentServiceJSONL(t, filepath.Join(codexHome, "sessions", "one.jsonl"),
		map[string]any{
			"timestamp": recent.Format(time.RFC3339),
			"type":      "session_meta",
			"payload":   map[string]any{"id": "one", "cwd": project},
		},
		map[string]any{"timestamp": recent.Add(time.Second).Format(time.RFC3339), "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "m1", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "Body should drop"}},
		}},
	)
	service := newIsolatedAgentService(newFakeRuntime())
	data, err := service.scanExternalAgentSessions(ctx, []string{"codex"}, -1, "", "", externalScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(data.sessions) != 1 {
		t.Fatalf("sessions = %#v", data.sessions)
	}
	if len(data.sessions[0].Messages) != 0 {
		t.Fatalf("scan retained %d bodies", len(data.sessions[0].Messages))
	}
	if data.sessions[0].MessageCount != 1 {
		t.Fatalf("message count = %d", data.sessions[0].MessageCount)
	}
}
