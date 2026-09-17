/**
 * Minimal host-side subset of the MCP Apps protocol (SEP-1865,
 * `io.modelcontextprotocol/ui`, spec version 2026-01-26).
 *
 * Field and method names are copied from
 * https://github.com/modelcontextprotocol/ext-apps/blob/main/src/spec.types.ts
 * instead of importing `@modelcontextprotocol/ext-apps`: its `AppBridge`
 * peer-depends on `@modelcontextprotocol/client`/`core` v2 and zod ^4.2, none
 * of which AgentGUI ships, and this host is display-only (no tools/call,
 * ui/message, ui/open-link, ui/update-model-context), so the bridge would be
 * mostly unused weight in the renderer and the rndmaster web bundle.
 */

export const MCP_APPS_PROTOCOL_VERSION = "2026-01-26";

/** MIME type a UI resource must carry (`text/html;profile=mcp-app`). */
export const MCP_APP_RESOURCE_MIME_TYPE = "text/html;profile=mcp-app";

export const MCP_APP_METHOD = {
  initialize: "ui/initialize",
  initialized: "ui/notifications/initialized",
  toolInput: "ui/notifications/tool-input",
  toolResult: "ui/notifications/tool-result",
  sizeChanged: "ui/notifications/size-changed",
  hostContextChanged: "ui/notifications/host-context-changed",
  resourceTeardown: "ui/resource-teardown",
  requestDisplayMode: "ui/request-display-mode",
  ping: "ping"
} as const;

/** JSON-RPC 2.0 "Method not found". */
export const JSON_RPC_METHOD_NOT_FOUND = -32601;

export type JsonRpcId = string | number;

export interface JsonRpcRequest {
  jsonrpc: "2.0";
  id: JsonRpcId;
  method: string;
  params?: unknown;
}

export interface JsonRpcNotification {
  jsonrpc: "2.0";
  method: string;
  params?: unknown;
}

export interface JsonRpcSuccessResponse {
  jsonrpc: "2.0";
  id: JsonRpcId;
  result: Record<string, unknown>;
}

export interface JsonRpcErrorResponse {
  jsonrpc: "2.0";
  id: JsonRpcId;
  error: { code: number; message: string };
}

export type JsonRpcMessage =
  | JsonRpcRequest
  | JsonRpcNotification
  | JsonRpcSuccessResponse
  | JsonRpcErrorResponse;

export type McpUiTheme = "light" | "dark";

export interface McpUiResourceCsp {
  connectDomains?: string[];
  resourceDomains?: string[];
}

export interface McpUiHostContext {
  toolInfo?: {
    id?: JsonRpcId;
    tool: {
      name: string;
      inputSchema: Record<string, unknown>;
    };
  };
  theme?: McpUiTheme;
  styles?: {
    /** Keys are McpUiStyleVariableKey names such as `--color-text-primary`. */
    variables?: Record<string, string>;
  };
  displayMode?: "inline";
  availableDisplayModes?: Array<"inline">;
  containerDimensions?: {
    width?: number;
    maxHeight?: number;
  };
  locale?: string;
  timeZone?: string;
  platform?: "web" | "desktop" | "mobile";
}

/** MCP `CallToolResult`, limited to the fields AgentGUI can project. */
export interface McpCallToolResult {
  [key: string]: unknown;
  content: Array<{ type: "text"; text: string }>;
  structuredContent?: Record<string, unknown>;
}
