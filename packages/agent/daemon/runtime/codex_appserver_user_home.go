package agentruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func appServerExtraSkillRoots(strategy providerregistry.AppServerSkillRootsStrategy, env []string) ([]string, error) {
	roots, err := tuttiAgentExtraSkillRoots(strategy, env)
	if err != nil || len(roots) > 0 {
		return roots, err
	}
	return codexUserHomeExtraSkillRoots(env)
}

func codexUserHomeExtraSkillRoots(env []string) ([]string, error) {
	value, found := lastEnvironmentValue(env, runtimeprep.CodexExtraSkillRootsEnv)
	if !found {
		return nil, nil
	}
	var roots []string
	if err := json.Unmarshal([]byte(value), &roots); err != nil {
		return nil, fmt.Errorf("decode codex extra skill roots: %w", err)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("codex extra skill roots must not be empty")
	}
	if len(roots) > tuttiAgentExtraSkillRootsLimit {
		return nil, fmt.Errorf("codex extra skill roots exceed limit")
	}
	cleaned := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "." || !filepath.IsAbs(root) {
			return nil, fmt.Errorf("codex extra skill root must be absolute")
		}
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		cleaned = append(cleaned, root)
	}
	return cleaned, nil
}

func readCodexDeveloperInstructions(env []string) (string, error) {
	path, found := lastEnvironmentValue(env, runtimeprep.CodexDeveloperInstructionsFileEnv)
	if !found {
		return "", nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("codex developer instructions file is empty")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read codex developer instructions: %w", err)
	}
	return strings.TrimSpace(string(content)), nil
}

func appendCodexDockDeveloperInstructions(env []string, params map[string]any) {
	text, err := readCodexDeveloperInstructions(env)
	if err != nil || text == "" {
		return
	}
	mode, _ := params["collaborationMode"].(map[string]any)
	if mode == nil {
		return
	}
	settings, _ := mode["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		mode["settings"] = settings
	}
	existing := strings.TrimSpace(asString(settings["developer_instructions"]))
	if existing == "" {
		settings["developer_instructions"] = text
		return
	}
	if strings.Contains(existing, text) {
		return
	}
	settings["developer_instructions"] = existing + "\n\n" + text
}

func applyCodexConfigOverrides(spec *ProcessSpec) error {
	raw, found := lastEnvironmentValue(spec.Env, runtimeprep.CodexConfigOverridesEnv)
	spec.Env = withoutEnvironmentKey(spec.Env, runtimeprep.CodexConfigOverridesEnv)
	spec.Env = withoutEnvironmentKey(spec.Env, runtimeprep.CodexDeveloperInstructionsFileEnv)
	spec.Env = withoutEnvironmentKey(spec.Env, runtimeprep.CodexExtraSkillRootsEnv)
	if !found {
		return nil
	}
	var overrides []string
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		return fmt.Errorf("decode codex config overrides: %w", err)
	}
	if len(overrides) == 0 {
		return fmt.Errorf("codex config overrides must not be empty")
	}
	if len(spec.Command) == 0 {
		return fmt.Errorf("codex command is empty")
	}
	for _, override := range overrides {
		if strings.TrimSpace(override) == "" || strings.ContainsAny(override, "\r\n") {
			return fmt.Errorf("codex config override is empty")
		}
	}
	spec.Command = insertCodexConfigOverrides(spec.Command, overrides)
	return nil
}

func insertCodexConfigOverrides(command []string, overrides []string) []string {
	extra := make([]string, 0, len(overrides)*2)
	for _, override := range overrides {
		extra = append(extra, "-c", override)
	}
	next := make([]string, 0, len(command)+len(extra))
	next = append(next, command[0])
	next = append(next, extra...)
	next = append(next, command[1:]...)
	return next
}
