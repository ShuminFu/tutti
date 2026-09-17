package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

const (
	testImportRootEnvVar      = "TUTTI_TEST_IMPORT_ROOT"
	testImportExtraRootsEnv   = "TUTTI_TEST_IMPORT_EXTRA_ROOTS"
	testClaudeExtraRootsEnv   = "TUTTI_CLAUDE_EXTRA_IMPORT_ROOTS"
	testImportDescriptorName  = "~/.claude"
	testDetachedExtraRootPath = "  "
)

func testImportDescriptor() providerregistry.ExternalImportDescriptor {
	return providerregistry.ExternalImportDescriptor{
		RootEnvVar:       testImportRootEnvVar,
		DefaultRoot:      testImportDescriptorName,
		ExtraRootsEnvVar: testImportExtraRootsEnv,
	}
}

func TestExternalProviderRootsFallsBackToDefaultRootWhenEnvRootUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(testImportRootEnvVar, "")
	t.Setenv(testImportExtraRootsEnv, "")

	roots := externalProviderRoots(testImportDescriptor())
	want := filepath.Join(home, ".claude")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("roots = %#v, want exactly [%s]", roots, want)
	}
}

func TestExternalProviderRootsPrefersEnvRootOverDefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	managed := filepath.Join(home, "managed-claude")
	// Whitespace counts as unset, matching the pre-existing single-root contract.
	t.Setenv(testImportRootEnvVar, "  "+managed+"  ")
	t.Setenv(testImportExtraRootsEnv, "")

	roots := externalProviderRoots(testImportDescriptor())
	if len(roots) != 1 || roots[0] != managed {
		t.Fatalf("roots = %#v, want exactly [%s]", roots, managed)
	}
}

func TestExternalProviderRootsAppendsDeclaredExtraRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	managed := filepath.Join(home, "managed-claude")
	userRoot := filepath.Join(home, "user-claude")
	t.Setenv(testImportRootEnvVar, managed)
	// The list repeats the primary root and carries a blank entry: neither
	// should produce a duplicate or an empty root.
	t.Setenv(testImportExtraRootsEnv, strings.Join([]string{
		userRoot,
		testDetachedExtraRootPath,
		managed,
	}, string(os.PathListSeparator)))

	roots := externalProviderRoots(testImportDescriptor())
	if len(roots) != 2 || roots[0] != managed || roots[1] != userRoot {
		t.Fatalf("roots = %#v, want [%s %s]", roots, managed, userRoot)
	}
}

func TestExternalProviderRootsKeepsExtraRootsWhenEnvRootUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userRoot := filepath.Join(home, "user-claude")
	t.Setenv(testImportRootEnvVar, "")
	t.Setenv(testImportExtraRootsEnv, userRoot)

	roots := externalProviderRoots(testImportDescriptor())
	wantDefault := filepath.Join(home, ".claude")
	if len(roots) != 2 || roots[0] != wantDefault || roots[1] != userRoot {
		t.Fatalf("roots = %#v, want [%s %s]", roots, wantDefault, userRoot)
	}
}

// TestServiceScanReadsHostDeclaredExtraImportRoot covers the deployment shape
// that motivated ExtraRootsEnvVar: the host redirects CLAUDE_CONFIG_DIR at its
// own managed config dir for isolation, and declares the user's own ~/.claude
// as an extra root so bare-terminal conversations stay importable. Both roots
// must be scanned; a session continued in the second root must not be counted
// or offered twice.
func TestServiceScanReadsHostDeclaredExtraImportRoot(t *testing.T) {
	ctx := context.Background()
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "ws-1", Name: "Workspace One"}); err != nil {
		t.Fatalf("Create workspace error = %v", err)
	}
	root := t.TempDir()
	project := filepath.Join(root, "project-a")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("create project error = %v", err)
	}
	if canonical, ok := canonicalExistingDir(project); ok {
		project = canonical
	}
	managedHome := filepath.Join(root, "managed-claude")
	userHome := filepath.Join(root, "user-claude")
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	t.Setenv("CLAUDE_CONFIG_DIR", managedHome)
	t.Setenv("GROK_HOME", filepath.Join(root, "grok-home"))
	t.Setenv(testClaudeExtraRootsEnv, userHome)

	newer := time.Now().UTC().Truncate(time.Second)
	older := newer.Add(-2 * time.Hour)
	// The user's own CLI root holds the original conversation ...
	writeAgentServiceJSONL(t, filepath.Join(userHome, "projects", "project-a", "shared.jsonl"),
		map[string]any{
			"timestamp": older.Format(time.RFC3339Nano), "sessionId": "claude-shared", "cwd": project, "uuid": "shared-old",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Earlier copy"}}},
		},
	)
	writeAgentServiceJSONL(t, filepath.Join(userHome, "projects", "project-a", "user-only.jsonl"),
		map[string]any{
			"timestamp": newer.Format(time.RFC3339Nano), "sessionId": "claude-user-only", "cwd": project, "uuid": "user-only-1",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "User only prompt"}}},
		},
	)
	// ... and the managed root holds the same session continued in the host.
	writeAgentServiceJSONL(t, filepath.Join(managedHome, "projects", "project-a", "shared.jsonl"),
		map[string]any{
			"timestamp": newer.Format(time.RFC3339Nano), "sessionId": "claude-shared", "cwd": project, "uuid": "shared-new-1",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Earlier copy"}}},
		},
		map[string]any{
			"timestamp": newer.Format(time.RFC3339Nano), "sessionId": "claude-shared", "cwd": project, "uuid": "shared-new-2",
			"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Continued answer"}}},
		},
	)

	service := newIsolatedAgentService(newFakeRuntime())
	scan, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Days: -1})
	if err != nil {
		t.Fatalf("ScanExternalImports error = %v", err)
	}
	if len(scan.Errors) != 0 {
		t.Fatalf("scan errors = %#v, want none", scan.Errors)
	}
	if scan.ScannedSessions != 2 || scan.ScannedMessages != 3 {
		t.Fatalf("scan = %#v, want 2 sessions and 3 messages (shared copy deduplicated)", scan)
	}
	sharedID := externalImportedSessionID(providerregistry.ClaudeCodeProviderID, "claude-shared")
	var shared *ExternalImportSession
	for index := range scan.Sessions {
		if scan.Sessions[index].ID == sharedID {
			shared = &scan.Sessions[index]
		}
	}
	if shared == nil {
		t.Fatalf("scan sessions = %#v, want the session from the user's own root", scan.Sessions)
	}
	if shared.MessageCount != 2 {
		t.Fatalf("shared session messageCount = %d, want the 2 messages of the newer copy", shared.MessageCount)
	}
	if shared.LastUpdatedAtUnixMS != newer.UnixMilli() {
		t.Fatalf("shared session updatedAt = %d, want the newer copy %d", shared.LastUpdatedAtUnixMS, newer.UnixMilli())
	}
	if !strings.HasPrefix(shared.SourcePath, managedHome+string(os.PathSeparator)) {
		t.Fatalf("shared session sourcePath = %q, want the newer copy under %q", shared.SourcePath, managedHome)
	}
	if shared.ProjectPath != project {
		t.Fatalf("shared session projectPath = %q, want %q", shared.ProjectPath, project)
	}

	// Importing the shared session must land exactly once, with the newer copy's
	// messages, even though the provider session id exists under both roots.
	projection := NewActivityProjection(store)
	service.SessionReader = projection
	service.MessageReader = projection
	service.ExternalImportStore = store
	result, err := service.ImportExternalSessions(ctx, "ws-1", ExternalImportInput{
		Projects: []ExternalImportProjectSelection{{Path: project}},
	})
	if err != nil {
		t.Fatalf("ImportExternalSessions error = %v", err)
	}
	if result.ImportedSessions != 2 {
		t.Fatalf("import result = %#v, want both sessions imported once", result)
	}
	importedMessages, err := service.ListMessages(ctx, "ws-1", sharedID, ListMessagesInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages(shared) error = %v", err)
	}
	if len(importedMessages.Messages) != 2 {
		t.Fatalf("imported shared messages = %#v, want the newer copy's 2 messages", importedMessages.Messages)
	}
}

// TestServiceScanIgnoresExtraImportRootsWhenUnset pins the historical contract:
// with no extra roots declared, only the env-var root is read, so a session
// living under the default root stays invisible.
func TestServiceScanIgnoresExtraImportRootsWhenUnset(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project-a")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("create project error = %v", err)
	}
	if canonical, ok := canonicalExistingDir(project); ok {
		project = canonical
	}
	managedHome := filepath.Join(root, "managed-claude")
	userHome := filepath.Join(root, "user-claude")
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	t.Setenv("CLAUDE_CONFIG_DIR", managedHome)
	t.Setenv("GROK_HOME", filepath.Join(root, "grok-home"))
	t.Setenv(testClaudeExtraRootsEnv, "")
	writeAgentServiceJSONL(t, filepath.Join(userHome, "projects", "project-a", "user-only.jsonl"),
		map[string]any{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "sessionId": "claude-user-only", "cwd": project, "uuid": "user-only-1",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "User only prompt"}}},
		},
	)

	service := newIsolatedAgentService(newFakeRuntime())
	scan, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Days: -1, Providers: []string{providerregistry.ClaudeCodeProviderID}})
	if err != nil {
		t.Fatalf("ScanExternalImports error = %v", err)
	}
	if scan.ScannedSessions != 0 {
		t.Fatalf("scan = %#v, want no sessions from the undeclared root", scan)
	}
	if len(scan.Providers) != 1 || scan.Providers[0].Root != managedHome {
		t.Fatalf("scan providers = %#v, want the env-var root %q", scan.Providers, managedHome)
	}
}
