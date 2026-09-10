import type { WorkspaceSummary } from "@tutti-os/client-tuttid-ts";
import { WorkspaceSettingsTrigger } from "./WorkspaceChromeActions";
import type {
  WorkspaceWallpaperDisplayMode,
  WorkspaceWallpaperId
} from "../services/workspaceWallpaper";

/**
 * The embedded shell intentionally has no top chrome. Keep the existing
 * settings trigger/panel, but mount it beside WorkbenchHost so the top-chrome
 * suppression cannot hide the only entry point.
 */
export function EmbeddedWorkspaceSettingsHost({
  selectedWallpaperDisplayMode,
  selectedWallpaperID,
  onSelectWallpaper,
  onSelectWallpaperDisplayMode,
  workspace
}: {
  onSelectWallpaper: (id: WorkspaceWallpaperId) => void;
  onSelectWallpaperDisplayMode: (
    displayMode: WorkspaceWallpaperDisplayMode
  ) => void;
  selectedWallpaperDisplayMode: WorkspaceWallpaperDisplayMode;
  selectedWallpaperID: WorkspaceWallpaperId;
  workspace: WorkspaceSummary;
}) {
  return (
    <div
      className="pointer-events-none absolute inset-x-0 top-0 z-[var(--z-panel-popover)] flex justify-end p-2"
      data-dintaldock-settings-host="true"
    >
      <div className="pointer-events-auto rounded-md bg-[color-mix(in_srgb,var(--background-fronted)_88%,transparent)] shadow-sm">
        <WorkspaceSettingsTrigger
          embedded
          onOpenExternalAgentImport={() => undefined}
          onSelectWallpaper={onSelectWallpaper}
          onSelectWallpaperDisplayMode={onSelectWallpaperDisplayMode}
          selectedWallpaperDisplayMode={selectedWallpaperDisplayMode}
          selectedWallpaperID={selectedWallpaperID}
          workspace={workspace}
        />
      </div>
    </div>
  );
}
