package main

import (
	"context"
	"strings"
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
)

// This file covers the runtime-observation hop of resume eligibility for an
// agent-extension session. The extension adapter is dynamic: it is reachable
// only through the controller's adapter resolver, never through the ordinary
// provider map, so Controller.CanResume authorizes such a session purely from
// the observed ProviderTargetRef. Any hop that drops that field reports a live,
// already-answered session as non-resumable while its cold persisted row still
// resumes.

const (
	resumeObservationWorkspaceID       = "workspace-resume-observation"
	resumeObservationAgentSessionID    = "session-resume-observation"
	resumeObservationProvider          = "acp:resume-observation"
	resumeObservationTargetID          = "extension:resume-observation"
	resumeObservationProviderSessionID = "provider-session-resume-observation"
	// The running session keeps the binding it was launched with while the
	// durable target row has already moved to a newer build, so a resumable
	// verdict can only come from the live observation.
	resumeObservationLiveBinding    = "resume-observation@1.0.0"
	resumeObservationStoredBinding  = "resume-observation@0.9.0"
	resumeObservationNestedKey      = "launch"
	resumeObservationNestedLaunchID = "nested-launch-1"
)

func resumeObservationLaunchRef(installationID string) map[string]any {
	return map[string]any{
		"kind":                    agenttargetbiz.LaunchRefTypeAgentExtension,
		"provider":                resumeObservationProvider,
		"targetId":                resumeObservationTargetID,
		"extensionInstallationId": installationID,
		"metadata": map[string]any{
			resumeObservationNestedKey: resumeObservationNestedLaunchID,
		},
	}
}

type resumeObservationAdapter struct{}

func (resumeObservationAdapter) Provider() string { return resumeObservationProvider }

func (resumeObservationAdapter) Start(_ context.Context, session agentruntime.Session) ([]activityshared.Event, error) {
	return []activityshared.Event{activityshared.NewSessionStarted(activityshared.EventContext{
		EventID:            "resume-observation-started",
		Provider:           activityshared.Provider(session.Provider),
		ProviderSessionID:  resumeObservationProviderSessionID,
		AgentSessionID:     session.AgentSessionID,
		SessionKind:        "root",
		RootAgentSessionID: session.AgentSessionID,
	})}, nil
}

func (resumeObservationAdapter) Resume(context.Context, agentruntime.Session) error { return nil }

func (resumeObservationAdapter) Close(context.Context, agentruntime.Session) error { return nil }

func (resumeObservationAdapter) Exec(
	context.Context,
	agentruntime.Session,
	[]agentruntime.PromptContentBlock,
	string,
	string,
	agentruntime.EventSink,
	agentruntime.CommandSnapshotSink,
) ([]activityshared.Event, error) {
	return nil, nil
}

func (resumeObservationAdapter) Cancel(context.Context, agentruntime.Session, string) ([]activityshared.Event, error) {
	return nil, nil
}

func (resumeObservationAdapter) CanResume(session agentruntime.Session) bool {
	return strings.TrimSpace(session.ProviderSessionID) != ""
}

type resumeObservationResolver struct{ adapter agentruntime.Adapter }

func (r resumeObservationResolver) ResolveAdapter(
	context.Context,
	agentruntime.AdapterResolveInput,
) (agentruntime.Adapter, error) {
	return r.adapter, nil
}

// resumeObservationRecorder is the runtime port handed to the agent service. It
// records the eligibility input the service actually produced before delegating
// the verdict to the real tuttid runtime adapter.
type resumeObservationRecorder struct {
	agentRuntimeAdapter
	inputs []agentservice.RuntimeResumeInput
}

func (r *resumeObservationRecorder) CanResume(input agentservice.RuntimeResumeInput) bool {
	r.inputs = append(r.inputs, input)
	return r.agentRuntimeAdapter.CanResume(input)
}

func (r *resumeObservationRecorder) liveBindingInputs() []agentservice.RuntimeResumeInput {
	result := make([]agentservice.RuntimeResumeInput, 0, len(r.inputs))
	for _, input := range r.inputs {
		if input.ProviderTargetRef["extensionInstallationId"] == resumeObservationLiveBinding {
			result = append(result, input)
		}
	}
	return result
}

type resumeObservationSessions struct {
	sessions map[string]agentservice.PersistedSession
}

func (r resumeObservationSessions) GetSession(workspaceID, agentSessionID string) (agentservice.PersistedSession, bool) {
	session, found := r.sessions[workspaceID+":"+agentSessionID]
	return session, found
}

func (r resumeObservationSessions) ListSessions(workspaceID string) ([]agentservice.PersistedSession, bool) {
	result := make([]agentservice.PersistedSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		if session.WorkspaceID == workspaceID {
			result = append(result, session)
		}
	}
	return result, true
}

func (resumeObservationSessions) SessionDeleted(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r resumeObservationSessions) ListSessionsPage(
	_ context.Context,
	input agentactivitybiz.ListSessionsPageInput,
) (agentservice.PersistedSessionListPage, bool, error) {
	result := make([]agentservice.PersistedSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		if session.WorkspaceID != input.WorkspaceID {
			continue
		}
		if targetID := strings.TrimSpace(input.AgentTargetID); targetID != "" && session.AgentTargetID != targetID {
			continue
		}
		result = append(result, session)
	}
	return agentservice.PersistedSessionListPage{Sessions: result}, true, nil
}

type resumeObservationTargets map[string]agenttargetbiz.Target

func (s resumeObservationTargets) GetAgentTarget(_ context.Context, id string) (agenttargetbiz.Target, error) {
	target, found := s[strings.TrimSpace(id)]
	if !found {
		return agenttargetbiz.Target{}, workspacedata.ErrAgentTargetNotFound
	}
	return target, nil
}

func resumeObservationPersistedSession() agentservice.PersistedSession {
	return agentservice.PersistedSession{
		ID:                resumeObservationAgentSessionID,
		WorkspaceID:       resumeObservationWorkspaceID,
		Kind:              agentactivitybiz.SessionKindRoot,
		AgentTargetID:     resumeObservationTargetID,
		Provider:          resumeObservationProvider,
		ProviderSessionID: resumeObservationProviderSessionID,
		RailSectionKey:    "conversations",
		Metadata:          agentactivitybiz.SessionMetadata{Visible: true},
		CreatedAtUnixMS:   1,
		UpdatedAtUnixMS:   2,
		LastEventUnixMS:   2,
	}
}

// startResumeObservationSession registers one live agent-extension session
// through the real tuttid runtime adapter.
func startResumeObservationSession(t *testing.T, adapter agentRuntimeAdapter) {
	t.Helper()
	started, err := adapter.Start(context.Background(), agentservice.RuntimeStartInput{
		WorkspaceID:       resumeObservationWorkspaceID,
		AgentSessionID:    resumeObservationAgentSessionID,
		AgentTargetID:     resumeObservationTargetID,
		Provider:          resumeObservationProvider,
		Title:             "Resume observation",
		ProviderTargetRef: resumeObservationLaunchRef(resumeObservationLiveBinding),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started.Session.ProviderSessionID != resumeObservationProviderSessionID {
		t.Fatalf("live provider session id = %q, want %q", started.Session.ProviderSessionID, resumeObservationProviderSessionID)
	}
}

func newResumeObservationChain() (*agentruntime.Controller, *resumeObservationRecorder) {
	controller := agentruntime.NewControllerWithAdapterResolver(
		nil,
		nil,
		resumeObservationResolver{adapter: resumeObservationAdapter{}},
	)
	recorder := &resumeObservationRecorder{agentRuntimeAdapter: newAgentRuntimeAdapter(controller)}
	return controller, recorder
}

// The observation hop itself must carry the fixed launch binding and own its
// copy, so a later mutation cannot rewrite whatever the service decides from.
func TestRuntimeObservationKeepsExtensionTargetBinding(t *testing.T) {
	controller, recorder := newResumeObservationChain()
	startResumeObservationSession(t, recorder.agentRuntimeAdapter)

	observation, found := recorder.Session(resumeObservationWorkspaceID, resumeObservationAgentSessionID)
	if !found {
		t.Fatal("Session() found = false, want the live registered session")
	}
	if observation.ProviderTargetRef["kind"] != agenttargetbiz.LaunchRefTypeAgentExtension ||
		observation.ProviderTargetRef["provider"] != resumeObservationProvider ||
		observation.ProviderTargetRef["targetId"] != resumeObservationTargetID ||
		observation.ProviderTargetRef["extensionInstallationId"] != resumeObservationLiveBinding {
		t.Fatalf("observed provider target ref = %#v, want the live extension binding", observation.ProviderTargetRef)
	}

	sessions := recorder.Sessions(resumeObservationWorkspaceID)
	if len(sessions) != 1 || sessions[0].ProviderTargetRef["extensionInstallationId"] != resumeObservationLiveBinding {
		t.Fatalf("Sessions() = %#v, want one session carrying the live extension binding", sessions)
	}

	// The observation must not alias the runtime registry, including nested
	// values: mutating it may neither rewrite the registry copy nor a later
	// observation.
	observation.ProviderTargetRef["extensionInstallationId"] = "mutated-observation"
	observation.ProviderTargetRef["metadata"].(map[string]any)[resumeObservationNestedKey] = "mutated-observation"
	reobserved, found := recorder.Session(resumeObservationWorkspaceID, resumeObservationAgentSessionID)
	if !found {
		t.Fatal("Session() found = false after mutation, want the live registered session")
	}
	if reobserved.ProviderTargetRef["extensionInstallationId"] != resumeObservationLiveBinding {
		t.Fatalf("re-observed binding = %#v, want the observed binding; the observation aliases the registry", reobserved.ProviderTargetRef["extensionInstallationId"])
	}
	if nested, ok := reobserved.ProviderTargetRef["metadata"].(map[string]any); !ok || nested[resumeObservationNestedKey] != resumeObservationNestedLaunchID {
		t.Fatalf("re-observed nested metadata = %#v, want the observed binding", reobserved.ProviderTargetRef["metadata"])
	}
	registrySession, found := controller.Session(resumeObservationWorkspaceID, resumeObservationAgentSessionID)
	if !found {
		t.Fatal("controller Session() found = false, want the registered session")
	}
	if registrySession.ProviderTargetRef["extensionInstallationId"] != resumeObservationLiveBinding {
		t.Fatalf("registry binding = %#v, want it untouched by the observation", registrySession.ProviderTargetRef["extensionInstallationId"])
	}
}

// End-to-end: real runtime registry -> real tuttid runtime adapter -> agent
// service workspace list -> resume eligibility. A live extension session must
// be reported resumable, and the eligibility input must carry the binding the
// session is actually running with.
func TestWorkspaceListKeepsLiveExtensionSessionResumable(t *testing.T) {
	_, recorder := newResumeObservationChain()
	startResumeObservationSession(t, recorder.agentRuntimeAdapter)

	service := agentservice.NewService(recorder)
	service.AgentTargetStore = resumeObservationTargets{
		resumeObservationTargetID: {
			ID:            resumeObservationTargetID,
			Provider:      resumeObservationProvider,
			LaunchRefJSON: `{"type":"` + agenttargetbiz.LaunchRefTypeAgentExtension + `","extensionInstallationId":"` + resumeObservationStoredBinding + `"}`,
			Name:          "Resume observation",
			Enabled:       true,
			Source:        agenttargetbiz.SourceUser,
		},
	}
	service.SessionReader = resumeObservationSessions{sessions: map[string]agentservice.PersistedSession{
		resumeObservationWorkspaceID + ":" + resumeObservationAgentSessionID: resumeObservationPersistedSession(),
	}}

	sessions, err := service.List(context.Background(), resumeObservationWorkspaceID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("List() returned %d sessions, want 1", len(sessions))
	}
	if !sessions[0].Resumable {
		t.Fatalf("session = %#v, want a resumable live extension session", sessions[0])
	}
	liveInputs := recorder.liveBindingInputs()
	if len(liveInputs) != 1 {
		t.Fatalf("resume eligibility inputs carrying the live binding = %d, want 1 (inputs: %#v)", len(liveInputs), recorder.inputs)
	}
	input := liveInputs[0]
	if input.AgentTargetID != resumeObservationTargetID ||
		input.Provider != resumeObservationProvider ||
		input.ProviderSessionID != resumeObservationProviderSessionID {
		t.Fatalf("resume eligibility input = %#v, want the live extension session identity", input)
	}
}
