package agent

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/tutti-os/tutti/services/tuttid/data/externalimportcatalog"
	_ "modernc.org/sqlite"
)

type codexTitleCache struct {
	mu    sync.Mutex
	conns map[string]*codexTitleConn
}

type codexTitleConn struct {
	dbPath      string
	fileID      string
	walSize     int64
	walMtimeNS  int64
	dataVersion int64
	db          *sql.DB
	titles      map[string]string
}

func (s *Service) titleCache() *codexTitleCache {
	if s == nil {
		return nil
	}
	if s.codexTitleCache == nil {
		s.codexTitleCache = &codexTitleCache{conns: map[string]*codexTitleConn{}}
	}
	return s.codexTitleCache
}

func (s *Service) closeExternalImportRuntime() {
	if s == nil {
		return
	}
	if s.codexTitleCache != nil {
		s.codexTitleCache.close()
	}
	if s.ExternalImportCatalog != nil {
		_ = s.ExternalImportCatalog.Close()
	}
}

func (c *codexTitleCache) close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.conns {
		if conn != nil && conn.db != nil {
			_ = conn.db.Close()
		}
	}
	c.conns = map[string]*codexTitleConn{}
}

func (s *Service) codexThreadTitlesCached(ctx context.Context, codexHome string) map[string]string {
	if cache := s.titleCache(); cache != nil {
		return cache.titles(ctx, codexHome)
	}
	return codexThreadTitles(codexHome)
}

// codexThreadTitles reads the Codex app-server state database and returns a map
// of thread id (the rollout session id) to its generated conversation title.
//
// Codex keeps the human-readable title in `state_<n>.sqlite` (table `threads`,
// column `title`) rather than in the rollout transcript, so importing the title
// requires reading that DB. The schema is versioned and undocumented, so every
// failure path degrades gracefully to an empty map and the caller falls back to
// the message-derived title.
func codexThreadTitles(codexHome string) map[string]string {
	return readCodexThreadTitlesOnce(context.Background(), codexHome)
}

func (c *codexTitleCache) titles(ctx context.Context, codexHome string) map[string]string {
	titles := map[string]string{}
	codexHome = strings.TrimSpace(codexHome)
	if c == nil || codexHome == "" {
		return titles
	}
	dbPath := codexStateDBPath(codexHome)
	if dbPath == "" {
		return titles
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conns == nil {
		c.conns = map[string]*codexTitleConn{}
	}
	conn := c.conns[codexHome]
	info, err := os.Stat(dbPath)
	if err != nil {
		return titles
	}
	fileID := ""
	if sig, inspectErr := externalimportcatalog.InspectPath(dbPath); inspectErr == nil {
		fileID = sig.FileID
	}
	walSize, walMtimeNS := walSignature(dbPath)
	if conn == nil || conn.dbPath != dbPath || conn.fileID != fileID || conn.db == nil {
		if conn != nil && conn.db != nil {
			_ = conn.db.Close()
		}
		db, openErr := openCodexStateDB(dbPath)
		if openErr != nil {
			return titles
		}
		conn = &codexTitleConn{dbPath: dbPath, db: db, fileID: fileID}
		c.conns[codexHome] = conn
	}
	version, err := sqliteDataVersion(ctx, conn.db)
	if err != nil {
		_ = conn.db.Close()
		delete(c.conns, codexHome)
		return titles
	}
	if conn.titles != nil &&
		conn.dataVersion == version &&
		conn.walSize == walSize &&
		conn.walMtimeNS == walMtimeNS &&
		conn.fileID == fileID &&
		info != nil {
		return cloneStringMap(conn.titles)
	}
	loaded, err := queryCodexThreadTitles(ctx, conn.db)
	if err != nil {
		return titles
	}
	conn.titles = loaded
	conn.dataVersion = version
	conn.walSize = walSize
	conn.walMtimeNS = walMtimeNS
	conn.fileID = fileID
	return cloneStringMap(loaded)
}

func readCodexThreadTitlesOnce(ctx context.Context, codexHome string) map[string]string {
	titles := map[string]string{}
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		return titles
	}
	dbPath := codexStateDBPath(codexHome)
	if dbPath == "" {
		return titles
	}
	db, err := openCodexStateDB(dbPath)
	if err != nil {
		return titles
	}
	defer db.Close()
	loaded, err := queryCodexThreadTitles(ctx, db)
	if err != nil {
		return titles
	}
	return loaded
}

func openCodexStateDB(dbPath string) (*sql.DB, error) {
	dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&_pragma=busy_timeout(2000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func queryCodexThreadTitles(ctx context.Context, db *sql.DB) (map[string]string, error) {
	titles := map[string]string{}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := db.QueryContext(ctx, "SELECT id, title FROM threads WHERE title IS NOT NULL AND title != ''")
	if err != nil {
		return titles, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			continue
		}
		id = strings.TrimSpace(id)
		title = strings.TrimSpace(title)
		if id != "" && title != "" {
			titles[id] = title
		}
	}
	return titles, rows.Err()
}

func sqliteDataVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var version int64
	err := db.QueryRowContext(ctx, "PRAGMA data_version").Scan(&version)
	return version, err
}

func walSignature(dbPath string) (int64, int64) {
	info, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return 0, 0
	}
	return info.Size(), info.ModTime().UnixNano()
}

func cloneStringMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// codexStateDBPath returns the highest-versioned state_<n>.sqlite under codexHome.
func codexStateDBPath(codexHome string) string {
	matches, err := filepath.Glob(filepath.Join(codexHome, "state_*.sqlite"))
	if err != nil {
		return ""
	}
	best := ""
	bestVersion := -1
	for _, match := range matches {
		if info, err := os.Stat(match); err != nil || info.IsDir() {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(match), "state_"), ".sqlite")
		version, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		if version > bestVersion {
			bestVersion = version
			best = match
		}
	}
	return best
}
