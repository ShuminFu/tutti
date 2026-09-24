package runtimeprep

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeCodexRun fabricates agent/runs/<id>/codex-home with a `.sandbox-bin`
// helper copy and a Codex state file, all stamped with lastActivity.
func makeCodexRun(t *testing.T, stateDir, id string, lastActivity time.Time) string {
	t.Helper()
	codexHome := filepath.Join(stateDir, "agent", "runs", id, "codex-home")
	binDir := filepath.Join(codexHome, ".sandbox-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(binDir, "codex.exe"):              "helper-bytes",
		filepath.Join(codexHome, "logs_2.sqlite"):       "log",
		filepath.Join(codexHome, "config.toml"):         "model = \"x\"\n",
		filepath.Join(codexHome, "sessions", "a.jsonl"): "{}",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(binDir, "codex.exe"), binDir,
		filepath.Join(codexHome, "logs_2.sqlite"), filepath.Join(codexHome, "config.toml"),
		filepath.Join(codexHome, "sessions", "a.jsonl"), filepath.Join(codexHome, "sessions"),
		codexHome,
	} {
		if err := os.Chtimes(path, lastActivity, lastActivity); err != nil {
			t.Fatal(err)
		}
	}
	return codexHome
}

func TestSweepStaleCodexSandboxBinsRemovesOnlyStaleHelperCopies(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	staleHome := makeCodexRun(t, stateDir, "stale", now.Add(-48*time.Hour))
	activeHome := makeCodexRun(t, stateDir, "active", now.Add(-time.Hour))

	result, err := LocalStore{StateDir: stateDir}.SweepStaleCodexSandboxBins(now, CodexSandboxBinStaleAfter)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.Removed != 1 || result.FreedBytes != int64(len("helper-bytes")) || result.Failed != 0 {
		t.Fatalf("result = %+v, want 1 removed, %d bytes", result, len("helper-bytes"))
	}
	if _, err := os.Stat(filepath.Join(staleHome, ".sandbox-bin")); !os.IsNotExist(err) {
		t.Fatalf("stale .sandbox-bin still present: %v", err)
	}
	// Everything resume needs stays in place.
	for _, keep := range []string{"logs_2.sqlite", "config.toml", filepath.Join("sessions", "a.jsonl")} {
		if _, err := os.Stat(filepath.Join(staleHome, keep)); err != nil {
			t.Fatalf("sweep removed %s: %v", keep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(activeHome, ".sandbox-bin", "codex.exe")); err != nil {
		t.Fatalf("active session helper removed: %v", err)
	}
}

// A session that was idle for days and just resumed writes to its Codex
// state; its old helper copy (old mtime) must not be judged stale.
func TestSweepStaleCodexSandboxBinsKeepsRecentlyResumedSession(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	home := makeCodexRun(t, stateDir, "resumed", now.Add(-72*time.Hour))
	recent := now.Add(-10 * time.Minute)
	if err := os.Chtimes(filepath.Join(home, "logs_2.sqlite"), recent, recent); err != nil {
		t.Fatal(err)
	}

	result, err := LocalStore{StateDir: stateDir}.SweepStaleCodexSandboxBins(now, CodexSandboxBinStaleAfter)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.Removed != 0 {
		t.Fatalf("result = %+v, want nothing removed", result)
	}
	if _, err := os.Stat(filepath.Join(home, ".sandbox-bin", "codex.exe")); err != nil {
		t.Fatalf("resumed session helper removed: %v", err)
	}
}

func TestSweepStaleCodexSandboxBinsIgnoresSymlinkedHelperDir(t *testing.T) {
	stateDir := t.TempDir()
	home := makeCodexRun(t, stateDir, "linked", time.Now().Add(-48*time.Hour))
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, ".sandbox-bin")
	if err := os.RemoveAll(binDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, binDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// staleAfter=0 so staleness cannot be what protects the link (creating
	// it just bumped codex-home's mtime); only the Lstat guard may.
	result, err := LocalStore{StateDir: stateDir}.SweepStaleCodexSandboxBins(time.Now().Add(time.Hour), 0)
	if err != nil || result.Removed != 0 {
		t.Fatalf("result = %+v err = %v, want symlink left alone", result, err)
	}
	if _, err := os.Lstat(binDir); err != nil {
		t.Fatalf("symlinked helper dir was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.exe")); err != nil {
		t.Fatalf("followed symlink and deleted outside file: %v", err)
	}
}

func TestSweepStaleCodexSandboxBinsWithoutRunsDirectory(t *testing.T) {
	result, err := LocalStore{StateDir: t.TempDir()}.SweepStaleCodexSandboxBins(time.Now(), CodexSandboxBinStaleAfter)
	if err != nil || result != (CodexSandboxBinSweepResult{}) {
		t.Fatalf("result = %+v err = %v, want empty no-op", result, err)
	}
}
