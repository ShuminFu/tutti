package agentextension

import (
	"context"
	"strings"
	"time"

	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
)

type setupDetectionKey struct {
	workspace, target, installation string
}

type setupDetection struct {
	done     chan struct{}
	snapshot SetupSnapshot
	err      error
}

// Share in-flight detection only. A manual retry still probes again; a caller
// leaving the page does not cancel the probe another caller is waiting for.
func (s *SetupService) GetSetup(ctx context.Context, input InstallPlanInput) (SetupSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SetupSnapshot{}, err
	}
	if s.Plans.Targets == nil {
		return s.detectSetup(ctx, input)
	}
	target, err := s.Plans.Targets.GetAgentTarget(ctx, strings.TrimSpace(input.AgentTargetID))
	if err != nil {
		return SetupSnapshot{}, err
	}
	key := setupDetectionKey{workspace: strings.TrimSpace(input.WorkspaceID),
		target: strings.TrimSpace(input.AgentTargetID)}
	// The launch reference includes the immutable installation identity. Use the
	// same authority as the install plan, rather than the display/provider name.
	ref, err := agenttargetbiz.RuntimeProviderTargetRef(target)
	if err != nil {
		return SetupSnapshot{}, err
	}
	key.installation, _ = ref["extensionInstallationId"].(string)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return SetupSnapshot{}, ErrSetupServiceClosed
	}
	if s.detections == nil {
		s.detections = make(map[setupDetectionKey]*setupDetection)
	}
	request := s.detections[key]
	if request == nil {
		request = &setupDetection{done: make(chan struct{})}
		s.detections[key] = request
		s.workers.Add(1)
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
		stopShutdown := func() bool { return false }
		if s.workerCtx != nil {
			stopShutdown = context.AfterFunc(s.workerCtx, cancel)
		}
		go func() {
			defer s.workers.Done()
			defer cancel()
			defer stopShutdown()
			request.snapshot, request.err = s.detectSetup(probeCtx, input)
			s.mu.Lock()
			delete(s.detections, key)
			close(request.done)
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SetupSnapshot{}, ctx.Err()
	case <-request.done:
		return request.snapshot, request.err
	}
}
