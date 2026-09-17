package agentruntime

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// MCP server instructions for providers that do not surface them.
//
// An MCP server may return `instructions` in its initialize result: server-wide
// guidance ("when to use this server at all") that does not belong to any one
// tool schema. claude-agent-acp and grok put it into the model prompt. The
// codex app-server does not: it demotes the text to tool-namespace metadata
// (openai/codex#29097), so a codex session never sees e.g. the RnDMaster
// server's "draw with show_widget, read show_widget_guide first" guidance.
//
// The server stays the only owner of that text. tuttid already runs a second,
// short-lived MCP client against the contract's stdio servers (MCPAppResolver);
// a resolver that also implements MCPServerInstructionsResolver answers the
// initialize instructions, and adapters whose provider drops them ask the
// Controller for the rendered block and deliver it through their own
// instruction channel. Adapters whose provider already consumes MCP
// instructions never install the source, so nothing is injected twice.

// MCPServerInstructionsResolver is optionally implemented by an MCPAppResolver.
type MCPServerInstructionsResolver interface {
	// ServerInstructions returns the server's initialize instructions ("" when
	// it has none). Implementations cache per server fingerprint and must honor
	// ctx so a slow or broken server never stalls a turn.
	ServerInstructions(ctx context.Context, server MCPAppServer) (string, error)
}

// MCPServerInstructionsSource renders the instructions of the session's
// contract MCP servers as one prompt block, or "" (fail-open).
type MCPServerInstructionsSource func(ctx context.Context, session Session) string

// MCPServerInstructionsSourceAdapter is implemented by adapters whose provider
// CLI does not put MCP server instructions into the model prompt.
type MCPServerInstructionsSourceAdapter interface {
	SetMCPServerInstructionsSource(MCPServerInstructionsSource)
}

// mcpServerInstructionsTimeout bounds the whole lookup on the turn path. A
// cached answer returns immediately; a cold probe of a local stdio server
// takes tens of milliseconds, so this only matters for a hung server.
const mcpServerInstructionsTimeout = 2 * time.Second

// mcpServerInstructionsMaxBytes skips a server whose instructions are
// unreasonably large instead of flooding every turn's developer message.
const mcpServerInstructionsMaxBytes = 64 << 10

// mcpServerInstructionsTotalMaxBytes caps the rendered block across servers;
// sections past the cap are dropped whole (in server-name order).
const mcpServerInstructionsTotalMaxBytes = 128 << 10

// contractMCPServerInstructions is the Controller's MCPServerInstructionsSource.
func (c *Controller) contractMCPServerInstructions(ctx context.Context, session Session) string {
	resolver, ok := c.currentMCPAppResolver().(MCPServerInstructionsResolver)
	if !ok || resolver == nil {
		return ""
	}
	if strings.TrimSpace(asString(payloadObject(session.RuntimeContext["rndmaster"])["contractFile"])) == "" {
		return ""
	}
	servers := mcpInstructionsProbeServers(session)
	if len(servers) == 0 {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, mcpServerInstructionsTimeout)
	defer cancel()

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	// Probe every server in parallel: each gets the whole window instead of
	// queueing behind a slow neighbour.
	type answer struct {
		text string
		err  error
	}
	answers := make([]answer, len(names))
	var wg sync.WaitGroup
	for index, name := range names {
		wg.Add(1)
		go func(index int, server MCPAppServer) {
			defer wg.Done()
			text, err := resolver.ServerInstructions(ctx, server)
			answers[index] = answer{text: text, err: err}
		}(index, servers[name])
	}
	wg.Wait()

	sections := make([]mcpServerInstructionsSection, 0, len(names))
	total := 0
	for index, name := range names {
		text, err := strings.TrimSpace(answers[index].text), answers[index].err
		if err != nil {
			// Fail-open: the turn proceeds without this server's guidance.
			slog.Warn("mcp_server_instructions unavailable",
				"agent_session_id", session.AgentSessionID,
				"server", name,
				"error", err,
			)
			continue
		}
		if text == "" {
			continue
		}
		if len(text) > mcpServerInstructionsMaxBytes || total+len(text) > mcpServerInstructionsTotalMaxBytes {
			slog.Warn("mcp_server_instructions skipped: too large",
				"agent_session_id", session.AgentSessionID,
				"server", name,
				"bytes", len(text),
				"total_bytes", total,
			)
			continue
		}
		total += len(text)
		sections = append(sections, mcpServerInstructionsSection{server: name, text: text})
	}
	return renderMCPServerInstructions(sections)
}

type mcpServerInstructionsSection struct {
	server string
	text   string
}

func renderMCPServerInstructions(sections []mcpServerInstructionsSection) string {
	if len(sections) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# MCP Server Instructions\n\n")
	b.WriteString("The following MCP servers provided instructions for how to use their tools.")
	for _, section := range sections {
		b.WriteString("\n\n## ")
		b.WriteString(mcpServerInstructionsHeading(section.server))
		b.WriteString("\n\n")
		b.WriteString(section.text)
	}
	return b.String()
}

// mcpServerInstructionsHeading keeps a contract-supplied server name on its
// one heading line: control characters (newlines included) become spaces.
func mcpServerInstructionsHeading(name string) string {
	heading := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)), " ")
	if heading == "" {
		return "(unnamed server)"
	}
	return heading
}

// mcpInstructionsProbeServers is mcpAppContractServers with tuttid's own runtime
// instructions file removed from the environment.
//
// rndmasterMCPServers hands every stdio server TUTTI_RUNTIME_INSTRUCTIONS_FILE so
// providers that read MCP instructions also receive DinTalDock's runtime policy
// through them. A provider that needs this probe already gets that same policy
// from its own instruction file (codex: CODEX_HOME/AGENTS.md), so asking with
// the file would deliver the policy twice. Asking without it yields exactly the
// server-owned part. A server-declared value that is not the session's file is
// left alone.
//
// The fingerprint is recomputed from the server's own env without that
// per-session path, so sessions whose servers are otherwise identical share one
// cached answer.
func mcpInstructionsProbeServers(session Session) map[string]MCPAppServer {
	specs := contractStdioServerSpecs(session)
	if len(specs) == 0 {
		return nil
	}
	sessionFile := strings.TrimSpace(envValueFromList(session.Env, runtimeInstructionsFileEnv))
	out := make(map[string]MCPAppServer, len(specs))
	for name, spec := range specs {
		server := spec.server
		strip := sessionFile != "" && strings.TrimSpace(envValueFromList(server.Env, runtimeInstructionsFileEnv)) == sessionFile
		ownEnv := make([]string, 0, len(spec.ownEnv)+1)
		for _, entry := range spec.ownEnv {
			if key, _, _ := strings.Cut(entry, "="); strip && strings.EqualFold(key, runtimeInstructionsFileEnv) {
				continue
			}
			ownEnv = append(ownEnv, entry)
		}
		if strip {
			env := make([]string, 0, len(server.Env)+1)
			for _, entry := range server.Env {
				if key, _, _ := strings.Cut(entry, "="); strings.EqualFold(key, runtimeInstructionsFileEnv) {
					continue
				}
				env = append(env, entry)
			}
			// An explicit empty value, so an inherited process env cannot leak it back.
			env = append(env, runtimeInstructionsFileEnv+"=")
			server.Env = env
		}
		// Distinct from the MCP App catalog cache identity of the same server.
		server.Fingerprint = mcpAppServerFingerprint(name, server.Command, server.Args,
			append(ownEnv, "probe=instructions"))
		out[name] = server
	}
	return out
}
