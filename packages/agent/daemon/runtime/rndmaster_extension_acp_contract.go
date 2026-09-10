package agentruntime

import (
	"strings"
)

// RNDMASTER_EXTENSION_RUNTIME_CONTRACT is the managed DinTalDock source marker for
// applying the RnDMaster runtime contract to every standard-acp extension:* target.
const RNDMASTER_EXTENSION_RUNTIME_CONTRACT = "RNDMASTER_EXTENSION_RUNTIME_CONTRACT"

func rndmasterMergeExtensionRuntimeContract() bool {
	return RNDMASTER_EXTENSION_RUNTIME_CONTRACT != ""
}

func standardACPExtensionTarget(agentTargetID string) bool {
	return strings.HasPrefix(strings.TrimSpace(agentTargetID), "extension:")
}

func (a *standardACPAdapter) appliesExtensionRuntimeContract(session Session) bool {
	if a == nil || !rndmasterMergeExtensionRuntimeContract() {
		return false
	}
	return standardACPExtensionTarget(a.config.agentTargetID) || standardACPExtensionTarget(session.AgentTargetID)
}

func (a *standardACPAdapter) mergeExtensionRnDMasterACPSession(session Session) (Session, rndmasterRuntimeContract, error) {
	if a == nil {
		return session, rndmasterRuntimeContract{}, nil
	}
	contract, err := rndmasterContractFromSession(session)
	if err != nil {
		return Session{}, rndmasterRuntimeContract{}, err
	}
	if strings.TrimSpace(contract.SystemPrompt) == "" && len(contract.Env) == 0 &&
		strings.TrimSpace(contract.CWD) == "" && strings.TrimSpace(contract.Model) == "" &&
		len(contract.MCPConfig) == 0 {
		return session, contract, nil
	}
	session.Env = rndmasterEnvListExcept(session.Env, contract.Env, a.config.isolatedRuntimeEnvNames)
	if cwd := strings.TrimSpace(contract.CWD); cwd != "" {
		session.CWD = cwd
	}
	if model := strings.TrimSpace(contract.Model); model != "" {
		settings := session.SettingsValue()
		settings.Model = model
		session.Settings = &settings
	}
	return session, contract, nil
}

func extensionACPInitialPromptContext(session Session) (string, error) {
	if !rndmasterMergeExtensionRuntimeContract() {
		return "", nil
	}
	if !standardACPExtensionTarget(session.AgentTargetID) {
		return "", nil
	}
	if strings.TrimSpace(session.ProviderSessionID) != "" {
		return "", nil
	}
	contract, err := rndmasterContractFromSession(session)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(contract.SystemPrompt), nil
}
