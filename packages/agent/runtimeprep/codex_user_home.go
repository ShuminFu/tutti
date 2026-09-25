package runtimeprep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	// CodexHomeModeEnv keeps a session on the per-session Codex home.
	// isolated, session, and legacy all select that fallback. Unset or user
	// uses the user's Codex home.
	CodexHomeModeEnv      = "TUTTI_CODEX_HOME_MODE"
	CodexHomeModeUser     = "user"
	CodexHomeModeIsolated = "isolated"

	// These ride the session env until the app-server spawn, then leave the
	// child environment. They exist so Dock policy does not have to be written
	// into the user's Codex home.
	CodexConfigOverridesEnv           = "TUTTI_CODEX_CONFIG_OVERRIDES"
	CodexDeveloperInstructionsFileEnv = "TUTTI_CODEX_DEVELOPER_INSTRUCTIONS_FILE"
	CodexExtraSkillRootsEnv           = "TUTTI_CODEX_EXTRA_SKILL_ROOTS_JSON"

	codexHomeModeSession  = "session"
	codexHomeModeLegacy   = "legacy"
	codexOverlayDirectory = "codex-overlay"
)

func codexUsesUserHome(input PrepareInput, runtimeRoot string) bool {
	if codexRuntimeIsolated(input) {
		return false
	}
	switch codexHomeModeValue(input.CodexHomeMode) {
	case CodexHomeModeIsolated:
		return false
	case CodexHomeModeUser:
		return true
	}
	if codexHomeModeValue(os.Getenv(CodexHomeModeEnv)) == CodexHomeModeIsolated {
		return false
	}
	if codexIsolatedHomeAlreadyUsed(runtimeRoot) {
		return false
	}
	return true
}

func codexHomeModeValue(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case CodexHomeModeUser:
		return CodexHomeModeUser
	case CodexHomeModeIsolated, codexHomeModeSession, codexHomeModeLegacy:
		return CodexHomeModeIsolated
	default:
		return ""
	}
}

// A session created before user-home mode already has its rollout under the
// per-session home. Stay there so resume can still find it.
func codexIsolatedHomeAlreadyUsed(runtimeRoot string) bool {
	runtimeRoot = strings.TrimSpace(runtimeRoot)
	if runtimeRoot == "" {
		return false
	}
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if codexDirectoryHasRollout(filepath.Join(runtimeRoot, codexHomeDirectory, directory)) {
			return true
		}
	}
	return false
}

func codexDirectoryHasRollout(root string) bool {
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		found = true
		return fs.SkipAll
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return true
	}
	return found
}

func (p CodexPreparer) prepareCodexUserHome(ctx context.Context, input ProviderPrepareInput) (ProviderPrepareResult, error) {
	if err := ctx.Err(); err != nil {
		return ProviderPrepareResult{}, err
	}
	userHome := configuredCodexHome()
	if userHome == "" {
		return ProviderPrepareResult{}, errors.New("codex user home is unavailable")
	}
	if err := os.MkdirAll(userHome, 0o700); err != nil {
		return ProviderPrepareResult{}, fmt.Errorf("create codex user home: %w", err)
	}
	configBytes, err := os.ReadFile(filepath.Join(userHome, "config.toml"))
	if err != nil && !os.IsNotExist(err) {
		return ProviderPrepareResult{}, fmt.Errorf("read codex config: %w", err)
	}
	overlay := filepath.Join(input.RuntimeRoot, codexOverlayDirectory)
	if err := os.MkdirAll(overlay, 0o700); err != nil {
		return ProviderPrepareResult{}, fmt.Errorf("create codex overlay: %w", err)
	}
	skillRoot := filepath.Join(overlay, "skills")
	releaseSkillNames, err := reserveUserCodexSkillNames(skillRoot)
	if err != nil {
		return ProviderPrepareResult{}, err
	}
	defer releaseSkillNames()
	skillPaths, err := installProviderNativeSkills(skillRoot, input.PrepareInput)
	if err != nil {
		return ProviderPrepareResult{}, err
	}
	instructions, err := codexUserHomeDeveloperInstructions(string(configBytes), input)
	if err != nil {
		return ProviderPrepareResult{}, err
	}
	overrides := codexUserHomeConfigOverrides(
		string(configBytes),
		input.PrepareInput,
		runtime.GOOS == "windows",
		os.Getenv(codexFastStartEnv) == "1",
	)
	if input.CodexSaverMode {
		rolePath, err := installCodexLunaWorkerRole(overlay)
		if err != nil {
			return ProviderPrepareResult{}, err
		}
		overrides = append(overrides,
			"agents.default.description="+strconv.Quote("Luna worker for cost-efficient, bounded, self-contained tasks"),
			"agents.default.config_file="+strconv.Quote(rolePath),
		)
	}
	encodedOverrides, err := json.Marshal(overrides)
	if err != nil {
		return ProviderPrepareResult{}, fmt.Errorf("encode codex config overrides: %w", err)
	}
	env := []string{
		"CODEX_HOME=" + userHome,
		CodexConfigOverridesEnv + "=" + string(encodedOverrides),
	}
	if instructions != "" {
		instructionsPath := filepath.Join(overlay, "developer-instructions.md")
		if err := os.WriteFile(instructionsPath, []byte(instructions), 0o600); err != nil {
			return ProviderPrepareResult{}, fmt.Errorf("write codex developer instructions: %w", err)
		}
		env = append(env, CodexDeveloperInstructionsFileEnv+"="+instructionsPath)
	}
	if len(skillPaths) > 0 {
		encodedRoots, err := json.Marshal([]string{skillRoot})
		if err != nil {
			return ProviderPrepareResult{}, fmt.Errorf("encode codex extra skill roots: %w", err)
		}
		env = append(env, CodexExtraSkillRootsEnv+"="+string(encodedRoots))
	}
	return ProviderPrepareResult{Cwd: input.Cwd, Env: env}, nil
}

func codexUserHomeDeveloperInstructions(config string, input ProviderPrepareInput) (string, error) {
	parts := make([]string, 0, 4)
	if existing := strings.TrimSpace(codexTopLevelConfigString(config, "developer_instructions")); existing != "" {
		parts = append(parts, existing)
	}
	if detail := strings.TrimSpace(agentConversationDetailModeInstructions(input.ConversationDetailMode)); detail != "" {
		parts = append(parts, detail)
	}
	policy, err := tuttiCLIPolicy(input.PrepareInput)
	if err != nil {
		return "", err
	}
	if input.CodexSaverMode {
		policy = strings.TrimSpace(policy) + "\n\n" + codexSaverModePolicy
	}
	if policy = strings.TrimSpace(policy); policy != "" {
		parts = append(parts, policy)
	}
	return strings.Join(parts, "\n\n"), nil
}

func codexUserHomeConfigOverrides(content string, input PrepareInput, windows bool, fastStart bool) []string {
	overrides := []string{"project_root_markers=[]"}
	if tier := codexUserHomeServiceTierOverride(content); tier != "" {
		overrides = append(overrides, tier)
	}
	if windows {
		overrides = append(overrides, `windows.sandbox="unelevated"`)
	}
	if budget := codexUserHomeSkillsBudgetOverride(content); budget != "" {
		overrides = append(overrides, budget)
	}
	if fastStart {
		overrides = append(overrides,
			"features.apps=false",
			"features.plugins=false",
			"features.remote_plugin=false",
		)
	}
	overrides = append(overrides, codexConnectorOverrideLines(input.MCPServers)...)
	return overrides
}

func codexUserHomeServiceTierOverride(content string) string {
	if !strings.EqualFold(strings.TrimSpace(codexTopLevelConfigString(content, "service_tier")), "priority") {
		return ""
	}
	return `service_tier="fast"`
}

func codexUserHomeSkillsBudgetOverride(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inRoot := true
	inSkills := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inRoot = false
			inSkills = trimmed == "[skills]"
			continue
		}
		if inRoot && (codexConfigLineHasKey(trimmed, "skills") || strings.HasPrefix(trimmed, "skills.")) {
			return ""
		}
		if inSkills && codexConfigLineHasKey(trimmed, "max_context_tokens") {
			return ""
		}
	}
	return "skills.max_context_tokens=" + strconv.Itoa(codexSkillsMaxContextTokens)
}

func codexConnectorOverrideLines(bindings []MCPServerBinding) []string {
	var connector *MCPServerBinding
	for index := range bindings {
		binding := bindings[index]
		if strings.TrimSpace(binding.Name) == "connector" && strings.TrimSpace(binding.Type) == "http" && strings.TrimSpace(binding.URL) != "" {
			connector = &binding
			break
		}
	}
	if connector == nil {
		return nil
	}
	lines := []string{"mcp_servers.connector.url=" + strconv.Quote(strings.TrimSpace(connector.URL))}
	if len(connector.Headers) == 0 {
		return lines
	}
	names := make([]string, 0, len(connector.Headers))
	for name := range connector.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var block strings.Builder
	block.WriteString("mcp_servers.connector.http_headers={")
	for index, name := range names {
		if index > 0 {
			block.WriteString(", ")
		}
		block.WriteString(strconv.Quote(name))
		block.WriteString(" = ")
		block.WriteString(strconv.Quote(connector.Headers[name]))
	}
	block.WriteByte('}')
	lines = append(lines, block.String())
	return lines
}

func codexTopLevelConfigString(content string, key string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			return ""
		}
		if !codexConfigLineHasKey(trimmed, key) {
			continue
		}
		value, _, ok := codexConfigStringAssignmentValueAt(lines, index, key)
		if !ok {
			return ""
		}
		return value
	}
	return ""
}

// Placeholders make Tutti skill names yield to an existing user skill. They
// are removed so the extra skill root does not publish empty directories.
func reserveUserCodexSkillNames(overlaySkills string) (func(), error) {
	if err := os.MkdirAll(overlaySkills, 0o755); err != nil {
		return nil, fmt.Errorf("create codex overlay skills: %w", err)
	}
	userHome := configuredCodexHome()
	if userHome == "" {
		return func() {}, nil
	}
	entries, err := os.ReadDir(filepath.Join(userHome, "skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return func() {}, nil
		}
		return nil, fmt.Errorf("read user codex skills: %w", err)
	}
	placeholders := make([]string, 0)
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		source := filepath.Join(userHome, "skills", name, "SKILL.md")
		info, err := os.Stat(source)
		if err != nil || info.IsDir() {
			continue
		}
		target := filepath.Join(overlaySkills, name)
		if _, err := os.Lstat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect codex overlay skill %s: %w", name, err)
		}
		if err := os.Mkdir(target, 0o755); err != nil {
			for _, path := range placeholders {
				_ = os.Remove(path)
			}
			return nil, fmt.Errorf("reserve codex skill name %s: %w", name, err)
		}
		placeholders = append(placeholders, target)
	}
	return func() {
		for _, path := range placeholders {
			entries, err := os.ReadDir(path)
			if err != nil || len(entries) > 0 {
				continue
			}
			_ = os.Remove(path)
		}
	}, nil
}

func (p *DefaultPreparer) bindSharedCodexHomeFork(ctx context.Context, input SessionForkProviderStateBindingInput) error {
	userHome := configuredCodexHome()
	if userHome == "" {
		return errors.New("codex user home is unavailable")
	}
	if err := os.MkdirAll(userHome, 0o700); err != nil {
		return fmt.Errorf("create codex user home: %w", err)
	}
	if _, _, _, err := findCodexRollout(ctx, userHome, input.TargetProviderSessionID, false); err == nil {
		return nil
	} else if !errors.Is(err, errCodexRolloutNotFound) {
		return fmt.Errorf("inspect shared codex home fork rollout: %w", err)
	}
	store := p.runtimeStore()
	sourceRoot, err := store.RuntimeRoot(input.WorkspaceID, input.SourceAgentSessionID)
	if err != nil {
		return fmt.Errorf("resolve source runtime root: %w", err)
	}
	sourcePath, relativePath, fingerprint, err := findCodexRollout(
		ctx,
		filepath.Join(sourceRoot, codexHomeDirectory),
		input.TargetProviderSessionID,
		false,
	)
	if err != nil {
		return err
	}
	targetPath := filepath.Join(userHome, relativePath)
	if err := ensurePathWithin(userHome, targetPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("prepare shared codex rollout directory: %w", err)
	}
	if err := copyRegularFileAtomically(sourcePath, targetPath, fingerprint); err != nil {
		return fmt.Errorf("copy app-server fork rollout into codex user home: %w", err)
	}
	return nil
}
