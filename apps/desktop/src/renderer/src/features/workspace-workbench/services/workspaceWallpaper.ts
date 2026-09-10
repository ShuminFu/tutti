import type { DesktopI18nKey } from "@shared/i18n";
import type { WorkbenchSnapshot } from "@tutti-os/workbench-snapshot";
import type { WorkbenchSurfaceWallpaperFit } from "@tutti-os/workbench-surface";

export type WorkspaceWallpaperId =
  | "default"
  | "tutti"
  | "ocean"
  | "sky"
  | "peaks"
  | "orbit"
  | "sand"
  | "dunes"
  | "custom";

export const customWorkspaceWallpaperId: WorkspaceWallpaperId = "custom";
export const defaultWorkspaceWallpaperId: WorkspaceWallpaperId = "tutti";
const customWorkspaceWallpaperTitleKey: DesktopI18nKey =
  "workspace.wallpaper.options.custom";

export type WorkspaceWallpaperAppearance = "light" | "dark";

export const workspaceWallpaperDisplayModes = [
  "original",
  "fit",
  "stretch",
  "center"
] as const;

export type WorkspaceWallpaperDisplayMode =
  | (typeof workspaceWallpaperDisplayModes)[number]
  | "fill";

const defaultWorkspaceWallpaperDisplayMode: WorkspaceWallpaperDisplayMode =
  "original";

function isWorkspaceWallpaperDisplayMode(
  value: string
): value is WorkspaceWallpaperDisplayMode {
  return (
    workspaceWallpaperDisplayModes.includes(
      value as (typeof workspaceWallpaperDisplayModes)[number]
    ) || value === "fill"
  );
}

export function workspaceWallpaperDisplayModeTitleKey(
  mode: WorkspaceWallpaperDisplayMode
): DesktopI18nKey {
  if (mode === "fill") {
    return "workspace.settings.appearance.wallpaperDisplayModeOptions.original";
  }

  return `workspace.settings.appearance.wallpaperDisplayModeOptions.${mode}`;
}

export function toWorkbenchSurfaceWallpaperFit(
  mode: WorkspaceWallpaperDisplayMode
): WorkbenchSurfaceWallpaperFit {
  switch (mode) {
    case "fill":
      return "cover";
    case "original":
    case "center":
      return "center";
    case "fit":
      return "contain";
    case "stretch":
      return "stretch";
  }
}

export function resolveWorkspaceWallpaperDisplayMode(
  wallpaperID: WorkspaceWallpaperId,
  displayMode: WorkspaceWallpaperDisplayMode
): WorkspaceWallpaperDisplayMode {
  if (wallpaperID !== customWorkspaceWallpaperId) {
    return "fill";
  }

  return displayMode;
}

export interface WorkspaceWallpaperOption {
  appearance: WorkspaceWallpaperAppearance;
  darkUrl?: string;
  id: WorkspaceWallpaperId;
  titleKey: DesktopI18nKey;
  url: string;
}

const workspaceWallpaperMetadataKey = "workspaceWallpaper";
const workspaceWallpaperMetadataSchemaVersion = 1;

// rndmaster: 内置壁纸只保留两张 —— "tutti"（其像素里的品牌字样由
// scripts/build-tutti-web.sh 的 apply_wallpaper_overlay 换成 dintaldock）与
// "default"。删掉的 6 张（ocean / sky / peaks / orbit / sand / dunes）源图仍在
// 仓库里，但没有代码引用，vite 不会打包 —— 产物少约 630KB。
//
// 不在本 patch 里删源图：git 的二进制 patch 会带 reverse 块（供 apply -R），
// 会让 patch 从纯文本涨到几 MB，而产物收益为零。
//
// WorkspaceWallpaperId 联合类型刻意保持不变：它不参与运行时判定
// （isWorkspaceWallpaperId 读的是本数组），保留旧 id 可让上游测试与 fixture
// 不产生类型错误，把 patch 面压到最小。旧快照里存着 ocean/sky 等选择的用户，
// 由该函数判定失败后自动回落到 defaultWorkspaceWallpaperId，无需迁移代码。
export const workspaceWallpaperOptions: WorkspaceWallpaperOption[] = [
  {
    appearance: "dark",
    id: "tutti",
    titleKey: "workspace.wallpaper.options.tutti",
    url: new URL(
      "../../../assets/workspace-wallpaper/tutti.png",
      import.meta.url
    ).href
  },
  {
    appearance: "light",
    id: "default",
    titleKey: "workspace.wallpaper.options.default",
    darkUrl: new URL(
      "../../../assets/workspace-wallpaper/default-dark.png",
      import.meta.url
    ).href,
    url: new URL(
      "../../../assets/workspace-wallpaper/default-light.png",
      import.meta.url
    ).href
  },
];

function isWorkspaceWallpaperId(value: string): value is WorkspaceWallpaperId {
  return (
    value === customWorkspaceWallpaperId ||
    workspaceWallpaperOptions.some((option) => option.id === value)
  );
}

export function getWorkspaceWallpaperOption(
  id: WorkspaceWallpaperId,
  appearance: WorkspaceWallpaperAppearance = "light",
  customWallpaperUrl?: string | null
): WorkspaceWallpaperOption {
  if (id === customWorkspaceWallpaperId) {
    if (customWallpaperUrl) {
      return {
        appearance,
        id: customWorkspaceWallpaperId,
        titleKey: customWorkspaceWallpaperTitleKey,
        url: customWallpaperUrl
      };
    }
    return getWorkspaceWallpaperOption("default", appearance);
  }

  const option = workspaceWallpaperOptions.find((item) => item.id === id);
  if (option) {
    return resolveWorkspaceWallpaperOption(option, appearance);
  }
  const fallback = workspaceWallpaperOptions[0];
  if (!fallback) {
    throw new Error("Workspace wallpaper catalog is empty.");
  }
  return resolveWorkspaceWallpaperOption(fallback, appearance);
}

function resolveWorkspaceWallpaperOption(
  option: WorkspaceWallpaperOption,
  appearance: WorkspaceWallpaperAppearance
): WorkspaceWallpaperOption {
  if (option.id !== "default" || appearance !== "dark" || !option.darkUrl) {
    return option;
  }

  return {
    ...option,
    appearance: "dark",
    url: option.darkUrl
  };
}

export function readWorkspaceWallpaperIdFromSnapshot(
  snapshot: WorkbenchSnapshot | null | undefined
): WorkspaceWallpaperId {
  return readWorkspaceWallpaperSnapshotMetadata(snapshot).selectedWallpaperID;
}

export function readWorkspaceWallpaperDisplayModeFromSnapshot(
  snapshot: WorkbenchSnapshot | null | undefined
): WorkspaceWallpaperDisplayMode {
  return readWorkspaceWallpaperSnapshotMetadata(snapshot).displayMode;
}

export function writeWorkspaceWallpaperIdToSnapshot(
  snapshot: WorkbenchSnapshot,
  wallpaperID: WorkspaceWallpaperId
): WorkbenchSnapshot {
  return writeWorkspaceWallpaperSnapshotMetadata(snapshot, {
    selectedWallpaperID: wallpaperID
  });
}

export function writeWorkspaceWallpaperDisplayModeToSnapshot(
  snapshot: WorkbenchSnapshot,
  displayMode: WorkspaceWallpaperDisplayMode
): WorkbenchSnapshot {
  return writeWorkspaceWallpaperSnapshotMetadata(snapshot, {
    displayMode
  });
}

export function preserveWorkspaceWallpaperSnapshotMetadata(
  previousSnapshot: WorkbenchSnapshot | null | undefined,
  nextSnapshot: WorkbenchSnapshot
): WorkbenchSnapshot {
  if (nextSnapshot.metadata?.[workspaceWallpaperMetadataKey] !== undefined) {
    return nextSnapshot;
  }

  const wallpaperMetadata =
    previousSnapshot?.metadata?.[workspaceWallpaperMetadataKey];
  if (wallpaperMetadata === undefined) {
    return nextSnapshot;
  }

  return {
    ...nextSnapshot,
    metadata: {
      ...(nextSnapshot.metadata ?? {}),
      [workspaceWallpaperMetadataKey]: wallpaperMetadata
    }
  };
}

export function replaceWorkspaceWallpaperSnapshotMetadata(
  authoritativeSnapshot: WorkbenchSnapshot | null | undefined,
  nextSnapshot: WorkbenchSnapshot
): WorkbenchSnapshot {
  const authoritativeMetadata =
    authoritativeSnapshot?.metadata?.[workspaceWallpaperMetadataKey];
  const {
    [workspaceWallpaperMetadataKey]: _discardedWallpaperMetadata,
    ...nextMetadata
  } = nextSnapshot.metadata ?? {};

  return {
    ...nextSnapshot,
    metadata:
      authoritativeMetadata === undefined
        ? nextMetadata
        : {
            ...nextMetadata,
            [workspaceWallpaperMetadataKey]: authoritativeMetadata
          }
  };
}

interface WorkspaceWallpaperSnapshotMetadata {
  displayMode: WorkspaceWallpaperDisplayMode;
  schemaVersion: typeof workspaceWallpaperMetadataSchemaVersion;
  selectedWallpaperID: WorkspaceWallpaperId;
}

function readWorkspaceWallpaperSnapshotMetadata(
  snapshot: WorkbenchSnapshot | null | undefined
): WorkspaceWallpaperSnapshotMetadata {
  const metadata = snapshot?.metadata?.[workspaceWallpaperMetadataKey];
  if (!metadata || typeof metadata !== "object" || Array.isArray(metadata)) {
    return createDefaultWorkspaceWallpaperSnapshotMetadata();
  }

  const raw = metadata as Partial<WorkspaceWallpaperSnapshotMetadata>;
  const selectedWallpaperID = raw.selectedWallpaperID;
  const displayMode = raw.displayMode;

  return {
    schemaVersion: workspaceWallpaperMetadataSchemaVersion,
    selectedWallpaperID:
      typeof selectedWallpaperID === "string" &&
      isWorkspaceWallpaperId(selectedWallpaperID)
        ? selectedWallpaperID
        : defaultWorkspaceWallpaperId,
    displayMode:
      typeof displayMode === "string" &&
      isWorkspaceWallpaperDisplayMode(displayMode)
        ? displayMode
        : defaultWorkspaceWallpaperDisplayMode
  };
}

function writeWorkspaceWallpaperSnapshotMetadata(
  snapshot: WorkbenchSnapshot,
  patch: Partial<WorkspaceWallpaperSnapshotMetadata>
): WorkbenchSnapshot {
  const current = readWorkspaceWallpaperSnapshotMetadata(snapshot);

  return {
    ...snapshot,
    metadata: {
      ...(snapshot.metadata ?? {}),
      [workspaceWallpaperMetadataKey]: {
        ...current,
        ...patch,
        schemaVersion: workspaceWallpaperMetadataSchemaVersion
      } satisfies WorkspaceWallpaperSnapshotMetadata
    }
  };
}

function createDefaultWorkspaceWallpaperSnapshotMetadata(): WorkspaceWallpaperSnapshotMetadata {
  return {
    schemaVersion: workspaceWallpaperMetadataSchemaVersion,
    selectedWallpaperID: defaultWorkspaceWallpaperId,
    displayMode: defaultWorkspaceWallpaperDisplayMode
  };
}
