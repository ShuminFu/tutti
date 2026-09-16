package agentstatus

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

func TestDiscoverCodexRuntimeCandidatesDeduplicatesLauncherAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture is Unix-only")
	}
	home := t.TempDir()
	target := filepath.Join(home, "package", "bin", "codex")
	aliasDir := filepath.Join(home, "alias-bin")
	alias := filepath.Join(aliasDir, "codex")
	writeExecutable(t, target, "#!/bin/sh\nexit 0\n")
	if err := os.MkdirAll(aliasDir, 0o755); err != nil {
		t.Fatalf("mkdir alias dir: %v", err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatalf("create launcher alias: %v", err)
	}

	service := probeTestService(home)
	service.Environ = func() []string {
		return []string{"PATH=" + filepath.Dir(target) + string(os.PathListSeparator) + aliasDir}
	}
	candidates := service.discoverCodexRuntimeCandidates(context.Background(), ProviderSpec{Provider: "codex"})
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want one: %#v", len(candidates), candidates)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if candidates[0].LauncherPath != target || candidates[0].RealPath != realTarget {
		t.Fatalf("candidate = %#v, want launcher %q and real path %q", candidates[0], target, realTarget)
	}
}

func TestCodexRuntimeCandidateCollectorDeduplicatesPackageRoots(t *testing.T) {
	home := t.TempDir()
	pkgRoot := filepath.Join(home, "node_modules", "@openai", "codex")
	writePackageManifest(t, pkgRoot, "@openai/codex", "0.142.0")
	first := filepath.Join(pkgRoot, "bin", "codex")
	second := filepath.Join(pkgRoot, "alternate", "codex")
	writeExecutable(t, first, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, second, "#!/bin/sh\nexit 0\n")

	collector := codexRuntimeCandidateCollector{}
	collector.add(first, codexRuntimeCandidateSourcePath)
	collector.add(second, codexRuntimeCandidateSourceBunGlobal)
	if len(collector.candidates) != 1 {
		t.Fatalf("candidate count = %d, want one: %#v", len(collector.candidates), collector.candidates)
	}
	candidate := collector.candidates[0]
	realPkgRoot, err := filepath.EvalSymlinks(pkgRoot)
	if err != nil {
		t.Fatalf("resolve package root: %v", err)
	}
	if candidate.PackageRoot != realPkgRoot {
		t.Fatalf("package root = %q, want %q", candidate.PackageRoot, realPkgRoot)
	}
	if got, want := candidate.Sources, []codexRuntimeCandidateSource{codexRuntimeCandidateSourcePath, codexRuntimeCandidateSourceBunGlobal}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %#v, want %#v", got, want)
	}
}

func concurrentManagerFixture(markerDir, markerName, resultCommand string) string {
	return `#!/bin/sh
marker_dir=` + strconv.Quote(markerDir) + `
touch "$marker_dir/` + markerName + `"
attempt=0
while [ ! -f "$marker_dir/bun" ] ||
      [ ! -f "$marker_dir/pnpm" ] ||
      [ ! -f "$marker_dir/npm" ] ||
      [ ! -f "$marker_dir/brew" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 100 ]; then
    exit 1
  fi
  sleep 0.02
done
` + resultCommand + `
`
}

func candidateLaunchers(candidates []codexRuntimeCandidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.LauncherPath)
	}
	return result
}
