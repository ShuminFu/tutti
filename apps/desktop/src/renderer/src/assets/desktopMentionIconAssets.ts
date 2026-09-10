import { resolveProviderIconAsset } from "@tutti-os/agent-gui/provider-icons";
import workspaceFileMentionIconUrl from "./workspace-canvas/dock/default/apps/document.png";
import workspaceFolderMentionIconUrl from "./workspace-canvas/dock/default/files.png";
import workspaceIssueMentionIconUrl from "./workspace-canvas/dock/default/issue.png";

export {
  workspaceFileMentionIconUrl,
  workspaceFolderMentionIconUrl,
  workspaceIssueMentionIconUrl
};

export const rendererSeededWorkspaceAppIconIds = [
  "agent-codex",
  "agent-claude-code",
  "agent-tutti-agent",
  "issue-manager"
] as const;

export function resolveRendererWorkspaceAppDefaultIconUrl(
  appId: string
): string | null {
  switch (appId.trim()) {
    case "agent-codex":
      return resolveProviderIconAsset("codex", "manage");
    case "agent-claude-code":
      return resolveProviderIconAsset("claude-code", "manage");
    case "agent-tutti-agent":
      return resolveProviderIconAsset("tutti", "manage");
    case "issue-manager":
      return workspaceIssueMentionIconUrl;
    default:
      return null;
  }
}

export function resolveRendererWorkspaceAppIconUrl(
  appId: string,
  iconUrl: string | null | undefined
): string | null {
  const normalizedIconUrl = iconUrl?.trim() ?? "";
  if (
    normalizedIconUrl &&
    !normalizedIconUrl.toLowerCase().startsWith("tutti-asset://")
  ) {
    return normalizedIconUrl;
  }
  return resolveRendererWorkspaceAppDefaultIconUrl(appId);
}
