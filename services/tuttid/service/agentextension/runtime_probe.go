package agentextension

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
	agentextensionbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentextension"
)

type RuntimeProbeStatus string

const (
	RuntimeProbeReady        RuntimeProbeStatus = "ready"
	RuntimeProbeAuthRequired RuntimeProbeStatus = "auth_required"
)

type RuntimeAuthMethod struct {
	ID          string
	Name        string
	Description string
	// Type is the provider-declared method kind (for example "terminal").
	Type string
	// TerminalCommand is the ready-to-run shell command for terminal-type
	// methods (runtime executable plus the provider-declared arguments).
	// Empty for methods driven through ACP authenticate.
	TerminalCommand string
	// TerminalStartupAction is an optional typed action submitted only after the
	// terminal runtime emits its bounded literal ready marker.
	TerminalStartupAction *RuntimeTerminalStartupAction
}

type RuntimeTerminalStartupAction struct {
	Type        string
	CommandName string
	ReadyText   string
}

type RuntimeProbeResult struct {
	Status      RuntimeProbeStatus
	AuthMethods []RuntimeAuthMethod
	Account     *RuntimeAuthenticatedAccount
}

type RuntimeAuthenticatedAccount = agentextensionbiz.AuthenticatedAccount

func ProbeRuntime(
	ctx context.Context,
	binding RuntimeBinding,
	agentTargetID string,
	cwd string,
	transport agentruntime.ProcessTransport,
	host agentruntime.HostMetadata,
) (RuntimeProbeResult, error) {
	return runRuntimeSetup(ctx, binding, agentTargetID, cwd, "", 75*time.Second, transport, host)
}

func AuthenticateRuntime(
	ctx context.Context,
	binding RuntimeBinding,
	agentTargetID string,
	cwd string,
	methodID string,
	transport agentruntime.ProcessTransport,
	host agentruntime.HostMetadata,
) (RuntimeProbeResult, error) {
	return runRuntimeSetup(ctx, binding, agentTargetID, cwd, methodID, 15*time.Minute, transport, host)
}

func runRuntimeSetup(
	ctx context.Context,
	binding RuntimeBinding,
	agentTargetID string,
	cwd string,
	methodID string,
	timeout time.Duration,
	transport agentruntime.ProcessTransport,
	host agentruntime.HostMetadata,
) (RuntimeProbeResult, error) {
	if transport == nil {
		return RuntimeProbeResult{}, errors.New("agent extension runtime probe transport is not configured")
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session := agentruntime.Session{
		RoomID: "agent-target-setup", AgentSessionID: "setup-probe-" + fmt.Sprint(time.Now().UnixNano()),
		AgentTargetID: agentTargetID, Provider: binding.Installation.Provider, CWD: cwd,
	}
	preparedBinding, temporaryRoot, err := prepareRuntimeProbe(probeCtx, binding, session)
	if temporaryRoot != "" {
		defer func() { _ = os.RemoveAll(temporaryRoot) }()
	}
	if err != nil {
		return RuntimeProbeResult{}, err
	}
	result, err := agentruntime.RunStandardACPSetup(
		probeCtx, runtimeAdapterConfig(preparedBinding, agentTargetID), transport, host, session, methodID,
	)
	if err != nil {
		return RuntimeProbeResult{}, err
	}
	methods := make([]RuntimeAuthMethod, 0, len(result.AuthMethods))
	for _, method := range result.AuthMethods {
		var declaration *AuthenticationMethodProfile
		if declared, ok := binding.AuthenticationMethods[method.ID]; ok &&
			strings.TrimSpace(method.Type) == strings.TrimSpace(declared.Type) {
			declaration = &declared
		}
		name := method.Name
		description := method.Description
		if declaration != nil {
			if declaredName := strings.TrimSpace(declaration.Name); declaredName != "" {
				name = declaredName
			}
			if declaredDescription := strings.TrimSpace(declaration.Description); declaredDescription != "" {
				description = declaredDescription
			}
		}
		terminalLaunch := terminalLoginLaunch(binding.Command, method, declaration)
		methods = append(methods, RuntimeAuthMethod{
			ID: method.ID, Name: name, Description: description,
			Type:                  method.Type,
			TerminalCommand:       terminalLaunch.Command,
			TerminalStartupAction: terminalLaunch.StartupAction,
		})
	}
	var account *RuntimeAuthenticatedAccount
	if result.Account != nil {
		account = &RuntimeAuthenticatedAccount{
			ID: result.Account.ID, DisplayName: result.Account.DisplayName,
			AuthMethodID: result.Account.AuthMethodID, Organization: result.Account.Organization,
		}
	}
	return RuntimeProbeResult{Status: RuntimeProbeStatus(result.Status), AuthMethods: methods, Account: account}, nil
}

type runtimeProbeCommandCatalog struct{}

func (runtimeProbeCommandCatalog) Capabilities(context.Context, runtimeprep.CommandContext) []runtimeprep.CommandCapability {
	return nil
}

func prepareRuntimeProbe(ctx context.Context, binding RuntimeBinding, session agentruntime.Session) (RuntimeBinding, string, error) {
	prep := binding.RuntimePrep
	if prep == nil || prep.Home == nil || prep.ModelEndpoint == nil {
		return binding, "", nil
	}
	homeEnv := strings.TrimSpace(prep.Home.EnvVar)
	tokenEnv := strings.TrimSpace(prep.ModelEndpoint.APIKeyEnv)
	binding.Env = runtimeProbeEnvWithout(binding.Env, homeEnv, tokenEnv)
	binding.Env = append(binding.Env, homeEnv+"=", tokenEnv+"=")

	endpoint := runtimeprep.HostModelEndpoint(binding.Installation.Provider)
	if !runtimeprep.ExtensionModelEndpointApplies(endpoint, prep.ModelEndpoint) || !runtimeProbeEndpointIsLoopback(endpoint.BaseURL) {
		return binding, "", nil
	}
	temporaryRoot, err := os.MkdirTemp("", "tutti-extension-runtime-probe-")
	if err != nil {
		return binding, "", fmt.Errorf("create extension runtime probe home: %w", err)
	}
	if err := os.Chmod(temporaryRoot, 0o700); err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return binding, "", fmt.Errorf("secure extension runtime probe home: %w", err)
	}
	probePrep := *prep
	probePrep.InstructionsFile = ""
	if strings.TrimSpace(probePrep.SessionInstructionsFile) == "" {
		probePrep.SessionInstructionsFile = ".tutti-probe-instructions.md"
	}
	preparer := runtimeprep.NewDefaultPreparer(temporaryRoot)
	preparer.CommandCatalog = runtimeProbeCommandCatalog{}
	prepared, err := preparer.Prepare(ctx, runtimeprep.PrepareInput{
		WorkspaceID: "agent-target-setup", AgentSessionID: session.AgentSessionID,
		AgentTargetID: session.AgentTargetID, Provider: session.Provider, Cwd: session.CWD,
		ExtensionRuntimePrep: &probePrep, ModelEndpoint: endpoint,
	})
	if err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return binding, "", fmt.Errorf("prepare extension runtime probe: %w", err)
	}
	homeValue := environmentValue(prepared.Env, homeEnv)
	tokenValue := environmentValue(prepared.Env, tokenEnv)
	if homeValue == "" || tokenValue == "" {
		_ = os.RemoveAll(temporaryRoot)
		return binding, "", errors.New("extension runtime probe preparation omitted required environment")
	}
	binding.Env = runtimeProbeEnvWithout(binding.Env, homeEnv, tokenEnv)
	binding.Env = append(binding.Env, homeEnv+"="+homeValue, tokenEnv+"="+tokenValue)
	return binding, temporaryRoot, nil
}

func runtimeProbeEnvWithout(environment []string, keys ...string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		remove := false
		for _, blocked := range keys {
			if ok && strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(blocked)) {
				remove = true
				break
			}
		}
		if !remove {
			result = append(result, entry)
		}
	}
	return result
}

func runtimeProbeEndpointIsLoopback(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Split(host, "%")[0])
	return ip != nil && ip.IsLoopback()
}

type terminalAuthLaunch struct {
	Command       string
	StartupAction *RuntimeTerminalStartupAction
}

// terminalLoginLaunch renders the interactive sign-in launch for a terminal
// auth method. The fresh ACP method type remains authoritative. A compatible
// signed extension declaration may select either a runtime subcommand or a
// bounded TUI slash command that is submitted only after a literal ready marker
// is observed.
func terminalLoginLaunch(
	command []string,
	method agentruntime.StandardACPAuthMethod,
	declaration *AuthenticationMethodProfile,
) terminalAuthLaunch {
	methodType := method.Type
	args := method.Args
	strategy := ""
	readyText := ""
	if declaration != nil &&
		strings.TrimSpace(method.Type) == strings.TrimSpace(declaration.Type) {
		args = declaration.Command.Args
		strategy = declaration.Command.Strategy
		readyText = declaration.Command.ReadyText
	}
	if methodType != "terminal" || len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return terminalAuthLaunch{}
	}
	if strategy == "runtime-slash-command" && len(args) == 1 {
		return terminalAuthLaunch{
			Command: shellQuote(command[0]),
			StartupAction: &RuntimeTerminalStartupAction{
				Type: "slash_command", CommandName: args[0], ReadyText: readyText,
			},
		}
	}
	base := command[:1]
	if strategy != "runtime-subcommand" && len(args) > 0 && strings.HasPrefix(args[0], "-") {
		base = command
	}
	parts := make([]string, 0, len(base)+len(args))
	for _, element := range base {
		parts = append(parts, shellQuote(element))
	}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return terminalAuthLaunch{Command: strings.Join(parts, " ")}
}

func shellQuote(value string) string {
	if runtime.GOOS == "windows" {
		return windowsShellQuote(value)
	}
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		}
		switch r {
		case '_', '.', '/', '-', ':', '=', '@', '+', ',':
			return false
		}
		return true
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func windowsShellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '"', '&', '|', '<', '>', '(', ')', '^', '%':
			return true
		default:
			return false
		}
	}) == -1 {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
