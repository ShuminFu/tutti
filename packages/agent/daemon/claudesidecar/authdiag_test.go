package claudesidecar

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAuthRefreshDiagnosticsRequireExplicitTruthyOverride(t *testing.T) {
	t.Setenv(claudeAuthRefreshDebugEnv, "")
	if claudeAuthRefreshDiagnosticsEnabled() {
		t.Fatal("diagnostics enabled without override")
	}
	t.Setenv(claudeAuthRefreshDebugEnv, "false")
	if claudeAuthRefreshDiagnosticsEnabled() {
		t.Fatal("diagnostics enabled with false override")
	}
	t.Setenv(claudeAuthRefreshDebugEnv, "1")
	if !claudeAuthRefreshDiagnosticsEnabled() {
		t.Fatal("diagnostics disabled with truthy override")
	}
}

func TestAuthDiagnosticSanitizerRemovesSecretBearingFields(t *testing.T) {
	sanitized := sanitizeClaudeAuthDiagnosticPayload(map[string]any{
		"stage":             "query_initialization.failed",
		"providerSessionId": "session-1",
		"cwd":               "/Users/alice/private-project",
		"credentials": map[string]any{
			"configDir":       "/Users/alice/.claude",
			"effectiveSource": "keychain",
			"keychain": map[string]any{
				"account":       "alice",
				"serviceName":   "Claude Code-credentials",
				"accessTokenFp": "1234abcd",
			},
			"plaintext": map[string]any{
				"path": "/Users/alice/.claude/.credentials.json",
			},
		},
		"error": map[string]any{
			"name":    "Error",
			"message": "failed to read /Users/alice/.claude",
			"stack":   "Error: secret-bearing stack",
			"cause":   map[string]any{"refreshToken": "refresh-secret"},
			"code":    "ENOENT",
		},
	})

	expected := map[string]any{
		"stage":             "query_initialization.failed",
		"providerSessionId": "session-1",
		"credentials": map[string]any{
			"effectiveSource": "keychain",
			"keychain":        map[string]any{"accessTokenFp": "1234abcd"},
			"plaintext":       map[string]any{},
		},
		"error": map[string]any{"name": "Error", "code": "ENOENT"},
	}
	if !reflect.DeepEqual(sanitized, expected) {
		t.Fatalf("sanitized = %#v", sanitized)
	}
	serialized, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"alice", "private-project", "refresh-secret"} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("serialized diagnostics leak %q: %s", secret, serialized)
		}
	}
}
