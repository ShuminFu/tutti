package externalimportcatalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const (
	SchemaVersion = 1
	ParserVersion = 1

	TimeSourceMessage     = "message"
	TimeSourceNowFallback = "now_fallback"

	busyTimeoutMillisec = 5000
)

// Key identifies one transcript file under a provider root.
type Key struct {
	Provider string
	Root     string
	RelPath  string
}

type Signature struct {
	Size          int64
	MtimeNS       int64
	FileID        string
	Symlink       bool
	TargetID      string
	ParserVersion int
	DescriptorSig string
}

type Summary struct {
	Valid           bool
	Empty           bool
	SessionID       string
	RawCwd          string
	Title           string
	SummaryTitle    string
	MessageCount    int
	StartedAtUnixMS int64
	UpdatedAtUnixMS int64
	TimeSource      string
	Model           string
	Effort          string
	NoProject       bool
	ResumeSupported *bool
}

type Entry struct {
	Key        Key
	Signature  Signature
	Summary    Summary
	Generation int64
}

type Store struct {
	path string
	db   *sql.DB
	mu   sync.Mutex
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("external import catalog path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create external import catalog directory: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open external import catalog: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable external import catalog wal: %w", err)
	}
	store := &Store{path: path, db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) Lookup(ctx context.Context, keys []Key) (map[Key]Entry, error) {
	out := make(map[Key]Entry, len(keys))
	if s == nil || len(keys) == 0 {
		return out, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("external import catalog is closed")
	}
	stmt, err := s.db.PrepareContext(ctx, `SELECT
		provider, root, rel_path, symlink, target_id, size, mtime_ns, file_id,
		parser_version, descriptor_sig, valid, empty, session_id, raw_cwd, title,
		summary_title, message_count, started_at, updated_at, time_source, model,
		effort, no_project, resume_supported, generation
		FROM files WHERE provider = ? AND root = ? AND rel_path = ?`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for _, key := range keys {
		entry, ok, err := scanEntry(stmt.QueryRowContext(ctx, key.Provider, key.Root, key.RelPath))
		if err != nil {
			return nil, err
		}
		if ok {
			out[key] = entry
		}
	}
	return out, nil
}

func (s *Store) Upsert(ctx context.Context, entries []Entry) error {
	if s == nil || len(entries) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("external import catalog is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO files (
		provider, root, rel_path, symlink, target_id, size, mtime_ns, file_id,
		parser_version, descriptor_sig, valid, empty, session_id, raw_cwd, title,
		summary_title, message_count, started_at, updated_at, time_source, model,
		effort, no_project, resume_supported, generation
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(provider, root, rel_path) DO UPDATE SET
		symlink=excluded.symlink,
		target_id=excluded.target_id,
		size=excluded.size,
		mtime_ns=excluded.mtime_ns,
		file_id=excluded.file_id,
		parser_version=excluded.parser_version,
		descriptor_sig=excluded.descriptor_sig,
		valid=excluded.valid,
		empty=excluded.empty,
		session_id=excluded.session_id,
		raw_cwd=excluded.raw_cwd,
		title=excluded.title,
		summary_title=excluded.summary_title,
		message_count=excluded.message_count,
		started_at=excluded.started_at,
		updated_at=excluded.updated_at,
		time_source=excluded.time_source,
		model=excluded.model,
		effort=excluded.effort,
		no_project=excluded.no_project,
		resume_supported=excluded.resume_supported,
		generation=excluded.generation`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, entry := range entries {
		if _, err := stmt.ExecContext(ctx, upsertArgs(entry)...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CompleteRoot records a successful enumeration and deletes catalog rows for
// files that were not observed in this generation. Incomplete walks must not
// call this, so cancellations and permission errors cannot mass-delete.
func (s *Store) CompleteRoot(ctx context.Context, provider, root string, generation int64, live []Key) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("external import catalog is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO roots (provider, root, generation, complete)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(provider, root) DO UPDATE SET generation=excluded.generation, complete=1`,
		provider, root, generation); err != nil {
		return err
	}
	keep := make(map[string]struct{}, len(live))
	for _, key := range live {
		keep[key.RelPath] = struct{}{}
	}
	rows, err := tx.QueryContext(ctx, `SELECT rel_path FROM files WHERE provider = ? AND root = ?`, provider, root)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			_ = rows.Close()
			return err
		}
		if _, ok := keep[rel]; !ok {
			stale = append(stale, rel)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, rel := range stale {
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE provider = ? AND root = ? AND rel_path = ?`,
			provider, root, rel); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func SignaturesEqual(left, right Signature) bool {
	return ContentSignaturesEqual(left, right) &&
		left.ParserVersion == right.ParserVersion &&
		left.DescriptorSig == right.DescriptorSig
}

func ContentSignaturesEqual(left, right Signature) bool {
	return left.Size == right.Size &&
		left.MtimeNS == right.MtimeNS &&
		left.FileID == right.FileID &&
		left.Symlink == right.Symlink &&
		left.TargetID == right.TargetID
}

func InspectPath(path string) (Signature, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Signature{}, err
	}
	sig := Signature{
		ParserVersion: ParserVersion,
		MtimeNS:       info.ModTime().UnixNano(),
		Size:          info.Size(),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		sig.Symlink = true
		targetInfo, err := os.Stat(path)
		if err != nil {
			return Signature{}, err
		}
		sig.Size = targetInfo.Size()
		sig.MtimeNS = targetInfo.ModTime().UnixNano()
		sig.FileID = fileIdentity(targetInfo)
		sig.TargetID = sig.FileID
		if linkID := fileIdentity(info); linkID != "" {
			sig.TargetID = linkID + "->" + sig.FileID
		}
		return sig, nil
	}
	sig.FileID = fileIdentity(info)
	sig.TargetID = sig.FileID
	return sig, nil
}

func (s *Store) migrate() error {
	var version int
	err := s.db.QueryRow(`SELECT value FROM schema_meta WHERE key = 'schema_version'`).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		if isMissingSchemaMeta(err) {
			if err := s.resetSchema(); err != nil {
				return err
			}
			return nil
		}
		return err
	}
	if version == SchemaVersion {
		return nil
	}
	return s.resetSchema()
}

func (s *Store) resetSchema() error {
	statements := []string{
		`DROP TABLE IF EXISTS files`,
		`DROP TABLE IF EXISTS roots`,
		`DROP TABLE IF EXISTS schema_meta`,
		`CREATE TABLE schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE files (
			provider TEXT NOT NULL,
			root TEXT NOT NULL,
			rel_path TEXT NOT NULL,
			symlink INTEGER NOT NULL DEFAULT 0,
			target_id TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL,
			mtime_ns INTEGER NOT NULL,
			file_id TEXT NOT NULL DEFAULT '',
			parser_version INTEGER NOT NULL,
			descriptor_sig TEXT NOT NULL,
			valid INTEGER NOT NULL DEFAULT 0,
			empty INTEGER NOT NULL DEFAULT 0,
			session_id TEXT NOT NULL DEFAULT '',
			raw_cwd TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			summary_title TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 0,
			started_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0,
			time_source TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			effort TEXT NOT NULL DEFAULT '',
			no_project INTEGER NOT NULL DEFAULT 0,
			resume_supported INTEGER,
			generation INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (provider, root, rel_path)
		)`,
		`CREATE TABLE roots (
			provider TEXT NOT NULL,
			root TEXT NOT NULL,
			generation INTEGER NOT NULL,
			complete INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (provider, root)
		)`,
		`CREATE INDEX files_session ON files(provider, session_id)`,
		`INSERT INTO schema_meta (key, value) VALUES ('schema_version', '1')`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("migrate external import catalog: %w", err)
		}
	}
	return nil
}

func isMissingSchemaMeta(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func scanEntry(row *sql.Row) (Entry, bool, error) {
	var (
		entry           Entry
		symlink         int
		valid           int
		empty           int
		noProject       int
		resumeSupported sql.NullInt64
	)
	err := row.Scan(
		&entry.Key.Provider, &entry.Key.Root, &entry.Key.RelPath,
		&symlink, &entry.Signature.TargetID, &entry.Signature.Size, &entry.Signature.MtimeNS,
		&entry.Signature.FileID, &entry.Signature.ParserVersion, &entry.Signature.DescriptorSig,
		&valid, &empty, &entry.Summary.SessionID, &entry.Summary.RawCwd, &entry.Summary.Title,
		&entry.Summary.SummaryTitle, &entry.Summary.MessageCount, &entry.Summary.StartedAtUnixMS,
		&entry.Summary.UpdatedAtUnixMS, &entry.Summary.TimeSource, &entry.Summary.Model,
		&entry.Summary.Effort, &noProject, &resumeSupported, &entry.Generation,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	entry.Signature.Symlink = symlink != 0
	entry.Summary.Valid = valid != 0
	entry.Summary.Empty = empty != 0
	entry.Summary.NoProject = noProject != 0
	if resumeSupported.Valid {
		value := resumeSupported.Int64 != 0
		entry.Summary.ResumeSupported = &value
	}
	return entry, true, nil
}

func upsertArgs(entry Entry) []any {
	var resume any
	if entry.Summary.ResumeSupported != nil {
		if *entry.Summary.ResumeSupported {
			resume = 1
		} else {
			resume = 0
		}
	}
	return []any{
		entry.Key.Provider,
		entry.Key.Root,
		entry.Key.RelPath,
		boolInt(entry.Signature.Symlink),
		entry.Signature.TargetID,
		entry.Signature.Size,
		entry.Signature.MtimeNS,
		entry.Signature.FileID,
		entry.Signature.ParserVersion,
		entry.Signature.DescriptorSig,
		boolInt(entry.Summary.Valid),
		boolInt(entry.Summary.Empty),
		entry.Summary.SessionID,
		entry.Summary.RawCwd,
		entry.Summary.Title,
		entry.Summary.SummaryTitle,
		entry.Summary.MessageCount,
		entry.Summary.StartedAtUnixMS,
		entry.Summary.UpdatedAtUnixMS,
		entry.Summary.TimeSource,
		entry.Summary.Model,
		entry.Summary.Effort,
		boolInt(entry.Summary.NoProject),
		resume,
		entry.Generation,
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func dsn(dbPath string, readOnly bool) string {
	query := url.Values{}
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMillisec))
	if readOnly {
		query.Set("mode", "ro")
		query.Add("_pragma", "query_only(1)")
	}
	path := dbPath
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, `\`, "/")
		if len(path) >= 2 && path[1] == ':' && !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func CatalogPath(stateDir string) string {
	return filepath.Join(strings.TrimSpace(stateDir), "external-import-catalog.sqlite")
}
