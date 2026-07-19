package claudesidecar

import (
	"os"
	"path/filepath"
	"strings"
)

// loadClaudeSettingsEnv reads the env block from <configDir>/settings.json.
func loadClaudeSettingsEnv(configDir string) map[string]string {
	return claudeSettingsEnvFromFile(filepath.Join(configDir, "settings.json"))
}

// claudeSettingsEnv mirrors Claude CLI settings layering: user settings first,
// then project settings from the filesystem root to cwd, with
// settings.local.json taking precedence over settings.json in each directory.
func claudeSettingsEnv(cwd string) map[string]string {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = ""
		}
		configDir = home + "/.claude"
	}
	merged := loadClaudeSettingsEnv(configDir)
	for _, path := range claudeProjectSettingsPaths(cwd) {
		for key, value := range claudeSettingsEnvFromFile(path) {
			merged[key] = value
		}
	}
	return merged
}

func claudeProjectSettingsPaths(cwd string) []string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return nil
	}
	var directories []string
	current, err := filepath.Abs(trimmed)
	if err != nil {
		current = trimmed
	}
	for {
		directories = append(directories, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	// Reverse so the filesystem root comes first and deeper directories win.
	for i, j := 0, len(directories)-1; i < j; i, j = i+1, j-1 {
		directories[i], directories[j] = directories[j], directories[i]
	}
	paths := make([]string, 0, len(directories)*2)
	for _, directory := range directories {
		paths = append(paths,
			filepath.Join(directory, ".claude", "settings.json"),
			filepath.Join(directory, ".claude", "settings.local.json"),
		)
	}
	return paths
}

func claudeSettingsEnvFromFile(path string) map[string]string {
	result := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	parsed := parseJSONObject(string(data))
	env := recordValue(parsed["env"])
	for key, value := range env {
		if text, ok := value.(string); ok {
			result[key] = text
		}
	}
	return result
}
