package runtimeprep

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CodexSandboxBinStaleAfter is how long a session's codex-home must stay
// untouched before its `.sandbox-bin` copy is reclaimed.
//
// Why this exists: on Windows, Codex copies its own executable (~285 MB) into
// `$CODEX_HOME/.sandbox-bin` so the sandbox user can launch it. DinTalDock
// gives every session its own CODEX_HOME under agent/runs/<session>/codex-home,
// and run roots are only removed when a session is deleted, so each session
// kept a full private copy forever (6.6 GB / 87 runs seen on one machine).
//
// Removing the copy is safe even for a session that later resumes: Codex
// resolves the helper on every launch (`resolve_exe_for_launch` →
// `copy_from_source_if_needed`) and re-materializes it when it is missing.
// The staleness window only avoids making an active session re-copy.
const CodexSandboxBinStaleAfter = 24 * time.Hour

const codexSandboxBinDirectory = ".sandbox-bin"

type CodexSandboxBinSweepResult struct {
	Removed    int
	FreedBytes int64
	// Failed counts copies that could not be removed (e.g. a locked exe on
	// Windows while a sandboxed command runs). They are retried next sweep.
	Failed int
}

// SweepStaleCodexSandboxBins removes `.sandbox-bin` from every run whose
// codex-home has had no activity for staleAfter. It never touches anything
// else in the run root, so resume keeps working from the old folder.
func (s LocalStore) SweepStaleCodexSandboxBins(now time.Time, staleAfter time.Duration) (CodexSandboxBinSweepResult, error) {
	var result CodexSandboxBinSweepResult
	stateDir := filepath.Clean(strings.TrimSpace(s.StateDir))
	if stateDir == "." || stateDir == string(filepath.Separator) {
		return result, errors.New("agent sidecar state directory is not configured")
	}
	runsRoot := filepath.Join(stateDir, "agent", "runs")
	entries, err := os.ReadDir(runsRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read agent runs directory: %w", err)
	}
	var firstErr error
	for _, entry := range entries {
		// Skip symlinks and files: only real run directories we created.
		if !entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		codexHome := filepath.Join(runsRoot, entry.Name(), codexHomeDirectory)
		binDir := filepath.Join(codexHome, codexSandboxBinDirectory)
		info, err := os.Lstat(binDir)
		if err != nil || !info.IsDir() { // Lstat: a symlinked helper dir is not ours
			continue
		}
		lastActivity, err := codexHomeLastActivity(codexHome)
		if err != nil || now.Sub(lastActivity) < staleAfter {
			continue
		}
		size := directorySize(binDir)
		if err := os.RemoveAll(binDir); err != nil {
			result.Failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("remove codex sandbox helper copy %s: %w", binDir, err)
			}
			continue
		}
		result.Removed++
		result.FreedBytes += size
	}
	return result, firstErr
}

// codexHomeLastActivity is the newest mtime among codex-home's direct
// children (Codex rewrites logs_*.sqlite / state_*.sqlite / sessions while a
// session runs).
func codexHomeLastActivity(codexHome string) (time.Time, error) {
	info, err := os.Stat(codexHome)
	if err != nil {
		return time.Time{}, err
	}
	latest := info.ModTime()
	entries, err := os.ReadDir(codexHome)
	if err != nil {
		return time.Time{}, err
	}
	for _, entry := range entries {
		childInfo, err := entry.Info()
		if err != nil {
			continue
		}
		if childInfo.ModTime().After(latest) {
			latest = childInfo.ModTime()
		}
	}
	return latest, nil
}

func directorySize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
