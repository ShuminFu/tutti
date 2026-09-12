package agentextension

import (
	"context"
	"errors"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	"testing"
)

type refreshingSetupTargetLookup struct {
	old, current agenttargetbiz.Target
	reads        int
}

func (l *refreshingSetupTargetLookup) GetAgentTarget(context.Context, string) (agenttargetbiz.Target, error) {
	l.reads++
	if l.reads == 1 {
		return l.old, nil
	}
	return l.current, nil
}

func TestAgentTargetSetupRechecksInstallationAfterProbe(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	service, targetID := setupFixture(t, "generic", "Generic Agent", "@example/generic-agent", "1.2.3", "generic-agent", ">=1.2.3 <2.0.0", &fixtureInstallRunner{}, &probeTransport{})
	old, err := service.Plans.Targets.GetAgentTarget(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifest()
	manifest.AgentKey = "generic"
	manifest.Version = "1.0.1"
	manifest.Runtime.Install.Args = []string{"install", "--prefix", "${installRoot}", "@example/generic-agent@1.2.4"}
	manifest.Runtime.Launch.Executable = "${installRoot}/node_modules/.bin/generic-agent"
	discovery := `{"schemaVersion":"tutti.agent.discovery.v1","candidates":[{"binaryNames":["generic-agent"],"version":{"args":["--version"],"constraint":">=1.2.3 <2.0.0"},"launchArgs":["--acp"],"probe":{"kind":"acp-initialize","timeoutMs":5000}}]}`
	installation, err := installTestPackage(t, service.Plans.Manager, Release{AgentKey: "generic", Version: "1.0.1"}, testPackageZIPFor(t, manifest, discovery))
	if err != nil {
		t.Fatal(err)
	}
	current := old
	current.LaunchRefJSON, err = agenttargetbiz.CanonicalLaunchRefJSON(installation.Provider, agenttargetbiz.LaunchRef{Type: agenttargetbiz.LaunchRefTypeAgentExtension, ExtensionInstallationID: installation.ID})
	if err != nil {
		t.Fatal(err)
	}
	lookup := &refreshingSetupTargetLookup{old: old, current: current}
	service.Plans.Targets = lookup
	snapshot, err := service.GetSetup(context.Background(), InstallPlanInput{WorkspaceID: "workspace-1", AgentTargetID: targetID})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != SetupNotInstalled || snapshot.Plan == nil || snapshot.Plan.ExtensionInstallationID != installation.ID || snapshot.Plan.PackageVersion != "1.2.4" {
		t.Fatalf("setup returned stale installation: %#v", snapshot)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	reads := lookup.reads
	if _, err := service.GetSetup(cancelled, InstallPlanInput{WorkspaceID: "workspace-1", AgentTargetID: targetID}); !errors.Is(err, context.Canceled) || lookup.reads != reads {
		t.Fatalf("cancelled setup did more resolution: err=%v reads=%d", err, lookup.reads)
	}
}
