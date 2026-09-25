//go:build darwin || linux

package agenthost

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// codexWriterLockHeld probes an existing writer lock without becoming the
// holder. A missing file means the desktop app is not holding the thread.
// Resume remains the source of truth when this probe is inconclusive.
func codexWriterLockHeld(codexHome, threadID string) (held bool, known bool) {
	codexHome = strings.TrimSpace(codexHome)
	threadID = strings.TrimSpace(threadID)
	if codexHome == "" || threadID == "" || strings.Contains(threadID, "..") || strings.ContainsAny(threadID, `/\`) {
		return false, false
	}
	path := filepath.Join(codexHome, "thread-writer-locks", threadID+".lock")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, true
	}
	if err != nil || info.IsDir() {
		return false, false
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, true
	}
	if err != nil {
		return false, false
	}
	defer file.Close()
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		return false, true
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return true, true
	}
	return false, false
}
