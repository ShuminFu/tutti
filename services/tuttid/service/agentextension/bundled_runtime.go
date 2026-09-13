package agentextension

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxBundledRuntimeFiles           = 60_000
	maxBundledRuntimeBytes           = int64(1 << 30)
	deepSeekHarnessRuntimeChannelEnv = "TUTTI_AGENT_EXTENSION_DEEPSEEK_HARNESS_RUNTIME_CHANNEL"
)

// A host bundle is already installed. Resolve its platform directory directly;
// unrelated extension files and other architectures never decide availability.
func buildBundledRuntimePlan(targetID string, installation Installation) (InstallPlan, error) {
	if !installation.HasLocalPackageProvenance() {
		// This descriptor is internal: bundled setup never publishes an install plan.
		// Its token only scopes setup actions; it does not attest runtime contents.
		return InstallPlan{}, errors.New("bundled runtime requires a trusted local extension package")
	}
	packageRoot := installation.RuntimePackageDir
	if packageRoot == "" {
		packageRoot = installation.PackageDir
	}
	channel := "runtime"
	packageName := selectedBundledRuntimePackageName(installation)
	if packageName == "bundled-runtime-previous" {
		channel = "runtime-previous"
	}
	platform := runtimePlatform()
	root := filepath.Join(packageRoot, channel, platform)
	executable := runtimeLaunchExecutable(installation.Manifest, root, platform)
	if executable == root || !pathWithin(executable, root) {
		return InstallPlan{}, errors.New("bundled runtime executable escapes platform directory")
	}
	return InstallPlan{
		AgentTargetID: targetID, ExtensionInstallationID: installation.ID,
		AgentKey: installation.AgentKey, ExtensionVersion: installation.Version,
		RuntimeKind: installation.Manifest.Runtime.Kind, Platform: platform,
		Runner: "bundled", PackageName: packageName, PackageVersion: installation.Manifest.Version,
		RuntimeIdentity: "bundled-" + channel + "-" + platform,
		InstallRoot:     root, Executable: executable,
		LaunchArgs: resolveRuntimeArguments(installation.Manifest.Runtime.Launch.Args, "", root),
		PlanDigest: "bundled:" + installation.ID + ":" + channel + ":" + platform,
	}, nil
}

func (m *Manager) resolveBundledRuntime(ctx context.Context, installation Installation, profile DiscoveryProfile, cwd string) (RuntimeBinding, error) {
	plan, err := buildBundledRuntimePlan(targetID(installation.AgentKey), installation)
	if err != nil {
		return RuntimeBinding{}, err
	}
	info, err := os.Lstat(plan.Executable)
	if err != nil {
		return RuntimeBinding{}, fmt.Errorf("open bundled runtime: %w", err)
	}
	if !isExecutableFileInfo(info) {
		return RuntimeBinding{}, errors.New("bundled runtime is not an executable file")
	}
	root, err := filepath.EvalSymlinks(plan.InstallRoot)
	if err != nil {
		return RuntimeBinding{}, err
	}
	executable, err := filepath.EvalSymlinks(plan.Executable)
	if err != nil || !pathWithin(executable, root) {
		return RuntimeBinding{}, errors.New("bundled runtime executable escapes platform directory")
	}
	var probeErr error
	for _, candidate := range profile.Candidates {
		version, err := m.runtimeVersionWithEnv(ctx, executable, candidate.Version.Args, candidate.Version.Constraint, m.RuntimeResolver.Env(nil))
		if err != nil {
			probeErr = err
			continue
		}
		args := resolveRuntimeArguments(installation.Manifest.Runtime.Launch.Args, cwd, plan.InstallRoot)
		return m.runtimeBinding(installation, append([]string{executable}, args...), version, "managed")
	}
	if probeErr != nil {
		return RuntimeBinding{}, fmt.Errorf("bundled runtime version probe failed: %w", probeErr)
	}
	return RuntimeBinding{}, errors.New("bundled runtime discovery profile has no candidates")
}
