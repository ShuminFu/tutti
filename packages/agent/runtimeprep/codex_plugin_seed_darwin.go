//go:build darwin

package runtimeprep

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneDirCOW makes an APFS copy-on-write clone of a whole directory tree in
// one syscall. clonefile(2) recurses into directories and shares every data
// block with the source until either side writes. It fails (e.g. ENOTSUP /
// EXDEV) on non-APFS volumes or across volumes; callers treat that as "no seed".
func cloneDirCOW(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}

// tryLockCodexPluginsSync takes a non-blocking exclusive flock on codex's
// plugins.sync.lock. If codex is mid-sync and holds it, we back off. A missing
// lock file just means codex never synced here; nothing to exclude against.
func tryLockCodexPluginsSync(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if os.IsNotExist(err) {
		return func() {}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}
