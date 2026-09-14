package agentextension

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/runtimecmd"
	agentextensiondata "github.com/tutti-os/tutti/services/tuttid/data/agentextension"
)

func TestBundledRuntimeUsesHostPackageAcrossArchitectureAndMetadataUpdates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("version probe fixture uses a POSIX shell")
	}
	t.Setenv(bundledRuntimeChannelEnv("gemini"), "")
	source := testResolvedTempDir(t)
	manifest := testManifest()
	manifest.Runtime.Install.Runner = "bundled"
	manifest.Runtime.Install.Args = nil
	manifest.Runtime.Launch.Executable = "${installRoot}/bin/gemini"
	manifest.Runtime.Launch.Args = []string{"--acp", "${projectRoot}"}
	metadata := testPackageZIPFor(t, manifest, `{"schemaVersion":"tutti.agent.discovery.v1","candidates":[{"binaryNames":["gemini"],"version":{"args":["--version"],"constraint":">=0.50.0 <1.0.0"},"launchArgs":["--acp"],"probe":{"kind":"acp-initialize","timeoutMs":5000}}]}`)
	if err := extractPackage(metadata, source); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(source, "runtime", runtimePlatform(), "bin", "gemini")
	writeBundledRuntimeFixture(t, executable)
	writeBundledRuntimeFixture(t, filepath.Join(source, "runtime-previous", runtimePlatform(), "bin", "gemini"))
	manager := &Manager{
		Installations:     agentextensiondata.NewFileInstallationStore(testResolvedTempDir(t)),
		RuntimeInstallDir: filepath.Join(testResolvedTempDir(t), "unused-managed-runtime"),
	}
	cwd := testResolvedTempDir(t)
	assertReady := func(installation Installation) InstallPlan {
		t.Helper()
		if installation.RuntimePackageDir != source || installation.PackageContentSHA256 != "" {
			t.Fatalf("host runtime provenance = %#v", installation)
		}
		for _, name := range []string{"runtime", "runtime-previous"} {
			if _, err := os.Stat(filepath.Join(installation.PackageDir, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("metadata snapshot copied %s: %v", name, err)
			}
		}
		plan, err := buildInstallPlan("extension:gemini", manager.RuntimeInstallDir, installation)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Executable != executable || len(plan.InstallCommand) != 0 {
			t.Fatalf("host runtime plan = %#v", plan)
		}
		binding, err := manager.ResolveRuntimeForCWD(context.Background(), installation.ID, cwd)
		if err != nil {
			t.Fatalf("resolve bundled runtime without checksum or managed activation: %v", err)
		}
		if len(binding.Command) != 3 || binding.Command[0] != executable || binding.Command[2] != cwd || binding.Version != "0.50.0" {
			t.Fatalf("host runtime binding = %#v", binding)
		}
		if binding.ExecutableIdentity != nil {
			t.Fatalf("bundled runtime unexpectedly requires binary digest: %#v", binding.ExecutableIdentity)
		}
		if _, err := os.Stat(manager.RuntimeInstallDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("bundled resolution created a managed installation: %v", err)
		}
		return plan
	}
	first, err := manager.installLocalPackage("gemini", source)
	if err != nil {
		t.Fatal(err)
	}
	firstPlan := assertReady(first)
	otherPlatform := "darwin-amd64"
	if runtimePlatform() == otherPlatform {
		otherPlatform = "darwin-arm64"
	}
	writeBundledRuntimeFixture(t, filepath.Join(source, "runtime", otherPlatform, "bin", "gemini"))
	second, err := manager.installLocalPackage("gemini", source)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan := assertReady(second)
	if second.Version != first.Version || secondPlan.RuntimeIdentity != firstPlan.RuntimeIdentity {
		t.Fatalf("another platform invalidated the existing runtime: first=%#v second=%#v", firstPlan, secondPlan)
	}
	locale := filepath.Join(source, "locales", "en.json")
	if err := os.WriteFile(locale, []byte(`{"agent.name":"Updated Gemini"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Now().Add(time.Second)
	if err := os.Chtimes(locale, modified, modified); err != nil {
		t.Fatal(err)
	}
	third, err := manager.installLocalPackage("gemini", source)
	if err != nil {
		t.Fatal(err)
	}
	thirdPlan := assertReady(third)
	if third.Version == first.Version || third.DisplayName != "Updated Gemini" || thirdPlan.RuntimeIdentity != firstPlan.RuntimeIdentity {
		t.Fatalf("metadata refresh changed runtime availability: first=%#v third=%#v", first, third)
	}
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResolveRuntimeForCWD(context.Background(), third.ID, cwd); err == nil {
		t.Fatal("missing bundled executable was accepted")
	}
}

func TestLocalPackageLoadDoesNotRequireStoredContentDigest(t *testing.T) {
	source := testResolvedTempDir(t)
	if err := extractPackage(testPackageZIP(t), source); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Installations: agentextensiondata.NewFileInstallationStore(testResolvedTempDir(t))}
	installation, err := manager.installLocalPackage("gemini", source)
	if err != nil {
		t.Fatal(err)
	}
	installation.PackageContentSHA256 = "obsolete-local-digest"
	if err := manager.Installations.PutActive(installation); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.loadInstallationByID(installation.ID); err != nil {
		t.Fatalf("stale local digest blocked a valid metadata snapshot: %v", err)
	}
}

func writeBundledRuntimeFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '0.50.0\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// Grok ships only the extension shell: the manifest still declares a bundled
// runner but runtime/<platform>/ is absent. The user's own install must then be
// discovered through the profile, while a present-but-broken bundle stays
// fail-closed and never silently swaps in a different binary.
func TestBundledRunnerFallsBackToUserRuntimeOnlyWhenBundleIsAbsent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("version probe fixture uses a POSIX shell")
	}
	t.Setenv(bundledRuntimeChannelEnv("gemini"), "")
	newManager := func(home string) *Manager {
		return &Manager{
			Installations:     agentextensiondata.NewFileInstallationStore(testResolvedTempDir(t)),
			RuntimeInstallDir: filepath.Join(testResolvedTempDir(t), "unused-managed-runtime"),
			RuntimeResolver: runtimecmd.Resolver{
				Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} },
				HomeDir: func() (string, error) { return home, nil },
			},
		}
	}
	install := func(t *testing.T, manager *Manager) (Installation, string) {
		t.Helper()
		source := testResolvedTempDir(t)
		manifest := testManifest()
		manifest.Runtime.Install.Runner = "bundled"
		manifest.Runtime.Install.Args = nil
		manifest.Runtime.Launch.Executable = "${installRoot}/bin/gemini"
		manifest.Runtime.Launch.Args = []string{"--acp"}
		metadata := testPackageZIPFor(t, manifest, `{"schemaVersion":"tutti.agent.discovery.v1","candidates":[{"binaryNames":["gemini"],"searchPaths":[{"scope":"user","path":".gemini/bin"}],"version":{"args":["--version"],"constraint":">=0.50.0 <1.0.0"},"launchArgs":["agent","stdio"],"probe":{"kind":"acp-initialize","timeoutMs":5000}}]}`)
		if err := extractPackage(metadata, source); err != nil {
			t.Fatal(err)
		}
		installation, err := manager.installLocalPackage("gemini", source)
		if err != nil {
			t.Fatal(err)
		}
		return installation, source
	}
	writeLocal := func(t *testing.T, home, version string) string {
		t.Helper()
		path := filepath.Join(home, ".gemini", "bin", "gemini")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '"+version+"\\n'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cwd := testResolvedTempDir(t)

	t.Run("absent bundle uses compatible user runtime", func(t *testing.T) {
		home := testResolvedTempDir(t)
		local := writeLocal(t, home, "0.50.0")
		manager := newManager(home)
		installation, _ := install(t, manager)
		binding, err := manager.ResolveRuntimeForCWD(context.Background(), installation.ID, cwd)
		if err != nil {
			t.Fatalf("bundle absent with compatible user runtime: %v", err)
		}
		if len(binding.Command) != 3 || binding.Command[0] != local || binding.Command[1] != "agent" || binding.Command[2] != "stdio" {
			t.Fatalf("binding command = %#v, want user runtime %s with discovery launch args", binding.Command, local)
		}
		if binding.Source != "local" || binding.Version != "0.50.0" {
			t.Fatalf("binding source/version = %q/%q, want local/0.50.0", binding.Source, binding.Version)
		}
	})

	t.Run("absent bundle with incompatible user runtime keeps bundled error", func(t *testing.T) {
		home := testResolvedTempDir(t)
		writeLocal(t, home, "0.10.0")
		manager := newManager(home)
		installation, _ := install(t, manager)
		_, err := manager.ResolveRuntimeForCWD(context.Background(), installation.ID, cwd)
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("err = %v, want the bundled not-exist error", err)
		}
	})

	t.Run("present but broken bundle never falls back", func(t *testing.T) {
		home := testResolvedTempDir(t)
		writeLocal(t, home, "0.50.0")
		manager := newManager(home)
		installation, source := install(t, manager)
		bundled := filepath.Join(source, "runtime", runtimePlatform(), "bin", "gemini")
		if err := os.MkdirAll(filepath.Dir(bundled), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bundled, []byte("not executable"), 0o600); err != nil {
			t.Fatal(err)
		}
		binding, err := manager.ResolveRuntimeForCWD(context.Background(), installation.ID, cwd)
		if err == nil {
			t.Fatalf("broken bundle resolved to %#v; want fail-closed", binding.Command)
		}
	})
}
