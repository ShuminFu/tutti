package mcpapp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
)

// Server instructions: the same second MCP client, asked a different question.
//
// codex (app-server) never puts an MCP server's initialize `instructions` into
// the model prompt, so the codex adapter asks tuttid for them and delivers them
// through its own developer-instructions channel (agentruntime
// mcp_server_instructions.go). Only initialize runs here: no tools/list, no
// resources/read, never tools/call.

const instructionsLogEvent = "mcp_server_instructions.resolve"

var _ agentruntime.MCPServerInstructionsResolver = (*Resolver)(nil)

type instructionsEntry struct {
	text      string
	err       error
	expiresAt time.Time
}

// ServerInstructions returns the server's initialize instructions. Results
// (including failures, for FailureTTL) are cached per fingerprint, so a turn
// only pays for a process launch when the cache is cold. It blocks for at most
// ctx's deadline or the resolver timeout, whichever is sooner.
func (r *Resolver) ServerInstructions(ctx context.Context, server agentruntime.MCPAppServer) (string, error) {
	if r == nil {
		return "", errors.New("MCP server instructions resolver is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := server.Fingerprint
	r.mu.Lock()
	if entry, ok := r.instructions[key]; ok && r.now().Before(entry.expiresAt) {
		r.mu.Unlock()
		return entry.text, entry.err
	}
	r.mu.Unlock()

	slots := r.concurrencySlots()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	started := r.now()
	text, err := r.fetchInstructions(ctx, server)
	<-slots

	if err != nil && ctx.Err() != nil {
		// The caller gave up (turn deadline); that says nothing about the server,
		// so do not negatively cache it.
		return "", err
	}
	ttl := r.ttl()
	if err != nil {
		ttl = r.failureTTL()
		slog.Warn("mcp_server_instructions.resolve failed",
			"event", instructionsLogEvent,
			"server", server.Name,
			"command", server.Command,
			"duration_ms", r.now().Sub(started).Milliseconds(),
			"error", err,
		)
	} else {
		slog.Info("mcp_server_instructions.resolve completed",
			"event", instructionsLogEvent,
			"server", server.Name,
			"instructions_bytes", len(text),
			"duration_ms", r.now().Sub(started).Milliseconds(),
		)
	}
	r.mu.Lock()
	if r.instructions == nil {
		r.instructions = make(map[string]instructionsEntry)
	}
	now := r.now()
	for cached, entry := range r.instructions {
		if !now.Before(entry.expiresAt) {
			delete(r.instructions, cached)
		}
	}
	if len(r.instructions) >= defaultMaxEntries {
		for cached := range r.instructions {
			delete(r.instructions, cached)
			if len(r.instructions) < defaultMaxEntries {
				break
			}
		}
	}
	r.instructions[key] = instructionsEntry{text: text, err: err, expiresAt: now.Add(ttl)}
	r.mu.Unlock()
	return text, err
}

func (r *Resolver) fetchInstructions(ctx context.Context, server agentruntime.MCPAppServer) (text string, err error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	_, closeClient, result, err := openInitializedClient(ctx, r.Transport, server)
	if err != nil {
		return "", err
	}
	closeClient(false)
	return result.Instructions, nil
}
