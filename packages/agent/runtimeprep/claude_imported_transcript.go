package runtimeprep

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// claudeProjectDirMaxLength mirrors CLAUDE_PROJECT_ID_MAX_LENGTH in the pinned
// Claude Agent SDK's local project-key algorithm.
const claudeProjectDirMaxLength = 200

// exposeClaudeImportedTranscriptFile symlinks the single Claude Code transcript
// an imported session was read from into the config root the resumed CLI will
// actually read, at the one path its own lookup derives:
//
//	<CLAUDE_CONFIG_DIR>/projects/<claudeProjectDir(input.Cwd)>/<session id>.jsonl
//
// Why this exists: the import scans **every** configured transcript root (the
// host declares the user's own ~/.claude as an extra root through
// TUTTI_CLAUDE_EXTRA_IMPORT_ROOTS, precisely so bare-terminal conversations stay
// importable — see externalProviderRoots in services/tuttid), but the resumed
// CLI only ever reads the one config root its process was started with, and only
// under the project directory implied by **its own cwd**. So a session imported
// from the user's own root is imported fine and then cannot be restored: the
// sidecar reports the provider session as missing, the daemon treats that as
// "not available locally" and recreates a *brand new* provider session bound to
// the same agent session (ResumeModeRecreate). The conversation keeps showing
// every earlier message while the model has none of that context — only a
// system notice hints at it.
//
// Mirroring the SDK's project key (instead of, say, reusing the source's own
// directory name) is what makes the resumed CLI find the file when the session's
// runtime cwd differs from the cwd recorded inside the transcript — the normal
// case for a session that ran inside a linked git worktree, whose imported cwd
// is folded onto the main checkout.
//
// sourcePath is empty for non-imported sessions, so this is a no-op for the
// overwhelming majority of sessions.
//
// Deliberately returns nothing: every failure path here degrades to the
// documented "recreatable" path (a fresh provider session plus a visible notice)
// rather than failing the resume. That degradation is user-visible on its own,
// while a hard error here would leave the conversation unusable — the user could
// not send a single message — for something as ordinary as two concurrent
// resumes racing to create the same link or a read-only config root.
func exposeClaudeImportedTranscriptFile(input ProviderPrepareInput) {
	sourcePath := strings.TrimSpace(input.ExternalRolloutSourcePath)
	if sourcePath == "" {
		return
	}
	// The SDK resolves both the config root and the project key from absolute
	// paths; a relative source would make the link depend on the daemon's own
	// cwd, so treat it as "not resolvable" and leave the fallback in place.
	if !filepath.IsAbs(sourcePath) {
		warnClaudeImportedTranscriptUnavailable("source path is not absolute", input, sourcePath)
		return
	}
	sourcePath = filepath.Clean(sourcePath)
	if info, err := os.Stat(sourcePath); err != nil || info.IsDir() {
		// Retention pruned it, or the user's own tree moved. Say so once per
		// resume: the recreate fallback that follows is otherwise only visible
		// as a notice inside the conversation (see recreateAdapterSession).
		warnClaudeImportedTranscriptUnavailable("source is missing or not a file", input, sourcePath)
		return
	}
	configDir := configuredClaudeConfigDir()
	cwd := strings.TrimSpace(input.Cwd)
	if configDir == "" || cwd == "" {
		warnClaudeImportedTranscriptUnavailable("config root or session cwd is unknown", input, sourcePath)
		return
	}
	sessionID := claudeTranscriptSessionID(sourcePath)
	if sessionID == "" {
		// Not a Claude Code transcript file name, so the path the CLI would look
		// under cannot be derived from it.
		warnClaudeImportedTranscriptUnavailable("source is not a session transcript", input, sourcePath)
		return
	}
	target := filepath.Join(configDir, "projects", claudeProjectDir(cwd), sessionID+".jsonl")
	if pathIsWithin(filepath.Dir(target), sourcePath) {
		// The transcript already lives exactly where the CLI will look for it:
		// this is a session the panel itself ran, not an imported one.
		return
	}
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			current, readErr := os.Readlink(target)
			if readErr == nil {
				resolved := filepath.Clean(resolveLinkTarget(target, current))
				if resolved == sourcePath {
					return // already exposed
				}
				if _, statErr := os.Stat(resolved); errors.Is(statErr, fs.ErrNotExist) {
					// A previous exposure whose source has since moved (the user
					// relocated their own config root): re-point it rather than
					// leaving a link the CLI will fail to read.
					if err := os.Remove(target); err != nil {
						warnClaudeImportedTranscriptFailure("remove stale link", input, sourcePath, target, err)
						return
					}
					if err := exposeClaudeImportedTranscriptAt(target, sourcePath, input); err != nil {
						warnClaudeImportedTranscriptFailure("repoint link", input, sourcePath, target, err)
					}
					return
				}
			}
		}
		// Something else owns this path (the panel's own session, or a link the
		// user's tree moved away from). Never clobber it; the recreate fallback
		// still applies, and the warning below is what makes that visible.
		slog.Warn("claude imported transcript target is occupied",
			"agent_session_id", strings.TrimSpace(input.AgentSessionID),
			"source", sourcePath, "target", target)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		warnClaudeImportedTranscriptFailure("stat link target", input, sourcePath, target, err)
		return
	}
	if err := exposeClaudeImportedTranscriptAt(target, sourcePath, input); err != nil {
		warnClaudeImportedTranscriptFailure("link transcript", input, sourcePath, target, err)
	}
}

func warnClaudeImportedTranscriptFailure(action string, input ProviderPrepareInput, sourcePath string, target string, err error) {
	slog.Warn("claude imported transcript could not be exposed for resume",
		"action", action,
		"agent_session_id", strings.TrimSpace(input.AgentSessionID),
		"source", sourcePath, "target", target, "error", err)
}

// warnClaudeImportedTranscriptUnavailable covers the early returns where there is
// nothing to link at all. They all end the same way — resume falls back to a
// fresh provider session — so they are logged too: an import whose transcript can
// never be exposed should not have to be inferred from a missing link.
func warnClaudeImportedTranscriptUnavailable(reason string, input ProviderPrepareInput, sourcePath string) {
	slog.Warn("claude imported transcript is unavailable for resume",
		"reason", reason,
		"agent_session_id", strings.TrimSpace(input.AgentSessionID),
		"source", sourcePath)
}

// exposeClaudeImportedTranscriptAt links sourcePath at target, creating the parent
// project directory first.
//
// A link, not a copy: the CLI keeps appending to this transcript, and a copy would
// fork the conversation into two divergent histories (the user's own
// `claude --resume` reads the original). Known caveat: the CLI sometimes rewrites
// its transcript through a temp file plus rename, which replaces the link with a
// regular file in the target root — from then on the two histories diverge instead
// of sharing one file. That is still strictly better than never finding it, and it
// is why the next resume re-checks rather than assuming the link is still a link.
func exposeClaudeImportedTranscriptAt(target string, sourcePath string, input ProviderPrepareInput) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := exposeImportedProviderFile(sourcePath, target, 0o600); err != nil {
		// A concurrent resume may have created exactly this link while we were
		// working; that is a success, not a failure.
		if claudeTranscriptLinkPointsAt(target, sourcePath) {
			return nil
		}
		return err
	}
	slog.Info("claude imported transcript exposed for resume",
		"agent_session_id", strings.TrimSpace(input.AgentSessionID),
		"source", sourcePath, "target", target)
	return nil
}

// claudeTranscriptLinkPointsAt reports whether path is currently a symlink whose
// resolved target is want.
func claudeTranscriptLinkPointsAt(path string, want string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	current, err := os.Readlink(path)
	if err != nil {
		return false
	}
	return filepath.Clean(resolveLinkTarget(path, current)) == want
}

// claudeTranscriptSessionID reads the session id off the transcript file name,
// which is how Claude Code names it (<session id>.jsonl).
func claudeTranscriptSessionID(sourcePath string) string {
	name := filepath.Base(sourcePath)
	if !strings.HasSuffix(name, ".jsonl") {
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(name, ".jsonl"))
}

// configuredClaudeConfigDir mirrors the config root the Claude CLI/SDK resolves
// for itself (packages/agent/claude-sdk-sidecar/src/settingsEnv.ts): an explicit
// absolute CLAUDE_CONFIG_DIR wins, otherwise ~/.claude. runtimeprep deliberately
// does not set CLAUDE_CONFIG_DIR (see ClaudeCodePreparer.Prepare), so the child
// inherits whatever this returns.
//
// A relative CLAUDE_CONFIG_DIR resolves against the *session's* cwd in the child,
// which this code cannot know, so it is treated as unavailable rather than
// guessed at.
func configuredClaudeConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		if !filepath.IsAbs(dir) {
			return ""
		}
		return filepath.Clean(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// claudeProjectDir mirrors the local project-key algorithm in the pinned Claude
// Agent SDK (the same one the sidecar mirrors in
// packages/agent/claude-sdk-sidecar/src/goalTranscript.ts): the symlink-resolved
// cwd with every character outside [a-zA-Z0-9] replaced by "-", truncated to 200
// characters with a base36 hash suffix when the result is longer.
//
// Both halves matter for the resume target: a wrong directory name is a
// transcript the CLI never reads, which is precisely the failure this file
// exists to fix. The hash loop follows the JS original exactly (UTF-16 code
// units, 32-bit wrapping, Math.abs, base36).
func claudeProjectDir(cwd string) string {
	resolved := strings.TrimSpace(cwd)
	if absolute, err := filepath.Abs(resolved); err == nil {
		resolved = absolute
	}
	if evaluated, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = evaluated
	}
	sanitized := sanitizeClaudeProjectName(resolved)
	units := utf16.Encode([]rune(sanitized))
	if len(units) <= claudeProjectDirMaxLength {
		return sanitized
	}
	return string(utf16.Decode(units[:claudeProjectDirMaxLength])) +
		"-" + claudeProjectDirHash(resolved)
}

func sanitizeClaudeProjectName(path string) string {
	var builder strings.Builder
	for _, unit := range utf16.Encode([]rune(path)) {
		switch {
		case unit >= 'a' && unit <= 'z', unit >= 'A' && unit <= 'Z', unit >= '0' && unit <= '9':
			builder.WriteRune(rune(unit))
		default:
			builder.WriteByte('-')
		}
	}
	return builder.String()
}

func claudeProjectDirHash(value string) string {
	var hash int32
	for _, unit := range utf16.Encode([]rune(value)) {
		hash = hash<<5 - hash + int32(unit)
	}
	magnitude := int64(hash)
	if magnitude < 0 {
		magnitude = -magnitude
	}
	if magnitude == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var reversed []byte
	for magnitude > 0 {
		reversed = append(reversed, digits[magnitude%36])
		magnitude /= 36
	}
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	return string(reversed)
}

func pathIsWithin(root string, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveLinkTarget(linkPath string, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(linkPath), target)
}
