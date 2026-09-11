package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Only user submission entry points call this. Model/status probes and runtime
// preparation never invoke host installation, so setup can inspect this daemon
// without re-entering the send or holding a runtime lifecycle lock.
func ensureEmbeddedDockProvider(ctx context.Context, provider string) error {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("RNDMASTER_DOCK_SETUP_BASE")), "/")
	if base == "" {
		return nil
	}
	switch strings.TrimSpace(provider) {
	case "claude-code":
		provider = "claude"
	case "codex", "cursor":
	case "acp:grok":
		provider = "grok"
	case "acp:deepseek-harness":
		provider = "deepseek-harness"
	default:
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/providers/"+provider+"/dock/ensure", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("RNDMASTER_DOCK_SETUP_TOKEN"))
	response, err := (&http.Client{Timeout: 11 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("prepare Dock provider: %w", err)
	}
	defer response.Body.Close()
	var result struct {
		Enabled bool   `json:"enabled"`
		Error   string `json:"error"`
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return fmt.Errorf("read Dock setup result: %w", err)
	}
	if response.StatusCode != http.StatusOK || !result.Enabled {
		return fmt.Errorf("Dock provider setup failed: %s", result.Error)
	}
	return nil
}
