package agentextension

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLocalPackageContentCacheReusesAndInvalidates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache localPackageContentCache
	calls := 0
	hash := func(root string) (string, error) { calls++; return packageContentSHA256(root) }
	first, err := cache.load(root, hash)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		got, err := cache.load(root, hash)
		if err != nil || got != first {
			t.Fatalf("cached digest=%q err=%v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("unchanged package hashed %d times", calls)
	}
	if err := os.WriteFile(path, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Second)
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	second, err := cache.load(root, hash)
	if err != nil || second == first || calls != 2 {
		t.Fatalf("changed digest=%q calls=%d err=%v", second, calls, err)
	}
	added := filepath.Join(root, "new.json")
	if err := os.WriteFile(added, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := cache.load(root, hash)
	if err != nil || third == second || calls != 3 {
		t.Fatalf("added digest=%q calls=%d err=%v", third, calls, err)
	}
	if err := os.Remove(added); err != nil {
		t.Fatal(err)
	}
	fourth, err := cache.load(root, hash)
	if err != nil || fourth != second || calls != 4 {
		t.Fatalf("removed digest=%q calls=%d err=%v", fourth, calls, err)
	}
	// An atomic replacement with preserved length and timestamp is a new file.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(t.TempDir(), "replacement")
	if err := os.WriteFile(replacement, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	fifth, err := cache.load(root, hash)
	if err != nil || fifth == fourth || calls != 5 {
		t.Fatalf("replaced digest=%q calls=%d err=%v", fifth, calls, err)
	}
}

func TestLocalPackageContentCacheDoesNotRetainFailuresOrChangingTree(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.json")
	if err := os.WriteFile(path, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache localPackageContentCache
	if _, err := cache.load(root, func(string) (string, error) { return "", errors.New("read failed") }); err == nil {
		t.Fatal("expected read failure")
	}
	calls := 0
	hash := func(root string) (string, error) {
		calls++
		digest, err := packageContentSHA256(root)
		if calls == 1 {
			if err := os.WriteFile(path, []byte("two changed"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return digest, err
	}
	if _, err := cache.load(root, hash); err != nil {
		t.Fatal(err)
	}
	got, err := cache.load(root, hash)
	want, wantErr := packageContentSHA256(root)
	if err != nil || wantErr != nil || got != want || calls != 2 {
		t.Fatalf("digest=%q want=%q calls=%d err=%v/%v", got, want, calls, err, wantErr)
	}
}

func TestLocalPackageContentCacheCoalescesConcurrentReads(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.json"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache localPackageContentCache
	var wg sync.WaitGroup
	calls := 0
	hash := func(root string) (string, error) { calls++; return packageContentSHA256(root) }
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.load(root, hash); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("concurrent readers hashed %d times", calls)
	}
}
