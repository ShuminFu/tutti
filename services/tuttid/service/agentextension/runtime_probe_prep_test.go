package agentextension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

const runtimeProbeSecret = "probe-loopback-secret"

func TestProbeRuntimePreparesDeclaredHostModelEndpointInTemporaryHome(t *testing.T) {
	cwd := t.TempDir()
	endpointFile := writeRuntimeProbeEndpoint(t, "http://127.0.0.1:18788/llmproxy/test/v1", runtimeProbeSecret, true)
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, endpointFile)
	t.Setenv(runtimeprep.HostModelEndpointsEnv, `{"version":1,"providers":{"acp:test":{"protocol":"openai","wireAPI":"chat","baseURL":"https://stale.example/v1","apiKey":"personal-secret","model":"stale"}}}`)
	t.Setenv("TEST_RUNTIME_HOME", filepath.Join(t.TempDir(), "personal-home"))
	t.Setenv("TEST_GATEWAY_TOKEN", "personal-secret")

	transport := &runtimeProbeCaptureTransport{}
	result, err := ProbeRuntime(
		context.Background(), runtimeProbeBinding(), "extension:test", cwd,
		transport, agentruntime.HostMetadata{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RuntimeProbeReady {
		t.Fatalf("ProbeRuntime() status = %q", result.Status)
	}
	if transport.home == "" || environmentValue(transport.spec.Env, "TEST_RUNTIME_HOME") != transport.home {
		t.Fatalf("probe runtime home env = %q, spec = %#v", transport.home, transport.spec.Env)
	}
	if got := environmentValue(transport.spec.Env, "TEST_GATEWAY_TOKEN"); got != runtimeProbeSecret {
		t.Fatalf("probe gateway token = %q", got)
	}
	if transport.rootMode != 0o700 {
		t.Fatalf("probe temporary root mode = %#o", transport.rootMode)
	}
	var config struct {
		Model struct {
			Provider string `json:"provider"`
			Default  string `json:"default"`
		} `json:"model"`
		Providers map[string]struct {
			API    string `json:"api"`
			KeyEnv string `json:"key_env"`
		} `json:"providers"`
		Gateway struct {
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"gateway"`
	}
	if err := json.Unmarshal(transport.config, &config); err != nil {
		t.Fatalf("decode probe config: %v\n%s", err, transport.config)
	}
	if config.Model.Provider != "gateway" || config.Model.Default != "enterprise-chat" ||
		config.Providers["gateway"].API != "http://127.0.0.1:18788/llmproxy/test/v1" ||
		config.Providers["gateway"].KeyEnv != "TEST_GATEWAY_TOKEN" ||
		len(config.Gateway.Models) != 2 || config.Gateway.Models[1].ID != "enterprise-reasoner" {
		t.Fatalf("probe config = %#v", config)
	}
	if strings.Contains(string(transport.config), runtimeProbeSecret) || strings.Contains(string(transport.config), "personal-secret") {
		t.Fatalf("probe config persisted a gateway token: %s", transport.config)
	}
	if _, err := os.Stat(filepath.Join(cwd, "runtime.json")); !os.IsNotExist(err) {
		t.Fatalf("probe wrote config into discovery cwd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("probe wrote instructions into discovery cwd: %v", err)
	}
	if _, err := os.Stat(transport.root); !os.IsNotExist(err) {
		t.Fatalf("probe temporary root remains after completion: %v", err)
	}
}

func TestProbeRuntimeCleansPreparedHomeAfterACPFailure(t *testing.T) {
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, writeRuntimeProbeEndpoint(t, "http://localhost:18788/v1", runtimeProbeSecret, true))
	transport := &runtimeProbeCaptureTransport{delegate: probeTransport{sessionError: "fixture session failure"}}
	if _, err := ProbeRuntime(context.Background(), runtimeProbeBinding(), "extension:test", t.TempDir(), transport, agentruntime.HostMetadata{}); err == nil {
		t.Fatal("ProbeRuntime() error = nil")
	}
	if transport.root == "" {
		t.Fatal("probe temporary root was not created")
	}
	if _, err := os.Stat(transport.root); !os.IsNotExist(err) {
		t.Fatalf("probe temporary root remains after failure: %v", err)
	}
}

func TestProbeRuntimeDoesNotUseUnsafeOrPersonalModelEndpoint(t *testing.T) {
	for _, test := range []struct {
		name      string
		baseURL   string
		token     string
		writeFile bool
	}{
		{name: "missing host endpoint"},
		{name: "non-loopback endpoint", baseURL: "https://gateway.example/v1", token: runtimeProbeSecret, writeFile: true},
		{name: "empty token", baseURL: "http://127.0.0.1:18788/v1", writeFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(runtimeprep.HostModelEndpointsFileEnv, "")
			t.Setenv(runtimeprep.HostModelEndpointsEnv, "")
			if test.writeFile {
				t.Setenv(runtimeprep.HostModelEndpointsFileEnv, writeRuntimeProbeEndpoint(t, test.baseURL, test.token, true))
			}
			t.Setenv("TEST_RUNTIME_HOME", filepath.Join(t.TempDir(), "personal-home"))
			t.Setenv("TEST_GATEWAY_TOKEN", "personal-secret")
			transport := &runtimeProbeCaptureTransport{}
			if _, err := ProbeRuntime(context.Background(), runtimeProbeBinding(), "extension:test", t.TempDir(), transport, agentruntime.HostMetadata{}); err != nil {
				t.Fatal(err)
			}
			if transport.home != "" || transport.root != "" {
				t.Fatalf("unsafe endpoint prepared a temporary home: %q", transport.home)
			}
			if got := environmentValue(transport.spec.Env, "TEST_RUNTIME_HOME"); got != "" {
				t.Fatalf("probe inherited personal runtime home %q", got)
			}
			if got := environmentValue(transport.spec.Env, "TEST_GATEWAY_TOKEN"); got != "" {
				t.Fatalf("probe inherited personal gateway token %q", got)
			}
		})
	}
}

func TestProbeRuntimeWithoutModelEndpointDeclarationIsUnchanged(t *testing.T) {
	binding := runtimeProbeBinding()
	binding.RuntimePrep = nil
	binding.Env = []string{"KEEP=unchanged"}
	transport := &runtimeProbeCaptureTransport{}
	if _, err := ProbeRuntime(context.Background(), binding, "extension:test", t.TempDir(), transport, agentruntime.HostMetadata{}); err != nil {
		t.Fatal(err)
	}
	if got := environmentValue(transport.spec.Env, "KEEP"); got != "unchanged" || transport.root != "" {
		t.Fatalf("plain extension probe changed: env=%#v root=%q", transport.spec.Env, transport.root)
	}
}

func runtimeProbeBinding() RuntimeBinding {
	return RuntimeBinding{
		Installation: Installation{ID: "test@1.0.0", AgentKey: "test", Provider: "acp:test", DisplayName: "Test"},
		Command:      []string{"test-acp"},
		RuntimePrep: &runtimeprep.ExtensionRuntimePrep{
			SessionInstructionsFile: "dintal-role.md",
			Home: &runtimeprep.ExtensionRuntimeHome{
				EnvVar: "TEST_RUNTIME_HOME", DirName: "test-runtime",
				ConfigFile: "runtime.json", ConfigFormat: "json",
			},
			ModelEndpoint: &runtimeprep.ExtensionModelEndpoint{
				Protocol: "openai", WireAPI: "chat", WireAPIConfigValue: "chat_completions",
				APIKeyEnv: "TEST_GATEWAY_TOKEN", ProviderValue: "gateway",
				ConfigKeys: runtimeprep.ExtensionModelEndpointConfigKeys{
					Provider: []string{"model", "provider"}, Model: []string{"model", "default"},
					BaseURL: []string{"providers", "gateway", "api"}, APIKeyEnv: []string{"providers", "gateway", "key_env"},
					WireAPI: []string{"providers", "gateway", "transport"}, Models: []string{"gateway", "models"},
				},
			},
		},
	}
}

func writeRuntimeProbeEndpoint(t *testing.T, baseURL string, token string, withModels bool) string {
	t.Helper()
	models := ""
	if withModels {
		models = `,"models":[{"id":"enterprise-chat","name":"Enterprise Chat"},{"id":"enterprise-reasoner","name":"Enterprise Reasoner"}]`
	}
	payload := `{"version":1,"providers":{"acp:test":{"protocol":"openai","wireAPI":"chat","baseURL":` +
		mustJSONString(t, baseURL) + `,"apiKey":` + mustJSONString(t, token) + `,"model":"enterprise-chat"` + models + `}}}`
	path := filepath.Join(t.TempDir(), "host-model-endpoints.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type runtimeProbeCaptureTransport struct {
	delegate probeTransport
	spec     agentruntime.ProcessSpec
	home     string
	root     string
	rootMode os.FileMode
	config   []byte
}

func (t *runtimeProbeCaptureTransport) Start(ctx context.Context, spec agentruntime.ProcessSpec) (agentruntime.ProcessConnection, error) {
	t.spec = spec
	t.home = environmentValue(spec.Env, "TEST_RUNTIME_HOME")
	if t.home != "" {
		t.config, _ = os.ReadFile(filepath.Join(t.home, "runtime.json"))
		for dir := t.home; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if strings.HasPrefix(filepath.Base(dir), "tutti-extension-runtime-probe-") {
				t.root = dir
				if info, err := os.Stat(dir); err == nil {
					t.rootMode = info.Mode().Perm()
				}
				break
			}
		}
	}
	return t.delegate.Start(ctx, spec)
}
