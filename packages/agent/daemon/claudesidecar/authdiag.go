package claudesidecar

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	claudeAuthRefreshLogPrefix = "CLAUDE_CODE_AUTH_REFRESH_DEBUG"
	claudeAuthRefreshDebugEnv  = "TUTTI_CLAUDE_AUTH_REFRESH_DEBUG"
)

var sensitiveDiagnosticKeys = map[string]struct{}{
	"accesstoken":  {},
	"account":      {},
	"args":         {},
	"cause":        {},
	"command":      {},
	"configdir":    {},
	"cwd":          {},
	"message":      {},
	"path":         {},
	"refreshtoken": {},
	"servicename":  {},
	"stack":        {},
}

func claudeAuthRefreshDiagnosticsEnabled() bool {
	return truthyEnvValue(os.Getenv(claudeAuthRefreshDebugEnv))
}

func truthyEnvValue(value string) bool {
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func debugClaudeAuthRefreshLog(stage string, payload map[string]any) {
	if !claudeAuthRefreshDiagnosticsEnabled() {
		return
	}
	entry := map[string]any{
		"stage":     stage,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
	for key, value := range payload {
		entry[key] = value
	}
	sanitized := sanitizeClaudeAuthDiagnosticPayload(entry)
	data, err := json.Marshal(sanitized)
	if err != nil {
		fallback, fallbackErr := json.Marshal(map[string]any{
			"stage":         "log_failed",
			"originalStage": stage,
		})
		if fallbackErr != nil {
			return
		}
		data = fallback
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", claudeAuthRefreshLogPrefix, data)
}

func claudeCredentialSnapshot() map[string]any {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	configDirDefault := configDir == ""
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configDir = home + "/.claude"
		}
	}
	scope := "default"
	if !configDirDefault {
		scope = "custom"
	}
	return map[string]any{
		"storageBackend":   "plaintext",
		"configDirDefault": configDirDefault,
		"configDirScope":   scope,
		"plaintext":        claudePlaintextCredentialSnapshot(configDir),
	}
}

func claudePlaintextCredentialSnapshot(configDir string) map[string]any {
	content, err := os.ReadFile(configDir + "/.credentials.json")
	if err != nil {
		return map[string]any{"found": false}
	}
	snapshot := map[string]any{"found": true}
	parsed := parseJSONObject(string(content))
	oauth := recordValue(parsed["claudeAiOauth"])
	expiresAt := numberValue(oauth["expiresAt"])
	accessToken := stringValue(oauth["accessToken"])
	refreshToken := stringValue(oauth["refreshToken"])
	snapshot["hasAccessToken"] = accessToken != ""
	snapshot["hasRefreshToken"] = refreshToken != ""
	// Short one-way fingerprints (never the tokens themselves) so a rotation
	// is visible across process boundaries. Empty string == token absent.
	snapshot["accessTokenFp"] = credentialFingerprint(accessToken)
	snapshot["refreshTokenFp"] = credentialFingerprint(refreshToken)
	snapshot["expiresAt"] = expiresAt
	if expiresAt > 0 {
		snapshot["expired"] = float64(time.Now().UnixMilli()) >= expiresAt
	}
	return snapshot
}

// credentialFingerprint reduces a secret to a short, non-reversible marker
// (first 8 hex of its SHA-256). It never leaves the raw token in a log line.
func credentialFingerprint(secret string) string {
	if secret == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])[:8]
}

func sanitizeClaudeAuthDiagnosticPayload(value any) any {
	switch typed := value.(type) {
	case []any:
		sanitized := make([]any, 0, len(typed))
		for _, item := range typed {
			sanitized = append(sanitized, sanitizeClaudeAuthDiagnosticPayload(item))
		}
		return sanitized
	case map[string]any:
		sanitized := map[string]any{}
		for key, nestedValue := range typed {
			if _, sensitive := sensitiveDiagnosticKeys[strings.ToLower(key)]; sensitive {
				continue
			}
			sanitized[key] = sanitizeClaudeAuthDiagnosticPayload(nestedValue)
		}
		return sanitized
	default:
		return value
	}
}
