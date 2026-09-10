package agentruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
)

type rndmasterRuntimeContract struct {
	Version      int               `json:"version"`
	Provider     string            `json:"provider"`
	SystemPrompt string            `json:"systemPrompt"`
	CWD          string            `json:"cwd"`
	Model        string            `json:"model"`
	MCPConfig    map[string]any    `json:"mcpConfig"`
	Env          map[string]string `json:"env"`
	GateHookURL  string            `json:"gateHookUrl"`
}

func rndmasterContractFromSession(session Session) (rndmasterRuntimeContract, error) {
	rndmaster := payloadObject(session.RuntimeContext["rndmaster"])
	path := strings.TrimSpace(asString(rndmaster["contractFile"]))
	if path == "" {
		return rndmasterRuntimeContract{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return rndmasterRuntimeContract{}, fmt.Errorf("read RnDMaster runtime contract: %w", err)
	}
	var contract rndmasterRuntimeContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		return rndmasterRuntimeContract{}, fmt.Errorf("decode RnDMaster runtime contract: %w", err)
	}
	if contract.Version != 1 {
		return rndmasterRuntimeContract{}, fmt.Errorf("unsupported RnDMaster runtime contract version %d", contract.Version)
	}
	return contract, nil
}

func rndmasterEnvList(base []string, extra map[string]string) []string {
	return rndmasterEnvListForOS(base, extra, runtime.GOOS)
}

func rndmasterEnvListExcept(base []string, extra map[string]string, reserved []string) []string {
	return rndmasterEnvListExceptForOS(base, extra, reserved, runtime.GOOS)
}

func rndmasterEnvListExceptForOS(base []string, extra map[string]string, reserved []string, goos string) []string {
	if len(extra) == 0 {
		return rndmasterEnvListForOS(base, extra, goos)
	}
	keyFor := func(name string) string {
		if goos == "windows" {
			return strings.ToUpper(name)
		}
		return name
	}
	skip := map[string]struct{}{}
	for _, name := range reserved {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		skip[keyFor(name)] = struct{}{}
	}
	filtered := make(map[string]string, len(extra))
	for name, value := range extra {
		if _, reservedName := skip[keyFor(name)]; reservedName {
			continue
		}
		filtered[name] = value
	}
	return rndmasterEnvListForOS(base, filtered, goos)
}

func rndmasterEnvListForOS(base []string, extra map[string]string, goos string) []string {
	if len(extra) == 0 && goos != "windows" {
		return append([]string(nil), base...)
	}
	type envValue struct {
		name  string
		value string
	}
	values := map[string]envValue{}
	order := make([]string, 0, len(base)+len(extra))
	keyFor := func(name string) string {
		if goos == "windows" {
			return strings.ToUpper(name)
		}
		return name
	}
	for _, entry := range base {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		key := keyFor(name)
		current, exists := values[key]
		if !exists {
			order = append(order, key)
			current.name = name
		}
		current.value = value
		values[key] = current
	}
	extraNames := make([]string, 0, len(extra))
	for name := range extra {
		extraNames = append(extraNames, name)
	}
	sort.Strings(extraNames)
	for _, name := range extraNames {
		key := keyFor(name)
		current, exists := values[key]
		if !exists {
			order = append(order, key)
			current.name = name
		}
		current.value = extra[name]
		values[key] = current
	}
	out := make([]string, 0, len(values))
	for _, key := range order {
		value := values[key]
		out = append(out, value.name+"="+value.value)
	}
	return out
}

func rndmasterMCPServers(contract rndmasterRuntimeContract) (map[string]any, bool) {
	raw, configured := contract.MCPConfig["mcpServers"]
	if !configured {
		return nil, false
	}
	return clonePayload(payloadObject(raw)), true
}
