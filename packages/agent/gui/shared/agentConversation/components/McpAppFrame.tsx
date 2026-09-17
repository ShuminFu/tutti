import { useLayoutEffect, useMemo, useRef, useState, type JSX } from "react";
import { getActiveUiLanguage } from "../../../i18n/index";
import {
  buildMcpAppContentSecurityPolicy,
  injectMcpAppContentSecurityPolicy,
  sanitizeMcpAppCspOrigins
} from "../mcpApps/mcpAppCsp";
import {
  MCP_APP_MAX_HEIGHT_PX,
  MCP_APP_MIN_HEIGHT_PX,
  McpAppHostBridge
} from "../mcpApps/mcpAppHostBridge";
import {
  observeMcpAppThemeSources,
  readMcpAppThemeContext,
  sameMcpAppThemeContext
} from "../mcpApps/mcpAppHostContext";
import type {
  McpCallToolResult,
  McpUiHostContext,
  McpUiResourceCsp
} from "../mcpApps/mcpAppProtocol";

export interface McpAppFrameProps {
  html: string;
  csp: McpUiResourceCsp | null | undefined;
  prefersBorder?: boolean;
  title: string;
  toolName: string;
  toolArguments: Record<string, unknown>;
  toolResult: McpCallToolResult;
}

/**
 * Single-layer MCP Apps sandbox: `sandbox="allow-scripts"` without
 * `allow-same-origin` gives the View an opaque origin, so it cannot read the
 * host DOM, storage or cookies (`parent.document` throws), cannot open popups,
 * submit forms or navigate the top window. Network access is limited by the
 * CSP meta injected into `srcdoc`. Mount one frame per resource (key it by
 * the resource hash); protocol state is not reset for a changed `html`.
 */
export function McpAppFrame({
  html,
  csp,
  prefersBorder,
  title,
  toolName,
  toolArguments,
  toolResult
}: McpAppFrameProps): JSX.Element {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const bridgeRef = useRef<McpAppHostBridge | null>(null);
  const loadCountRef = useRef(0);
  const [height, setHeight] = useState(MCP_APP_MIN_HEIGHT_PX);
  const [navigatedAway, setNavigatedAway] = useState(false);

  const cspKey = JSON.stringify([
    csp?.resourceDomains ?? [],
    csp?.connectDomains ?? []
  ]);
  const appliedCsp = useMemo<McpUiResourceCsp>(() => {
    const [resourceDomains, connectDomains] = JSON.parse(cspKey) as [
      unknown[],
      unknown[]
    ];
    return {
      resourceDomains: sanitizeMcpAppCspOrigins(resourceDomains),
      connectDomains: sanitizeMcpAppCspOrigins(connectDomains)
    };
  }, [cspKey]);
  const srcDoc = useMemo(
    () =>
      injectMcpAppContentSecurityPolicy(
        html,
        buildMcpAppContentSecurityPolicy(appliedCsp)
      ),
    [appliedCsp, html]
  );

  // `tool-input` is sent at most once per View, so the frame keeps the data it
  // was mounted with; later projections of the same completed call are equal.
  const [toolData] = useState(() => ({ toolName, toolArguments, toolResult }));

  // Layout effect: the listener must exist before the srcdoc document can run
  // its first script and post `ui/initialize`.
  useLayoutEffect(() => {
    const iframe = iframeRef.current;
    const container = containerRef.current;
    if (!iframe || !container) {
      return;
    }
    const ownerDocument = container.ownerDocument;
    const ownerWindow = ownerDocument.defaultView;
    if (!ownerWindow) {
      return;
    }
    const readContainerWidth = (): number =>
      Math.round(container.getBoundingClientRect().width);
    let lastTheme = readMcpAppThemeContext(container);
    let lastWidth = readContainerWidth();

    const bridge = new McpAppHostBridge({
      // A sandboxed srcdoc document has the opaque origin "null", which cannot
      // be named as a targetOrigin, so "*" is required. The data is only what
      // this View already renders (its own tool input and theme tokens).
      postToView: (message) => iframe.contentWindow?.postMessage(message, "*"),
      getHostContext: (): McpUiHostContext => ({
        ...readMcpAppThemeContext(container),
        toolInfo: {
          tool: { name: toolData.toolName, inputSchema: { type: "object" } }
        },
        displayMode: "inline",
        availableDisplayModes: ["inline"],
        containerDimensions: {
          width: readContainerWidth(),
          maxHeight: MCP_APP_MAX_HEIGHT_PX
        },
        locale: getActiveUiLanguage(),
        timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        platform: "desktop"
      }),
      appliedCsp,
      toolArguments: toolData.toolArguments,
      toolResult: toolData.toolResult,
      onSizeChanged: ({ height: nextHeight }) => setHeight(nextHeight)
    });
    bridgeRef.current = bridge;

    const onMessage = (event: MessageEvent): void => {
      // Only the WindowProxy identity can attribute a message to this frame:
      // its origin is "null" (shared by every sandboxed/opaque document), and
      // the host page also receives messages from other frames and from an
      // embedding parent (rndmaster web host).
      if (!event.source || event.source !== iframe.contentWindow) {
        return;
      }
      bridge.handleViewMessage(event.data);
    };
    ownerWindow.addEventListener("message", onMessage);

    const stopThemeObservation = observeMcpAppThemeSources(
      ownerDocument,
      () => {
        const nextTheme = readMcpAppThemeContext(container);
        if (sameMcpAppThemeContext(lastTheme, nextTheme)) {
          return;
        }
        lastTheme = nextTheme;
        bridge.notifyHostContextChanged(nextTheme);
      }
    );

    let resizeObserver: ResizeObserver | null = null;
    if (typeof ResizeObserver === "function") {
      // Forwards width changes as containerDimensions; disconnected on unmount.
      // presentation-work: lifetime of the mounted View only
      resizeObserver = new ResizeObserver(() => {
        const nextWidth = readContainerWidth();
        if (nextWidth === lastWidth) {
          return;
        }
        lastWidth = nextWidth;
        bridge.notifyHostContextChanged({
          containerDimensions: {
            width: nextWidth,
            maxHeight: MCP_APP_MAX_HEIGHT_PX
          }
        });
      });
      resizeObserver.observe(container);
    }

    return () => {
      stopThemeObservation();
      resizeObserver?.disconnect();
      bridgeRef.current = null;
      // Best effort: React removes the iframe right after this cleanup, so a
      // View's teardown answer may never arrive; the bridge times out.
      void bridge.teardown().finally(() => {
        ownerWindow.removeEventListener("message", onMessage);
      });
    };
  }, [appliedCsp, srcDoc, toolData]);

  const handleLoad = (): void => {
    loadCountRef.current += 1;
    if (loadCountRef.current === 1) {
      return;
    }
    // A second load means the View navigated its own frame away from the
    // srcdoc document (self-navigation is not blockable by CSP). That document
    // is outside our policy, so stop talking to it and drop the frame.
    bridgeRef.current?.dispose();
    setNavigatedAway(true);
  };

  if (navigatedAway) {
    return <div ref={containerRef} hidden data-testid="agent-mcp-app-frame" />;
  }

  return (
    <div
      ref={containerRef}
      className={
        prefersBorder === false
          ? "block w-full max-w-full"
          : "block w-full max-w-full overflow-hidden rounded-[10px] border border-[var(--line-2)]"
      }
      data-testid="agent-mcp-app-frame"
    >
      <iframe
        ref={iframeRef}
        className="block w-full border-0 bg-transparent"
        referrerPolicy="no-referrer"
        sandbox="allow-scripts"
        srcDoc={srcDoc}
        // Inline geometry so the frame never falls back to the 300px default
        // iframe width or UA border if a host bundle lacks these utilities.
        style={{ border: 0, display: "block", height, width: "100%" }}
        title={title}
        onLoad={handleLoad}
      />
    </div>
  );
}
