import type {
  AgentProviderTerminalCommandRunner,
  AgentProviderStatusActionContext
} from "@renderer/features/workspace-agent";
import type { AgentProviderTerminalCommand } from "@tutti-os/client-tuttid-ts";
import type { DesktopRuntimeApi } from "@preload/types";
import type { WorkbenchHostHandle } from "@tutti-os/workbench-surface";
import { classifyDesktopErrorCode } from "../../../../../shared/errors/desktopErrors.ts";
import {
  isHostBridgeAvailable,
  requestHostCapability
} from "@renderer/platform/desktop/web/webHostBridgeClient";
import { defaultWorkspaceTerminalWorkbenchTypeId } from "./internal/workspaceTerminalWorkbenchConstants.ts";

export function createAgentProviderTerminalCommandRunner(
  runtimeApi: DesktopRuntimeApi
): AgentProviderTerminalCommandRunner {
  return {
    async runTerminalCommand(command, context) {
      logTerminalCommandEvent(runtimeApi, {
        command,
        context,
        event: "agent-provider.terminal-command.start",
        level: "info"
      });
      const host = readWorkbenchHost(context);
      if (!host) {
        logTerminalCommandEvent(runtimeApi, {
          command,
          context,
          event: "agent-provider.terminal-command.missing-host",
          level: "error"
        });
        // Embedded DinTalDock: the host shell owns the terminal (the web build
        // has no workbench node surface), so delegate the launch to the
        // embedding host over the postMessage bridge. Outside an iframe the
        // desktop error below is preserved.
        if (isHostBridgeAvailable()) {
          return launchTerminalCommandViaHostBridge(command, context);
        }
        throw new Error("Missing workbench host for terminal command.");
      }
      try {
        const nodeId = await launchTerminalCommand({
          command,
          host,
          runtimeApi,
          workspaceId: context?.workspaceId
        });
        logTerminalCommandEvent(runtimeApi, {
          command,
          context,
          event: "agent-provider.terminal-command.complete",
          level: "info"
        });
        return {
          close: () => {
            try {
              host.closeNode(nodeId);
            } catch (error) {
              logTerminalCommandEvent(runtimeApi, {
                command,
                context,
                error,
                event: "agent-provider.terminal-command.close-error",
                level: "warn",
                nodeId
              });
            }
          }
        };
      } catch (error) {
        logTerminalCommandEvent(runtimeApi, {
          command,
          context,
          error,
          event: "agent-provider.terminal-command.error",
          level: "error"
        });
        // Embedded DinTalDock: the workbench surface registers no terminal node
        // type (single-purpose Agent surface), so launchNode fails here too.
        // Delegate to the embedding host over the bridge whenever embedded.
        if (isHostBridgeAvailable()) {
          return launchTerminalCommandViaHostBridge(command, context);
        }
        throw error;
      }
    }
  };
}

function readWorkbenchHost(
  context: AgentProviderStatusActionContext | undefined
): WorkbenchHostHandle | null {
  const host = context?.workbenchHost;
  if (!host || typeof host !== "object") {
    return null;
  }
  if (!("launchNode" in host) || typeof host.launchNode !== "function") {
    return null;
  }
  return host as WorkbenchHostHandle;
}

async function launchTerminalCommand(input: {
  command: AgentProviderTerminalCommand;
  host: WorkbenchHostHandle;
  runtimeApi: DesktopRuntimeApi;
  workspaceId?: string;
}): Promise<string> {
  const exitedFullscreenNodeId = exitFocusedFullscreenNode(input.host);
  if (exitedFullscreenNodeId) {
    logTerminalCommandEvent(input.runtimeApi, {
      command: input.command,
      event: "agent-provider.terminal-command.exit-fullscreen",
      level: "info",
      workspaceId: input.workspaceId,
      extraDetails: {
        exitedFullscreenNodeId
      }
    });
  }
  const nodeId = await input.host.launchNode({
    payload: {
      cwd: input.command.cwd,
      initialInput: terminalRunInput(input.command.input)
    },
    reason: "host",
    typeId: defaultWorkspaceTerminalWorkbenchTypeId
  });
  logTerminalCommandEvent(input.runtimeApi, {
    command: input.command,
    event: "agent-provider.terminal-command.launch-node-result",
    level: nodeId ? "info" : "error",
    nodeId,
    workspaceId: input.workspaceId
  });
  if (!nodeId) {
    throw new Error("Terminal command did not open a workbench node.");
  }
  return nodeId;
}

// launchTerminalCommandViaHostBridge opens the terminal through the embedding
// host (DinTalClaw shell) when the embedded web build has no workbench node
// surface. The host owns the PTY lifecycle; we only carry a close handle.
async function launchTerminalCommandViaHostBridge(
  command: AgentProviderTerminalCommand,
  context: AgentProviderStatusActionContext | undefined
): Promise<{ close: () => void }> {
  const result = await requestHostCapability<{ id?: string } | null>(
    "launchTerminal",
    [{ input: command.input, cwd: command.cwd ?? null }]
  );
  const id = result?.id;
  if (!id || typeof id !== "string" || !id.trim()) {
    throw new Error("Host did not open a terminal for the login command.");
  }
  return {
    close: () => {
      void requestHostCapability("closeTerminal", [id]).catch(() => undefined);
    }
  };
}

function terminalRunInput(input: string): string {
  return /[\r\n]$/u.test(input) ? input : `${input}\n`;
}

function exitFocusedFullscreenNode(host: WorkbenchHostHandle): string | null {
  const snapshot = host.getSnapshot();
  const focusedNodeId = snapshot?.nodeStack.at(-1);
  if (!focusedNodeId) {
    return null;
  }
  const focusedNode = snapshot.nodes.find((node) => node.id === focusedNodeId);
  if (focusedNode?.displayMode !== "fullscreen") {
    return null;
  }
  host.exitFullscreenNode(focusedNode.id);
  return focusedNode.id;
}

function logTerminalCommandEvent(
  runtimeApi: DesktopRuntimeApi,
  input: {
    command: AgentProviderTerminalCommand;
    context?: AgentProviderStatusActionContext;
    error?: unknown;
    event: string;
    extraDetails?: Record<string, string | number | boolean | null>;
    level: "debug" | "info" | "warn" | "error";
    nodeId?: string | null;
    workspaceId?: string;
  }
): void {
  const workspaceId = input.workspaceId ?? input.context?.workspaceId ?? null;
  void runtimeApi
    .logTerminalDiagnostic({
      details: {
        commandLength: input.command.input.length,
        commandHasTrailingNewline: /[\r\n]$/u.test(input.command.input),
        cwd: input.command.cwd ?? null,
        errorCode: input.error ? classifyDesktopErrorCode(input.error) : null,
        errorMessage: input.error instanceof Error ? input.error.message : null,
        hasWorkbenchHost: input.context
          ? readWorkbenchHost(input.context) !== null
          : true,
        ...input.extraDetails
      },
      event: input.event,
      level: input.level,
      nodeId: input.nodeId ?? null,
      workspaceId
    })
    .catch(() => undefined);
}
