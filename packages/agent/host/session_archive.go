package agenthost

import (
	"context"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

type SessionArchiveStore interface {
	UpdateSessionArchived(context.Context, string, string, bool, int64) (storesqlite.Session, bool, error)
}

type UpdateArchiveInput struct {
	WorkspaceID    string
	AgentSessionID string
	Archived       bool
}

func (h *Host) UpdateArchive(ctx context.Context, input UpdateArchiveInput) (GetSessionResult, error) {
	ref := normalizedSessionRef(SessionRef{WorkspaceID: input.WorkspaceID, AgentSessionID: input.AgentSessionID})
	if h == nil || h.runtime == nil || ref.WorkspaceID == "" || ref.AgentSessionID == "" {
		return GetSessionResult{}, ErrInvalidArgument
	}
	store, ok := h.sessionManagement.(SessionArchiveStore)
	if !ok {
		return GetSessionResult{}, ErrInvalidArgument
	}
	release, err := h.acquireSession(ctx, ref)
	if err != nil {
		return GetSessionResult{}, err
	}
	defer release()
	canonical, found, err := store.UpdateSessionArchived(ctx, ref.WorkspaceID, ref.AgentSessionID, input.Archived, h.now().UnixMilli())
	if err != nil {
		return GetSessionResult{}, err
	}
	if !found {
		return GetSessionResult{}, ErrSessionNotFound
	}
	live, liveFound := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID)
	return GetSessionResult{Canonical: canonical, Session: live, Live: liveFound}, nil
}

func (s *SQLiteWorkspaceStore) UpdateSessionArchived(ctx context.Context, workspaceID, sessionID string, archived bool, now int64) (storesqlite.Session, bool, error) {
	store, err := s.store(workspaceID)
	if err != nil {
		return storesqlite.Session{}, false, err
	}
	session, found, err := store.UpdateSessionArchived(ctx, workspaceID, sessionID, archived, now)
	if err == nil && found && session.CommitTransactionID != "" {
		NotifyCommitted(ctx, s.Observer, CanonicalDelta(session.CommitDelta))
	}
	return session, found, err
}
