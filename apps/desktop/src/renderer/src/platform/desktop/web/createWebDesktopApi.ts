import type {
  DesktopApi,
  DesktopComputerUseApi,
  DesktopDeveloperApi,
  DesktopDockPreviewCacheApi,
  DesktopHostApi,
  DesktopPlatformApi,
  DesktopRuntimeApi,
  DesktopUpdateApi,
  DesktopWallpaperApi
} from "@preload/types";
import type {
  AppUpdateState,
  ClearDeveloperLogsResult,
  DesktopBackendConfig,
  DesktopDeveloperLogsState,
  ExportDeveloperLogsResult
} from "@shared/contracts/ipc";
import { desktopErrorCodes } from "@shared/errors/desktopErrors";
import { resolveWebBackendConfigFrom } from "./resolveWebBackendConfig";
import {
  HostBridgeUnavailableError,
  installHostFocusRecovery,
  requestHostCapability
} from "./webHostBridgeClient";

const webAppUpdateState: AppUpdateState = {
  channel: "rc",
  checkedAt: null,
  currentVersion: "dev-web",
  downloadedBytes: null,
  downloadPercent: null,
  latestVersion: null,
  message: null,
  policy: "off",
  releaseDate: null,
  releaseName: null,
  releaseNotesUrl: null,
  status: "idle",
  totalBytes: null
};

export function createWebDesktopApi(): DesktopApi {
  const backendConfig = resolveWebBackendConfig();
  installHostFocusRecovery();

  return {
    computerUse: createWebComputerUseApi(),
    developer: createWebDeveloperApi(),
    dockPreviewCache: createWebDockPreviewCacheApi(),
    host: createWebHostApi(),
    platform: createWebPlatformApi(),
    runtime: createWebRuntimeApi(backendConfig),
    update: createWebUpdateApi(),
    wallpaper: createWebWallpaperApi()
  };
}

function createWebComputerUseApi(): DesktopComputerUseApi {
  return {
    checkStatus() {
      return Promise.resolve({
        installed: false,
        permissions: null,
        authorization: "unknown",
        reason: "not-installed"
      });
    },
    install() {
      return Promise.reject(electronDebugRequired("computerUse.install"));
    },
    uninstall() {
      return Promise.reject(electronDebugRequired("computerUse.uninstall"));
    },
    grantPermissions() {
      return Promise.reject(
        electronDebugRequired("computerUse.grantPermissions")
      );
    },
    startPermissionGrant() {
      return Promise.reject(
        electronDebugRequired("computerUse.startPermissionGrant")
      );
    },
    getPermissionGrantStatus() {
      return Promise.reject(
        electronDebugRequired("computerUse.getPermissionGrantStatus")
      );
    },
    openPermissionSettings() {
      return Promise.reject(
        electronDebugRequired("computerUse.openPermissionSettings")
      );
    },
    restartDriver() {
      return Promise.reject(electronDebugRequired("computerUse.restartDriver"));
    }
  };
}

function createWebDockPreviewCacheApi(): DesktopDockPreviewCacheApi {
  return {
    read() {
      return Promise.resolve(null);
    },
    write() {
      return Promise.resolve();
    }
  };
}

function createWebWallpaperApi(): DesktopWallpaperApi {
  return {
    clearCustom() {
      return Promise.reject(electronDebugRequired("clearCustom"));
    },
    getCustom() {
      return Promise.resolve(null);
    },
    setCustom() {
      return Promise.reject(electronDebugRequired("setCustom"));
    }
  };
}

function createWebRuntimeApi(
  backendConfig: Promise<DesktopBackendConfig>
): DesktopRuntimeApi {
  return {
    getAgentSessionReplayPlayback() {
      return Promise.resolve({
        active: false,
        paused: false,
        playbackElapsedMs: 0,
        speed: 1,
        timingMode: "realtime"
      });
    },
    getAgentSessionReplayStatus() {
      return Promise.resolve({ active: false });
    },
    getBackendConfig() {
      return backendConfig;
    },
    getBusinessEventStreamUrl() {
      return backendConfig.then((config) =>
        resolveWebSocketUrl(config, "/v1/events/ws").toString()
      );
    },
    importAgentSessionReplayCassettes() {
      return Promise.reject(
        electronDebugRequired("importAgentSessionReplayCassettes")
      );
    },
    isAgentSessionReplayRuntime() {
      return false;
    },
    listWorkspaceAgentProbes(input) {
      return Promise.resolve({
        capturedAtUnixMs: Date.now(),
        providers: [],
        workspaceId: input.workspaceId
      });
    },
    launchAgentSessionReplay() {
      return Promise.reject(electronDebugRequired("launchAgentSessionReplay"));
    },
    revealAgentSessionReplayCassette() {
      return Promise.reject(
        electronDebugRequired("revealAgentSessionReplayCassette")
      );
    },
    setAgentSessionReplayPlayback() {
      return Promise.reject(
        electronDebugRequired("setAgentSessionReplayPlayback")
      );
    },
    sendAgentSessionReplayControl() {
      return Promise.reject(
        electronDebugRequired("sendAgentSessionReplayControl")
      );
    },
    waitForAgentSessionReplay() {
      return Promise.reject(electronDebugRequired("waitForAgentSessionReplay"));
    },
    async getTerminalStreamUrl(input) {
      const url = resolveWebSocketUrl(
        await backendConfig,
        `/v1/workspaces/${encodeURIComponent(input.workspaceId)}/terminals/${encodeURIComponent(input.sessionId)}/ws`
      );
      if (input.afterSeq !== undefined) {
        url.searchParams.set("afterSeq", String(input.afterSeq));
      }
      return url.toString();
    },
    logRendererDiagnostic(input) {
      const method =
        input.level === "error"
          ? console.error
          : input.level === "warn"
            ? console.warn
            : input.level === "debug"
              ? console.debug
              : console.info;
      method("[tutti dev-web] renderer diagnostic", {
        details: input.details ?? {},
        event: input.event,
        source: input.source,
        workspaceId: input.workspaceId ?? null
      });
      return Promise.resolve();
    },
    logTerminalDiagnostic(input) {
      const method =
        input.level === "error"
          ? console.error
          : input.level === "warn"
            ? console.warn
            : input.level === "debug"
              ? console.debug
              : console.info;
      method(`[tutti dev-web] terminal diagnostic ${input.event}`, {
        details: input.details ?? {},
        event: input.event,
        nodeId: input.nodeId ?? null,
        sessionId: input.sessionId ?? null,
        workspaceId: input.workspaceId ?? null
      });
      return Promise.resolve();
    }
  };
}

function createWebDeveloperApi(): DesktopDeveloperApi {
  return {
    clearLogs(): Promise<ClearDeveloperLogsResult> {
      return Promise.reject(electronDebugRequired("clearLogs"));
    },
    exportLogs(): Promise<ExportDeveloperLogsResult> {
      return Promise.reject(electronDebugRequired("exportLogs"));
    },
    getLogsState(): Promise<DesktopDeveloperLogsState> {
      return Promise.resolve({
        desktopVersion: "dev-web",
        files: [],
        logsDir: "",
        totalFiles: 0,
        totalSizeBytes: 0
      });
    },
    openLogDirectory() {
      return Promise.reject(electronDebugRequired("openLogDirectory"));
    },
    openLogFile() {
      return Promise.reject(electronDebugRequired("openLogFile"));
    }
  };
}

function createWebPlatformApi(): DesktopPlatformApi {
  return {
    distribution: "direct",
    appName: "",
    homeDirectory: "",
    os: inferPlatform(),
    resolveDroppedEntries() {
      return [];
    },
    resolveDroppedPaths() {
      return [];
    }
  };
}

function createWebHostApi(): DesktopHostApi {
  return {
    files: {
      createUserDocumentsProjectDirectory(input) {
        // Embedded DinTalDock serves this UI and the directory endpoint from
        // the same cliagent origin. Prefer the direct route so session creation
        // does not depend on the parent postMessage bridge lifecycle.
        const bootstrapUrl = new URLSearchParams(window.location.search)
          .get("tuttiBootstrapUrl")
          ?.trim();
        if (bootstrapUrl) {
          let endpoint: URL | null = null;
          try {
            endpoint = new URL("/tutti/project-directory", bootstrapUrl);
          } catch {
            endpoint = null;
          }
          if (endpoint && endpoint.origin === window.location.origin) {
            return fetch(endpoint, {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({
                name: input.name,
                allow_existing: Boolean(input.allowExisting),
              }),
            }).then(async (response) => {
              if (!response.ok) {
                throw new Error(
                  `createUserDocumentsProjectDirectory failed: HTTP ${response.status}`
                );
              }
              const result = (await response.json()) as { path?: string };
              if (!result.path) {
                throw electronDebugRequired(
                  "createUserDocumentsProjectDirectory"
                );
              }
              return { path: result.path };
            });
          }
        }

        // Keep the host capability as the fallback for non-embedded web
        // environments that provide their own desktop bridge.
        return requestHostCapability<{ path?: string } | null>(
          "createUserDocumentsProjectDirectory",
          [input]
        ).then(
          (result) => {
            if (!result?.path) {
              return Promise.reject(
                electronDebugRequired("createUserDocumentsProjectDirectory")
              );
            }
            return { path: result.path };
          },
          (error) => {
            if (error instanceof HostBridgeUnavailableError) {
              return Promise.reject(
                electronDebugRequired("createUserDocumentsProjectDirectory")
              );
            }
            return Promise.reject(error);
          }
        );
      },
      selectAppArchive() {
        return Promise.reject(electronDebugRequired("selectAppArchive"));
      },
      selectAppArchiveExportPath() {
        return Promise.reject(
          electronDebugRequired("selectAppArchiveExportPath")
        );
      },
      selectAppIconImage() {
        return Promise.reject(electronDebugRequired("selectAppIconImage"));
      },
      selectDirectory() {
        // Delegate to the embedding host when available; the host answers with
        // a native dialog result shaped as { path }. Fall back to the web
        // rejection when not embedded / unsupported / timed out.
        return requestHostCapability<{ path?: string } | null>(
          "selectDirectory"
        ).then(
          (result) => result?.path ?? null,
          (error) => {
            if (error instanceof HostBridgeUnavailableError) {
              return Promise.reject(electronDebugRequired("selectDirectory"));
            }
            return Promise.reject(error);
          }
        );
      },
      openFile() {
        return Promise.reject(electronDebugRequired("openFile"));
      },
      listOpenWithApplications() {
        return Promise.reject(
          electronDebugRequired("listOpenWithApplications")
        );
      },
      openFileWithApplication() {
        return Promise.reject(electronDebugRequired("openFileWithApplication"));
      },
      openFileWithOtherApplication() {
        return Promise.reject(
          electronDebugRequired("openFileWithOtherApplication")
        );
      },
      openFileInBrowser() {
        return Promise.reject(electronDebugRequired("openFileInBrowser"));
      },
      resolveWorkspaceFileFileUrl() {
        return Promise.reject(
          electronDebugRequired("resolveWorkspaceFileFileUrl")
        );
      },
      revealInFolder() {
        return Promise.reject(electronDebugRequired("revealInFolder"));
      },
      revealWorkspaceFile() {
        return Promise.reject(electronDebugRequired("revealWorkspaceFile"));
      },
      openExternal(url) {
        window.open(url, "_blank", "noopener,noreferrer");
        return Promise.resolve();
      },
      openTerminalLink() {
        return Promise.reject(electronDebugRequired("openTerminalLink"));
      },
      readLocalFileText() {
        return Promise.reject(electronDebugRequired("readLocalFileText"));
      },
      readLocalPreviewFile() {
        return Promise.reject(electronDebugRequired("readLocalPreviewFile"));
      },
      archiveAgentPromptFile() {
        return Promise.reject(electronDebugRequired("archiveAgentPromptFile"));
      },
      readPreviewFile() {
        return Promise.reject(electronDebugRequired("readPreviewFile"));
      },
      resolveEntryIcon() {
        return Promise.resolve(null);
      },
      selectUploadFiles(input) {
        // Delegate to the embedding host; it returns the native picker result
        // shaped as { files: [{ path }] } (or a bare string[]). Normalise to the
        // string[] shape expected here. Fall back to the web rejection when not
        // embedded / unsupported / timed out.
        return requestHostCapability<
          { files?: Array<{ path?: string }> } | string[] | null
        >("selectUploadFiles", input === undefined ? [] : [input]).then(
          (result) => {
            if (Array.isArray(result)) {
              return result;
            }
            return (result?.files ?? [])
              .map((file) => file?.path)
              .filter((path): path is string => typeof path === "string");
          },
          (error) => {
            if (error instanceof HostBridgeUnavailableError) {
              return Promise.reject(electronDebugRequired("selectUploadFiles"));
            }
            return Promise.reject(error);
          }
        );
      },
      copyImageToClipboard() {
        return Promise.reject(electronDebugRequired("copyImageToClipboard"));
      },
      copyFilesToClipboard() {
        return Promise.reject(electronDebugRequired("copyFilesToClipboard"));
      }
    },
    window: {
      approveClose() {
        window.close();
        return Promise.resolve();
      },
      capturePreview() {
        return Promise.resolve(null);
      },
      capturePreviewImages() {
        return Promise.resolve(null);
      },
      minimize() {
        return Promise.reject(electronDebugRequired("minimize"));
      },
      openAgentWindow() {
        return Promise.reject(electronDebugRequired("openAgentWindow"));
      },
      onCloseRequest() {
        return () => {};
      },
      onLayout() {
        return () => {};
      },
      onQuitShortcutToast() {
        return () => {};
      },
      resolveCloseRequest() {
        return undefined;
      },
      resizeContentWidth() {
        return Promise.reject(electronDebugRequired("resizeContentWidth"));
      },
      toggleMaximize() {
        return Promise.reject(electronDebugRequired("toggleMaximize"));
      }
    },
    notifications: {
      show() {
        return Promise.resolve({
          reason: "unsupported",
          shown: false
        });
      },
      onNavigate() {
        return () => {};
      }
    },
    workspace: {
      broadcastAgentStatus() {},
      onOpenFeatureRequest() {
        return () => {};
      },
      onOpenFileRequest() {
        return () => {};
      },
      openWorkspaceAppFolder() {
        return Promise.reject(electronDebugRequired("openWorkspaceAppFolder"));
      },
      replaceWorkspaceWindow({ mode, workspaceId }) {
        const url = new URL(window.location.href);
        url.searchParams.set("view", mode === "agent" ? "agent" : "workspace");
        url.searchParams.set("workspaceId", workspaceId);
        window.location.assign(url.toString());
        return Promise.resolve();
      },
      showWorkspace(workspaceID) {
        const url = new URL(window.location.href);
        url.searchParams.set("view", "workspace");
        url.searchParams.set("workspaceId", workspaceID);
        window.location.assign(url.toString());
        return Promise.resolve();
      }
    }
  };
}

function createWebUpdateApi(): DesktopUpdateApi {
  return {
    checkForUpdates() {
      return Promise.resolve(webAppUpdateState);
    },
    configure() {
      return Promise.resolve(webAppUpdateState);
    },
    downloadUpdate() {
      return Promise.resolve(webAppUpdateState);
    },
    getState() {
      return Promise.resolve(webAppUpdateState);
    },
    installUpdate() {
      return Promise.resolve();
    },
    onState() {
      return () => {};
    }
  };
}

function resolveWebBackendConfig(): Promise<DesktopBackendConfig> {
  // Runtime injection first (URL query), then build-time env fallback so a
  // single static build can target any tuttid instance.
  return resolveWebBackendConfigFrom({
    env: {
      VITE_TUTTID_ACCESS_TOKEN: import.meta.env.VITE_TUTTID_ACCESS_TOKEN,
      VITE_TUTTID_BASE_URL: import.meta.env.VITE_TUTTID_BASE_URL
    },
    search: window.location.search,
    fetchBootstrap: async (endpoint, nonce) => {
      const url = new URL(endpoint, window.location.origin);
      if (
        url.protocol !== "http:" ||
        (url.hostname !== "127.0.0.1" && url.hostname !== "[::1]") ||
        !url.port ||
        url.pathname !== "/tutti/bootstrap" ||
        url.username ||
        url.password ||
        url.search ||
        url.hash
      ) {
        throw new Error("managed tutti bootstrap endpoint is invalid");
      }
      url.searchParams.set("nonce", nonce);
      const response = await fetch(url, {
        cache: "no-store",
        credentials: "omit",
        headers: { Accept: "application/json" }
      });
      if (!response.ok) {
        throw new Error("managed tutti bootstrap unavailable");
      }
      return response.json() as Promise<unknown>;
    }
  });
}

function resolveWebSocketUrl(
  backendConfig: DesktopBackendConfig,
  pathname: string
): URL {
  const url = new URL(pathname, backendConfig.baseUrl);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.searchParams.set("access_token", backendConfig.accessToken);
  return url;
}

function electronDebugRequired(action: string): Error & { code: string } {
  const error = new Error(
    `${action} is only available when debugging through Electron.`
  ) as Error & { code: string };
  error.code = desktopErrorCodes.electronDebugRequired;
  return error;
}

function inferPlatform(): NodeJS.Platform {
  const ua = navigator.userAgent.toLowerCase();
  if (ua.includes("mac os")) {
    return "darwin";
  }
  if (ua.includes("windows")) {
    return "win32";
  }
  return "linux";
}
