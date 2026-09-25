package agent

import (
	"context"
	"strings"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
)

func sendNotStartedKey(workspaceID, agentSessionID string) string {
	return strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(agentSessionID)
}

func (s *Service) noteSendDidNotStart(workspaceID, agentSessionID string) {
	if s == nil {
		return
	}
	s.sendNotStarted.Store(sendNotStartedKey(workspaceID, agentSessionID), struct{}{})
}

func (s *Service) clearSendDidNotStart(workspaceID, agentSessionID string) {
	if s == nil {
		return
	}
	s.sendNotStarted.Delete(sendNotStartedKey(workspaceID, agentSessionID))
}

func (s *Service) consumeSendDidNotStart(workspaceID, agentSessionID string) bool {
	if s == nil {
		return false
	}
	_, loaded := s.sendNotStarted.LoadAndDelete(sendNotStartedKey(workspaceID, agentSessionID))
	return loaded
}

func (s *Service) applicationHostIfReady() *agenthost.Host {
	if s == nil {
		return nil
	}
	s.applicationHostMu.Lock()
	provider := s.applicationHostProvider
	host := s.applicationHost
	s.applicationHostMu.Unlock()
	if provider != nil {
		return provider()
	}
	return host
}

func (s *Service) codexDesktopHold(workspaceID, agentSessionID, providerSessionID string) *CodexDesktopHold {
	if s == nil {
		return nil
	}
	if s.codexDesktopHoldLookup != nil {
		return s.codexDesktopHoldLookup(workspaceID, agentSessionID)
	}
	host := s.applicationHostIfReady()
	if host == nil {
		return nil
	}
	state := host.CodexDesktopHold(workspaceID, agentSessionID)
	if state.QueuedCount == 0 && !state.Held {
		return nil
	}
	id := strings.TrimSpace(state.ProviderSessionID)
	if id == "" {
		id = strings.TrimSpace(providerSessionID)
	}
	openURL := strings.TrimSpace(state.OpenURL)
	if openURL == "" {
		openURL = "codex://threads/" + id
	}
	if id == "" {
		openURL = ""
	}
	return &CodexDesktopHold{
		Held:              true,
		ReasonCode:        agenthost.CodexThreadHeldExternallyReason,
		QueuedCount:       state.QueuedCount,
		ProviderSessionID: id,
		OpenURL:           openURL,
	}
}

func codexDesktopHoldQueued(session Session) bool {
	return session.CodexDesktopHold != nil && session.CodexDesktopHold.QueuedCount > 0
}

func (s *Service) RetryCodexDesktopHold(ctx context.Context, workspaceID, agentSessionID string) (Session, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	agentSessionID = strings.TrimSpace(agentSessionID)
	if workspaceID == "" || agentSessionID == "" {
		return Session{}, ErrInvalidArgument
	}
	if err := s.ApplicationHost().RetryCodexDesktopHold(ctx, agenthost.SessionRef{
		WorkspaceID: workspaceID, AgentSessionID: agentSessionID,
	}); err != nil {
		return Session{}, err
	}
	s.clearSendDidNotStart(workspaceID, agentSessionID)
	return s.Get(ctx, workspaceID, agentSessionID)
}
