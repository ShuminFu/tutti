package agentstatus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	canonical "github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

// HostRuntimeSelectionFileEnv names the file the host (RnDMaster) projects its
// CLI runtime registry into: for each provider, the absolute path of the binary
// DinTalDock should run.
//
// Same shape as host_model_endpoints: the *path* travels in the process
// environment (a stable value, so freezing it at spawn costs nothing), the
// *content* is rewritten by the host on every registry write and read here on
// every provider spec resolution.
//
// Why it exists: the operator override (CLAUDE_CODE_EXECUTABLE) is baked into
// this process's environment at spawn time, so a path the user saves afterwards
// is invisible to us until the sidecar restarts. Reading the host's projection
// makes the executor consult the registry at use time instead of at boot.
const HostRuntimeSelectionFileEnv = "RNDMASTER_CLI_RUNTIME_SELECTION_FILE"

// hostRuntimeSelectionMaxBytes bounds the projection we are willing to parse.
// The document is a handful of paths; anything larger is not ours.
const hostRuntimeSelectionMaxBytes = 64 * 1024

type hostRuntimeSelectionDocument struct {
	Version   int                                  `json:"version"`
	Providers map[string]hostRuntimeSelectionEntry `json:"providers"`
}

type hostRuntimeSelectionEntry struct {
	BinPath string `json:"binPath"`
	Version string `json:"version"`
}

// hostRuntimeSelectionState distinguishes "the host did not speak" from "the
// host says there is no selection" — the two lead to different fallbacks.
type hostRuntimeSelectionState int

const (
	// hostRuntimeSelectionAbsent: no usable projection (no env key, unreadable
	// file, bad JSON, unexpected version, or the document simply does not
	// mention this provider). Callers keep their legacy chain.
	hostRuntimeSelectionAbsent hostRuntimeSelectionState = iota
	// hostRuntimeSelectionCleared: the host mentions the provider but carries an
	// empty binPath — it is telling us the provider has no selected runtime, so
	// callers must NOT fall back to the (now stale) inline environment.
	hostRuntimeSelectionCleared
	// hostRuntimeSelectionSet: the host selected this absolute path.
	hostRuntimeSelectionSet
)

// hostRuntimeSelection returns the binary the host selected for provider.
//
// Only "is it a non-empty absolute path" is checked here. File mode and
// `--version` stay with the existing validators at the call site, so the host
// projection and the inline environment are judged by exactly one criterion.
// entry.Version is provenance only: host data must not be able to skip probing.
func hostRuntimeSelection(provider string) (string, hostRuntimeSelectionState) {
	document, ok := hostRuntimeSelectionDocumentFromEnv()
	if !ok {
		return "", hostRuntimeSelectionAbsent
	}
	want := canonicalProviderID(provider)
	if want == "" {
		return "", hostRuntimeSelectionAbsent
	}
	entry, found := document.providers()[want]
	if !found {
		return "", hostRuntimeSelectionAbsent
	}
	binPath := strings.TrimSpace(entry.BinPath)
	if binPath == "" {
		return "", hostRuntimeSelectionCleared
	}
	if !filepath.IsAbs(binPath) {
		return "", hostRuntimeSelectionAbsent
	}
	return binPath, hostRuntimeSelectionSet
}

// providers indexes the document by canonical provider id so that a host
// writing either the canonical id ("claude-code") or its alias ("claude")
// resolves to the same entry.
func (d hostRuntimeSelectionDocument) providers() map[string]hostRuntimeSelectionEntry {
	indexed := make(map[string]hostRuntimeSelectionEntry, len(d.Providers))
	for key, entry := range d.Providers {
		id := canonicalProviderID(key)
		if id == "" {
			id = strings.ToLower(strings.TrimSpace(key))
		}
		if id == "" {
			continue
		}
		indexed[id] = entry
	}
	return indexed
}

func canonicalProviderID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if identity, ok := canonical.FindProviderIdentity(value); ok {
		return identity.ID
	}
	return ""
}

func hostRuntimeSelectionDocumentFromEnv() (hostRuntimeSelectionDocument, bool) {
	path := strings.TrimSpace(os.Getenv(HostRuntimeSelectionFileEnv))
	if !filepath.IsAbs(path) {
		return hostRuntimeSelectionDocument{}, false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > hostRuntimeSelectionMaxBytes {
		return hostRuntimeSelectionDocument{}, false
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return hostRuntimeSelectionDocument{}, false
	}
	var document hostRuntimeSelectionDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return hostRuntimeSelectionDocument{}, false
	}
	if document.Version != 1 {
		return hostRuntimeSelectionDocument{}, false
	}
	return document, true
}
