import type { AgentMcpAppRowVM } from "../contracts/agentMcpAppRowVM";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import type { McpCallToolResult } from "../mcpApps/mcpAppProtocol";

const SHA256_HEX_PATTERN = /^[a-f0-9]{64}$/i;

/**
 * Projects the `mcpApp` reference tuttid attaches to a tool-call payload
 * (contract C3) into an `mcp-app` transcript row.
 *
 * Returns null — so only the ordinary tool card is shown — unless the call is
 * completed and every reference field is well formed. The argument object is
 * located only through `argumentsPointer`: tuttid is the single owner of the
 * provider-specific payload shapes (claude `/input`, grok
 * `/input/tool_input`, codex `/input/arguments`), so no shape is guessed here.
 */
export function projectAgentMcpAppRow(
  call: AgentToolCallVM,
  turnId: string
): AgentMcpAppRowVM | null {
  if (call.statusKind !== "completed" || !call.payload) {
    return null;
  }
  const reference = recordValue(call.payload.mcpApp);
  if (!reference) {
    return null;
  }
  const serverName = nonEmptyString(reference.serverName);
  const toolName = nonEmptyString(reference.toolName);
  const resourceUri = nonEmptyString(reference.resourceUri);
  const resourceSha256 = nonEmptyString(reference.resourceSha256);
  const argumentsPointer = reference.argumentsPointer;
  if (
    !serverName ||
    !toolName ||
    !resourceUri?.startsWith("ui://") ||
    !resourceSha256 ||
    !SHA256_HEX_PATTERN.test(resourceSha256) ||
    typeof argumentsPointer !== "string"
  ) {
    return null;
  }
  const toolArguments = recordValue(
    resolveJsonPointer(call.payload, argumentsPointer)
  );
  if (!toolArguments) {
    return null;
  }
  return {
    kind: "mcp-app",
    id: `mcp-app:${call.id}`,
    turnId,
    sourceCallId: call.id,
    serverName,
    toolName,
    resourceUri,
    resourceSha256: resourceSha256.toLowerCase(),
    toolArguments,
    toolResult: projectToolResult(call.output),
    occurredAtUnixMs: call.occurredAtUnixMs
  };
}

/** RFC 6901 JSON Pointer lookup over own properties only. */
export function resolveJsonPointer(
  document: unknown,
  pointer: string
): unknown {
  if (pointer === "") {
    return document;
  }
  if (!pointer.startsWith("/")) {
    return undefined;
  }
  let current: unknown = document;
  for (const rawToken of pointer.slice(1).split("/")) {
    const token = rawToken.replaceAll("~1", "/").replaceAll("~0", "~");
    if (Array.isArray(current)) {
      if (!/^(?:0|[1-9]\d*)$/.test(token)) {
        return undefined;
      }
      current = current[Number(token)];
      continue;
    }
    if (
      !current ||
      typeof current !== "object" ||
      !Object.prototype.hasOwnProperty.call(current, token)
    ) {
      return undefined;
    }
    current = (current as Record<string, unknown>)[token];
  }
  return current;
}

// Persisted tool output is provider-normalized text (grok keeps only
// `{type:"MCP"}`), so the View gets a best-effort CallToolResult. Display-only
// widgets render from tool-input; tool-result is sent for protocol order.
function projectToolResult(
  output: Record<string, unknown> | null
): McpCallToolResult {
  const text =
    nonEmptyString(output?.text) ?? nonEmptyString(output?.stdout) ?? null;
  const structuredContent = recordValue(output?.structuredContent);
  return {
    content: text ? [{ type: "text", text }] : [],
    ...(structuredContent ? { structuredContent } : {})
  };
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function nonEmptyString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
