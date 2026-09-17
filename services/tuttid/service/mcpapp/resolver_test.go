package mcpapp

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

const fakeServerModeEnv = "MCPAPP_FAKE_SERVER_MODE"

const fakeWidgetHTML = "<!doctype html><html><head></head><body>mcp app shell</body></html>"

// TestMain doubles as a fake stdio MCP server: the resolver launches this test
// binary with MCPAPP_FAKE_SERVER_MODE set, exactly like a contract server.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeServerModeEnv); mode != "" {
		os.Exit(runFakeMCPServer(mode))
	}
	os.Exit(m.Run())
}

func runFakeMCPServer(mode string) int {
	if mode == "crash" {
		fmt.Fprintln(os.Stderr, "fake MCP server crashed on startup")
		return 3
	}
	if marker := os.Getenv("MCPAPP_FAKE_SERVER_MARKER"); marker != "" {
		file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(file, "start apps=%s\n", os.Getenv("TUTTI_MCP_APPS"))
			_ = file.Close()
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return 1
		}
		if len(request.ID) == 0 {
			continue
		}
		reply := func(result any) {
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		}
		switch request.Method {
		case "initialize":
			if mode == "hang" {
				continue
			}
			reply(map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}}})
		case "tools/list":
			reply(map[string]any{"tools": fakeTools(mode)})
		case "resources/read":
			if mode == "crash-on-read" {
				os.Exit(4)
			}
			uri, _ := request.Params["uri"].(string)
			content := map[string]any{"uri": uri, "mimeType": ResourceMIMEType}
			switch {
			case uri == "ui://fake/legacy":
				content["blob"] = base64.StdEncoding.EncodeToString([]byte(fakeWidgetHTML))
			case uri == "ui://fake/wrong-type":
				content["mimeType"] = "text/html"
				content["text"] = fakeWidgetHTML
			default:
				content["text"] = fakeWidgetHTML
				content["_meta"] = map[string]any{"ui": map[string]any{
					"csp":           map[string]any{"resourceDomains": []any{"https://registry.npmmirror.com"}, "connectDomains": []any{}},
					"prefersBorder": false,
				}}
			}
			reply(map[string]any{"contents": []any{content}})
		case "tools/call":
			// The resolver must never call tools; make it loud if it does.
			fmt.Fprintln(os.Stderr, "unexpected tools/call")
			return 9
		default:
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601, "message": "unknown"}})
		}
	}
	return 0
}

func fakeTools(mode string) []any {
	plain := map[string]any{"name": "peer_send", "inputSchema": map[string]any{"type": "object"}}
	if mode == "plain" {
		return []any{plain}
	}
	return []any{
		plain,
		map[string]any{"name": "show_widget", "inputSchema": map[string]any{"type": "object"},
			"_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://fake/widget"}}},
		map[string]any{"name": "legacy_widget", "inputSchema": map[string]any{"type": "object"},
			"_meta": map[string]any{"ui/resourceUri": "ui://fake/legacy"}},
		map[string]any{"name": "wrong_type", "inputSchema": map[string]any{"type": "object"},
			"_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://fake/wrong-type"}}},
		map[string]any{"name": "not_ui_scheme", "inputSchema": map[string]any{"type": "object"},
			"_meta": map[string]any{"ui": map[string]any{"resourceUri": "https://example.com/x"}}},
	}
}

func openSnapshotStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "agent.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store := storesqlite.New(db, storesqlite.Options{})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func fakeServer(t *testing.T, mode string, extraEnv ...string) agentruntime.MCPAppServer {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append([]string{fakeServerModeEnv + "=" + mode, "TUTTI_MCP_APPS=1"}, extraEnv...)
	return agentruntime.MCPAppServer{
		Name:        "fake",
		Command:     executable,
		Args:        []string{"-test.run=^$"},
		Env:         env,
		CWD:         t.TempDir(),
		Fingerprint: "fingerprint-" + mode + "-" + t.Name(),
	}
}

func resolveAndWait(t *testing.T, resolver *Resolver, server agentruntime.MCPAppServer, within time.Duration) time.Duration {
	t.Helper()
	done := make(chan struct{})
	started := time.Now()
	resolver.Resolve(server, func() { close(done) })
	select {
	case <-done:
		return time.Since(started)
	case <-time.After(within):
		t.Fatalf("resolution did not finish within %s", within)
		return 0
	}
}

func newTestResolver(t *testing.T, store SnapshotStore) *Resolver {
	return &Resolver{Transport: agentruntime.NewLocalProcessTransport(), Store: store, Timeout: 5 * time.Second}
}

func TestResolverSnapshotsUIToolResources(t *testing.T) {
	store := openSnapshotStore(t)
	resolver := newTestResolver(t, store)
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "ui", "MCPAPP_FAKE_SERVER_MARKER="+marker)

	if _, _, known := resolver.CachedToolUI(server, "show_widget"); known {
		t.Fatal("catalog known before resolution")
	}
	resolveAndWait(t, resolver, server, 5*time.Second)

	ui, hasUI, known := resolver.CachedToolUI(server, "show_widget")
	if !known || !hasUI || ui.ResourceURI != "ui://fake/widget" || !storesqlite.ValidMCPAppResourceSHA256(ui.ResourceSHA256) {
		t.Fatalf("show_widget ui=%#v hasUI=%v known=%v", ui, hasUI, known)
	}
	snapshot, ok, err := store.GetMCPAppResourceSnapshot(context.Background(), ui.ResourceSHA256)
	if err != nil || !ok {
		t.Fatalf("snapshot ok=%v err=%v", ok, err)
	}
	if snapshot.HTML != fakeWidgetHTML || snapshot.MimeType != ResourceMIMEType || snapshot.URI != "ui://fake/widget" ||
		!strings.Contains(snapshot.MetaJSON, `"resourceDomains":["https://registry.npmmirror.com"]`) {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	legacy, hasLegacy, _ := resolver.CachedToolUI(server, "legacy_widget")
	if !hasLegacy || legacy.ResourceURI != "ui://fake/legacy" {
		t.Fatalf("deprecated _meta[\"ui/resourceUri\"] + blob not resolved: %#v", legacy)
	}
	for _, tool := range []string{"peer_send", "wrong_type", "not_ui_scheme", "missing"} {
		if _, hasUI, known := resolver.CachedToolUI(server, tool); hasUI || !known {
			t.Fatalf("%s hasUI=%v known=%v, want known without UI", tool, hasUI, known)
		}
	}

	// Cached: no second process launch.
	resolver.CachedToolUI(server, "show_widget")
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "start apps=1"); got != 1 {
		t.Fatalf("server launches = %q, want exactly one with TUTTI_MCP_APPS=1", raw)
	}
}

func TestResolverServerWithoutMetaHasNoUITools(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	server := fakeServer(t, "plain")
	resolveAndWait(t, resolver, server, 5*time.Second)
	if _, hasUI, known := resolver.CachedToolUI(server, "peer_send"); hasUI || !known {
		t.Fatalf("hasUI=%v known=%v", hasUI, known)
	}
}

func TestResolverTimeoutIsNegativelyCached(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	resolver.Timeout = 300 * time.Millisecond
	server := fakeServer(t, "hang")
	elapsed := resolveAndWait(t, resolver, server, 3*time.Second)
	if elapsed > 2*time.Second {
		t.Fatalf("hung server stalled resolution for %s", elapsed)
	}
	if _, hasUI, known := resolver.CachedToolUI(server, "show_widget"); hasUI || !known {
		t.Fatalf("hasUI=%v known=%v, want negative cache", hasUI, known)
	}
}

func TestResolverCrashIsNegativelyCachedAndExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var clock sync.Mutex
	resolver := newTestResolver(t, openSnapshotStore(t))
	resolver.FailureTTL = time.Minute
	resolver.Now = func() time.Time { clock.Lock(); defer clock.Unlock(); return now }
	for _, mode := range []string{"crash", "crash-on-read"} {
		server := fakeServer(t, mode)
		resolveAndWait(t, resolver, server, 5*time.Second)
		if _, hasUI, known := resolver.CachedToolUI(server, "show_widget"); hasUI || !known {
			t.Fatalf("%s: hasUI=%v known=%v, want negative cache", mode, hasUI, known)
		}
	}
	clock.Lock()
	now = now.Add(2 * time.Minute)
	clock.Unlock()
	if _, _, known := resolver.CachedToolUI(fakeServer(t, "crash"), "show_widget"); known {
		t.Fatal("failure cache did not expire")
	}
}

func TestResolverDeduplicatesConcurrentResolution(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "ui", "MCPAPP_FAKE_SERVER_MARKER="+marker)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		resolver.Resolve(server, wg.Done)
	}
	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("waiters not released")
	}
	raw, _ := os.ReadFile(marker)
	if got := strings.Count(string(raw), "start"); got != 1 {
		t.Fatalf("launches = %d, want 1", got)
	}
}

func TestIsResourceMIMEType(t *testing.T) {
	for value, want := range map[string]bool{
		"text/html;profile=mcp-app":      true,
		"TEXT/HTML; profile=\"mcp-app\"": true,
		"text/html":                      false,
		"text/plain;profile=mcp-app":     false,
		"text/html;profile=other":        false,
	} {
		if got := IsResourceMIMEType(value); got != want {
			t.Fatalf("IsResourceMIMEType(%q) = %v, want %v", value, got, want)
		}
	}
}
