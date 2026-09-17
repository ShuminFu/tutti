import {
  JSON_RPC_METHOD_NOT_FOUND,
  MCP_APP_METHOD,
  MCP_APPS_PROTOCOL_VERSION,
  type JsonRpcId,
  type JsonRpcMessage,
  type McpCallToolResult,
  type McpUiHostContext,
  type McpUiResourceCsp
} from "./mcpAppProtocol";

/** Size reported by the View, already clamped to the host's bounds. */
export interface McpAppViewSize {
  height: number;
}

export const MCP_APP_MIN_HEIGHT_PX = 40;
export const MCP_APP_MAX_HEIGHT_PX = 800;

const HOST_INFO = { name: "tutti-agent-gui", version: "1.0.0" } as const;
const TEARDOWN_TIMEOUT_MS = 500;

export interface McpAppHostBridgeOptions {
  /** Delivers one JSON-RPC message to the View (iframe contentWindow). */
  postToView: (message: JsonRpcMessage) => void;
  /** Full host context, read when the View initializes. */
  getHostContext: () => McpUiHostContext;
  /** CSP actually applied to the iframe, echoed as `hostCapabilities.sandbox`. */
  appliedCsp: McpUiResourceCsp;
  toolArguments: Record<string, unknown>;
  toolResult: McpCallToolResult;
  onSizeChanged: (size: McpAppViewSize) => void;
}

/**
 * Display-only MCP Apps host side of one View.
 *
 * The bridge is transport-agnostic: the React frame feeds it messages that
 * already passed the `event.source` check and gives it a `postToView`
 * function. It owns only protocol ordering:
 *
 *   View `ui/initialize` → host result (hostContext, capabilities)
 *   View `ui/notifications/initialized` → host `tool-input` then `tool-result`
 *
 * Per spec the host sends nothing to the View before `initialized`, so theme
 * changes that happen earlier are simply picked up from `getHostContext()`
 * during initialize. Every request this host does not implement (tools/call,
 * ui/message, ui/open-link, ui/update-model-context, resources/read, …) gets
 * JSON-RPC `-32601`, and no capability for them is advertised.
 */
export class McpAppHostBridge {
  private readonly options: McpAppHostBridgeOptions;
  private initialized = false;
  private disposed = false;
  private nextRequestId = 1;
  private readonly pendingResponses = new Map<JsonRpcId, () => void>();

  constructor(options: McpAppHostBridgeOptions) {
    this.options = options;
  }

  get isInitialized(): boolean {
    return this.initialized;
  }

  handleViewMessage(data: unknown): void {
    if (this.disposed || !isJsonRpcMessage(data)) {
      return;
    }
    if ("method" in data && typeof data.method === "string") {
      if ("id" in data && isJsonRpcId(data.id)) {
        this.handleRequest(data.id, data.method);
        return;
      }
      this.handleNotification(data.method, data.params);
      return;
    }
    if ("id" in data && isJsonRpcId(data.id)) {
      const resolve = this.pendingResponses.get(data.id);
      if (resolve) {
        this.pendingResponses.delete(data.id);
        resolve();
      }
    }
  }

  /** Sends a partial host-context update; ignored until the View initialized. */
  notifyHostContextChanged(partial: McpUiHostContext): void {
    if (this.disposed || !this.initialized) {
      return;
    }
    this.options.postToView({
      jsonrpc: "2.0",
      method: MCP_APP_METHOD.hostContextChanged,
      params: partial
    });
  }

  /**
   * Sends `ui/resource-teardown` and resolves when the View answers or after a
   * short timeout, then stops handling messages. A View that never
   * initialized has no protocol session to tear down.
   */
  teardown(): Promise<void> {
    if (this.disposed) {
      return Promise.resolve();
    }
    if (!this.initialized) {
      this.dispose();
      return Promise.resolve();
    }
    const id = `host-${this.nextRequestId++}`;
    return new Promise<void>((resolve) => {
      const finish = (): void => {
        clearTimeout(timer);
        this.pendingResponses.delete(id);
        this.dispose();
        resolve();
      };
      const timer = setTimeout(finish, TEARDOWN_TIMEOUT_MS);
      this.pendingResponses.set(id, finish);
      this.options.postToView({
        jsonrpc: "2.0",
        id,
        method: MCP_APP_METHOD.resourceTeardown,
        params: {}
      });
    });
  }

  /** Stops the bridge immediately (e.g. the iframe navigated away). */
  dispose(): void {
    this.disposed = true;
    this.pendingResponses.clear();
  }

  private handleRequest(id: JsonRpcId, method: string): void {
    switch (method) {
      case MCP_APP_METHOD.initialize:
        this.respond(id, {
          protocolVersion: MCP_APPS_PROTOCOL_VERSION,
          hostInfo: { ...HOST_INFO },
          hostCapabilities: {
            sandbox: { csp: this.options.appliedCsp }
          },
          hostContext: this.options.getHostContext()
        });
        return;
      case MCP_APP_METHOD.ping:
        this.respond(id, {});
        return;
      case MCP_APP_METHOD.requestDisplayMode:
        // The spec requires the host to answer with the resulting mode. Only
        // inline exists here, so the answer is always the current mode.
        this.respond(id, { mode: "inline" });
        return;
      default:
        this.options.postToView({
          jsonrpc: "2.0",
          id,
          error: {
            code: JSON_RPC_METHOD_NOT_FOUND,
            message: `Method not supported by this host: ${method}`
          }
        });
    }
  }

  private handleNotification(method: string, params: unknown): void {
    switch (method) {
      case MCP_APP_METHOD.initialized:
        if (this.initialized) {
          return;
        }
        this.initialized = true;
        // tool-input is sent at most once and must precede tool-result.
        this.options.postToView({
          jsonrpc: "2.0",
          method: MCP_APP_METHOD.toolInput,
          params: { arguments: this.options.toolArguments }
        });
        this.options.postToView({
          jsonrpc: "2.0",
          method: MCP_APP_METHOD.toolResult,
          params: this.options.toolResult
        });
        return;
      case MCP_APP_METHOD.sizeChanged: {
        const height = readFiniteNumber(params, "height");
        if (height !== null) {
          this.options.onSizeChanged({ height: clampMcpAppHeight(height) });
        }
        return;
      }
      default:
        // notifications/message, request-teardown, unknown notifications:
        // nothing to do, and notifications never get a response.
        return;
    }
  }

  private respond(id: JsonRpcId, result: Record<string, unknown>): void {
    this.options.postToView({ jsonrpc: "2.0", id, result });
  }
}

/**
 * Keeps a View between 40px (never collapses to an invisible strip) and 800px
 * (one widget cannot push the whole transcript away); taller content scrolls
 * inside the iframe document.
 */
export function clampMcpAppHeight(height: number): number {
  return Math.round(
    Math.min(MCP_APP_MAX_HEIGHT_PX, Math.max(MCP_APP_MIN_HEIGHT_PX, height))
  );
}

function isJsonRpcMessage(value: unknown): value is JsonRpcMessage {
  return (
    Boolean(value) &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    (value as { jsonrpc?: unknown }).jsonrpc === "2.0"
  );
}

function isJsonRpcId(value: unknown): value is JsonRpcId {
  return (
    (typeof value === "string" && value.length > 0) ||
    (typeof value === "number" && Number.isFinite(value))
  );
}

function readFiniteNumber(params: unknown, key: string): number | null {
  if (!params || typeof params !== "object") {
    return null;
  }
  const value = (params as Record<string, unknown>)[key];
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}
