import { useCallback, useEffect, useState } from "react";
import { AGENT_GUI_WORKBENCH_OPEN_EXTERNAL_IMPORT_EVENT } from "@tutti-os/agent-gui/workbench/contribution";
import type { WorkspaceSummary } from "@tutti-os/client-tuttid-ts";
import { ExternalAgentSessionImportWizard } from "./ExternalAgentSessionImportWizard";
import { WorkspaceSettingsTrigger } from "./WorkspaceChromeActions";
import type {
  WorkspaceWallpaperDisplayMode,
  WorkspaceWallpaperId
} from "../services/workspaceWallpaper";

/**
 * The embedded shell intentionally has no top chrome. This mounts the settings
 * PANEL (and the deep-link request bridge it owns) beside WorkbenchHost; the
 * visible trigger is gone. It used to float a gear over the transcript's
 * top-right corner, which collided with the split pane header's action icons
 * and sat on top of the conversation. The entry point survives in the provider
 * rail's own "..." menu, which publishes into the same request store.
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
  const [externalImportOpen, setExternalImportOpen] = useState(false);
  const openExternalAgentImport = useCallback(() => {
    setExternalImportOpen(true);
  }, []);

  useEffect(() => {
    window.addEventListener(
      AGENT_GUI_WORKBENCH_OPEN_EXTERNAL_IMPORT_EVENT,
      openExternalAgentImport
    );
    return () => {
      window.removeEventListener(
        AGENT_GUI_WORKBENCH_OPEN_EXTERNAL_IMPORT_EVENT,
        openExternalAgentImport
      );
    };
  }, [openExternalAgentImport]);

  return (
    <>
      <WorkspaceSettingsTrigger
        embedded
        onOpenExternalAgentImport={openExternalAgentImport}
        onSelectWallpaper={onSelectWallpaper}
        onSelectWallpaperDisplayMode={onSelectWallpaperDisplayMode}
        selectedWallpaperDisplayMode={selectedWallpaperDisplayMode}
        selectedWallpaperID={selectedWallpaperID}
        workspace={workspace}
      />
      <ExternalAgentSessionImportWizard
        open={externalImportOpen}
        workspace={workspace}
        onOpenChange={setExternalImportOpen}
      />
    </>
  );
}
