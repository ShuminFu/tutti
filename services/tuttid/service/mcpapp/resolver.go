// Package mcpapp is tuttid's MCP Apps (io.modelcontextprotocol/ui, spec
// 2026-01-26) resource resolver: display only.
//
// Why a second MCP client exists at all: the provider CLI (claude / codex /
// grok) is the MCP client that actually calls the RnDMaster contract's stdio
// servers, and none of them surface a tool's `_meta.ui.resourceUri` or the
// `ui://` resource to tuttid. tuttid is the UI host, so it launches its own
// short-lived client against the same server configuration, runs
// initialize -> tools/list -> resources/read, and snapshots the HTML into the
// agent store. The server process is spawned for catalog reads only; it is
// never sent tools/call.
package mcpapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	runtimemcp "github.com/tutti-os/tutti/packages/connector/runtime/mcp"
)

const (
	// ResourceMIMEType is the only UI resource type the spec defines.
	ResourceMIMEType = "text/html;profile=mcp-app"
	// ExtensionID is advertised in initialize so standard servers that follow
	// the spec's capability negotiation also expose their UI tools.
	ExtensionID = "io.modelcontextprotocol/ui"

	protocolVersion     = "2025-06-18"
	defaultTimeout      = 5 * time.Second
	defaultTTL          = 10 * time.Minute
	defaultFailureTTL   = 30 * time.Second
	defaultMaxEntries   = 256
	defaultConcurrency  = 4
	maxToolsListPages   = 20
	resolveLogEvent     = "mcp_app.resolve"
	resolverProcessName = "mcp-app-resolver"
)

// SnapshotStore persists content-addressed UI resource snapshots.
// errSnapshotWrite marks a store failure while saving a resource snapshot. Unlike a
// bad resource (wrong mime, empty html) it is transient, so the resolver retries soon.
var errSnapshotWrite = errors.New("store MCP App resource snapshot")

type SnapshotStore interface {
	PutMCPAppResourceSnapshot(context.Context, storesqlite.PutMCPAppResourceSnapshotInput) (storesqlite.MCPAppResourceSnapshot, error)
}

// Resolver implements agentruntime.MCPAppResolver.
type Resolver struct {
	Transport agentruntime.ProcessTransport
	Store     SnapshotStore
	// Timeout bounds one whole resolution: process start, initialize,
	// tools/list and every resources/read. Defaults to 5s.
	Timeout time.Duration
	// TTL is how long a resolved catalog is trusted. A server binary can be
	// upgraded under a long-lived tuttid; a new snapshot then gets a new sha.
	TTL time.Duration
	// FailureTTL is how long a failed resolution is negatively cached so a
	// broken server is not respawned on every tool call.
	FailureTTL time.Duration
	Now        func() time.Time

	mu       sync.Mutex
	cache    map[string]catalogEntry
	inflight map[string][]func()
	slots    chan struct{}
	slotOnce sync.Once
}

type catalogEntry struct {
	tools     map[string]agentruntime.MCPAppToolUI
	expiresAt time.Time
}

var _ agentruntime.MCPAppResolver = (*Resolver)(nil)

func (r *Resolver) CachedToolUI(server agentruntime.MCPAppServer, toolName string) (agentruntime.MCPAppToolUI, bool, bool) {
	if r == nil {
		return agentruntime.MCPAppToolUI{}, false, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[server.Fingerprint]
	if !ok || !r.now().Before(entry.expiresAt) {
		return agentruntime.MCPAppToolUI{}, false, false
	}
	ui, hasUI := entry.tools[toolName]
	return ui, hasUI, true
}

func (r *Resolver) Resolve(server agentruntime.MCPAppServer, done func()) {
	if r == nil {
		return
	}
	key := server.Fingerprint
	r.mu.Lock()
	if r.inflight == nil {
		r.inflight = make(map[string][]func())
	}
	waiters, running := r.inflight[key]
	if done != nil {
		waiters = append(waiters, done)
	}
	r.inflight[key] = waiters
	r.mu.Unlock()
	if running {
		return
	}
	go r.run(server)
}

func (r *Resolver) run(server agentruntime.MCPAppServer) {
	slots := r.concurrencySlots()
	slots <- struct{}{}
	started := r.now()
	tools, retrySoon, err := r.resolve(server)
	<-slots

	ttl := r.ttl()
	if retrySoon {
		// A snapshot write failed (e.g. SQLite busy): the catalog is incomplete for a
		// transient reason, so keep it only as long as a failure instead of hiding
		// that widget for the full TTL.
		ttl = r.failureTTL()
	}
	if err != nil {
		ttl = r.failureTTL()
		tools = nil
		slog.Warn("mcp_app.resolve failed",
			"event", resolveLogEvent,
			"server", server.Name,
			"command", server.Command,
			"duration_ms", r.now().Sub(started).Milliseconds(),
			"error", err,
		)
	} else {
		slog.Info("mcp_app.resolve completed",
			"event", resolveLogEvent,
			"server", server.Name,
			"ui_tool_count", len(tools),
			"duration_ms", r.now().Sub(started).Milliseconds(),
		)
	}

	r.mu.Lock()
	if r.cache == nil {
		r.cache = make(map[string]catalogEntry)
	}
	now := r.now()
	for key, entry := range r.cache {
		if !now.Before(entry.expiresAt) {
			delete(r.cache, key)
		}
	}
	if len(r.cache) >= defaultMaxEntries {
		for key := range r.cache {
			delete(r.cache, key)
			if len(r.cache) < defaultMaxEntries {
				break
			}
		}
	}
	r.cache[server.Fingerprint] = catalogEntry{tools: tools, expiresAt: now.Add(ttl)}
	waiters := r.inflight[server.Fingerprint]
	delete(r.inflight, server.Fingerprint)
	r.mu.Unlock()

	for _, waiter := range waiters {
		waiter()
	}
}

func (r *Resolver) resolve(server agentruntime.MCPAppServer) (tools map[string]agentruntime.MCPAppToolUI, retrySoon bool, err error) {
	if r.Transport == nil || r.Store == nil {
		return nil, false, errors.New("MCP App resolver is not configured")
	}
	if strings.TrimSpace(server.Command) == "" {
		return nil, false, errors.New("MCP App server command is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout())
	defer cancel()

	command := append([]string{server.Command}, server.Args...)
	conn, err := r.Transport.Start(ctx, agentruntime.ProcessSpec{
		Provider: resolverProcessName,
		CWD:      server.CWD,
		Command:  command,
		Env:      append([]string(nil), server.Env...),
	})
	if err != nil {
		return nil, false, fmt.Errorf("start MCP server: %w", err)
	}
	defer func() {
		// Never let process teardown delay the catalog: a well-behaved server
		// exits on stdin EOF, but Close's grace periods would otherwise stall
		// the done callbacks for seconds. A failed/hung server is killed.
		if err != nil {
			if killer, ok := conn.(interface{ Kill() error }); ok {
				_ = killer.Kill()
			}
		}
		go func() { _ = conn.Close() }()
	}()
	client, err := runtimemcp.NewStdioClient(runtimemcp.StdioClientConfig{
		Connection:  conn,
		ProcessName: server.Name + " MCP App resolver",
	})
	if err != nil {
		return nil, false, err
	}

	if _, err := client.Call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"extensions": map[string]any{
				ExtensionID: map[string]any{"mimeTypes": []string{ResourceMIMEType}},
			},
		},
		"clientInfo": map[string]any{"name": "tuttid-mcp-app-resolver", "version": "1"},
	}); err != nil {
		return nil, false, fmt.Errorf("initialize: %w", err)
	}
	if err := client.Notify("notifications/initialized", map[string]any{}); err != nil {
		return nil, false, fmt.Errorf("notifications/initialized: %w", err)
	}

	toolResources, err := listUIToolResources(ctx, client)
	if err != nil {
		return nil, false, err
	}
	tools = make(map[string]agentruntime.MCPAppToolUI, len(toolResources))
	snapshots := map[string]string{}
	for toolName, uri := range toolResources {
		sha, read := snapshots[uri]
		if !read {
			sha, err = r.snapshotResource(ctx, client, uri)
			if err != nil {
				if errors.Is(err, errSnapshotWrite) {
					retrySoon = true
				}
				// One broken resource must not hide the server's other UI tools.
				slog.Warn("mcp_app.resolve resource skipped",
					"event", resolveLogEvent,
					"server", server.Name,
					"tool", toolName,
					"resource_uri", uri,
					"error", err,
				)
			}
			snapshots[uri] = sha
		}
		if sha == "" {
			continue
		}
		tools[toolName] = agentruntime.MCPAppToolUI{ResourceURI: uri, ResourceSHA256: sha}
	}
	return tools, retrySoon, nil
}

type toolsListResult struct {
	Tools []struct {
		Name string `json:"name"`
		Meta struct {
			UI struct {
				ResourceURI string `json:"resourceUri"`
			} `json:"ui"`
			// Deprecated flat key, still accepted until the spec's GA.
			LegacyResourceURI string `json:"ui/resourceUri"`
		} `json:"_meta"`
	} `json:"tools"`
	NextCursor string `json:"nextCursor"`
}

func listUIToolResources(ctx context.Context, client *runtimemcp.StdioClient) (map[string]string, error) {
	out := map[string]string{}
	cursor := ""
	for page := 0; page < maxToolsListPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := client.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		var result toolsListResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode tools/list: %w", err)
		}
		for _, tool := range result.Tools {
			name := strings.TrimSpace(tool.Name)
			uri := strings.TrimSpace(tool.Meta.UI.ResourceURI)
			if uri == "" {
				uri = strings.TrimSpace(tool.Meta.LegacyResourceURI)
			}
			if name == "" || !strings.HasPrefix(uri, "ui://") {
				continue
			}
			out[name] = uri
		}
		cursor = strings.TrimSpace(result.NextCursor)
		if cursor == "" {
			return out, nil
		}
	}
	return out, nil
}

type resourceReadResult struct {
	Contents []struct {
		URI      string         `json:"uri"`
		MimeType string         `json:"mimeType"`
		Text     *string        `json:"text"`
		Blob     *string        `json:"blob"`
		Meta     map[string]any `json:"_meta"`
	} `json:"contents"`
}

func (r *Resolver) snapshotResource(ctx context.Context, client *runtimemcp.StdioClient, uri string) (string, error) {
	raw, err := client.Call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", fmt.Errorf("resources/read: %w", err)
	}
	var result resourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode resources/read: %w", err)
	}
	index := -1
	for candidate, content := range result.Contents {
		if strings.TrimSpace(content.URI) == uri {
			index = candidate
			break
		}
	}
	if index < 0 && len(result.Contents) > 0 {
		index = 0
	}
	if index < 0 {
		return "", errors.New("resources/read returned no contents")
	}
	content := result.Contents[index]
	if !IsResourceMIMEType(content.MimeType) {
		return "", fmt.Errorf("unsupported MCP App mime type %q", content.MimeType)
	}
	html := ""
	switch {
	case content.Text != nil:
		html = *content.Text
	case content.Blob != nil:
		decoded, err := base64.StdEncoding.DecodeString(*content.Blob)
		if err != nil {
			return "", fmt.Errorf("decode resource blob: %w", err)
		}
		html = string(decoded)
	}
	if strings.TrimSpace(html) == "" {
		return "", errors.New("MCP App resource has no html")
	}
	meta, _ := content.Meta["ui"].(map[string]any)
	snapshot, err := r.Store.PutMCPAppResourceSnapshot(ctx, storesqlite.PutMCPAppResourceSnapshotInput{
		URI:      uri,
		MimeType: ResourceMIMEType,
		HTML:     html,
		Meta:     meta,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", errSnapshotWrite, err)
	}
	return snapshot.SHA256, nil
}

// IsResourceMIMEType accepts `text/html;profile=mcp-app` modulo case and the
// optional whitespace MIME parameters allow.
func IsResourceMIMEType(value string) bool {
	mediaType, params, found := strings.Cut(value, ";")
	if !found || !strings.EqualFold(strings.TrimSpace(mediaType), "text/html") {
		return false
	}
	for _, param := range strings.Split(params, ";") {
		key, val, ok := strings.Cut(param, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "profile") &&
			strings.EqualFold(strings.Trim(strings.TrimSpace(val), `"`), "mcp-app") {
			return true
		}
	}
	return false
}

func (r *Resolver) concurrencySlots() chan struct{} {
	r.slotOnce.Do(func() {
		r.slots = make(chan struct{}, defaultConcurrency)
	})
	return r.slots
}

func (r *Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Resolver) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return defaultTimeout
}

func (r *Resolver) ttl() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return defaultTTL
}

func (r *Resolver) failureTTL() time.Duration {
	if r.FailureTTL > 0 {
		return r.FailureTTL
	}
	return defaultFailureTTL
}
