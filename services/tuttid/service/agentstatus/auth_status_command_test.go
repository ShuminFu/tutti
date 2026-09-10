package agentstatus

import (
	"context"
	"testing"
	"time"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
	claudecodeservice "github.com/tutti-os/tutti/services/tuttid/service/claudecode"
)

func TestDefaultRegistryAllowsClaudeAuthStatusToFinish(t *testing.T) {
	specs, err := DefaultRegistry().Select([]string{agentprovider.ClaudeCode})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("spec count = %d, want 1", len(specs))
	}

	if got := authStatusTimeout(specs[0]); got != 10*time.Minute {
		t.Fatalf("Claude auth status timeout = %s, want 10m", got)
	}
}

func TestClaudeAuthStatusSharesCredentialStartupGate(t *testing.T) {
	if err := claudecodeservice.DefaultStartupGate.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire startup gate: %v", err)
	}
	defer claudecodeservice.DefaultStartupGate.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, ok := runAuthStatusCommand(ctx, ProviderSpec{
		Provider:          agentprovider.ClaudeCode,
		AuthStatusCommand: []string{"auth", "status"},
	}, "/bin/echo", nil); ok {
		t.Fatal("Claude auth status bypassed the shared credential startup gate")
	}
}

func TestAuthStatusTimeoutDefaultsToShortProbeWindow(t *testing.T) {
	spec := ProviderSpec{Provider: agentprovider.Codex}
	if got := authStatusTimeout(spec); got != 5*time.Second {
		t.Fatalf("default auth status timeout = %s, want 5s", got)
	}
}

func TestAPIUsageBillingCredentialsSkipOfficialAuthStatusCommands(t *testing.T) {
	tests := []struct {
		provider string
		env      string
	}{
		{provider: agentprovider.ClaudeCode, env: "ANTHROPIC_AUTH_TOKEN=runtime-token"},
		{provider: agentprovider.Codex, env: "OPENAI_API_KEY=runtime-key"},
		{provider: agentprovider.OpenCode, env: `OPENCODE_CONFIG_CONTENT={"provider":{"internal":{"options":{"apiKey":"runtime-key"}}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			specs, err := DefaultRegistry().Select([]string{tt.provider})
			if err != nil || len(specs) != 1 {
				t.Fatalf("Select(%q) = %#v, %v", tt.provider, specs, err)
			}
			calls := 0
			service := Service{
				Environ: func() []string { return []string{tt.env} },
				RunAuthStatusCommand: func(context.Context, ProviderSpec, string) (AuthInfo, bool) {
					calls++
					return AuthInfo{Status: AuthRequired}, true
				},
			}
			auth, _ := service.resolveAuthAndCLIVersion(context.Background(), specs[0], true, "/provider")
			if calls != 0 {
				t.Fatalf("auth status command calls = %d, want 0", calls)
			}
			if auth.Status != AuthAuthenticated || auth.AuthMethod != "apiKey" {
				t.Fatalf("auth = %#v, want API billing authentication", auth)
			}
		})
	}
}

func TestHostModelEndpointSkipsCodexOfficialAuthAndNetworkProbes(t *testing.T) {
	t.Setenv("TUTTI_HOST_MODEL_ENDPOINTS_FILE", "")
	t.Setenv("TUTTI_HOST_MODEL_ENDPOINTS", `{"version":1,"providers":{"codex":{"protocol":"openai","baseURL":"http://127.0.0.1:18799/llmproxy/openai/v1","apiKey":"loopback","wireAPI":"responses"}}}`)
	specs, err := DefaultRegistry().Select([]string{agentprovider.Codex})
	if err != nil || len(specs) != 1 {
		t.Fatalf("Select(codex) = %#v, %v", specs, err)
	}
	calls := 0
	service := Service{
		RunAuthStatusCommand: func(context.Context, ProviderSpec, string) (AuthInfo, bool) {
			calls++
			return AuthInfo{Status: AuthRequired}, true
		},
	}
	auth, _ := service.resolveAuthAndCLIVersion(context.Background(), specs[0], true, "/codex")
	if calls != 0 || auth.Status != AuthAuthenticated || auth.AuthMethod != "apiKey" {
		t.Fatalf("auth = %#v, calls = %d; want host API credential without official auth probe", auth, calls)
	}
	if !service.providerUsesCustomConfig(agentprovider.Codex) {
		t.Fatal("host Codex endpoint must skip official api.openai.com network probes")
	}
}

func TestClaudeACPRuntimeDoesNotRequireSDKSidecar(t *testing.T) {
	t.Setenv(claudeCodeRuntimeEnv, claudeCodeRuntimeACP)
	service := Service{}
	specs, err := DefaultRegistry().Select([]string{agentprovider.ClaudeCode})
	if err != nil || len(specs) != 1 {
		t.Fatalf("Select(claude-code) = %#v, %v", specs, err)
	}
	resolved := service.resolveClaudeCodeProviderSpec(context.Background(), specs[0], true)
	if resolved.ExternalRegistryID != "claude-acp" {
		t.Fatalf("ExternalRegistryID = %q, want claude-acp", resolved.ExternalRegistryID)
	}
	if resolved.AdapterUnavailableReasonCode == ReasonClaudeSDKSidecarUnavailable {
		t.Fatalf("ACP runtime was gated by SDK sidecar: %#v", resolved)
	}
}
