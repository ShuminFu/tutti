import { useEffect, useState, type JSX } from "react";
import {
  useOptionalAgentGUIRuntime,
  type AgentActivityRuntimeLoadMcpAppResourceInput,
  type AgentActivityRuntimeMcpAppResource,
  type AgentGUIRuntime
} from "../../../agentActivityRuntime";
import { translate } from "../../../i18n/index";
import type { AgentMcpAppRowVM } from "../contracts/agentMcpAppRowVM";
import { MCP_APP_RESOURCE_MIME_TYPE } from "../mcpApps/mcpAppProtocol";
import { McpAppFrame } from "./McpAppFrame";

/**
 * Transcript row for an MCP Apps View. It renders nothing until the snapshot
 * is loaded, and nothing at all when the snapshot is missing, unreadable or
 * not an MCP App resource: the tool card in the preceding group remains the
 * visible record, so a failed load never blanks or errors the transcript.
 */
export function AgentMcpAppRow({
  row
}: {
  row: AgentMcpAppRowVM;
}): JSX.Element | null {
  const runtime = useOptionalAgentGUIRuntime();
  const resourceKey = mcpAppResourceCacheKey(row);
  const [loaded, setLoaded] = useState<{
    key: string;
    resource: AgentActivityRuntimeMcpAppResource | null;
  } | null>(null);

  // Keyed on the exact (workspace, snapshot) identity rather than run once on
  // mount: tuttid may attach `mcpApp` to an already rendered tool call in a
  // later message version, and the projection then mounts this row fresh.
  useEffect(() => {
    let canceled = false;
    const input = {
      workspaceId: row.workspaceId,
      resourceSha256: row.resourceSha256
    };
    const key = mcpAppResourceCacheKey(input);
    void loadMcpAppResourceCached(runtime, input).then((resource) => {
      if (!canceled) {
        setLoaded({ key, resource });
      }
    });
    return () => {
      canceled = true;
    };
  }, [row.resourceSha256, row.workspaceId, runtime]);

  const resource = loaded?.key === resourceKey ? loaded.resource : null;
  if (!resource) {
    return null;
  }
  const argumentTitle = row.toolArguments.title;
  const title =
    typeof argumentTitle === "string" && argumentTitle.trim()
      ? argumentTitle.trim()
      : row.toolName;

  return (
    <div
      className="flex w-full max-w-full"
      data-testid="agent-mcp-app-artifact"
    >
      <McpAppFrame
        key={resourceKey}
        html={resource.html}
        csp={resource.meta?.csp}
        prefersBorder={resource.meta?.prefersBorder}
        title={translate("agentHost.agentTool.details.mcpAppFrameTitle", {
          title
        })}
        toolName={row.toolName}
        toolArguments={row.toolArguments}
        toolResult={row.toolResult}
      />
    </div>
  );
}

// Snapshots are content-addressed and immutable, so a per-runtime promise
// cache is safe and keeps virtualized remounts from refetching. Misses and
// failures are not cached: a snapshot may be stored after the first read.
const MCP_APP_RESOURCE_CACHE_LIMIT = 64;
const resourceCacheByRuntime = new WeakMap<
  AgentGUIRuntime,
  Map<string, Promise<AgentActivityRuntimeMcpAppResource | null>>
>();

function mcpAppResourceCacheKey(
  input: AgentActivityRuntimeLoadMcpAppResourceInput
): string {
  return `${input.workspaceId}\u0000${input.resourceSha256}`;
}

export function loadMcpAppResourceCached(
  runtime: AgentGUIRuntime | null,
  input: AgentActivityRuntimeLoadMcpAppResourceInput
): Promise<AgentActivityRuntimeMcpAppResource | null> {
  const loadMcpAppResource = runtime?.loadMcpAppResource;
  if (!runtime || !loadMcpAppResource) {
    return Promise.resolve(null);
  }
  let cache = resourceCacheByRuntime.get(runtime);
  if (!cache) {
    cache = new Map();
    resourceCacheByRuntime.set(runtime, cache);
  }
  const key = mcpAppResourceCacheKey(input);
  const cached = cache.get(key);
  if (cached) {
    return cached;
  }
  const resourceCache = cache;
  const pending: Promise<AgentActivityRuntimeMcpAppResource | null> =
    Promise.resolve()
      .then(() => loadMcpAppResource.call(runtime, input))
      .then(validMcpAppResource)
      .catch(() => null)
      .then((resource) => {
        if (!resource && resourceCache.get(key) === pending) {
          resourceCache.delete(key);
        }
        return resource;
      });
  cache.set(key, pending);
  if (cache.size > MCP_APP_RESOURCE_CACHE_LIMIT) {
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) {
      cache.delete(oldest);
    }
  }
  return pending;
}

function validMcpAppResource(
  resource: AgentActivityRuntimeMcpAppResource | null | undefined
): AgentActivityRuntimeMcpAppResource | null {
  if (
    !resource ||
    typeof resource.html !== "string" ||
    !resource.html.trim() ||
    typeof resource.mimeType !== "string" ||
    resource.mimeType.replace(/\s+/g, "").toLowerCase() !==
      MCP_APP_RESOURCE_MIME_TYPE
  ) {
    return null;
  }
  return resource;
}
