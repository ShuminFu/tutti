package storesqlite

import (
	"context"
	"errors"
	"strings"
)

const ArchivedSessionPageKey = "archive"

// UpdateSessionArchived changes only root organization metadata. A retry is a
// no-op, including its timestamp and canonical version.
func (s *Store) UpdateSessionArchived(ctx context.Context, workspaceID, sessionID string, archived bool, now int64) (Session, bool, error) {
	if s == nil || s.db == nil {
		return Session{}, false, errors.New("workspace database is not initialized")
	}
	workspaceID, sessionID = strings.TrimSpace(workspaceID), strings.TrimSpace(sessionID)
	if workspaceID == "" || sessionID == "" || now <= 0 {
		return Session{}, false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	session, found, err := getAgentSessionForUpdate(ctx, tx, workspaceID, sessionID)
	if err != nil || !found || session.DeletedAtUnixMS > 0 || session.Kind != SessionKindRoot {
		return Session{}, false, err
	}
	mutations := []TransactionMutation{}
	if (session.ArchivedAtUnixMS > 0) != archived {
		archivedAt := int64(0)
		if archived {
			archivedAt = now
		}
		version := max(session.UpdatedAtUnixMS+1, now)
		_, err = tx.ExecContext(ctx, `UPDATE workspace_agent_sessions
SET session_metadata_json = json_set(session_metadata_json, '$.archivedAtUnixMs', ?),
    updated_at_unix_ms = ?
WHERE workspace_id = ? AND agent_session_id = ? AND session_kind = 'root' AND deleted_at_unix_ms = 0`, archivedAt, version, workspaceID, sessionID)
		if err != nil {
			return Session{}, false, err
		}
		mutations = append(mutations, transactionMutation(workspaceID, sessionID, MutationEntitySession, sessionID, "upsert", version))
	}
	delta, err := s.commitTransaction(ctx, tx, workspaceID, mutations)
	if err != nil {
		return Session{}, false, err
	}
	result, ok, err := s.GetSession(ctx, workspaceID, sessionID)
	result.CommitTransactionID, result.CommitDelta = delta.TransactionID, delta
	return result, ok, err
}
