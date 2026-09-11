package agentstatus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHostRuntimeSelection(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host-cli-runtime-selection.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write host projection: %v", err)
	}
	return path
}

func TestHostRuntimeSelectionIgnoresMissingOrUnusableSource(t *testing.T) {
	home := t.TempDir()
	directory := filepath.Join(home, "a-directory")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := writeHostRuntimeSelection(t, `{"version":1,"providers":{"claude-code":{"binPath":"/host/claude"}}}`)
	symlink := filepath.Join(home, "linked.json")
	if err := os.Symlink(realFile, symlink); err != nil {
		t.Fatal(err)
	}
	oversize := writeHostRuntimeSelection(t, `{"version":1,"providers":{"claude-code":{"binPath":"`+
		strings.Repeat("a", hostRuntimeSelectionMaxBytes)+`"}}}`)

	for name, value := range map[string]string{
		"relative path":  "relative/selection.json",
		"directory":      directory,
		"symlink":        symlink,
		"oversize":       oversize,
		"missing file":   filepath.Join(home, "absent.json"),
		"malformed json": writeHostRuntimeSelection(t, `{"version":1,`),
		"unknown schema": writeHostRuntimeSelection(t, `{"version":2,"providers":{}}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(HostRuntimeSelectionFileEnv, value)
			path, state := hostRuntimeSelection("claude-code")
			if state != hostRuntimeSelectionAbsent || path != "" {
				t.Fatalf("hostRuntimeSelection = %q, %v; want absent", path, state)
			}
		})
	}
}

func TestHostRuntimeSelectionReadsSelectedProvider(t *testing.T) {
	path := writeHostRuntimeSelection(t, `{"version":1,"providers":{"claude-code":{"binPath":"/host/claude","version":"2.1.268"},"codex":{"binPath":"/host/codex"}}}`)
	t.Setenv(HostRuntimeSelectionFileEnv, path)

	selected, state := hostRuntimeSelection("claude-code")
	if state != hostRuntimeSelectionSet || selected != "/host/claude" {
		t.Fatalf("hostRuntimeSelection = %q, %v; want /host/claude", selected, state)
	}
	// A provider the document does not mention is "the host did not speak", not
	// "the host cleared it".
	if selected, state := hostRuntimeSelection("cursor"); state != hostRuntimeSelectionAbsent || selected != "" {
		t.Fatalf("unlisted provider = %q, %v; want absent", selected, state)
	}
}

func TestHostRuntimeSelectionMatchesProviderAliases(t *testing.T) {
	for name, document := range map[string]string{
		"canonical key, alias query": `{"version":1,"providers":{"claude-code":{"binPath":"/host/claude"}}}`,
		"alias key, canonical query": `{"version":1,"providers":{"claude":{"binPath":"/host/claude"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t, document))
			selected, state := hostRuntimeSelection("claude-code")
			if state != hostRuntimeSelectionSet || selected != "/host/claude" {
				t.Fatalf("hostRuntimeSelection = %q, %v; want /host/claude", selected, state)
			}
		})
	}
}

func TestHostRuntimeSelectionReportsExplicitlyClearedProvider(t *testing.T) {
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"claude-code":{"binPath":""}}}`))
	selected, state := hostRuntimeSelection("claude-code")
	if state != hostRuntimeSelectionCleared || selected != "" {
		t.Fatalf("hostRuntimeSelection = %q, %v; want cleared", selected, state)
	}
}

func TestHostRuntimeSelectionRejectsNonAbsoluteBinPath(t *testing.T) {
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"claude-code":{"binPath":"bin/claude"}}}`))
	selected, state := hostRuntimeSelection("claude-code")
	if state != hostRuntimeSelectionAbsent || selected != "" {
		t.Fatalf("hostRuntimeSelection = %q, %v; want absent for a relative binPath", selected, state)
	}
}
