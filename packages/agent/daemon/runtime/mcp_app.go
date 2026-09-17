package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
)

// MCP Apps (io.modelcontextprotocol/ui, spec 2026-01-26) — host side, display only.
//
// A provider CLI (claude / codex / grok) is the real MCP client of the
// RnDMaster contract's stdio servers, and it drops every `_meta` on the floor:
// neither the tool's `_meta.ui.resourceUri` nor the resource HTML ever reaches
// tuttid through the provider protocol. So tuttid runs a *second*, short-lived
// MCP client against the same server (same command/args/env) to learn which
// tools carry a UI resource and to snapshot that resource. The Controller only
// decides *which* stored tool_call message is an MCP call of which contract
// server; talking MCP and persisting snapshots belongs to the injected
// MCPAppResolver (tuttid composes it, since the shared MCP client package
// depends on this package).

// MCPAppPayloadKey is the canonical tool_call payload key the GUI reads.
const MCPAppPayloadKey = "mcpApp"

// JSON Pointers into the stored tool_call payload at which the tool arguments
// live. tuttid is the single owner of the per-provider payload shape: the GUI
// follows the pointer and never guesses.
const (
	MCPAppArgumentsPointerInput          = "/input"
	MCPAppArgumentsPointerGrokToolInput  = "/input/tool_input"
	MCPAppArgumentsPointerCodexArguments = "/input/arguments"
)

// MCPAppServer is one contract stdio MCP server exactly as the provider CLI
// launches it.
type MCPAppServer struct {
	Name    string
	Command string
	Args    []string
	// Env holds the process env overrides (session env followed by the
	// server's own env); the process transport layers them over os.Environ.
	Env []string
	CWD string
	// Fingerprint identifies the server configuration (name, command, args and
	// the server's own env). Resolution results are cached per fingerprint.
	Fingerprint string
}

// MCPAppToolUI is a resolved UI resource bound to one tool.
type MCPAppToolUI struct {
	ResourceURI    string
	ResourceSHA256 string
}

// MCPAppResolver resolves and snapshots MCP App UI resources.
type MCPAppResolver interface {
	// CachedToolUI never blocks. known=false means the server's tool catalog
	// has not been resolved yet (or the cache expired); callers then Resolve.
	CachedToolUI(server MCPAppServer, toolName string) (ui MCPAppToolUI, hasUI bool, known bool)
	// Resolve fetches the catalog in the background (deduplicated per
	// fingerprint, bounded by the resolver's timeout) and calls done once the
	// catalog is known — successfully resolved or negatively cached.
	Resolve(server MCPAppServer, done func())
}

type mcpAppToolCall struct {
	ServerName       string
	ToolName         string
	ArgumentsPointer string
}

// SetMCPAppResolver enables MCP App annotation of tool_call message updates.
func (c *Controller) SetMCPAppResolver(resolver MCPAppResolver) {
	if c == nil {
		return
	}
	c.mcpAppResolverMu.Lock()
	c.mcpAppResolver = resolver
	c.mcpAppResolverMu.Unlock()
}

func (c *Controller) currentMCPAppResolver() MCPAppResolver {
	if c == nil {
		return nil
	}
	c.mcpAppResolverMu.RLock()
	defer c.mcpAppResolverMu.RUnlock()
	return c.mcpAppResolver
}

// annotateMCPAppMessageUpdates adds payload.mcpApp to tool_call updates whose
// tool has a cached UI resource. When the server catalog is still unknown it
// starts resolution and re-reports the same message once the catalog lands,
// so the GUI picks it up through the ordinary message-version update without
// a refresh. It never blocks the report path.
func (c *Controller) annotateMCPAppMessageUpdates(session Session, report *agentsessionstore.ReportActivityInput) {
	resolver := c.currentMCPAppResolver()
	if resolver == nil || report == nil || len(report.MessageUpdates) == 0 {
		return
	}
	if strings.TrimSpace(asString(payloadObject(session.RuntimeContext["rndmaster"])["contractFile"])) == "" {
		return
	}
	var servers map[string]MCPAppServer
	for index := range report.MessageUpdates {
		update := &report.MessageUpdates[index]
		if strings.TrimSpace(update.Kind) != "tool_call" || len(update.Payload) == 0 {
			continue
		}
		if servers == nil {
			servers = mcpAppContractServers(session)
			if len(servers) == 0 {
				return
			}
		}
		call, ok := identifyMCPToolCall(update.Payload, servers)
		if !ok {
			continue
		}
		server := servers[call.ServerName]
		ui, hasUI, known := resolver.CachedToolUI(server, call.ToolName)
		if known {
			if hasUI {
				update.Payload[MCPAppPayloadKey] = mcpAppPayload(call, ui)
			}
			continue
		}
		pending := mcpAppRepublishUpdate(*update)
		resolver.Resolve(server, func() {
			c.republishMCPAppUpdate(session, server, call, pending)
		})
	}
}

func (c *Controller) republishMCPAppUpdate(
	session Session,
	server MCPAppServer,
	call mcpAppToolCall,
	update agentsessionstore.WorkspaceAgentMessageUpdate,
) {
	resolver := c.currentMCPAppResolver()
	if resolver == nil {
		return
	}
	ui, hasUI, known := resolver.CachedToolUI(server, call.ToolName)
	if !known || !hasUI {
		return
	}
	update.Payload = map[string]any{MCPAppPayloadKey: mcpAppPayload(call, ui)}
	report := reportActivityInput(session, nil)
	report.MessageUpdates = []agentsessionstore.WorkspaceAgentMessageUpdate{update}
	c.enqueueReport(context.Background(), report)
}

// mcpAppRepublishUpdate keeps only the message identity; the canonical store
// merges the later payload into the existing row and never regresses a
// terminal status, so re-reporting only {mcpApp} is safe.
func mcpAppRepublishUpdate(update agentsessionstore.WorkspaceAgentMessageUpdate) agentsessionstore.WorkspaceAgentMessageUpdate {
	return agentsessionstore.WorkspaceAgentMessageUpdate{
		AgentSessionID:   update.AgentSessionID,
		MessageID:        update.MessageID,
		Seq:              update.Seq,
		TurnID:           update.TurnID,
		Role:             update.Role,
		Kind:             update.Kind,
		Status:           update.Status,
		CallID:           update.CallID,
		ParentCallID:     update.ParentCallID,
		RootCallID:       update.RootCallID,
		OccurredAtUnixMS: update.OccurredAtUnixMS,
	}
}

func mcpAppPayload(call mcpAppToolCall, ui MCPAppToolUI) map[string]any {
	return map[string]any{
		"serverName":       call.ServerName,
		"toolName":         call.ToolName,
		"resourceUri":      ui.ResourceURI,
		"resourceSha256":   ui.ResourceSHA256,
		"argumentsPointer": call.ArgumentsPointer,
	}
}

// mcpAppContractServers lists the contract's stdio servers with the env the
// provider CLI sees (TUTTI_MCP_APPS and the runtime instructions file are
// already injected by rndmasterMCPServers). HTTP servers are out of scope.
func mcpAppContractServers(session Session) map[string]MCPAppServer {
	specs := contractStdioServerSpecs(session)
	if len(specs) == 0 {
		return nil
	}
	out := make(map[string]MCPAppServer, len(specs))
	for name, spec := range specs {
		out[name] = spec.server
	}
	return out
}

// contractStdioServerSpec keeps the server's own env entries (sorted
// KEY=value, excluding the session env) next to the launch spec, so a caller
// that alters that env can recompute a fingerprint on the same basis.
type contractStdioServerSpec struct {
	server MCPAppServer
	ownEnv []string
}

func contractStdioServerSpecs(session Session) map[string]contractStdioServerSpec {
	contract, err := rndmasterContractFromSession(session)
	if err != nil {
		return nil
	}
	raw, configured := rndmasterMCPServers(contract, session.Env)
	if !configured {
		return nil
	}
	out := make(map[string]contractStdioServerSpec, len(raw))
	for name, value := range raw {
		body := payloadObject(value)
		command := strings.TrimSpace(asString(body["command"]))
		if strings.TrimSpace(name) == "" || command == "" {
			continue
		}
		args, err := rndmasterACPStringList(body["args"])
		if err != nil {
			continue
		}
		serverEnv := payloadObject(body["env"])
		envNames := make([]string, 0, len(serverEnv))
		for key := range serverEnv {
			envNames = append(envNames, key)
		}
		sort.Strings(envNames)
		env := append([]string(nil), session.Env...)
		fingerprintEnv := make([]string, 0, len(envNames))
		for _, key := range envNames {
			entry := key + "=" + fmt.Sprint(serverEnv[key])
			env = append(env, entry)
			fingerprintEnv = append(fingerprintEnv, entry)
		}
		out[name] = contractStdioServerSpec{
			server: MCPAppServer{
				Name:        name,
				Command:     command,
				Args:        args,
				Env:         env,
				CWD:         strings.TrimSpace(session.CWD),
				Fingerprint: mcpAppServerFingerprint(name, command, args, fingerprintEnv),
			},
			ownEnv: fingerprintEnv,
		}
	}
	return out
}

func mcpAppServerFingerprint(name, command string, args, env []string) string {
	raw, _ := json.Marshal(map[string]any{"name": name, "command": command, "args": args, "env": env})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// identifyMCPToolCall recognizes the three provider shapes of an MCP tool call
// in a tool_call message payload:
//
//   - claude-code: name/toolName = "mcp__<server>__<tool>", arguments = input
//   - grok (ACP):  name = "use_tool", input.tool_name = "<server>__<tool>"
//     (no mcp__ prefix), arguments = input.tool_input
//   - codex:       name/title = "<server>.<tool>" (codex_appserver_event_items.go
//     mcpToolCall), arguments = input.arguments (rawInput.arguments)
//
// Server names may themselves contain underscores, so the server is matched
// against the contract's server names rather than split on the first "__".
func identifyMCPToolCall(payload map[string]any, servers map[string]MCPAppServer) (mcpAppToolCall, bool) {
	if len(payload) == 0 || len(servers) == 0 {
		return mcpAppToolCall{}, false
	}
	input := mcpAppToolInput(payload)
	names := []string{asString(payload["name"]), asString(payload["toolName"]), asString(payload["title"])}

	for _, name := range names {
		if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
			if server, tool, ok := matchMCPServerTool(rest, "__", servers); ok && input != nil {
				return mcpAppToolCall{ServerName: server, ToolName: tool, ArgumentsPointer: MCPAppArgumentsPointerInput}, true
			}
		}
	}
	if strings.EqualFold(asString(payload["name"]), "use_tool") || strings.EqualFold(asString(payload["toolName"]), "use_tool") {
		qualified := asString(input["tool_name"])
		qualified = strings.TrimPrefix(qualified, "mcp__")
		if server, tool, ok := matchMCPServerTool(qualified, "__", servers); ok && payloadObject(input["tool_input"]) != nil {
			return mcpAppToolCall{ServerName: server, ToolName: tool, ArgumentsPointer: MCPAppArgumentsPointerGrokToolInput}, true
		}
		return mcpAppToolCall{}, false
	}
	if _, ok := input["arguments"].(map[string]any); ok {
		for _, name := range names {
			if server, tool, ok := matchMCPServerTool(name, ".", servers); ok {
				return mcpAppToolCall{ServerName: server, ToolName: tool, ArgumentsPointer: MCPAppArgumentsPointerCodexArguments}, true
			}
		}
	}
	return mcpAppToolCall{}, false
}

// mcpAppToolInput mirrors canonical compactToolInput: rawInput keys are folded
// into input, so pointers under /input address the stored payload.
func mcpAppToolInput(payload map[string]any) map[string]any {
	input := payloadObject(payload["input"])
	raw := payloadObject(firstPresentPayloadValue(input["rawInput"], input["raw_input"]))
	if len(raw) == 0 {
		return input
	}
	merged := make(map[string]any, len(input)+len(raw))
	for key, value := range raw {
		merged[key] = value
	}
	for key, value := range input {
		merged[key] = value
	}
	return merged
}

func firstPresentPayloadValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func matchMCPServerTool(qualified, separator string, servers map[string]MCPAppServer) (string, string, bool) {
	qualified = strings.TrimSpace(qualified)
	if qualified == "" {
		return "", "", false
	}
	best := ""
	for name := range servers {
		if len(name) > len(best) && strings.HasPrefix(qualified, name+separator) && len(qualified) > len(name)+len(separator) {
			best = name
		}
	}
	if best == "" {
		return "", "", false
	}
	return best, qualified[len(best)+len(separator):], true
}
