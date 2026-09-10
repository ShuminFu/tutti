package agentextension

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledRuntimeChecksumManifestRejectsUnsafePaths(t *testing.T) {
	for _, relative := range []string{"../escape", "/absolute", `dir\windows`, "dir/../escape", "."} {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			manifest := filepath.Join(t.TempDir(), bundledRuntimeChecksumFile)
			if err := os.WriteFile(manifest, []byte(strings.Repeat("a", 64)+"  "+relative+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readBundledRuntimeChecksums(manifest); err == nil {
				t.Fatalf("unsafe checksum path %q was accepted", relative)
			}
		})
	}
}

func TestBundledRuntimeFilesRejectSymlinksAndRequireFullCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "agent"), []byte("runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("agent", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBundledRuntimeFiles(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
	if err := os.Remove(filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	files, err := inspectBundledRuntimeFiles(root)
	if err != nil || len(files) != 1 || files[0] != "agent" {
		t.Fatalf("files = %#v, error = %v", files, err)
	}
	digest := sha256.Sum256([]byte("runtime"))
	if err := os.WriteFile(filepath.Join(root, bundledRuntimeChecksumFile), []byte(hex.EncodeToString(digest[:])+"  agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checksums, err := readBundledRuntimeChecksums(filepath.Join(root, bundledRuntimeChecksumFile))
	if err != nil || checksums["agent"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("checksums = %#v, error = %v", checksums, err)
	}
}
