package runtimeprep

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Codex curated-plugin marketplace seed.
//
// Every tutti codex session owns a private CODEX_HOME under
// <state>/agent/runs/<session>/codex-home. On its first `codex app-server`
// start the codex CLI clones the "OpenAI Curated" plugin marketplace git repo
// into CODEX_HOME/.tmp/plugins (+ plugins.sha) in the background — ~89MB per
// session, identical across sessions (measured: 40 real copies = 3.9GB on one
// peer). Codex does not re-download when .tmp/plugins.sha already matches
// upstream, so we keep ONE shared copy next to the runs root and hand each new
// session an APFS clone of it (copy-on-write: zero extra disk until codex
// changes a file).
//
// Layout (immutable per sha, so readers never see a half-written seed):
//
//	<state>/agent/codex-curated-plugins/<sha>/plugins/     git checkout
//	<state>/agent/codex-curated-plugins/<sha>/plugins.sha  40-hex sha
//
// Everything here is a best-effort optimization: any failure (non-darwin,
// non-APFS volume, concurrent swap, malformed content) is logged at debug and
// swallowed, and codex simply downloads the repo itself as before.

const (
	codexCuratedPluginsSeedDirName = "codex-curated-plugins"
	codexPluginsTmpDirName         = ".tmp"
	codexPluginsDirName            = "plugins"
	codexPluginsShaFileName        = "plugins.sha"
	codexPluginsSyncLockFileName   = "plugins.sync.lock"
	// codex stages an in-flight download as .tmp/plugins-clone-XXXX.
	codexPluginsInFlightPrefix = "plugins-clone-"
	codexSeedStagePrefix       = ".stage-"
)

// errCodexPluginSeedUnsupported is returned by the platform clone primitive on
// systems without a cheap copy-on-write directory clone.
var errCodexPluginSeedUnsupported = errors.New("codex plugin seed clone unsupported on this platform")

// Platform hooks; overridable in tests.
var (
	codexPluginSeedCloneDir  = cloneDirCOW
	codexPluginSeedTryLock   = tryLockCodexPluginsSync
	codexPluginSeedLogDebugf = func(msg string, args ...any) { slog.Debug(msg, args...) }
)

// codexCuratedPluginsSeedRoot derives the shared seed directory from a session
// runtime root (<state>/agent/runs/<session>). It returns "" when the runtime
// root is not laid out under a "runs" directory, so tests or unusual layouts
// never scatter seed dirs in unexpected places.
func codexCuratedPluginsSeedRoot(runtimeRoot string) string {
	runtimeRoot = filepath.Clean(strings.TrimSpace(runtimeRoot))
	runsRoot := filepath.Dir(runtimeRoot)
	if runtimeRoot == "." || filepath.Base(runsRoot) != "runs" {
		return ""
	}
	return filepath.Join(filepath.Dir(runsRoot), codexCuratedPluginsSeedDirName)
}

// codexPluginsSnapshot describes a complete, idle .tmp-style directory.
type codexPluginsSnapshot struct {
	dir     string // directory containing plugins/ and plugins.sha
	sha     string
	shaTime time.Time
}

// readCodexPluginsSnapshot validates dir as a finished curated-plugins sync:
// a 40-hex plugins.sha, a plugins/ checkout with .git or README.md, and no
// in-flight plugins-clone-* staging dir left by codex.
func readCodexPluginsSnapshot(dir string) (codexPluginsSnapshot, bool) {
	shaPath := filepath.Join(dir, codexPluginsShaFileName)
	raw, err := os.ReadFile(shaPath)
	if err != nil {
		return codexPluginsSnapshot{}, false
	}
	sha := strings.TrimSpace(string(raw))
	if !isHexSHA(sha) {
		return codexPluginsSnapshot{}, false
	}
	info, err := os.Stat(shaPath)
	if err != nil {
		return codexPluginsSnapshot{}, false
	}
	pluginsDir := filepath.Join(dir, codexPluginsDirName)
	if st, err := os.Lstat(pluginsDir); err != nil || !st.IsDir() {
		return codexPluginsSnapshot{}, false
	}
	if !pathExists(filepath.Join(pluginsDir, ".git")) && !pathExists(filepath.Join(pluginsDir, "README.md")) {
		return codexPluginsSnapshot{}, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return codexPluginsSnapshot{}, false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), codexPluginsInFlightPrefix) {
			return codexPluginsSnapshot{}, false
		}
	}
	return codexPluginsSnapshot{dir: dir, sha: sha, shaTime: info.ModTime()}, true
}

// latestCodexPluginsSeed returns the newest valid seed generation.
func latestCodexPluginsSeed(seedRoot string) (codexPluginsSnapshot, bool) {
	entries, err := os.ReadDir(seedRoot)
	if err != nil {
		return codexPluginsSnapshot{}, false
	}
	var best codexPluginsSnapshot
	found := false
	for _, entry := range entries {
		if !entry.IsDir() || !isHexSHA(entry.Name()) {
			continue
		}
		snap, ok := readCodexPluginsSnapshot(filepath.Join(seedRoot, entry.Name()))
		if !ok || snap.sha != entry.Name() {
			continue
		}
		if !found || snap.shaTime.After(best.shaTime) {
			best, found = snap, true
		}
	}
	return best, found
}

// seedCodexCuratedPlugins gives a fresh codex home a COW clone of the shared
// seed. It never overwrites an existing .tmp/plugins and never returns an
// error: the worst case is that codex downloads the repo itself.
func seedCodexCuratedPlugins(codexHome, seedRoot string) (seeded bool) {
	if seedRoot == "" {
		return false
	}
	tmpDir := filepath.Join(codexHome, codexPluginsTmpDirName)
	targetPlugins := filepath.Join(tmpDir, codexPluginsDirName)
	if _, err := os.Lstat(targetPlugins); !os.IsNotExist(err) {
		return false // already present (or unreadable): leave it to codex
	}
	seed, ok := latestCodexPluginsSeed(seedRoot)
	if !ok {
		return false
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		codexPluginSeedLogDebugf("codex plugin seed skipped", "event", "agent.runtime_prep.codex_plugin_seed_skipped", "reason", "mkdir", "error", err)
		return false
	}
	// Clone plugins/ first, then plugins.sha: a partially seeded home must
	// never carry a sha without the checkout it vouches for.
	// Clone into a private stage and rename into place: if two prepares race on
	// the same home, the loser only ever deletes its own stage, never the
	// winner's plugins/ checkout.
	stage, err := os.MkdirTemp(tmpDir, codexSeedStagePrefix)
	if err != nil {
		return false
	}
	defer os.RemoveAll(stage) // no-op after a successful rename
	stagePlugins := filepath.Join(stage, codexPluginsDirName)
	if err := codexPluginSeedCloneDir(filepath.Join(seed.dir, codexPluginsDirName), stagePlugins); err != nil {
		codexPluginSeedLogDebugf("codex plugin seed skipped", "event", "agent.runtime_prep.codex_plugin_seed_skipped", "reason", "clone", "error", err)
		return false
	}
	if err := os.Rename(stagePlugins, targetPlugins); err != nil {
		return false // someone else placed plugins/ first: leave theirs alone
	}
	shaTarget := filepath.Join(tmpDir, codexPluginsShaFileName)
	if err := writeFileAtomic(shaTarget, []byte(seed.sha+"\n"), 0o644); err != nil {
		_ = os.RemoveAll(targetPlugins)
		codexPluginSeedLogDebugf("codex plugin seed skipped", "event", "agent.runtime_prep.codex_plugin_seed_skipped", "reason", "sha", "error", err)
		return false
	}
	_ = os.Chtimes(shaTarget, seed.shaTime, seed.shaTime)
	slog.Info("codex curated plugins seeded", "event", "agent.runtime_prep.codex_plugin_seeded", "sha", seed.sha, "codexHome", codexHome)
	return true
}

// refreshCodexCuratedPluginsSeed publishes the run's finished curated-plugins
// sync as the new seed generation when it is newer than the current seed.
// Safe under concurrency: each generation is staged privately and published
// with a single rename; losing the race just discards the stage.
func refreshCodexCuratedPluginsSeed(codexHome, seedRoot string) (refreshed bool) {
	if seedRoot == "" {
		return false
	}
	tmpDir := filepath.Join(codexHome, codexPluginsTmpDirName)
	run, ok := readCodexPluginsSnapshot(tmpDir)
	if !ok {
		return false
	}
	current, hasSeed := latestCodexPluginsSeed(seedRoot)
	// Only move forward: an old session ending late must not roll the seed
	// back to an older marketplace revision.
	if hasSeed && (current.sha == run.sha || !run.shaTime.After(current.shaTime)) {
		return false
	}
	// Hold codex's own sync lock (when it uses flock) so we never snapshot a
	// checkout that codex is rewriting right now.
	unlock, err := codexPluginSeedTryLock(filepath.Join(tmpDir, codexPluginsSyncLockFileName))
	if err != nil {
		codexPluginSeedLogDebugf("codex plugin seed refresh skipped", "event", "agent.runtime_prep.codex_plugin_seed_refresh_skipped", "reason", "locked", "error", err)
		return false
	}
	defer unlock()
	if err := os.MkdirAll(seedRoot, 0o755); err != nil {
		return false
	}
	stage, err := os.MkdirTemp(seedRoot, codexSeedStagePrefix)
	if err != nil {
		return false
	}
	defer os.RemoveAll(stage) // no-op after a successful publish rename
	if err := codexPluginSeedCloneDir(filepath.Join(tmpDir, codexPluginsDirName), filepath.Join(stage, codexPluginsDirName)); err != nil {
		codexPluginSeedLogDebugf("codex plugin seed refresh skipped", "event", "agent.runtime_prep.codex_plugin_seed_refresh_skipped", "reason", "clone", "error", err)
		return false
	}
	// Re-read the sha after cloning: if codex swapped revisions mid-clone the
	// stage may mix two revisions, so drop it.
	after, ok := readCodexPluginsSnapshot(tmpDir)
	if !ok || after.sha != run.sha {
		return false
	}
	// Verify the staged checkout really is that revision, complete: HEAD must
	// equal plugins.sha and no tracked file may be modified or missing. A bad
	// seed would be served to every future session (its sha matches upstream,
	// so codex would never re-download), so when git is unavailable or the
	// check fails we simply don't publish.
	if !codexPluginsCheckoutComplete(filepath.Join(stage, codexPluginsDirName), run.sha) {
		codexPluginSeedLogDebugf("codex plugin seed refresh skipped", "event", "agent.runtime_prep.codex_plugin_seed_refresh_skipped", "reason", "incomplete_checkout")
		return false
	}
	shaPath := filepath.Join(stage, codexPluginsShaFileName)
	if err := os.WriteFile(shaPath, []byte(run.sha+"\n"), 0o644); err != nil {
		return false
	}
	_ = os.Chtimes(shaPath, run.shaTime, run.shaTime)
	if err := os.Rename(stage, filepath.Join(seedRoot, run.sha)); err != nil {
		// Another session published the same sha first; that's fine.
		return false
	}
	pruneCodexPluginsSeeds(seedRoot, run.sha)
	slog.Info("codex curated plugins seed refreshed", "event", "agent.runtime_prep.codex_plugin_seed_refreshed", "sha", run.sha)
	return true
}

// pruneCodexPluginsSeeds removes every generation except keep, plus stale
// stages. Readers mid-clone of a removed generation fail and fall back to a
// codex download, which is acceptable.
func pruneCodexPluginsSeeds(seedRoot, keep string) {
	entries, err := os.ReadDir(seedRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case name == keep:
		case isHexSHA(name):
			_ = os.RemoveAll(filepath.Join(seedRoot, name))
		case strings.HasPrefix(name, codexSeedStagePrefix):
			// Only reap stages old enough to be abandoned, not a live peer's.
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.RemoveAll(filepath.Join(seedRoot, name))
			}
		}
	}
}

// codexPluginSeedCleanup wraps a provider cleanup so the run's curated-plugin
// sync is offered back to the shared seed when the session ends. Seed errors
// are swallowed; only the wrapped cleanup's error is returned.
func codexPluginSeedCleanup(inner func(context.Context) error, codexHome, seedRoot string) func(context.Context) error {
	if seedRoot == "" {
		return inner
	}
	return func(ctx context.Context) error {
		var err error
		if inner != nil {
			err = inner(ctx)
		}
		refreshCodexCuratedPluginsSeed(codexHome, seedRoot)
		return err
	}
}

func isHexSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("publish %s: %w", filepath.Base(path), err)
	}
	return nil
}

// codexPluginsCheckoutComplete reports whether dir is a git checkout at sha
// with no modified or deleted tracked files. It is a var so tests can stub it
// without a git binary.
var codexPluginsCheckoutComplete = func(dir, sha string) bool {
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != sha {
		return false
	}
	status, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=no").Output()
	return err == nil && len(strings.TrimSpace(string(status))) == 0
}
