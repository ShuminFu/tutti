package agentruntime

import (
	"log/slog"
	"strings"
)

// RNDMASTER_ACP_MCP_TRANSPORT_FILTER is the managed Tutti source marker for
// dropping contract MCP entries the target extension does not declare.
const RNDMASTER_ACP_MCP_TRANSPORT_FILTER = "RNDMASTER_ACP_MCP_TRANSPORT_FILTER"

func rndmasterACPFilterMCPTransport() bool {
	return RNDMASTER_ACP_MCP_TRANSPORT_FILTER != ""
}

func rndmasterACPServerTransport(raw any) string {
	body := payloadObject(raw)
	kind := strings.ToLower(strings.TrimSpace(asString(body["type"])))
	if kind == "http" || kind == "sse" || strings.TrimSpace(asString(body["url"])) != "" {
		return "http"
	}
	return "stdio"
}

func rndmasterACPHasHTTP(servers []any) bool {
	for _, raw := range servers {
		if rndmasterACPServerTransport(raw) == "http" {
			return true
		}
	}
	return false
}

// rndmasterFilterACPMCPServers drops contract MCP entries the target does not
// accept: stdio:false discards stdio (WARN with server name and target id);
// HTTP follows supportsHTTPMCP (declared http or initialize http).
func rndmasterFilterACPMCPServers(servers []any, stdioDeclared *bool, httpSupported bool, targetID string) []any {
	if !rndmasterACPFilterMCPTransport() {
		return servers
	}
	out := make([]any, 0, len(servers))
	for _, raw := range servers {
		name := strings.TrimSpace(asString(payloadObject(raw)["name"]))
		switch rndmasterACPServerTransport(raw) {
		case "http":
			if !httpSupported {
				continue
			}
		default:
			if stdioDeclared != nil && !*stdioDeclared {
				slog.Warn("dropping stdio MCP server unsupported by target",
					"mcp_server", name, "agent_target_id", strings.TrimSpace(targetID))
				continue
			}
		}
		out = append(out, raw)
	}
	return out
}
