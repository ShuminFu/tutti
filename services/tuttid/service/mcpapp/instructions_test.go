package mcpapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerInstructionsReturnsInitializeInstructionsAndCaches(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "ui",
		"MCPAPP_FAKE_SERVER_MARKER="+marker,
		"MCPAPP_FAKE_SERVER_INSTRUCTIONS=draw with show_widget; read show_widget_guide first",
	)

	for i := 0; i < 2; i++ {
		text, err := resolver.ServerInstructions(context.Background(), server)
		if err != nil {
			t.Fatalf("ServerInstructions() error = %v", err)
		}
		if text != "draw with show_widget; read show_widget_guide first" {
			t.Fatalf("ServerInstructions() = %q", text)
		}
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "start "); got != 1 {
		t.Fatalf("server launches = %q, want one (second answer cached)", raw)
	}
	// Instructions and the UI catalog are separate questions: asking for
	// instructions must not mark the catalog as known.
	if _, _, known := resolver.CachedToolUI(server, "show_widget"); known {
		t.Fatal("instructions lookup populated the MCP App catalog")
	}
}

func TestServerInstructionsEmptyWhenServerHasNone(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	text, err := resolver.ServerInstructions(context.Background(), fakeServer(t, "plain"))
	if err != nil || text != "" {
		t.Fatalf("ServerInstructions() = %q, %v; want empty, nil", text, err)
	}
}

func TestServerInstructionsHungServerFailsFastAndIsNegativelyCached(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	resolver.Timeout = 300 * time.Millisecond
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "hang", "MCPAPP_FAKE_SERVER_MARKER="+marker)

	started := time.Now()
	if _, err := resolver.ServerInstructions(context.Background(), server); err == nil {
		t.Fatal("hung server returned no error")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("hung server stalled the lookup for %s", elapsed)
	}
	if _, err := resolver.ServerInstructions(context.Background(), server); err == nil {
		t.Fatal("negative cache lost the failure")
	}
	raw, _ := os.ReadFile(marker)
	if got := strings.Count(string(raw), "start "); got != 1 {
		t.Fatalf("server launches = %q, want one (failure cached)", raw)
	}
}

func TestServerInstructionsCallerDeadlineIsNotNegativelyCached(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "hang", "MCPAPP_FAKE_SERVER_MARKER="+marker)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := resolver.ServerInstructions(ctx, server); err == nil {
		t.Fatal("expired caller deadline returned no error")
	}
	resolver.mu.Lock()
	_, cached := resolver.instructions[server.Fingerprint]
	resolver.mu.Unlock()
	if cached {
		t.Fatal("a caller's own deadline was cached as a server failure")
	}
}
