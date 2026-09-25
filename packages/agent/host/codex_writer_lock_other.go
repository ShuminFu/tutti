//go:build !darwin && !linux

package agenthost

// Non-unix hosts cannot cheaply probe the Codex flock. Callers treat an
// unknown probe as "do not start app-server" and wait for an explicit retry,
// which resumes once and is the source of truth.
func codexWriterLockHeld(string, string) (bool, bool) {
	return false, false
}
