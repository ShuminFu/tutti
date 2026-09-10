import type { DesktopApi } from "@preload/types";
import { createWebDesktopApi } from "./web/createWebDesktopApi";

export interface ResolvedDesktopEnvironment {
  desktopApi: DesktopApi;
  mode: "desktop" | "web";
  startupWorkspaceID: string | null;
}

let webDesktopApi: DesktopApi | undefined;

export function resolveDesktopEnvironment(
  desktopApi: DesktopApi | undefined
): ResolvedDesktopEnvironment {
  if (desktopApi) {
    return {
      desktopApi,
      mode: "desktop",
      startupWorkspaceID: null
    };
  }

  if (!isWebDesktopDevEnabled()) {
    throw new Error(
      "Desktop API is unavailable outside Electron and web dev mode is not enabled."
    );
  }

  return {
    desktopApi: (webDesktopApi ??= createWebDesktopApi()),
    mode: "web",
    startupWorkspaceID: readEnvString(
      import.meta.env.VITE_TUTTI_WEB_WORKSPACE_ID
    )
  };
}

function isWebDesktopDevEnabled(): boolean {
  return import.meta.env.VITE_TUTTI_WEB_DEV === "1";
}

function readEnvString(value: string | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}
