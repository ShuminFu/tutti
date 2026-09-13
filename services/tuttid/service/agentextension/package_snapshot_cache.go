package agentextension

import (
	"os"
	"path/filepath"
	"sync"
)

type verifiedPackageSnapshot struct {
	digest string
	root   os.FileInfo
}

// Installation directories are immutable, atomically activated snapshots.
// Foreground reads check the snapshot identity; background reconciliation owns
// the full content scan. It must not hold up reads of the active installation.
type packageSnapshotCache struct {
	verified sync.Map
}

func (c *packageSnapshotCache) remember(root, digest string) {
	info, err := os.Lstat(root)
	if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		c.verified.Store(filepath.Clean(root), verifiedPackageSnapshot{digest: digest, root: info})
	}
}

func (c *packageSnapshotCache) matches(root, digest string) bool {
	key := filepath.Clean(root)
	value, ok := c.verified.Load(key)
	if !ok {
		return false
	}
	snapshot := value.(verifiedPackageSnapshot)
	info, err := os.Lstat(root)
	if err != nil || snapshot.digest != digest || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(snapshot.root, info) || !snapshot.root.ModTime().Equal(info.ModTime()) {
		c.verified.Delete(key)
		return false
	}
	return true
}

func (c *packageSnapshotCache) forget(root string) {
	c.verified.Delete(filepath.Clean(root))
}
