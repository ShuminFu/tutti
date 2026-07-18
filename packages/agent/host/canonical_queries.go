package agenthost

import (
	"context"
	"strings"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

// GetTurn exposes canonical turn truth without requiring Host consumers to
// retain or type-assert the concrete store used by the Host adapter.
func (h *Host) GetTurn(ctx context.Context, ref SessionRef, turnID string) (storesqlite.Turn, bool, error) {
	ref.WorkspaceID = strings.TrimSpace(ref.WorkspaceID)
	ref.AgentSessionID = strings.TrimSpace(ref.AgentSessionID)
	turnID = strings.TrimSpace(turnID)
	if h == nil || h.store == nil || ref.WorkspaceID == "" || ref.AgentSessionID == "" || turnID == "" {
		return storesqlite.Turn{}, false, ErrInvalidArgument
	}
	return h.store.GetTurn(ctx, ref.WorkspaceID, ref.AgentSessionID, turnID)
}

// FindTurnByClientSubmitID exposes the canonical idempotency lookup without
// requiring callers to depend on a concrete SQLite store.
func (h *Host) FindTurnByClientSubmitID(ctx context.Context, ref SessionRef, clientSubmitID string) (string, bool, error) {
	ref.WorkspaceID = strings.TrimSpace(ref.WorkspaceID)
	ref.AgentSessionID = strings.TrimSpace(ref.AgentSessionID)
	clientSubmitID = strings.TrimSpace(clientSubmitID)
	if h == nil || h.store == nil || ref.WorkspaceID == "" || ref.AgentSessionID == "" || clientSubmitID == "" {
		return "", false, ErrInvalidArgument
	}
	return h.store.FindTurnByClientSubmitID(ctx, ref.WorkspaceID, ref.AgentSessionID, clientSubmitID)
}
