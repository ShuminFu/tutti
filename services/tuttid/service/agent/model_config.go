package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	codexConfigFileName = "config.toml"
)

// readCodexConfiguredDefaultModel reports the model the Codex installation the
// daemon owns is configured with.
//
// It only answers when CODEX_HOME names that installation. DinTalDock provisions
// one codex home per agent session under its own data directory and hands it to
// the runtime per session, so the fallback — ~/.codex/config.toml, the *host
// user's personal* Codex CLI config — describes an installation this daemon does
// not run. Reading it anyway is how a gateway-bound session acquired a model
// nobody chose: this machine's file says `gpt-6-astra`, that value rode the model
// catalog as its default, and new codex sessions were created on it. A daemon
// with no codex home of its own has no configured default to report; callers
// fall back to what the runtime advertises.
func readCodexConfiguredDefaultModel() string {
	return readCodexTopLevelConfigString("model")
}

// readCodexTopLevelConfigString reads one top-level key from the owned codex
// home; see readCodexConfiguredDefaultModel for why there is no fallback.
func readCodexTopLevelConfigString(key string) string {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		return ""
	}
	return readTopLevelTomlString(filepath.Join(codexHome, codexConfigFileName), key)
}

func readClaudeCodeConfiguredDefaultModel() string {
	claudeConfigDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if claudeConfigDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		claudeConfigDir = filepath.Join(home, ".claude")
	}
	settings := readJSONRecord(filepath.Join(claudeConfigDir, "settings.json"))
	// Keep whatever model the user configured, including a concrete id
	// (e.g. claude-opus-4-6) that is not one of the static aliases. Dropping it
	// here is what hid the user's chosen model from the composer catalog.
	return normalizeModelID(settings["model"])
}

func readJSONRecord(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	return parsed
}

func normalizeModelID(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func readTopLevelTomlString(path string, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripTomlComment(rawLine))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return ""
		}
		before, after, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(before) != key {
			continue
		}
		return parseTomlStringValue(after)
	}
	return ""
}

func stripTomlComment(line string) string {
	var quote rune
	escaped := false
	for index, char := range line {
		if quote == '"' {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				quote = 0
			}
			continue
		}
		if quote == '\'' {
			if char == '\'' {
				quote = 0
			}
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
			continue
		}
		if char == '#' {
			return line[:index]
		}
	}
	return line
}

func parseTomlStringValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, `"`) {
		var parsed string
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return ""
		}
		return strings.TrimSpace(parsed)
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		return strings.TrimSpace(value[1 : len(value)-1])
	}
	return strings.TrimSpace(value)
}
