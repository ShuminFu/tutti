package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// codexSandboxBinSweeper plugs runtimeprep's `.sandbox-bin` sweep into the
// idle-gated agent maintenance loop. See runtimeprep.CodexSandboxBinStaleAfter
// for why per-session Codex helper copies pile up and why removal is safe.
type codexSandboxBinSweeper struct {
	Store runtimeprep.LocalStore
}

func (s codexSandboxBinSweeper) SweepStaleRunArtifacts(ctx context.Context, now time.Time) {
	result, err := s.Store.SweepStaleCodexSandboxBins(now, runtimeprep.CodexSandboxBinStaleAfter)
	if result.Removed > 0 || result.Failed > 0 || err != nil {
		// Locked helpers (a sandboxed command still running) are expected on
		// Windows; they are retried on the next maintenance pass.
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "codex sandbox helper copies swept",
			"event", "agent_run.codex_sandbox_bin.swept",
			"removed", result.Removed,
			"freed_bytes", result.FreedBytes,
			"failed", result.Failed,
			"error", err)
	}
}
