import type {
  WorkbenchContribution
} from "@tutti-os/workbench-surface";
import { createElement, type ReactNode, useSyncExternalStore } from "react";
import { workspaceAgentGuiNodeID } from "../services/workspaceAgentGuiLaunch.ts";

type EmbeddedDintalDockNode = NonNullable<
  WorkbenchContribution["nodes"]
>[number];
type EmbeddedDintalDockBodyContext = Parameters<
  EmbeddedDintalDockNode["renderBody"]
>[0];

interface EmbeddedDintalDockBodyProps {
  context: EmbeddedDintalDockBodyContext;
  renderBody: EmbeddedDintalDockNode["renderBody"];
}

type EmbeddedDintalDockHostWithController =
  EmbeddedDintalDockBodyContext["host"] & {
    controller: {
      getSnapshot: EmbeddedDintalDockBodyContext["host"]["getSnapshot"];
      subscribe(listener: () => void): () => void;
    };
  };

interface EmbeddedDintalDockHost {
  closeNode(nodeId: string): void;
  exitFullscreenNode(nodeId: string): void;
  focusNode(nodeId: string): void;
  getSnapshot(): {
    nodeStack: readonly string[];
    nodes: readonly {
      data: { typeId: string };
      displayMode: string;
      id: string;
    }[];
  };
  launchNode(input: { reason: "host"; typeId: string }): Promise<string | null>;
  load(): Promise<void>;
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
>(
  context: TContext,
  surfaceSize: { height: number; width: number }
): TContext {
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
  renderBody
}: EmbeddedDintalDockBodyProps): ReactNode {
  const host = context.host as EmbeddedDintalDockHostWithController;
  useSyncExternalStore(
    (listener) => host.controller.subscribe(listener),
    () => host.controller.getSnapshot().surfaceSize,
    () => host.controller.getSnapshot().surfaceSize
  );
  return renderEmbeddedDintalDockBody(context, renderBody);
}

export function applyEmbeddedDintalDockContributions(
  contributions: readonly WorkbenchContribution[] | undefined
): readonly WorkbenchContribution[] | undefined {
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
          createElement(EmbeddedDintalDockBody, { context, renderBody }),
        renderHeader: renderHeader
          ? (context) => {
              const projectedContext = projectEmbeddedDintalDockNodeContext(
                context,
                context.surfaceSize
              );
              return renderHeader({
                ...projectedContext,
                dragHandleProps: {
                  ...projectedContext.dragHandleProps,
                  onDoubleClick: undefined,
                  onPointerDown: undefined
                }
              });
            }
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
