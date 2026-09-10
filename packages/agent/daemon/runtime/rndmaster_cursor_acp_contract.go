package agentruntime

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	AppErrorLegacySessionUnavailable = "legacy_session_unavailable"
	AppErrorInteractionRequired      = "interaction_required"
)

func (a *standardACPAdapter) prepareRnDMasterACPSession(session Session) (Session, rndmasterRuntimeContract, error) {
	if a != nil && strings.TrimSpace(a.config.provider) == ProviderCursor {
		contract, err := rndmasterContractFromSession(session)
		if err != nil {
			return Session{}, rndmasterRuntimeContract{}, err
		}
		if provider := strings.TrimSpace(contract.Provider); provider != "" && !strings.EqualFold(provider, ProviderCursor) {
			return Session{}, rndmasterRuntimeContract{}, fmt.Errorf("RnDMaster runtime contract provider %q does not match Cursor", provider)
		}
		session.Env = rndmasterEnvList(session.Env, contract.Env)
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
	if a.appliesExtensionRuntimeContract(session) {
		return a.mergeExtensionRnDMasterACPSession(session)
	}
	return session, rndmasterRuntimeContract{}, nil
}

func rndmasterResumeProviderSessionID(session Session) string {
	return strings.TrimSpace(asString(payloadObject(session.RuntimeContext["rndmaster"])["resumeProviderSessionId"]))
}

func rndmasterLegacySessionUnavailable(session Session, reason string, cause error) error {
	return &AppError{
		Code:    AppErrorLegacySessionUnavailable,
		Message: "legacy_session_unavailable: Cursor history could not be imported.",
		DebugMessage: fmt.Sprintf("Cursor ACP session/load unavailable: room_id=%s agent_session_id=%s provider_session_id=%s reason=%s",
			strings.TrimSpace(session.RoomID), strings.TrimSpace(session.AgentSessionID),
			strings.TrimSpace(session.ProviderSessionID), strings.TrimSpace(reason)),
		Cause: cause,
	}
}

func rndmasterACPLoadSupported(raw json.RawMessage) bool {
	var result map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil {
		return false
	}
	return truthyNested(result, "sessionCapabilities", "load") ||
		truthyNested(result, "sessionCapabilities", "loadSession") ||
		truthyNested(result, "agentCapabilities", "loadSession") ||
		truthyNested(result, "agentCapabilities", "load")
}

func rndmasterACPMCPServers(contract rndmasterRuntimeContract) ([]any, bool, error) {
	raw, configured := contract.MCPConfig["mcpServers"]
	if !configured {
		return nil, false, nil
	}
	servers := payloadObject(raw)
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	hasHTTP := false
	for _, name := range names {
		body := payloadObject(servers[name])
		kind := strings.ToLower(strings.TrimSpace(asString(body["type"])))
		if url := strings.TrimSpace(asString(body["url"])); url != "" || kind == "http" || kind == "sse" {
			if url == "" {
				return nil, false, fmt.Errorf("RnDMaster MCP server %q requires url", name)
			}
			out = append(out, map[string]any{"name": name, "type": "http", "url": url,
				"headers": rndmasterACPNameValueList(payloadObject(body["headers"]))})
			hasHTTP = true
			continue
		}
		command := strings.TrimSpace(asString(body["command"]))
		if command == "" {
			return nil, false, fmt.Errorf("RnDMaster MCP server %q requires command or url", name)
		}
		args, err := rndmasterACPStringList(body["args"])
		if err != nil {
			return nil, false, fmt.Errorf("RnDMaster MCP server %q args: %w", name, err)
		}
		out = append(out, map[string]any{"name": name, "command": command, "args": args,
			"env": rndmasterACPNameValueList(payloadObject(body["env"]))})
	}
	return out, hasHTTP, nil
}

func rndmasterACPNameValueList(values map[string]any) []any {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, name := range names {
		out = append(out, map[string]any{"name": name, "value": fmt.Sprint(values[name])})
	}
	return out
}

func rndmasterACPStringList(value any) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("entries must be strings")
		}
		out = append(out, text)
	}
	return out, nil
}

func rndmasterMergeACPMCPServers(base, extra []any) []any {
	byName := map[string]any{}
	order := []string{}
	for _, group := range [][]any{base, extra} {
		for _, raw := range group {
			name := strings.TrimSpace(asString(payloadObject(raw)["name"]))
			if name == "" {
				continue
			}
			if _, exists := byName[name]; !exists {
				order = append(order, name)
			}
			byName[name] = raw
		}
	}
	out := make([]any, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}
