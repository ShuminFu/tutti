package storesqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const schemaMigrationAgentMCPAppResourcesV1 = "agent_mcp_app_resources_v1"

// MCPAppResourceSnapshotMaxHTMLBytes bounds one stored MCP App document.
const MCPAppResourceSnapshotMaxHTMLBytes = 2 << 20

// MCPAppResourceSnapshot is an immutable copy of an MCP App UI resource
// (`resources/read` contents[0]) as it was when a tool call used it.
//
// Why snapshots instead of re-reading the server: a conversation must keep
// rendering exactly what the user saw, after the MCP server was upgraded, its
// session ended, or tuttid restarted — history never goes back to the server.
// Snapshots are content addressed, so thousands of tool calls that share one
// shell HTML cost one row.
type MCPAppResourceSnapshot struct {
	SHA256          string
	URI             string
	MimeType        string
	HTML            string
	MetaJSON        string
	CreatedAtUnixMS int64
}

type PutMCPAppResourceSnapshotInput struct {
	URI      string
	MimeType string
	HTML     string
	// Meta is the resource's `_meta.ui` object (csp, prefersBorder, ...).
	Meta map[string]any
}

var mcpAppResourceSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidMCPAppResourceSHA256 reports whether value is a snapshot address.
func ValidMCPAppResourceSHA256(value string) bool {
	return mcpAppResourceSHA256Pattern.MatchString(value)
}

// MCPAppResourceSnapshotSHA256 is the content address: sha256 over the HTML,
// a NUL separator, and the canonical (key-sorted) `_meta.ui` JSON. The CSP is
// part of what gets rendered, so the same HTML under a changed domain allow
// list is a different snapshot rather than a silent rewrite of history.
func MCPAppResourceSnapshotSHA256(html string, metaJSON string) string {
	hash := sha256.New()
	hash.Write([]byte(html))
	hash.Write([]byte{0})
	hash.Write([]byte(metaJSON))
	return hex.EncodeToString(hash.Sum(nil))
}

func canonicalMCPAppResourceMetaJSON(meta map[string]any) (string, error) {
	if len(meta) == 0 {
		return "{}", nil
	}
	// encoding/json sorts map keys, which makes the encoding canonical.
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("encode MCP App resource meta: %w", err)
	}
	return string(raw), nil
}

func (s *Store) applyAgentMCPAppResourcesV1(ctx context.Context) error {
	applied, err := s.hasMigration(ctx, schemaMigrationAgentMCPAppResourcesV1)
	if err != nil || applied {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin agent MCP App resources v1: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS agent_mcp_app_resources (
  sha256 TEXT PRIMARY KEY,
  uri TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  html TEXT NOT NULL,
  meta_json TEXT NOT NULL DEFAULT '{}',
  created_at_unix_ms INTEGER NOT NULL
);
`); err != nil {
		return fmt.Errorf("create agent MCP App resources: %w", err)
	}
	if err := recordMigrationTx(ctx, tx, schemaMigrationAgentMCPAppResourcesV1); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent MCP App resources v1: %w", err)
	}
	return nil
}

// PutMCPAppResourceSnapshot stores a snapshot and returns its address. Storing
// identical content again is a no-op that returns the existing row.
func (s *Store) PutMCPAppResourceSnapshot(ctx context.Context, input PutMCPAppResourceSnapshotInput) (MCPAppResourceSnapshot, error) {
	if s == nil || s.db == nil {
		return MCPAppResourceSnapshot{}, errors.New("workspace database is not initialized")
	}
	uri := strings.TrimSpace(input.URI)
	mimeType := strings.TrimSpace(input.MimeType)
	if uri == "" || mimeType == "" || input.HTML == "" {
		return MCPAppResourceSnapshot{}, errors.New("MCP App resource snapshot requires uri, mime type and html")
	}
	if len(input.HTML) > MCPAppResourceSnapshotMaxHTMLBytes {
		return MCPAppResourceSnapshot{}, fmt.Errorf("MCP App resource html exceeds %d bytes", MCPAppResourceSnapshotMaxHTMLBytes)
	}
	metaJSON, err := canonicalMCPAppResourceMetaJSON(input.Meta)
	if err != nil {
		return MCPAppResourceSnapshot{}, err
	}
	snapshot := MCPAppResourceSnapshot{
		SHA256:          MCPAppResourceSnapshotSHA256(input.HTML, metaJSON),
		URI:             uri,
		MimeType:        mimeType,
		HTML:            input.HTML,
		MetaJSON:        metaJSON,
		CreatedAtUnixMS: unixMs(time.Now().UTC()),
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO agent_mcp_app_resources (sha256, uri, mime_type, html, meta_json, created_at_unix_ms)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(sha256) DO NOTHING
`, snapshot.SHA256, snapshot.URI, snapshot.MimeType, snapshot.HTML, snapshot.MetaJSON, snapshot.CreatedAtUnixMS); err != nil {
		return MCPAppResourceSnapshot{}, fmt.Errorf("store MCP App resource snapshot: %w", err)
	}
	stored, ok, err := s.GetMCPAppResourceSnapshot(ctx, snapshot.SHA256)
	if err != nil {
		return MCPAppResourceSnapshot{}, err
	}
	if !ok {
		return MCPAppResourceSnapshot{}, fmt.Errorf("MCP App resource snapshot %s vanished after insert", snapshot.SHA256)
	}
	return stored, nil
}

// GetMCPAppResourceSnapshot reads a snapshot by its address.
func (s *Store) GetMCPAppResourceSnapshot(ctx context.Context, sha256Hex string) (MCPAppResourceSnapshot, bool, error) {
	if s == nil || s.db == nil {
		return MCPAppResourceSnapshot{}, false, errors.New("workspace database is not initialized")
	}
	sha256Hex = strings.ToLower(strings.TrimSpace(sha256Hex))
	if !ValidMCPAppResourceSHA256(sha256Hex) {
		return MCPAppResourceSnapshot{}, false, nil
	}
	var snapshot MCPAppResourceSnapshot
	err := s.db.QueryRowContext(ctx, `
SELECT sha256, uri, mime_type, html, meta_json, created_at_unix_ms
FROM agent_mcp_app_resources
WHERE sha256 = ?
`, sha256Hex).Scan(
		&snapshot.SHA256,
		&snapshot.URI,
		&snapshot.MimeType,
		&snapshot.HTML,
		&snapshot.MetaJSON,
		&snapshot.CreatedAtUnixMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPAppResourceSnapshot{}, false, nil
	}
	if err != nil {
		return MCPAppResourceSnapshot{}, false, fmt.Errorf("read MCP App resource snapshot: %w", err)
	}
	return snapshot, true, nil
}
