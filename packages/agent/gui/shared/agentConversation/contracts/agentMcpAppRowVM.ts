import type { McpCallToolResult } from "../mcpApps/mcpAppProtocol";

/**
 * A completed MCP tool call whose server declared an MCP Apps UI resource
 * (`_meta.ui.resourceUri`). tuttid resolves the resource and stores an HTML
 * snapshot addressed by `resourceSha256`; AgentGUI only follows that
 * reference and the JSON Pointer to the tool arguments (contract C3).
 */
export interface AgentMcpAppRowVM {
  kind: "mcp-app";
  id: string;
  turnId: string;
  sourceCallId: string;
  serverName: string;
  toolName: string;
  resourceUri: string;
  resourceSha256: string;
  toolArguments: Record<string, unknown>;
  toolResult: McpCallToolResult;
  occurredAtUnixMs: number | null;
}
