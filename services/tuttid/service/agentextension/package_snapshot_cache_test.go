package agentextension

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedSnapshotRequiresSameDirectoryAndDigest(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "installed")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	var cache packageSnapshotCache
	if cache.matches(root, "first") {
		t.Fatal("unverified snapshot reused")
	}
	cache.remember(root, "first")
	if !cache.matches(root, "first") {
		t.Fatal("unchanged verified snapshot not reused")
	}
	if cache.matches(root, "other") {
		t.Fatal("different content identity reused")
	}
	cache.remember(root, "first")
	if err := os.Rename(root, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if cache.matches(root, "first") {
		t.Fatal("replaced directory reused verification")
	}
	cache.remember(root, "first")
	cache.forget(root)
	if cache.matches(root, "first") {
		t.Fatal("background rejection left foreground verification cached")
	}
}
