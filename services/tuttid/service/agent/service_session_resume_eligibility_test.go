package agent

import (
	"context"
	"testing"

	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
)

const (
	extensionResumeWorkspaceID       = "workspace-1"
	extensionResumeAgentSessionID    = "session-1"
	extensionResumeTargetID          = "extension:codebuddy"
	extensionResumeProvider          = "acp:codebuddy"
	extensionResumeProviderSessionID = "provider-session-1"
	// A running session keeps the binding it was launched with, while the
	// installed target row may already have moved to a newer extension build.
	// The live observation must win for a session that is still running.
	extensionResumeLiveBinding = "codebuddy@1.0.0"
	// The durable target row deliberately points at a different build so a
	// resumable verdict can only come from the live runtime observation.
	extensionResumePersistedBinding = "codebuddy@0.9.0"
)

func extensionResumeTargetRef(installationID string) map[string]any {
	return map[string]any{
		"kind":                    agenttargetbiz.LaunchRefTypeAgentExtension,
		"provider":                extensionResumeProvider,
		"targetId":                extensionResumeTargetID,
		"extensionInstallationId": installationID,
	}
}

func extensionResumeLaunchRefJSON(installationID string) string {
	return `{"type":"` + agenttargetbiz.LaunchRefTypeAgentExtension +
		`","extensionInstallationId":"` + installationID + `"}`
}

// liveExtensionResumeService seeds one live agent-extension session: a persisted
// canonical row, a target row whose binding differs from the running session,
// and the live runtime observation that carries the fixed launch binding.
//
// CanResume only authorizes the exact binding the live session is running with,
// which mirrors Controller.CanResume requiring the observed ProviderTargetRef.
// A live session that loses that binding during observation therefore reports
// resumable=false even though runtimeLive=true.
func liveExtensionResumeService(runtime *fakeRuntime) *Service {
	service := newIsolatedAgentService(runtime)
	service.AgentTargetStore = fakeAgentTargetStore{targets: map[string]agenttargetbiz.Target{
		extensionResumeTargetID: {
			ID:            extensionResumeTargetID,
			Provider:      extensionResumeProvider,
			LaunchRefJSON: extensionResumeLaunchRefJSON(extensionResumePersistedBinding),
			Name:          "CodeBuddy",
			Enabled:       true,
			Source:        agenttargetbiz.SourceUser,
		},
	}}
	service.SessionReader = fakeSessionReader{sessions: map[string]PersistedSession{
		extensionResumeWorkspaceID + ":" + extensionResumeAgentSessionID: {
			ID:                extensionResumeAgentSessionID,
			WorkspaceID:       extensionResumeWorkspaceID,
			Kind:              agentactivitybiz.SessionKindRoot,
			AgentTargetID:     extensionResumeTargetID,
			Provider:          extensionResumeProvider,
			ProviderSessionID: extensionResumeProviderSessionID,
			RailSectionKey:    "conversations",
			Metadata:          agentactivitybiz.SessionMetadata{Visible: true},
			CreatedAtUnixMS:   1,
			UpdatedAtUnixMS:   2,
			LastEventUnixMS:   2,
		},
	}}
	runtime.sessions[extensionResumeWorkspaceID+":"+extensionResumeAgentSessionID] = ProviderRuntimeSession{
		ID:                extensionResumeAgentSessionID,
		WorkspaceID:       extensionResumeWorkspaceID,
		AgentTargetID:     extensionResumeTargetID,
		Provider:          extensionResumeProvider,
		ProviderSessionID: extensionResumeProviderSessionID,
		ProviderTargetRef: extensionResumeTargetRef(extensionResumeLiveBinding),
		Status:            "ready",
		Resumable:         true,
		Visible:           true,
		CreatedAtUnixMS:   1,
		UpdatedAtUnixMS:   2,
	}
	runtime.canResumeHook = func(input RuntimeResumeInput) bool {
		return input.ProviderTargetRef["extensionInstallationId"] == extensionResumeLiveBinding
	}
	return service
}

func recordedLiveBindingCalls(runtime *fakeRuntime) []RuntimeResumeInput {
	result := make([]RuntimeResumeInput, 0, len(runtime.canResumeCalls))
	for _, input := range runtime.canResumeCalls {
		if input.ProviderTargetRef["extensionInstallationId"] == extensionResumeLiveBinding {
			result = append(result, input)
		}
	}
	return result
}

func assertExtensionResumeInputCarriesLiveBinding(t *testing.T, input RuntimeResumeInput) {
	t.Helper()
	ref := input.ProviderTargetRef
	if ref["kind"] != agenttargetbiz.LaunchRefTypeAgentExtension ||
		ref["provider"] != extensionResumeProvider ||
		ref["targetId"] != extensionResumeTargetID ||
		ref["extensionInstallationId"] != extensionResumeLiveBinding {
		t.Fatalf("resume input provider target ref = %#v, want the live extension binding", ref)
	}
	if input.AgentTargetID != extensionResumeTargetID ||
		input.Provider != extensionResumeProvider ||
		input.ProviderSessionID != extensionResumeProviderSessionID {
		t.Fatalf("resume input = %#v, want live extension session identity", input)
	}
}

// A live extension session must stay resumable in the workspace list. This is
// the reported symptom: runtimeLive=true while resumable=false, even though the
// session is running and the cold persisted row still resumes.
func TestServiceListKeepsLiveExtensionSessionResumable(t *testing.T) {
	runtime := newFakeRuntime()
	service := liveExtensionResumeService(runtime)

	sessions, err := service.List(context.Background(), extensionResumeWorkspaceID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("List() returned %d sessions, want 1", len(sessions))
	}
	if !sessions[0].RuntimeLive {
		t.Fatalf("session runtimeLive = false, want a live provider process")
	}
	if !sessions[0].Resumable {
		t.Fatalf("session = %#v, want a resumable live extension session", sessions[0])
	}
	liveCalls := recordedLiveBindingCalls(runtime)
	if len(liveCalls) != 1 {
		t.Fatalf("CanResume calls carrying the live binding = %d, want 1 (calls: %#v)", len(liveCalls), runtime.canResumeCalls)
	}
	assertExtensionResumeInputCarriesLiveBinding(t, liveCalls[0])
}

// The detail projection must reach the same verdict for the same live session.
func TestServiceGetKeepsLiveExtensionSessionResumable(t *testing.T) {
	runtime := newFakeRuntime()
	service := liveExtensionResumeService(runtime)

	session, err := service.Get(context.Background(), extensionResumeWorkspaceID, extensionResumeAgentSessionID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !session.RuntimeLive {
		t.Fatalf("session runtimeLive = false, want a live provider process")
	}
	if !session.Resumable {
		t.Fatalf("session = %#v, want a resumable live extension session", session)
	}
	liveCalls := recordedLiveBindingCalls(runtime)
	if len(liveCalls) != 1 {
		t.Fatalf("CanResume calls carrying the live binding = %d, want 1 (calls: %#v)", len(liveCalls), runtime.canResumeCalls)
	}
	assertExtensionResumeInputCarriesLiveBinding(t, liveCalls[0])
}

// The observation that feeds resume eligibility owns its binding: neither a
// later mutation of the runtime observation nor a mutation of the eligibility
// input may leak into the other.
func TestRuntimeResumeInputFromRuntimeSessionClonesProviderTargetRef(t *testing.T) {
	observation := ProviderRuntimeSession{
		ID:                extensionResumeAgentSessionID,
		WorkspaceID:       extensionResumeWorkspaceID,
		AgentTargetID:     extensionResumeTargetID,
		Provider:          extensionResumeProvider,
		ProviderSessionID: extensionResumeProviderSessionID,
		ProviderTargetRef: map[string]any{
			"kind":                    agenttargetbiz.LaunchRefTypeAgentExtension,
			"provider":                extensionResumeProvider,
			"targetId":                extensionResumeTargetID,
			"extensionInstallationId": extensionResumeLiveBinding,
			"nested":                  map[string]any{"binding": extensionResumeLiveBinding},
		},
	}

	input := runtimeResumeInputFromRuntimeSession(observation)

	observation.ProviderTargetRef["extensionInstallationId"] = "mutated-observation"
	observation.ProviderTargetRef["nested"].(map[string]any)["binding"] = "mutated-observation"
	if input.ProviderTargetRef["extensionInstallationId"] != extensionResumeLiveBinding {
		t.Fatalf("eligibility input binding = %#v, want the observed binding", input.ProviderTargetRef["extensionInstallationId"])
	}
	if nested, ok := input.ProviderTargetRef["nested"].(map[string]any); !ok || nested["binding"] != extensionResumeLiveBinding {
		t.Fatalf("eligibility input nested binding = %#v, want the observed binding", input.ProviderTargetRef["nested"])
	}

	input.ProviderTargetRef["targetId"] = "mutated-input"
	if observation.ProviderTargetRef["targetId"] != extensionResumeTargetID {
		t.Fatalf("runtime observation target = %#v, want it untouched by the eligibility input", observation.ProviderTargetRef["targetId"])
	}
}

func TestServiceListRebuildsExtensionTargetRefForPersistedSessionResume(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.canResumeHook = func(input RuntimeResumeInput) bool {
		return input.ProviderTargetRef["kind"] == agenttargetbiz.LaunchRefTypeAgentExtension
	}
	service := newIsolatedAgentService(runtime)
	service.AgentTargetStore = fakeAgentTargetStore{targets: map[string]agenttargetbiz.Target{
		extensionResumeTargetID: {
			ID:            extensionResumeTargetID,
			Provider:      extensionResumeProvider,
			LaunchRefJSON: extensionResumeLaunchRefJSON(extensionResumeLiveBinding),
			Name:          "CodeBuddy",
			Enabled:       true,
			Source:        agenttargetbiz.SourceUser,
		},
	}}
	service.SessionReader = fakeSessionReader{sessions: map[string]PersistedSession{
		extensionResumeWorkspaceID + ":" + extensionResumeAgentSessionID: {
			ID:                extensionResumeAgentSessionID,
			WorkspaceID:       extensionResumeWorkspaceID,
			Kind:              agentactivitybiz.SessionKindRoot,
			AgentTargetID:     extensionResumeTargetID,
			Provider:          extensionResumeProvider,
			ProviderSessionID: extensionResumeProviderSessionID,
			RailSectionKey:    "conversations",
			Metadata:          agentactivitybiz.SessionMetadata{Visible: true},
		},
	}}

	sessions, err := service.List(context.Background(), extensionResumeWorkspaceID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Resumable {
		t.Fatalf("sessions = %#v, want one resumable persisted extension session", sessions)
	}
	if len(runtime.canResumeCalls) != 1 {
		t.Fatalf("CanResume calls = %d, want 1", len(runtime.canResumeCalls))
	}
	input := runtime.canResumeCalls[0]
	if input.ProviderTargetRef["provider"] != extensionResumeProvider ||
		input.ProviderTargetRef["targetId"] != extensionResumeTargetID ||
		input.ProviderTargetRef["extensionInstallationId"] != extensionResumeLiveBinding {
		t.Fatalf("provider target ref = %#v, want fixed CodeBuddy installation binding", input.ProviderTargetRef)
	}
}
