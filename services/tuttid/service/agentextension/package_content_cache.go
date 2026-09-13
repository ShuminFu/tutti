package agentextension

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type packageFileState struct {
	path string
	info os.FileInfo
}

type localPackageContentEntry struct {
	mu     sync.Mutex
	files  []packageFileState
	digest string
}

// Local packages are immutable installation snapshots. Stat the complete tree on
// each read, but only reread its bytes when a file or directory has changed.
// Bundled runtimes contain thousands of files and composer lookups repeat often.
type localPackageContentCache struct {
	entries sync.Map
}

func (c *localPackageContentCache) load(root string, hash func(string) (string, error)) (string, error) {
	root = filepath.Clean(root)
	value, _ := c.entries.LoadOrStore(root, &localPackageContentEntry{})
	entry := value.(*localPackageContentEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	files, err := packageFileStates(root)
	if err != nil {
		return "", err
	}
	if entry.digest != "" && samePackageFileStates(entry.files, files) {
		return entry.digest, nil
	}
	entry.files, entry.digest = nil, ""
	digest, err := hash(root)
	if err != nil {
		return "", err
	}
	after, err := packageFileStates(root)
	if err != nil {
		return "", err
	}
	// A concurrently replaced snapshot must be checked again on the next read.
	if samePackageFileStates(files, after) {
		entry.files, entry.digest = after, digest
	}
	return digest, nil
}

func packageFileStates(root string) ([]packageFileState, error) {
	var result []packageFileState
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if internalPackageRecord(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("extension package contains non-regular entry: %s", relative)
		}
		result = append(result, packageFileState{path: relative, info: info})
		return nil
	})
	return result, err
}

func samePackageFileStates(a, b []packageFileState) bool {
	if len(a) != len(b) {
		return false
	}
	for i, left := range a {
		right := b[i]
		if left.path != right.path || left.info.Mode() != right.info.Mode() ||
			left.info.Size() != right.info.Size() || !left.info.ModTime().Equal(right.info.ModTime()) ||
			!os.SameFile(left.info, right.info) {
			return false
		}
	}
	return true
}
