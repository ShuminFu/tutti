import { createDecorator } from "@tutti-os/infra/di";
import type { DesktopLocale } from "@shared/i18n";
import type {
  DesktopAgentComposerDefaultsPatch,
  DesktopAgentConversationDetailMode,
  DesktopAgentProvider,
  DesktopAgentSessionLaunchMode,
  DesktopDefaultAgentProvider,
  DesktopAppCatalogChannel,
  DesktopBrowserUseConnectionMode,
  DesktopDockIconStyle,
  DesktopDockPlacement,
  DeletedAgentConversationRetentionDays,
  DesktopFeatureFlags,
  DesktopFileDefaultOpenersByExtension,
  DesktopMinimizeAnimation,
  DesktopSleepPreventionMode,
  DesktopUpdateChannel,
  DesktopUpdatePolicy,
  DesktopWorkbenchShortcuts,
  DesktopWorkbenchWindowSnapping,
} from "@shared/preferences";
import type { DesktopThemeSource, DesktopThemeState } from "@shared/theme";
import type { DesktopPreferencesReadableStoreState } from "./desktopPreferencesTypes.ts";

export type DesktopAgentComposerDefaultsField =
  keyof DesktopAgentComposerDefaultsPatch;

export interface DesktopAgentComposerDefaultsRejectedField {
  field: DesktopAgentComposerDefaultsField;
  reasonCode: string;
}

export interface DesktopAgentComposerDefaultsPatchResult {
  acknowledgedFields: DesktopAgentComposerDefaultsField[];
  supersededFields: DesktopAgentComposerDefaultsField[];
  // rejectedFields lists the fields the daemon refused to persist. The daemon
  // validates every field of a snapshot independently, so a rejected sibling
  // never drops the fields that were legal.
  rejectedFields: DesktopAgentComposerDefaultsRejectedField[];
}

// The per-field outcome of one published defaults patch as reported by the
// daemon's resolved event. The desktop client correlates it by clientMutationId.
export interface DesktopAgentComposerDefaultsPatchOutcome {
  applied: DesktopAgentComposerDefaultsField[];
  rejected: DesktopAgentComposerDefaultsRejectedField[];
}

export interface IDesktopPreferencesService {
  readonly _serviceBrand: undefined;
  readonly store: DesktopPreferencesReadableStoreState;

  setAgentCliUpdateCheckEnabled(enabled: boolean): Promise<boolean>;
  // DINTAL-5308：Agent 进程常驻的三个旋钮。返回值一律是后端归一化后的权威值，
  // 所以界面永远显示后端认的那个数，而不是用户刚点的那个。
  setAgentRuntimeKeepAliveEnabled(enabled: boolean): Promise<boolean>;
  setAgentRuntimeIdleMinutes(minutes: number): Promise<number>;
  setAgentRuntimeMaxResident(maxResident: number): Promise<number>;
  setDefaultAgentProvider(
    provider: DesktopDefaultAgentProvider,
  ): Promise<DesktopDefaultAgentProvider>;
  setAgentConversationDetailMode(
    mode: DesktopAgentConversationDetailMode,
  ): Promise<DesktopAgentConversationDetailMode>;
  setAppCatalogChannel(
    channel: DesktopAppCatalogChannel,
  ): Promise<DesktopAppCatalogChannel>;
  setBrowserUseConnectionMode(
    mode: DesktopBrowserUseConnectionMode,
  ): Promise<DesktopBrowserUseConnectionMode>;
  setDockPlacement(
    placement: DesktopDockPlacement,
  ): Promise<DesktopDockPlacement>;
  setDeletedAgentConversationRetentionDays(
    days: DeletedAgentConversationRetentionDays,
  ): Promise<DeletedAgentConversationRetentionDays>;
  setDockIconStyle(style: DesktopDockIconStyle): Promise<DesktopDockIconStyle>;
  setFeatureFlags(flags: DesktopFeatureFlags): Promise<DesktopFeatureFlags>;
  setFileDefaultOpenersByExtension(
    openersByExtension: DesktopFileDefaultOpenersByExtension,
  ): Promise<DesktopFileDefaultOpenersByExtension>;
  setLocale(locale: DesktopLocale): Promise<DesktopLocale>;
  setMinimizeAnimation(
    animation: DesktopMinimizeAnimation,
  ): Promise<DesktopMinimizeAnimation>;
  setSleepPreventionMode(
    mode: DesktopSleepPreventionMode,
  ): Promise<DesktopSleepPreventionMode>;
  setShowAppDeveloperSources(show: boolean): Promise<boolean>;
  setThemeSource(source: DesktopThemeSource): Promise<DesktopThemeState>;
  setUpdateChannel(
    channel: DesktopUpdateChannel,
  ): Promise<DesktopUpdateChannel>;
  setUpdatePolicy(policy: DesktopUpdatePolicy): Promise<DesktopUpdatePolicy>;
  setWorkbenchShortcuts(
    shortcuts: DesktopWorkbenchShortcuts,
  ): Promise<DesktopWorkbenchShortcuts>;
  setWorkbenchWindowSnapping(
    value: DesktopWorkbenchWindowSnapping,
  ): Promise<DesktopWorkbenchWindowSnapping>;
  rememberAgentComposerDefaultsForAgentTarget(
    agentTargetId: string,
    defaults: DesktopAgentComposerDefaultsPatch | null,
  ): Promise<DesktopAgentComposerDefaultsPatchResult>;
  rememberAgentGuiConversationRailCollapsed(
    provider: DesktopAgentProvider,
    collapsed: boolean,
  ): Promise<void>;
  rememberAgentSessionLaunchMode(
    workspaceId: string,
    projectSectionKey: string,
    mode: DesktopAgentSessionLaunchMode,
  ): Promise<void>;
}

export const IDesktopPreferencesService =
  createDecorator<IDesktopPreferencesService>("desktop-preferences-service");
