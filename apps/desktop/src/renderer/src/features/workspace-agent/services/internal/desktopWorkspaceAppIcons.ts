import type { WorkspaceAppCenterApp } from "@tutti-os/workspace-app-center";
import {
  rendererSeededWorkspaceAppIconIds,
  resolveRendererWorkspaceAppDefaultIconUrl,
  resolveRendererWorkspaceAppIconUrl
} from "../../../../assets/desktopMentionIconAssets.ts";

export interface DesktopWorkspaceAppIconEntry {
  appId: string;
  iconUrl: string;
  workspaceId: string;
}

type DesktopWorkspaceAppIconSource = Pick<
  WorkspaceAppCenterApp,
  "appId" | "availableIconUrl" | "iconUrl"
>;

export function resolveDesktopWorkspaceAppIconEntries(input: {
  apps: readonly DesktopWorkspaceAppIconSource[];
  workspaceId: string;
}): DesktopWorkspaceAppIconEntry[] {
  const entriesByKey = new Map<string, DesktopWorkspaceAppIconEntry>();
  for (const app of input.apps) {
    addWorkspaceAppIconEntry(entriesByKey, {
      appId: app.appId,
      iconUrl: resolveRendererWorkspaceAppIconUrl(
        app.appId,
        app.iconUrl ?? app.availableIconUrl
      ),
      workspaceId: input.workspaceId
    });
  }
  for (const appId of rendererSeededWorkspaceAppIconIds) {
    if (entriesByKey.has(workspaceAppIconEntryKey(appId, input.workspaceId))) {
      continue;
    }
    const iconUrl = resolveRendererWorkspaceAppDefaultIconUrl(appId);
    if (!iconUrl) {
      continue;
    }
    addWorkspaceAppIconEntry(entriesByKey, {
      appId,
      iconUrl,
      workspaceId: input.workspaceId
    });
  }
  return [...entriesByKey.values()];
}

function addWorkspaceAppIconEntry(
  entriesByKey: Map<string, DesktopWorkspaceAppIconEntry>,
  input: {
    appId: string | null | undefined;
    iconUrl: string | null | undefined;
    workspaceId: string;
  }
): void {
  const appId = input.appId?.trim() ?? "";
  const iconUrl = input.iconUrl?.trim() ?? "";
  if (!appId || !iconUrl) {
    return;
  }
  entriesByKey.set(workspaceAppIconEntryKey(appId, input.workspaceId), {
    appId,
    iconUrl,
    workspaceId: input.workspaceId
  });
}

function workspaceAppIconEntryKey(appId: string, workspaceId: string): string {
  return `${workspaceId}\u0000${appId.trim()}`;
}
