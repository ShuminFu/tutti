package mcpapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
)

func serverLaunches(t *testing.T, marker string) int {
	t.Helper()
	raw, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "start ")
}

// recordingTransport remembers the process name of every launch.
type recordingTransport struct {
	agentruntime.ProcessTransport
	mu        sync.Mutex
	providers []string
}

func (t *recordingTransport) Start(ctx context.Context, spec agentruntime.ProcessSpec) (agentruntime.ProcessConnection, error) {
	t.mu.Lock()
	t.providers = append(t.providers, spec.Provider)
	t.mu.Unlock()
	return t.ProcessTransport.Start(ctx, spec)
}

func TestServerInstructionsReturnsInitializeInstructionsAndCaches(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	transport := &recordingTransport{ProcessTransport: resolver.Transport}
	resolver.Transport = transport
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
	if got := serverLaunches(t, marker); got != 1 {
		t.Fatalf("server launches = %d, want one (second answer cached)", got)
	}
	// Instructions and the UI catalog are separate questions: asking for
	// instructions must not mark the catalog as known.
	if _, _, known := resolver.CachedToolUI(server, "show_widget"); known {
		t.Fatal("instructions lookup populated the MCP App catalog")
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.providers) != 1 || transport.providers[0] != instructionsProcessName {
		t.Fatalf("probe process names = %q, want [%q]", transport.providers, instructionsProcessName)
	}
}

func TestServerInstructionsEmptyWhenServerHasNone(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	text, err := resolver.ServerInstructions(context.Background(), fakeServer(t, "plain"))
	if err != nil || text != "" {
		t.Fatalf("ServerInstructions() = %q, %v; want empty, nil", text, err)
	}
}

// Production wiring leaves Timeout unset (5s) while the codex turn gives the
// whole lookup 2s. The probe must time out inside the caller's window and be
// cached as a server failure; otherwise a hung server costs 2s and one process
// launch on every single turn.
func TestServerInstructionsHungServerIsNegativelyCachedWithinCallerDeadline(t *testing.T) {
	resolver := &Resolver{Transport: agentruntime.NewLocalProcessTransport(), Store: openSnapshotStore(t)}
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "hang", "MCPAPP_FAKE_SERVER_MARKER="+marker)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := resolver.ServerInstructions(ctx, server); err == nil {
		t.Fatal("hung server returned no error")
	} else if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		t.Fatalf("caller deadline fired before the probe timeout: %v", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	started := time.Now()
	if _, err := resolver.ServerInstructions(ctx2, server); err == nil {
		t.Fatal("negative cache lost the failure")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("second lookup waited %s, want a cached answer", elapsed)
	}
	if got := serverLaunches(t, marker); got != 1 {
		t.Fatalf("server launches = %d, want one (failure cached)", got)
	}
}

// A caller with almost no time left still starts a real probe; its outcome is
// cached for the next caller even though the first one stopped waiting.
func TestServerInstructionsProbeOutlivesImpatientCaller(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "hang", "MCPAPP_FAKE_SERVER_MARKER="+marker)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := resolver.ServerInstructions(ctx, server); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("impatient caller err = %v, want its own deadline", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resolver.mu.Lock()
		_, cached := resolver.instructions[server.Fingerprint]
		resolver.mu.Unlock()
		if cached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("probe outcome was never cached")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := resolver.ServerInstructions(context.Background(), server); err == nil {
		t.Fatal("cached failure not returned")
	}
	if got := serverLaunches(t, marker); got != 1 {
		t.Fatalf("server launches = %d, want one", got)
	}
}

func TestServerInstructionsConcurrentCallersShareOneProbe(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	marker := filepath.Join(t.TempDir(), "starts.log")
	server := fakeServer(t, "ui",
		"MCPAPP_FAKE_SERVER_MARKER="+marker,
		"MCPAPP_FAKE_SERVER_INSTRUCTIONS=shared",
	)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text, err := resolver.ServerInstructions(context.Background(), server)
			if err == nil && text != "shared" {
				err = errors.New("unexpected text " + text)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := serverLaunches(t, marker); got != 1 {
		t.Fatalf("server launches = %d, want one shared probe", got)
	}
}

// A malformed instructions field is the instructions probe's problem only; the
// MCP App catalog of the same server still resolves.
func TestMalformedInstructionsDoNotBreakCatalogResolution(t *testing.T) {
	resolver := newTestResolver(t, openSnapshotStore(t))
	server := fakeServer(t, "ui", "MCPAPP_FAKE_SERVER_BAD_INSTRUCTIONS=1")

	resolveAndWait(t, resolver, server, 5*time.Second)
	if ui, hasUI, known := resolver.CachedToolUI(server, "show_widget"); !known || !hasUI || ui.ResourceURI != "ui://fake/widget" {
		t.Fatalf("catalog ui=%#v hasUI=%v known=%v", ui, hasUI, known)
	}
	if _, err := resolver.ServerInstructions(context.Background(), server); err == nil {
		t.Fatal("malformed instructions decoded without error")
	}
}
