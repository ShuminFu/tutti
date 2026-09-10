import { workspaceAgentGuiNodeID } from "../services/workspaceAgentGuiLaunch.ts";

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
