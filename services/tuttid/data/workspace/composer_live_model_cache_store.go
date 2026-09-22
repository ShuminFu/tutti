package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ComposerLiveModelCacheRow is the durable composer model list for one
// live-model scope key. ModelsJSON is a JSON array of composer option values.
type ComposerLiveModelCacheRow struct {
	ScopeKey        string
	Provider        string
	ModelsJSON      string
	FetchedAtUnixMS int64
	LastError       string
}

func (s *SQLiteStore) applyComposerLiveModelCacheV1(ctx context.Context) error {
	applied, err := s.hasMigration(ctx, schemaMigrationComposerLiveModelCacheV1)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	now := unixMs(time.Now().UTC())
	_, err = s.writeDB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS composer_live_model_cache (
  scope_key TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  models_json TEXT NOT NULL,
  fetched_at_unix_ms INTEGER NOT NULL,
  last_error TEXT NOT NULL DEFAULT ''
);
INSERT INTO tuttid_schema_migrations (id, applied_at_unix_ms)
  VALUES (?, ?);
`, schemaMigrationComposerLiveModelCacheV1, now)
	if err != nil {
		return fmt.Errorf("migrate workspace database for composer live model cache: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetComposerLiveModelCache(
	ctx context.Context,
	scopeKey string,
) (ComposerLiveModelCacheRow, bool, error) {
	if s == nil || s.writeDB == nil {
		return ComposerLiveModelCacheRow{}, false, errors.New("workspace database is not initialized")
	}
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" {
		return ComposerLiveModelCacheRow{}, false, nil
	}
	queryer := s.writeDB
	if s.readDB != nil {
		queryer = s.readDB
	}
	row := queryer.QueryRowContext(ctx, `
SELECT scope_key, provider, models_json, fetched_at_unix_ms, last_error
FROM composer_live_model_cache
WHERE scope_key = ?
`, scopeKey)
	var item ComposerLiveModelCacheRow
	if err := row.Scan(
		&item.ScopeKey,
		&item.Provider,
		&item.ModelsJSON,
		&item.FetchedAtUnixMS,
		&item.LastError,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ComposerLiveModelCacheRow{}, false, nil
		}
		return ComposerLiveModelCacheRow{}, false, fmt.Errorf("get composer live model cache: %w", err)
	}
	return item, true, nil
}

func (s *SQLiteStore) PutComposerLiveModelCache(ctx context.Context, row ComposerLiveModelCacheRow) error {
	if s == nil || s.writeDB == nil {
		return errors.New("workspace database is not initialized")
	}
	row.ScopeKey = strings.TrimSpace(row.ScopeKey)
	row.Provider = strings.TrimSpace(row.Provider)
	if row.ScopeKey == "" || row.Provider == "" {
		return errors.New("composer live model cache scope is required")
	}
	if strings.TrimSpace(row.ModelsJSON) == "" {
		row.ModelsJSON = "[]"
	}
	_, err := s.writeDB.ExecContext(ctx, `
INSERT INTO composer_live_model_cache (
  scope_key, provider, models_json, fetched_at_unix_ms, last_error
) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(scope_key) DO UPDATE SET
  provider = excluded.provider,
  models_json = excluded.models_json,
  fetched_at_unix_ms = excluded.fetched_at_unix_ms,
  last_error = excluded.last_error
`, row.ScopeKey, row.Provider, row.ModelsJSON, row.FetchedAtUnixMS, row.LastError)
	if err != nil {
		return fmt.Errorf("put composer live model cache: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteComposerLiveModelCachePrefix(ctx context.Context, prefix string) error {
	if s == nil || s.writeDB == nil {
		return errors.New("workspace database is not initialized")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	_, err := s.writeDB.ExecContext(ctx, `
DELETE FROM composer_live_model_cache
WHERE scope_key LIKE ? ESCAPE '\'
`, escapeLikePrefix(prefix)+"%")
	if err != nil {
		return fmt.Errorf("delete composer live model cache: %w", err)
	}
	return nil
}

func escapeLikePrefix(prefix string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(prefix)
}
