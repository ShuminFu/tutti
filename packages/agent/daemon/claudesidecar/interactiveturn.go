package claudesidecar

import (
	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

// resolveInteractiveTurnID mirrors interactiveTurnResolver.ts: pick the
// canonical Turn that owns an interactive request.
func resolveInteractiveTurnID(
	callbackOptions claudecli.ToolPermissionRequest,
	turns *TurnLifecycle,
	activities *ToolActivityProjector,
) string {
	toolUseID := normalizeTitle(callbackOptions.ToolUseID)
	if toolUseID != "" {
		if delegatedTurnID := activities.ResolveInteractiveTurnID(toolUseID); delegatedTurnID != "" {
			return delegatedTurnID
		}
	}
	if turns.AwaitingContinuation() {
		if runningDelegatedTurnID := activities.RunningDelegatedTurnID(); runningDelegatedTurnID != "" {
			return runningDelegatedTurnID
		}
		if latestDelegatedTurnID := activities.LatestDelegatedTurnID(); latestDelegatedTurnID != "" {
			return latestDelegatedTurnID
		}
	}
	if turns.ActiveID() != "" {
		return turns.ActiveID()
	}
	queue := turns.Queue()
	for index := len(queue) - 1; index >= 0; index-- {
		turn := queue[index]
		if turn != nil && !turn.Settled && !turn.Synthetic {
			return turn.TurnID
		}
	}
	if runningDelegatedTurnID := activities.RunningDelegatedTurnID(); runningDelegatedTurnID != "" {
		return runningDelegatedTurnID
	}
	for index := len(queue) - 1; index >= 0; index-- {
		turn := queue[index]
		if turn != nil && !turn.Settled {
			return turn.TurnID
		}
	}
	return activities.LatestDelegatedTurnID()
}
