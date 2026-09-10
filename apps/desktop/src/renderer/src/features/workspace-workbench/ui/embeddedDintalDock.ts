import type {
  WorkbenchContribution,
  WorkbenchHostHandle
} from "@tutti-os/workbench-surface";
import {
  createElement,
  type ReactNode,
  useEffect,
  useSyncExternalStore
} from "react";
import { installHostAgentSessionBridge } from "../../../platform/desktop/web/webHostBridgeClient.ts";
import { registerEmbeddedHostCreatedSessionOpener } from "../../workspace-agent/services/internal/embeddedHostCreatedSessionOpener.ts";
import {
  agentGuiWorkbenchOpenSessionActivationType,
  workspaceAgentGuiNodeID
} from "../services/workspaceAgentGuiLaunch.ts";

type EmbeddedDintalDockNode = NonNullable<
  WorkbenchContribution["nodes"]
>[number];
type EmbeddedDintalDockBodyContext = Parameters<
  EmbeddedDintalDockNode["renderBody"]
>[0];

interface EmbeddedDintalDockBodyProps {
  context: EmbeddedDintalDockBodyContext;
  renderBody: EmbeddedDintalDockNode["renderBody"];
  railStore: EmbeddedDintalDockRailStore;
}

type EmbeddedDintalDockHeaderContext = Parameters<
  NonNullable<EmbeddedDintalDockNode["renderHeader"]>
>[0];

interface EmbeddedDintalDockHeaderProps {
  context: EmbeddedDintalDockHeaderContext;
  renderHeader: NonNullable<EmbeddedDintalDockNode["renderHeader"]>;
  railStore: EmbeddedDintalDockRailStore;
}

interface EmbeddedDintalDockRailPresentation {
  conversationRailNarrowExpanded: boolean;
  onConversationRailNarrowExpandedChange(expanded: boolean): void;
}

interface EmbeddedDintalDockRailStore {
  get(nodeId: string): boolean;
  set(nodeId: string, expanded: boolean): void;
  subscribe(nodeId: string, listener: () => void): () => void;
}

function createEmbeddedDintalDockRailStore(): EmbeddedDintalDockRailStore {
  const expandedNodeIds = new Set<string>();
  const listenersByNodeId = new Map<string, Set<() => void>>();
  return {
    get: (nodeId) => expandedNodeIds.has(nodeId),
    set(nodeId, expanded) {
      if (expanded === expandedNodeIds.has(nodeId)) {
        return;
      }
      if (expanded) {
        expandedNodeIds.add(nodeId);
      } else {
        expandedNodeIds.delete(nodeId);
      }
      for (const listener of listenersByNodeId.get(nodeId) ?? []) {
        listener();
      }
    },
    subscribe(nodeId, listener) {
      const listeners = listenersByNodeId.get(nodeId) ?? new Set();
      listeners.add(listener);
      listenersByNodeId.set(nodeId, listeners);
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
          listenersByNodeId.delete(nodeId);
        }
      };
    }
  };
}

type EmbeddedDintalDockHostWithController =
  EmbeddedDintalDockBodyContext["host"] & {
    controller: {
      getSnapshot: EmbeddedDintalDockBodyContext["host"]["getSnapshot"];
      subscribe(listener: () => void): () => void;
    };
  };

type EmbeddedDintalDockHost = Pick<
  WorkbenchHostHandle,
  | "activateNode"
  | "closeNode"
  | "exitFullscreenNode"
  | "focusNode"
  | "getSnapshot"
  | "launchNode"
  | "load"
>;

export function installEmbeddedDintalDockSessionBridge(
  host: EmbeddedDintalDockHost,
  windowRef: Window = window
): () => void {
  const openSession = (agentSessionId: string): boolean =>
    activateEmbeddedDintalDockSession(host, agentSessionId);
  const unregisterOpener = registerEmbeddedHostCreatedSessionOpener(openSession);
  const uninstallBridge = installHostAgentSessionBridge(openSession, windowRef);
  return () => {
    uninstallBridge();
    unregisterOpener();
  };
}

export function activateEmbeddedDintalDockSession(
  host: EmbeddedDintalDockHost,
  agentSessionId: string
): boolean {
  const snapshot = host.getSnapshot();
  const nodeId = [...snapshot.nodeStack]
    .reverse()
    .find((id) => snapshot.nodes.some(
      (node) => node.id === id && node.data.typeId === workspaceAgentGuiNodeID
    ));
  if (!nodeId) return false;
  host.activateNode(
    { nodeId },
    {
      payload: { agentSessionId },
      type: agentGuiWorkbenchOpenSessionActivationType
    }
  );
  host.focusNode(nodeId);
  return true;
}

export function isEmbeddedDintalDock(
  search = globalThis.location?.search ?? ""
): boolean {
  return new URLSearchParams(search).has("tuttiBootstrap");
}

function projectEmbeddedDintalDockNodeContext<
  TContext extends {
    node: { frame: { height: number; width: number; x: number; y: number } };
  }
>(context: TContext, surfaceSize: { height: number; width: number }): TContext {
  if (
    !Number.isFinite(surfaceSize.width) ||
    !Number.isFinite(surfaceSize.height) ||
    surfaceSize.width <= 0 ||
    surfaceSize.height <= 0
  ) {
    return context;
  }
  return {
    ...context,
    node: {
      ...context.node,
      frame: {
        height: surfaceSize.height,
        width: surfaceSize.width,
        x: 0,
        y: 0
      }
    }
  } as TContext;
}

export function renderEmbeddedDintalDockBody(
  context: EmbeddedDintalDockBodyContext,
  renderBody: EmbeddedDintalDockNode["renderBody"]
): ReactNode {
  return renderBody(
    projectEmbeddedDintalDockNodeContext(
      context,
      context.host.getSnapshot().surfaceSize
    )
  );
}

function EmbeddedDintalDockBody({
  context,
  renderBody,
  railStore
}: EmbeddedDintalDockBodyProps): ReactNode {
  const host = context.host as EmbeddedDintalDockHostWithController;
  useSyncExternalStore(
    (listener) => host.controller.subscribe(listener),
    () => host.controller.getSnapshot().surfaceSize,
    () => host.controller.getSnapshot().surfaceSize
  );
  const nodeId = context.node.id;
  const narrowExpanded = useSyncExternalStore(
    (listener) => railStore.subscribe(nodeId, listener),
    () => railStore.get(nodeId),
    () => railStore.get(nodeId)
  );
  const projectedContext = projectEmbeddedDintalDockNodeContext(
    context,
    context.host.getSnapshot().surfaceSize
  );
  return renderBody({
    ...projectedContext,
    conversationRailNarrowExpanded: narrowExpanded,
    onConversationRailNarrowExpandedChange: (expanded: boolean) =>
      railStore.set(nodeId, expanded)
  } as typeof projectedContext & EmbeddedDintalDockRailPresentation);
}

function EmbeddedDintalDockHeader({
  context,
  renderHeader,
  railStore
}: EmbeddedDintalDockHeaderProps): ReactNode {
  const nodeId = context.node.id;
  const narrowExpanded = useSyncExternalStore(
    (listener) => railStore.subscribe(nodeId, listener),
    () => railStore.get(nodeId),
    () => railStore.get(nodeId)
  );
  useEffect(() => {
    if (!narrowExpanded) {
      return;
    }
    document
      .getElementById("agent-gui-conversation-rail")
      ?.querySelector<HTMLElement>(
        'button:not(:disabled), [href], input:not(:disabled), [tabindex]:not([tabindex="-1"])'
      )
      ?.focus();
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      railStore.set(nodeId, false);
      globalThis.requestAnimationFrame?.(() => {
        document
          .querySelector<HTMLElement>(
            '[data-testid="agent-gui-toggle-conversation-rail"]'
          )
          ?.focus();
      });
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [narrowExpanded, nodeId, railStore]);

  const projectedContext = projectEmbeddedDintalDockNodeContext(
    context,
    context.surfaceSize
  );
  return renderHeader({
    ...projectedContext,
    conversationRailNarrowExpanded: narrowExpanded,
    dragHandleProps: {
      ...projectedContext.dragHandleProps,
      onDoubleClick: undefined,
      onPointerDown: undefined
    },
    onConversationRailNarrowExpandedChange: (expanded: boolean) =>
      railStore.set(nodeId, expanded)
  } as typeof projectedContext & EmbeddedDintalDockRailPresentation);
}

export function applyEmbeddedDintalDockContributions(
  contributions: readonly WorkbenchContribution[] | undefined
): readonly WorkbenchContribution[] | undefined {
  const railStore = createEmbeddedDintalDockRailStore();
  return contributions?.map((contribution) => ({
    ...contribution,
    nodes: contribution.nodes?.map((node) => {
      if (node.typeId !== workspaceAgentGuiNodeID) {
        return node;
      }
      const getHeaderFrameRenderKey = node.getHeaderFrameRenderKey;
      const renderBody = node.renderBody;
      const renderHeader = node.renderHeader;
      return {
        ...node,
        getHeaderFrameRenderKey: getHeaderFrameRenderKey
          ? (context) =>
              getHeaderFrameRenderKey(
                projectEmbeddedDintalDockNodeContext(
                  context,
                  context.surfaceSize
                )
              )
          : undefined,
        renderBody: (context) =>
          createElement(EmbeddedDintalDockBody, {
            context,
            railStore,
            renderBody
          }),
        renderHeader: renderHeader
          ? (context) =>
              createElement(EmbeddedDintalDockHeader, {
                context,
                railStore,
                renderHeader
              })
          : undefined
      };
    })
  }));
}

// The embedded product is a single-purpose Agent surface. Reconcile the
// persisted desktop snapshot once so stale app windows cannot reappear.
export async function reconcileEmbeddedDintalDock(
  host: EmbeddedDintalDockHost
): Promise<string | null> {
  await host.load();
  const snapshot = host.getSnapshot();
  const agents = snapshot.nodes.filter(
    (node) => node.data.typeId === workspaceAgentGuiNodeID
  );
  const keepId =
    [...snapshot.nodeStack]
      .reverse()
      .find((id) => agents.some((node) => node.id === id)) ??
    agents[0]?.id ??
    null;

  for (const node of snapshot.nodes) {
    if (node.id !== keepId) {
      host.closeNode(node.id);
    }
  }

  if (!keepId) {
    return host.launchNode({
      reason: "host",
      typeId: workspaceAgentGuiNodeID
    });
  }

  const kept = agents.find((node) => node.id === keepId);
  if (kept?.displayMode === "fullscreen") {
    host.exitFullscreenNode(keepId);
  }
  host.focusNode(keepId);
  return keepId;
}
