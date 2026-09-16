import type {
  DesktopPreferencesStateResponse,
  TuttidEventStreamClient,
  TuttidClient,
  PutDesktopPreferencesRequest
} from "@tutti-os/client-tuttid-ts";
import {
  defaultDesktopMinimizeAnimation,
  desktopFeatureFlagsEqual,
  normalizeDesktopAgentSessionLaunchModesByWorkspace,
  desktopWorkbenchShortcutsEqual,
  desktopWorkbenchWindowSnappingEqual,
  normalizeDesktopAgentConversationDetailMode,
  normalizeDesktopFeatureFlags,
  normalizeDesktopWorkbenchShortcuts,
  normalizeDesktopWorkbenchWindowSnapping
} from "../../../../../../../shared/preferences/index.ts";
import type {
  DesktopAgentComposerDefaultsPatch,
  DesktopAgentSessionLaunchMode
} from "../../../../../../../shared/preferences/index.ts";
import { desktopAgentComposerDefaultsFields } from "../../../../../../../shared/preferences/index.ts";
import type {
  DesktopAgentComposerDefaultsField,
  DesktopAgentComposerDefaultsPatchOutcome,
  DesktopAgentComposerDefaultsRejectedField
} from "../desktopPreferencesService.interface.ts";

export interface DesktopPreferencesClient {
  connect(): Promise<void>;
  dispose(): void;
  getDesktopPreferences(): Promise<DesktopPreferencesStateResponse>;
  patchAgentComposerDefaultsForTarget(input: {
    agentTargetId: string;
    clientMutationId: string;
    patch: DesktopAgentComposerDefaultsPatch;
  }): Promise<DesktopAgentComposerDefaultsPatchOutcome>;
  patchAgentSessionLaunchMode(input: {
    workspaceId: string;
    projectSectionKey: string;
    mode: DesktopAgentSessionLaunchMode;
  }): Promise<void>;
  updateDesktopPreferences(
    request: PutDesktopPreferencesRequest
  ): Promise<PutDesktopPreferencesRequest["preferences"]>;
  subscribeToDesktopPreferencesUpdated(
    listener: (preferences: PutDesktopPreferencesRequest["preferences"]) => void
  ): () => void;
}

export interface CreateDesktopPreferencesClientOptions {
  authoritativeEventTimeoutMs?: number;
  composerDefaultsResolvedEventTimeoutMs?: number;
}

interface PendingDesktopPreferencesUpdate {
  key: string;
  preferences: PutDesktopPreferencesRequest["preferences"];
  promise: Promise<PutDesktopPreferencesRequest["preferences"]>;
  reject: (error: Error) => void;
  resolve: (preferences: PutDesktopPreferencesRequest["preferences"]) => void;
  timeoutHandle: ReturnType<typeof setTimeout> | null;
}

interface PendingComposerDefaultsPatch {
  agentTargetId: string;
  patch: DesktopAgentComposerDefaultsPatch;
  reject: (error: Error) => void;
  resolve: (outcome: DesktopAgentComposerDefaultsPatchOutcome) => void;
  timeoutHandle: ReturnType<typeof setTimeout> | null;
}

export function createDesktopPreferencesClient(
  tuttidClient: Pick<TuttidClient, "getDesktopPreferences">,
  eventStreamClient: TuttidEventStreamClient,
  options: CreateDesktopPreferencesClientOptions = {}
): DesktopPreferencesClient {
  const authoritativeEventTimeoutMs =
    options.authoritativeEventTimeoutMs ?? 1_000;
  const composerDefaultsResolvedEventTimeoutMs =
    options.composerDefaultsResolvedEventTimeoutMs ?? 2_000;
  const listeners = new Set<
    (preferences: PutDesktopPreferencesRequest["preferences"]) => void
  >();
  const pendingUpdates = new Map<string, PendingDesktopPreferencesUpdate>();
  const pendingComposerDefaultsPatches = new Map<
    string,
    PendingComposerDefaultsPatch
  >();
  const unsubscribeEventStream = eventStreamClient.subscribe(
    "preferences.desktop.updated",
    (event) => {
      applyAuthoritativePreferences(event.payload.preferences);
    }
  );
  const unsubscribeComposerDefaultsResolved = eventStreamClient.subscribe(
    "preferences.agent.composer.defaults.resolved",
    (event) => {
      const { clientMutationId } = event.payload;
      if (!clientMutationId) {
        return;
      }
      const pending = pendingComposerDefaultsPatches.get(clientMutationId);
      if (!pending) {
        return;
      }
      clearPendingComposerDefaultsTimeout(pending);
      pendingComposerDefaultsPatches.delete(clientMutationId);
      pending.resolve(
        normalizeComposerDefaultsPatchOutcome(
          event.payload.applied,
          event.payload.rejected ?? []
        )
      );
    }
  );

  return {
    connect() {
      return eventStreamClient.connect();
    },
    dispose() {
      unsubscribeEventStream();
      unsubscribeComposerDefaultsResolved();
      const disposeError = new Error(
        "Desktop preferences client was disposed."
      );
      for (const pendingUpdate of pendingUpdates.values()) {
        rejectPendingUpdate(pendingUpdate, disposeError);
      }
      pendingUpdates.clear();
      for (const pendingPatch of pendingComposerDefaultsPatches.values()) {
        clearPendingComposerDefaultsTimeout(pendingPatch);
        pendingPatch.reject(disposeError);
      }
      pendingComposerDefaultsPatches.clear();
    },
    getDesktopPreferences() {
      return tuttidClient.getDesktopPreferences();
    },
    patchAgentComposerDefaultsForTarget(input) {
      // Register the waiter BEFORE the intent is published: the resolved event
      // can arrive in the same tick the intent handler returns.
      const pending: PendingComposerDefaultsPatch = {
        agentTargetId: input.agentTargetId,
        patch: input.patch,
        reject: () => {},
        resolve: () => {},
        timeoutHandle: null
      };
      const promise = new Promise<DesktopAgentComposerDefaultsPatchOutcome>(
        (resolve, reject) => {
          pending.resolve = resolve;
          pending.reject = reject;
        }
      );
      pendingComposerDefaultsPatches.set(input.clientMutationId, pending);
      void eventStreamClient
        .publishIntent(
          "preferences.agent.composer.defaults.patch.requested",
          input
        )
        .then(() => {
          const current = pendingComposerDefaultsPatches.get(
            input.clientMutationId
          );
          if (current !== pending) {
            return;
          }
          pending.timeoutHandle = setTimeout(() => {
            if (
              pendingComposerDefaultsPatches.get(input.clientMutationId) !==
              pending
            ) {
              return;
            }
            pendingComposerDefaultsPatches.delete(input.clientMutationId);
            void confirmComposerDefaultsOutcomeFromServer(pending);
          }, composerDefaultsResolvedEventTimeoutMs);
        })
        .catch((error: unknown) => {
          const current = pendingComposerDefaultsPatches.get(
            input.clientMutationId
          );
          if (current !== pending) {
            return;
          }
          pendingComposerDefaultsPatches.delete(input.clientMutationId);
          pending.reject(
            error instanceof Error ? error : new Error(String(error))
          );
        });
      return promise;
    },
    patchAgentSessionLaunchMode(input) {
      return eventStreamClient.publishIntent(
        "preferences.agent.session.launch.mode.patch.requested",
        input
      );
    },
    async updateDesktopPreferences(request) {
      const key = createPreferencesKey(request.preferences);
      const existingPendingUpdate = pendingUpdates.get(key);
      if (existingPendingUpdate) {
        return await existingPendingUpdate.promise;
      }

      const pendingUpdate = createPendingUpdate(key, request.preferences);
      pendingUpdates.set(key, pendingUpdate);

      try {
        await eventStreamClient.publishIntent(
          "preferences.desktop.update.requested",
          {
            preferences: request.preferences
          }
        );
      } catch (error) {
        if (pendingUpdates.get(key) === pendingUpdate) {
          rejectPendingUpdate(
            pendingUpdate,
            error instanceof Error ? error : new Error(String(error))
          );
        }
      }

      if (pendingUpdates.get(key) === pendingUpdate) {
        scheduleAuthoritativeConfirmation(pendingUpdate);
      }

      return await pendingUpdate.promise;
    },
    subscribeToDesktopPreferencesUpdated(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    }
  };

  // The resolved event is the precise per-field outcome. When it never arrives
  // (daemon without the topic, dropped event), fall back to the authoritative
  // stored defaults and diff them against the requested patch: fields that
  // landed are applied, fields that did not are rejected with internal_error.
  async function confirmComposerDefaultsOutcomeFromServer(
    pending: PendingComposerDefaultsPatch
  ): Promise<void> {
    try {
      const state = await tuttidClient.getDesktopPreferences();
      if (state.initialized) {
        pending.resolve(
          diffComposerDefaultsPatchOutcome(state.preferences, pending)
        );
        return;
      }
    } catch (error) {
      pending.reject(error instanceof Error ? error : new Error(String(error)));
      return;
    }
    pending.reject(
      new Error("Composer defaults outcome could not be confirmed.")
    );
  }

  function clearPendingComposerDefaultsTimeout(
    pending: PendingComposerDefaultsPatch
  ): void {
    if (pending.timeoutHandle !== null) {
      clearTimeout(pending.timeoutHandle);
      pending.timeoutHandle = null;
    }
  }

  function createPendingUpdate(
    key: string,
    preferences: PutDesktopPreferencesRequest["preferences"]
  ): PendingDesktopPreferencesUpdate {
    let rejectFn: (error: Error) => void = () => {};
    let resolveFn: (
      authoritativePreferences: PutDesktopPreferencesRequest["preferences"]
    ) => void = () => {};
    const promise = new Promise<PutDesktopPreferencesRequest["preferences"]>(
      (resolve, reject) => {
        resolveFn = resolve;
        rejectFn = reject;
      }
    );

    return {
      key,
      preferences,
      promise,
      reject: rejectFn,
      resolve: resolveFn,
      timeoutHandle: null
    };
  }

  function scheduleAuthoritativeConfirmation(
    pendingUpdate: PendingDesktopPreferencesUpdate
  ): void {
    pendingUpdate.timeoutHandle = setTimeout(() => {
      void confirmPendingUpdateFromServer(pendingUpdate);
    }, authoritativeEventTimeoutMs);
  }

  async function confirmPendingUpdateFromServer(
    pendingUpdate: PendingDesktopPreferencesUpdate
  ): Promise<void> {
    if (pendingUpdates.get(pendingUpdate.key) !== pendingUpdate) {
      return;
    }

    try {
      const currentState = await tuttidClient.getDesktopPreferences();
      if (
        currentState.initialized &&
        preferencesEqual(currentState.preferences, pendingUpdate.preferences)
      ) {
        applyAuthoritativePreferences(currentState.preferences);
        return;
      }
    } catch (error) {
      rejectPendingUpdate(
        pendingUpdate,
        new Error(
          error instanceof Error
            ? `Desktop preferences update could not be confirmed: ${error.message}`
            : "Desktop preferences update could not be confirmed."
        )
      );
      return;
    }

    rejectPendingUpdate(
      pendingUpdate,
      new Error(
        "Desktop preferences update was acknowledged, but the authoritative update did not arrive."
      )
    );
  }

  function applyAuthoritativePreferences(
    preferences: PutDesktopPreferencesRequest["preferences"]
  ): void {
    for (const listener of listeners) {
      listener(preferences);
    }

    const pendingUpdate = pendingUpdates.get(createPreferencesKey(preferences));
    if (pendingUpdate) {
      resolvePendingUpdate(pendingUpdate, preferences);
    }
  }

  function resolvePendingUpdate(
    pendingUpdate: PendingDesktopPreferencesUpdate,
    preferences: PutDesktopPreferencesRequest["preferences"]
  ): void {
    clearPendingUpdateTimeout(pendingUpdate);
    pendingUpdates.delete(pendingUpdate.key);
    pendingUpdate.resolve(preferences);
  }

  function rejectPendingUpdate(
    pendingUpdate: PendingDesktopPreferencesUpdate,
    error: Error
  ): void {
    clearPendingUpdateTimeout(pendingUpdate);
    pendingUpdates.delete(pendingUpdate.key);
    pendingUpdate.reject(error);
  }

  function clearPendingUpdateTimeout(
    pendingUpdate: PendingDesktopPreferencesUpdate
  ): void {
    if (pendingUpdate.timeoutHandle !== null) {
      clearTimeout(pendingUpdate.timeoutHandle);
      pendingUpdate.timeoutHandle = null;
    }
  }
}

function createPreferencesKey(
  preferences: PutDesktopPreferencesRequest["preferences"]
): string {
  const workbenchWindowSnapping = normalizeDesktopWorkbenchWindowSnapping(
    preferences.workbenchWindowSnapping
  );
  return [
    preferences.agentCliUpdateCheckEnabled
      ? "agent-cli-updates:on"
      : "agent-cli-updates:off",
    // agentComposerDefaultsByProvider is deliberately excluded: the daemon
    // freezes that legacy field (client input is ignored), so including it
    // would make authoritative responses never match pending updates.
    stableAgentGuiConversationRailCollapsedByProviderKey(
      preferences.agentGuiConversationRailCollapsedByProvider
    ),
    stableAgentSessionLaunchModesByWorkspaceKey(
      preferences.agentSessionLaunchModesByWorkspace
    ),
    normalizeDesktopAgentConversationDetailMode(
      preferences.agentConversationDetailMode
    ),
    preferences.appCatalogChannel,
    preferences.browserUseConnectionMode ?? "isolated",
    preferences.defaultAgentProvider,
    preferences.dockIconStyle,
    preferences.dockPlacement,
    preferences.deletedAgentConversationRetentionDays ?? 30,
    preferences.minimizeAnimation ?? defaultDesktopMinimizeAnimation,
    stableFileDefaultOpenersByExtensionKey(
      preferences.fileDefaultOpenersByExtension
    ),
    stableDesktopFeatureFlagsKey(preferences.featureFlags),
    stableDesktopWorkbenchShortcutsKey(preferences.workbenchShortcuts),
    preferences.locale,
    preferences.sleepPreventionMode,
    preferences.showAppDeveloperSources ? "app-sources:on" : "app-sources:off",
    preferences.themeSource,
    preferences.updateChannel,
    preferences.updatePolicy,
    workbenchWindowSnapping.enabled ? "snapping:on" : "snapping:off",
    workbenchWindowSnapping.shortcutPreset
  ].join("::");
}

function preferencesEqual(
  left: PutDesktopPreferencesRequest["preferences"],
  right: PutDesktopPreferencesRequest["preferences"]
): boolean {
  return (
    left.agentCliUpdateCheckEnabled === right.agentCliUpdateCheckEnabled &&
    // agentComposerDefaultsByProvider is deliberately excluded (frozen
    // server-side; see createPreferencesKey).
    stableAgentGuiConversationRailCollapsedByProviderKey(
      left.agentGuiConversationRailCollapsedByProvider
    ) ===
      stableAgentGuiConversationRailCollapsedByProviderKey(
        right.agentGuiConversationRailCollapsedByProvider
      ) &&
    stableAgentSessionLaunchModesByWorkspaceKey(
      left.agentSessionLaunchModesByWorkspace
    ) ===
      stableAgentSessionLaunchModesByWorkspaceKey(
        right.agentSessionLaunchModesByWorkspace
      ) &&
    normalizeDesktopAgentConversationDetailMode(
      left.agentConversationDetailMode
    ) ===
      normalizeDesktopAgentConversationDetailMode(
        right.agentConversationDetailMode
      ) &&
    (left.browserUseConnectionMode ?? "isolated") ===
      (right.browserUseConnectionMode ?? "isolated") &&
    left.appCatalogChannel === right.appCatalogChannel &&
    left.defaultAgentProvider === right.defaultAgentProvider &&
    left.dockIconStyle === right.dockIconStyle &&
    left.dockPlacement === right.dockPlacement &&
    (left.deletedAgentConversationRetentionDays ?? 30) ===
      (right.deletedAgentConversationRetentionDays ?? 30) &&
    (left.minimizeAnimation ?? defaultDesktopMinimizeAnimation) ===
      (right.minimizeAnimation ?? defaultDesktopMinimizeAnimation) &&
    stableFileDefaultOpenersByExtensionKey(
      left.fileDefaultOpenersByExtension
    ) ===
      stableFileDefaultOpenersByExtensionKey(
        right.fileDefaultOpenersByExtension
      ) &&
    desktopFeatureFlagsEqual(left.featureFlags, right.featureFlags) &&
    desktopWorkbenchShortcutsEqual(
      left.workbenchShortcuts,
      right.workbenchShortcuts
    ) &&
    left.locale === right.locale &&
    left.sleepPreventionMode === right.sleepPreventionMode &&
    (left.showAppDeveloperSources ?? false) ===
      (right.showAppDeveloperSources ?? false) &&
    left.themeSource === right.themeSource &&
    left.updateChannel === right.updateChannel &&
    left.updatePolicy === right.updatePolicy &&
    desktopWorkbenchWindowSnappingEqual(
      left.workbenchWindowSnapping,
      right.workbenchWindowSnapping
    )
  );
}

const desktopAgentProviderKeys = [
  "claude-code",
  "codex",
  "tutti-agent",
  "cursor",
  "nexight",
  "openclaw"
] as const;

function stableAgentGuiConversationRailCollapsedByProviderKey(
  value: unknown
): string {
  if (!value || typeof value !== "object") {
    return "{}";
  }
  const input = value as Record<string, unknown>;
  const output: Record<string, boolean> = {};
  for (const provider of desktopAgentProviderKeys) {
    if (typeof input[provider] === "boolean") {
      output[provider] = input[provider];
    }
  }
  return JSON.stringify(output);
}

function stableFileDefaultOpenersByExtensionKey(value: unknown): string {
  if (!value || typeof value !== "object") {
    return "{}";
  }
  const input = value as Record<string, unknown>;
  const output: Record<string, string> = {};
  for (const extension of Object.keys(input).sort()) {
    const normalizedExtension = extension
      .trim()
      .toLowerCase()
      .replace(/^\.+/u, "");
    const opener = input[extension];
    if (
      /^[a-z0-9][a-z0-9_-]{0,31}$/u.test(normalizedExtension) &&
      typeof opener === "string"
    ) {
      output[normalizedExtension] = opener;
    }
  }
  return JSON.stringify(output);
}

function stableAgentSessionLaunchModesByWorkspaceKey(value: unknown): string {
  const normalized = normalizeDesktopAgentSessionLaunchModesByWorkspace(value);
  const output: Record<string, Record<string, string>> = {};
  for (const workspaceId of Object.keys(normalized).sort()) {
    output[workspaceId] = {};
    for (const projectKey of Object.keys(normalized[workspaceId]!).sort()) {
      output[workspaceId]![projectKey] = normalized[workspaceId]![projectKey]!;
    }
  }
  return JSON.stringify(output);
}

function stableDesktopFeatureFlagsKey(value: unknown): string {
  const normalized = normalizeDesktopFeatureFlags(value);
  const output: Record<string, boolean> = {};
  for (const key of Object.keys(normalized).sort()) {
    output[key] = normalized[key]!;
  }
  return JSON.stringify(output);
}

function stableDesktopWorkbenchShortcutsKey(value: unknown): string {
  return JSON.stringify(normalizeDesktopWorkbenchShortcuts(value));
}

function isDesktopAgentComposerDefaultsField(
  value: string
): value is DesktopAgentComposerDefaultsField {
  return (desktopAgentComposerDefaultsFields as readonly string[]).includes(
    value
  );
}

function normalizeComposerDefaultsPatchOutcome(
  applied: readonly string[],
  rejected: readonly {
    field: string;
    reasonCode: string;
    message?: string;
  }[]
): DesktopAgentComposerDefaultsPatchOutcome {
  const normalizedRejected: DesktopAgentComposerDefaultsRejectedField[] = [];
  for (const entry of rejected) {
    if (!isDesktopAgentComposerDefaultsField(entry.field)) {
      continue;
    }
    normalizedRejected.push({
      field: entry.field,
      reasonCode: entry.reasonCode
    });
  }
  return {
    applied: applied.filter(isDesktopAgentComposerDefaultsField),
    rejected: normalizedRejected
  };
}

// diffComposerDefaultsPatchOutcome compares the patch the client sent against
// the stored defaults it reads back. It is only the timeout fallback: the
// resolved event is the precise source of truth. codexSaverMode never appears
// in the stored defaults payload, so it counts as applied here — the daemon
// already validated it before storing.
function diffComposerDefaultsPatchOutcome(
  preferences: PutDesktopPreferencesRequest["preferences"],
  pending: PendingComposerDefaultsPatch
): DesktopAgentComposerDefaultsPatchOutcome {
  const stored =
    preferences.agentComposerDefaultsByAgentTarget?.[pending.agentTargetId] ??
    {};
  const applied: DesktopAgentComposerDefaultsField[] = [];
  const rejected: DesktopAgentComposerDefaultsRejectedField[] = [];
  for (const field of desktopAgentComposerDefaultsFields) {
    if (!(field in pending.patch)) {
      continue;
    }
    if (field === "codexSaverMode") {
      applied.push(field);
      continue;
    }
    const requested = pending.patch[field];
    const normalizedRequested =
      typeof requested === "string" ? requested.trim() || null : null;
    const storedValue = stored[field] ?? null;
    const normalizedStored =
      typeof storedValue === "string"
        ? storedValue.trim() || null
        : storedValue;
    if (normalizedRequested === normalizedStored) {
      applied.push(field);
    } else {
      rejected.push({ field, reasonCode: "internal_error" });
    }
  }
  return { applied, rejected };
}
