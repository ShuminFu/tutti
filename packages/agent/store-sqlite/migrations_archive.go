package storesqlite

import "context"

func (s *Store) applySessionArchiveIndex(ctx context.Context) error {
	const migration = "workspace_agent_session_archive_index_v1"
	applied, err := s.hasMigration(ctx, migration)
	if err != nil || applied {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_workspace_agent_sessions_archive_page
ON workspace_agent_sessions(workspace_id, COALESCE(json_extract(session_metadata_json, '$.archivedAtUnixMs'), 0) DESC, agent_session_id ASC)
WHERE session_kind = 'root' AND deleted_at_unix_ms = 0
  AND json_extract(session_metadata_json, '$.visible') IS NOT 0
  AND COALESCE(json_extract(session_metadata_json, '$.archivedAtUnixMs'), 0) > 0;
`)
	if err != nil {
		return err
	}
	return s.recordMigration(ctx, migration)
}
