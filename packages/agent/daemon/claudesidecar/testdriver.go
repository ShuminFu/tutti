package claudesidecar

import (
	"context"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

// SidecarTestDriver mirrors testDriver.ts: deterministic sidecar behavior for
// runtime integration tests, with no Claude process involved.
type SidecarTestDriver struct {
	turns        *TurnLifecycle
	interactions *InteractiveCoordinator
	emit         Emitter
}

func NewSidecarTestDriver(turns *TurnLifecycle, interactions *InteractiveCoordinator, emit Emitter) *SidecarTestDriver {
	return &SidecarTestDriver{turns: turns, interactions: interactions, emit: emit}
}

// Exec is called with the session runtime lock held; interactive flows run on
// their own goroutine because they block until the daemon answers.
func (d *SidecarTestDriver) Exec(turnID string, prompt string) {
	d.turns.ActivateTransient(turnID)
	if strings.Contains(prompt, "approval") {
		d.runInteraction(turnID, "Bash", map[string]any{"command": "touch approval.txt"}, "test-approval-tool", "Approval accepted.")
		return
	}
	if strings.Contains(prompt, "ask-user") {
		d.runInteraction(turnID, "AskUserQuestion", map[string]any{
			"questions": []any{
				map[string]any{
					"header":   "Choice",
					"question": "Pick one",
					"options":  []any{map[string]any{"label": "A", "description": "Alpha"}},
				},
			},
		}, "test-ask-user-tool", "Question answered.")
		return
	}
	if strings.Contains(prompt, "exit-plan") {
		d.runInteraction(turnID, "ExitPlanMode", map[string]any{
			"plan": "1. Inspect\n2. Implement\n3. Verify",
		}, "test-exit-plan-tool", "Plan captured.")
		return
	}
	d.emit("assistant_delta", "", map[string]any{
		"turnId":   turnID,
		"content":  "Echo: " + prompt,
		"snapshot": "Echo: " + prompt,
	})
	d.completeTurn(turnID, "Echo: "+prompt)
}

func (d *SidecarTestDriver) Guide(prompt string) {
	d.emit("assistant_delta", "", map[string]any{
		"turnId":   d.turns.ActiveID(),
		"content":  "Echo: " + prompt,
		"snapshot": "Echo: " + prompt,
	})
}

func (d *SidecarTestDriver) runInteraction(turnID string, toolName string, toolInput map[string]any, toolUseID string, completion string) {
	go func() {
		_, err := d.interactions.HandleToolPermission(toolName, toolInput, claudecli.ToolPermissionRequest{
			Ctx:            context.Background(),
			ToolUseID:      toolUseID,
			HasSuggestions: toolName == "Bash",
		})
		if err != nil {
			d.failTurn(turnID, err)
			return
		}
		d.completeTurn(turnID, completion)
	}()
}

func (d *SidecarTestDriver) completeTurn(turnID string, content string) {
	d.emit("assistant_completed", "", map[string]any{
		"turnId":  turnID,
		"content": content,
	})
	d.emit("turn_completed", "", map[string]any{
		"turnId":     turnID,
		"stopReason": "end_turn",
	})
}

func (d *SidecarTestDriver) failTurn(turnID string, err error) {
	d.emit("turn_failed", "", map[string]any{
		"turnId": turnID,
		"error":  errorMessage(err),
	})
}
