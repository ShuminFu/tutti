package runtimeprep

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	testShaA = "1dc195897af4161d039b80d8471ec0a10c9bbc89"
	testShaB = "2dc195897af4161d039b80d8471ec0a10c9bbc89"
)

// writeCodexPluginsTree fabricates a finished codex curated-plugins sync
// (plugins/.git, plugins/README.md, plugins.sha) inside dir.
func writeCodexPluginsTree(t *testing.T, dir, sha, marker string, shaTime time.Time) {
	t.Helper()
	plugins := filepath.Join(dir, codexPluginsDirName)
	if err := os.MkdirAll(filepath.Join(plugins, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "README.md"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	shaPath := filepath.Join(dir, codexPluginsShaFileName)
	if err := os.WriteFile(shaPath, []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(shaPath, shaTime, shaTime); err != nil {
		t.Fatal(err)
	}
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, codexPluginsDirName, "README.md"))
	if err != nil {
		t.Fatalf("read marker in %s: %v", dir, err)
	}
	return string(raw)
}

// stubCheckoutComplete replaces the git completeness check for fake trees.
func stubCheckoutComplete(t *testing.T, ok bool) {
	t.Helper()
	orig := codexPluginsCheckoutComplete
	codexPluginsCheckoutComplete = func(string, string) bool { return ok }
	t.Cleanup(func() { codexPluginsCheckoutComplete = orig })
}

func requireDarwinClone(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("APFS clonefile seeding is darwin-only")
	}
}

func TestCodexCuratedPluginsSeedRootFollowsRunsRoot(t *testing.T) {
	got := codexCuratedPluginsSeedRoot(filepath.Join("/state", "agent", "runs", "s1"))
	if want := filepath.Join("/state", "agent", codexCuratedPluginsSeedDirName); got != want {
		t.Fatalf("seed root = %q, want %q", got, want)
	}
	if got := codexCuratedPluginsSeedRoot("/somewhere/else/s1"); got != "" {
		t.Fatalf("seed root outside runs = %q, want empty", got)
	}
}

func TestSeedCodexCuratedPluginsNoSeedIsNoop(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if seedCodexCuratedPlugins(codexHome, filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("seeded without a seed")
	}
	if _, err := os.Lstat(filepath.Join(codexHome, codexPluginsTmpDirName)); !os.IsNotExist(err) {
		t.Fatalf(".tmp created without a seed: %v", err)
	}
}

// Main regression test: a new session home prepared through CodexPreparer
// gets the shared seed, so codex does not download 89MB again.
func TestCodexPrepareSeedsCuratedPluginsFromSharedSeed(t *testing.T) {
	requireDarwinClone(t)
	setTestHome(t, t.TempDir())
	stateDir := t.TempDir()
	seedGen := filepath.Join(stateDir, "agent", codexCuratedPluginsSeedDirName, testShaA)
	writeCodexPluginsTree(t, seedGen, testShaA, "seed-content", time.Now().Add(-time.Hour))

	preparer := newTestPreparer(stateDir)
	preparer.RegisterProvider(CodexPreparer{})
	result, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace", AgentSessionID: "session", AgentTargetID: "local:codex", Provider: "codex", Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := filepath.Join(envValue(result.Env, "CODEX_HOME"), codexPluginsTmpDirName)
	if got := readMarker(t, tmpDir); got != "seed-content" {
		t.Fatalf("seeded marker = %q", got)
	}
	raw, err := os.ReadFile(filepath.Join(tmpDir, codexPluginsShaFileName))
	if err != nil || strings.TrimSpace(string(raw)) != testShaA {
		t.Fatalf("seeded sha = %q, %v", raw, err)
	}
}

func TestSeedCodexCuratedPluginsNeverOverwritesExisting(t *testing.T) {
	requireDarwinClone(t)
	seedRoot := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(seedRoot, testShaA), testShaA, "seed-content", time.Now())
	codexHome := t.TempDir()
	tmpDir := filepath.Join(codexHome, codexPluginsTmpDirName)
	writeCodexPluginsTree(t, tmpDir, testShaB, "own-content", time.Now())

	if seedCodexCuratedPlugins(codexHome, seedRoot) {
		t.Fatal("seeded over an existing .tmp/plugins")
	}
	if got := readMarker(t, tmpDir); got != "own-content" {
		t.Fatalf("existing plugins overwritten: %q", got)
	}
}

func TestSeedCodexCuratedPluginsSwallowsCloneFailure(t *testing.T) {
	seedRoot := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(seedRoot, testShaA), testShaA, "seed-content", time.Now())
	orig := codexPluginSeedCloneDir
	codexPluginSeedCloneDir = func(src, dst string) error {
		_ = os.MkdirAll(dst, 0o755) // simulate a partial clone
		return errors.New("boom")
	}
	t.Cleanup(func() { codexPluginSeedCloneDir = orig })

	codexHome := t.TempDir()
	if seedCodexCuratedPlugins(codexHome, seedRoot) {
		t.Fatal("reported seeded after clone failure")
	}
	tmpDir := filepath.Join(codexHome, codexPluginsTmpDirName)
	for _, name := range []string{codexPluginsDirName, codexPluginsShaFileName} {
		if _, err := os.Lstat(filepath.Join(tmpDir, name)); !os.IsNotExist(err) {
			t.Fatalf("partial %s left behind: %v", name, err)
		}
	}
}

func TestRefreshCodexCuratedPluginsSeed(t *testing.T) {
	requireDarwinClone(t)
	stubCheckoutComplete(t, true)
	seedRoot := filepath.Join(t.TempDir(), codexCuratedPluginsSeedDirName)
	old := time.Now().Add(-2 * time.Hour)

	// Seed missing -> created from the run.
	runA := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(runA, codexPluginsTmpDirName), testShaA, "rev-a", old)
	if !refreshCodexCuratedPluginsSeed(runA, seedRoot) {
		t.Fatal("seed not created from run")
	}
	if got := readMarker(t, filepath.Join(seedRoot, testShaA)); got != "rev-a" {
		t.Fatalf("seed marker = %q", got)
	}

	// Same sha -> untouched.
	runSame := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(runSame, codexPluginsTmpDirName), testShaA, "rev-a-other", time.Now())
	if refreshCodexCuratedPluginsSeed(runSame, seedRoot) {
		t.Fatal("seed replaced although sha is the same")
	}

	// Different but OLDER sha -> no rollback.
	runOlder := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(runOlder, codexPluginsTmpDirName), testShaB, "rev-b-old", old.Add(-time.Hour))
	if refreshCodexCuratedPluginsSeed(runOlder, seedRoot) {
		t.Fatal("seed rolled back to an older revision")
	}

	// In-flight codex download -> skipped.
	runBusy := t.TempDir()
	busyTmp := filepath.Join(runBusy, codexPluginsTmpDirName)
	writeCodexPluginsTree(t, busyTmp, testShaB, "rev-b-busy", time.Now())
	if err := os.MkdirAll(filepath.Join(busyTmp, codexPluginsInFlightPrefix+"x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if refreshCodexCuratedPluginsSeed(runBusy, seedRoot) {
		t.Fatal("seed refreshed from an in-flight sync")
	}

	// Different, newer sha -> replaced, old generation pruned.
	runB := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(runB, codexPluginsTmpDirName), testShaB, "rev-b", time.Now())
	if !refreshCodexCuratedPluginsSeed(runB, seedRoot) {
		t.Fatal("seed not refreshed to newer sha")
	}
	latest, ok := latestCodexPluginsSeed(seedRoot)
	if !ok || latest.sha != testShaB || readMarker(t, latest.dir) != "rev-b" {
		t.Fatalf("latest seed = %+v ok=%v", latest, ok)
	}
	if _, err := os.Lstat(filepath.Join(seedRoot, testShaA)); !os.IsNotExist(err) {
		t.Fatalf("old generation not pruned: %v", err)
	}
	entries, _ := os.ReadDir(seedRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), codexSeedStagePrefix) {
			t.Fatalf("stage left behind: %s", e.Name())
		}
	}
}

func TestRefreshCodexCuratedPluginsSeedSwallowsCloneFailure(t *testing.T) {
	seedRoot := filepath.Join(t.TempDir(), codexCuratedPluginsSeedDirName)
	origClone, origLock := codexPluginSeedCloneDir, codexPluginSeedTryLock
	codexPluginSeedCloneDir = func(string, string) error { return errors.New("boom") }
	codexPluginSeedTryLock = func(string) (func(), error) { return func() {}, nil }
	t.Cleanup(func() { codexPluginSeedCloneDir, codexPluginSeedTryLock = origClone, origLock })

	stubCheckoutComplete(t, true)
	run := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(run, codexPluginsTmpDirName), testShaA, "rev-a", time.Now())
	if refreshCodexCuratedPluginsSeed(run, seedRoot) {
		t.Fatal("reported refreshed after clone failure")
	}
	if _, ok := latestCodexPluginsSeed(seedRoot); ok {
		t.Fatal("seed published after clone failure")
	}
	entries, _ := os.ReadDir(seedRoot)
	if len(entries) != 0 {
		t.Fatalf("seed root not clean after failure: %v", entries)
	}
}

func TestCodexPluginSeedCleanupKeepsInnerError(t *testing.T) {
	want := errors.New("inner")
	// Seed refresh has nothing to do here (no .tmp), which must not mask or
	// replace the wrapped cleanup's own result.
	cleanup := codexPluginSeedCleanup(func(context.Context) error { return want }, t.TempDir(), t.TempDir())
	if err := cleanup(t.Context()); !errors.Is(err, want) {
		t.Fatalf("cleanup error = %v, want %v", err, want)
	}
	if err := codexPluginSeedCleanup(nil, t.TempDir(), t.TempDir())(t.Context()); err != nil {
		t.Fatalf("nil inner cleanup error = %v", err)
	}
}

// A half-written checkout (e.g. codex killed mid in-place update) carries a
// valid-looking sha; publishing it would poison every future session.
func TestRefreshCodexCuratedPluginsSeedRejectsIncompleteCheckout(t *testing.T) {
	requireDarwinClone(t)
	stubCheckoutComplete(t, false)
	seedRoot := filepath.Join(t.TempDir(), codexCuratedPluginsSeedDirName)
	run := t.TempDir()
	writeCodexPluginsTree(t, filepath.Join(run, codexPluginsTmpDirName), testShaA, "rev-a", time.Now())
	if refreshCodexCuratedPluginsSeed(run, seedRoot) {
		t.Fatal("published an incomplete checkout")
	}
	if _, ok := latestCodexPluginsSeed(seedRoot); ok {
		t.Fatal("incomplete checkout became the seed")
	}
	entries, _ := os.ReadDir(seedRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), codexSeedStagePrefix) {
			t.Fatalf("stage left behind: %s", e.Name())
		}
	}
}

// The real check against an actual git checkout: complete → true; a deleted
// tracked file or a wrong sha → false.
func TestCodexPluginsCheckoutCompleteWithRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	sha := run("rev-parse", "HEAD")
	if !codexPluginsCheckoutComplete(dir, sha) {
		t.Fatal("complete checkout rejected")
	}
	if codexPluginsCheckoutComplete(dir, testShaB) {
		t.Fatal("wrong sha accepted")
	}
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	if codexPluginsCheckoutComplete(dir, sha) {
		t.Fatal("checkout with a missing tracked file accepted")
	}
}
