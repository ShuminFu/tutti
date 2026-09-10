package agentruntime

import (
	"context"
	"log/slog"
	"strings"
)

// RNDMASTER_ACP_AUTO_PERMISSION is the managed DinTalDock source marker for
// resolving session/request_permission from the RnDMaster runtime contract
// when the target has no permission tier.
const RNDMASTER_ACP_AUTO_PERMISSION = "RNDMASTER_ACP_AUTO_PERMISSION"

func rndmasterACPAutoPermissionEnabled() bool {
	return RNDMASTER_ACP_AUTO_PERMISSION != ""
}

func (a *standardACPAdapter) ApplyPermissionMode(ctx context.Context, session Session) error {
	if a != nil && a.config.launchPermission != nil {
		return nil
	}
	acpSession := a.getSession(session.AgentSessionID)
	if acpSession == nil || acpSession.client == nil {
		return nil
	}
	if strings.TrimSpace(session.ProviderSessionID) == "" {
		session.ProviderSessionID = acpSession.providerSessionID
	}
	// Track the live tier so automatic decisions (for example full-access
	// approval or read-only denial) affect subsequent requests without respawn.
	a.setSessionPermissionModeID(session.AgentSessionID, session.PermissionModeID)
	if a.config.permissionModeID == nil || a.config.permissionModeID(session.PermissionModeID) == "" {
		return nil
	}
	return a.applyACPMode(ctx, acpSession.client, session, a.effectiveModeID(session))
}

func (a *standardACPAdapter) effectiveWorkflowModeConfigOptionID() string {
	if a == nil {
		return ""
	}
	if a.config.planModeRuntimeID != "" && a.config.planModeDisabledRuntimeID != "" {
		return "mode"
	}
	return a.effectivePermissionConfigOptionID()
}

func (a *standardACPAdapter) setSessionPermissionModeID(agentSessionID string, permissionModeID string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if session := a.sessions[strings.TrimSpace(agentSessionID)]; session != nil {
		session.permissionModeID = strings.TrimSpace(permissionModeID)
	}
}

func (a *standardACPAdapter) setSessionPlanMode(agentSessionID string, enabled bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if session := a.sessions[strings.TrimSpace(agentSessionID)]; session != nil {
		session.planMode = enabled
	}
}

// automaticPermissionDecision resolves the decision the provider's live
// permission tier applies to a permission request, or "" to prompt the user.
func (a *standardACPAdapter) automaticPermissionDecision(agentSessionID string) string {
	return a.automaticPermissionDecisionFor(Session{AgentSessionID: agentSessionID})
}

func (a *standardACPAdapter) automaticPermissionDecisionFor(session Session) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	acpSession := a.sessions[strings.TrimSpace(session.AgentSessionID)]
	permissionModeID := ""
	planMode := false
	if acpSession != nil {
		permissionModeID = acpSession.permissionModeID
		planMode = acpSession.planMode
	}
	a.mu.Unlock()
	if planMode || session.SettingsValue().PlanMode {
		return "denied"
	}
	if decision := a.contractPermissionDecision(session); decision != "" {
		return decision
	}
	if a.config.automaticPermissionDecision == nil {
		return ""
	}
	return a.config.automaticPermissionDecision(permissionModeID)
}

func (a *standardACPAdapter) hasPermissionTier() bool {
	if a == nil {
		return false
	}
	if a.config.automaticPermissionDecision != nil {
		return true
	}
	return len(a.config.permissionModes) > 0
}

func (a *standardACPAdapter) contractPermissionDecision(session Session) string {
	if !rndmasterACPAutoPermissionEnabled() || a.hasPermissionTier() {
		return ""
	}
	decision := rndmasterContractAutomaticDecision(session)
	if decision == "" {
		return ""
	}
	slog.Info("auto-resolving permission request from RnDMaster contract",
		"event", "agent_session.acp.permission.contract_auto_resolve",
		"provider", a.config.provider,
		"adapter", a.config.adapterName,
		"agent_session_id", session.AgentSessionID,
		"decision", decision,
	)
	return decision
}

func (a *standardACPAdapter) effectiveModeID(session Session) string {
	if a == nil || a.config.permissionModeID == nil {
		return ""
	}
	if session.SettingsValue().PlanMode {
		if a.config.planModeRuntimeID != "" {
			return a.config.planModeRuntimeID
		}
		if modeID := a.config.permissionModeID("plan"); modeID != "" {
			return modeID
		}
		if a.config.launchPermission != nil {
			return ""
		}
	}
	if a.config.planModeDisabledRuntimeID != "" {
		return a.config.planModeDisabledRuntimeID
	}
	if a.config.launchPermission != nil {
		return ""
	}
	return a.config.permissionModeID(session.PermissionModeID)
}
