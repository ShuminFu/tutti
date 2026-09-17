import type {
  EffortLevel,
  PermissionMode,
  Settings
} from "@anthropic-ai/claude-agent-sdk";
import { recordValue } from "./normalizer.ts";
import { booleanValue, stringValue } from "./runtimeValues.ts";

const claudePlansDirectoryEnv = "TUTTI_CLAUDE_PLANS_DIRECTORY";

/**
 * Pending live flag-settings write. Mirrors the SDK's own `applyFlagSettings`
 * parameter shape: `effortLevel` accepts the full {@link EffortLevel} union
 * (including `max`, which is session-scoped and deliberately absent from the
 * persisted settings type), every other key keeps its `Settings` type.
 */
export type PendingFlagSettings = {
  [K in keyof Settings]?: K extends "effortLevel"
    ? EffortLevel | null
    : Settings[K] | null;
};

export type SidecarSessionSettings = {
  model: string;
  permissionModeId: string;
  planMode: boolean;
  plansDirectory?: string;
  effort: string;
  speed: string;
};

export type SidecarConfigOption = {
  id: string;
  name?: string;
  description?: string;
  category?: string;
  type?: string;
  currentValue?: string;
  /**
   * Provider-resolved runtime value. This stays separate from currentValue so
   * an inherited `default` selection is not rewritten into a pinned model.
   */
  effectiveValue?: string;
  options: Array<{
    value: string;
    name: string;
    description?: string;
  }>;
};

export function sidecarSessionSettings(
  payload: Record<string, unknown>
): SidecarSessionSettings {
  const settings = recordValue(payload.settings) ?? {};
  const env = recordValue(payload.env) ?? {};
  return {
    model: stringValue(settings.model),
    permissionModeId:
      stringValue(payload.permissionModeId) ||
      stringValue(settings.permissionModeId) ||
      "default",
    planMode: booleanValue(settings.planMode),
    plansDirectory: stringValue(env[claudePlansDirectoryEnv]),
    effort:
      stringValue(payload.effort) ||
      stringValue(settings.effort) ||
      stringValue(settings.reasoningEffort),
    speed: stringValue(settings.speed)
  };
}

export function effectivePermissionMode(
  settings: SidecarSessionSettings
): PermissionMode | undefined {
  if (settings.planMode) {
    return "plan";
  }
  const permissionMode = permissionModeValue(settings.permissionModeId);
  if (permissionMode === "bypassPermissions" && !canBypassPermissions()) {
    return "default";
  }
  return permissionMode;
}

export function permissionModeValue(value: string): PermissionMode | undefined {
  switch (value) {
    case "default":
    case "acceptEdits":
    case "bypassPermissions":
    case "plan":
    case "auto":
      return value;
    case "dontAsk":
      return "default";
    default:
      return undefined;
  }
}

export function modelOptionValue(value: string): string | undefined {
  const model = stringValue(value);
  return model && model !== "default" ? model : undefined;
}

/**
 * Effort levels the CLI/SDK itself understands (`EffortLevel` in the SDK types),
 * i.e. the protocol's ladder. This is not a per-model capability table: the
 * sidecar relays the user's choice and leaves "can this upstream honor it?" to
 * the upstream.
 */
const claudeEffortLevels: readonly EffortLevel[] = [
  "low",
  "medium",
  "high",
  "xhigh",
  "max"
];

export function sidecarModelOptionsFromInitializationResult(
  value: Record<string, unknown>
): SidecarConfigOption["options"] {
  const rawModels = Array.isArray(value.models) ? value.models : [];
  const options: SidecarConfigOption["options"] = [];
  const seen = new Set<string>();
  for (const item of rawModels) {
    const model = recordValue(item);
    if (!model) {
      continue;
    }
    const modelValue =
      stringValue(model.value) ||
      stringValue(model.id) ||
      stringValue(model.modelId) ||
      stringValue(model.model_id);
    if (!modelValue || seen.has(modelValue)) {
      continue;
    }
    seen.add(modelValue);
    const name =
      stringValue(model.displayName) ||
      stringValue(model.display_name) ||
      stringValue(model.name) ||
      modelValue;
    const description = stringValue(model.description);
    options.push({
      value: modelValue,
      name,
      ...(description ? { description } : {})
    });
  }
  return options;
}

export function defaultSidecarModelOptionValue(
  options: SidecarConfigOption["options"]
): string {
  return (
    options.find((option) => option.value === "default")?.value ??
    options[0]?.value ??
    "default"
  );
}

/**
 * Effort level as the live-apply vehicle accepts it: every SDK `EffortLevel`,
 * `max` included (`applyFlagSettings` documents `effortLevel` as additionally
 * accepting `'max'`, session-scoped).
 */
export function effortLevelValue(value: string): EffortLevel | null {
  const level = stringValue(value) as EffortLevel;
  return claudeEffortLevels.includes(level) ? level : null;
}

/**
 * Effort level as create-time `Settings.effortLevel` accepts it. `max` is
 * deliberately excluded by the SDK's persisted settings type ("never persisted
 * to settings files"), so it must ride {@link queryEffortOverride} instead.
 */
export function settingsEffortLevel(
  value: string
): Settings["effortLevel"] | null {
  const level = effortLevelValue(value);
  if (!level || level === "max") {
    return null;
  }
  return level;
}

/**
 * Effort levels create-time settings cannot express and that therefore ride the
 * query `Options.effort` field instead. Currently only `max`; the indirection
 * exists so the two vehicles can never drift apart.
 */
export function queryEffortOverride(value: string): EffortLevel | undefined {
  return effortLevelValue(value) === "max" ? "max" : undefined;
}

/**
 * Whether creating the next query already carries this effort level, i.e.
 * whether live `applyFlagSettings` no longer has to deliver it. Covers both
 * vehicles: `Settings.effortLevel` (low..xhigh) and `Options.effort` (max).
 * Anything else — empty or unknown — is NOT carried and must stay pending, or
 * the level is silently dropped instead of reaching the CLI.
 */
export function queryCreateCarriesEffort(value: string): boolean {
  return (
    settingsEffortLevel(value) !== null ||
    queryEffortOverride(value) !== undefined
  );
}

export function flagSettingsFromSessionSettings(
  settings: SidecarSessionSettings
): PendingFlagSettings {
  const result: PendingFlagSettings = {};
  if (settings.effort) {
    result.effortLevel = effortLevelValue(settings.effort);
  }
  if (settings.speed === "fast") {
    result.fastMode = true;
  } else if (settings.speed === "standard") {
    result.fastMode = false;
  }
  return result;
}

export function querySettingsFromSessionSettings(
  settings: SidecarSessionSettings
): Partial<Settings> {
  const result: Partial<Settings> = {};
  if (settings.plansDirectory) {
    result.plansDirectory = settings.plansDirectory;
  }
  if (settings.speed === "fast") {
    result.fastMode = true;
  } else if (settings.speed === "standard") {
    result.fastMode = false;
  }
  // Bake effort into query create settings. Live applyFlagSettings(effort) on a
  // quiet/resumed Claude query can return while still delaying the next prompt
  // for tens of seconds, which trips host delivery confirmation (~30s).
  const effortLevel = settingsEffortLevel(settings.effort);
  if (effortLevel) {
    result.effortLevel = effortLevel;
  }
  return result;
}

export function approvalOptions(): Array<Record<string, unknown>> {
  return [
    {
      kind: "allow_always",
      name: "Allow for session",
      optionId: "allow_always"
    },
    { kind: "allow_once", name: "Allow", optionId: "allow" },
    { kind: "reject_once", name: "Reject", optionId: "reject" }
  ];
}

export function exitPlanOptions(): Array<Record<string, unknown>> {
  const options = [
    {
      kind: "allow_always",
      name: 'Yes, and use "auto" mode',
      optionId: "auto"
    },
    {
      kind: "allow_always",
      name: "Yes, and auto-accept edits",
      optionId: "acceptEdits"
    },
    {
      kind: "allow_once",
      name: "Yes, and manually approve edits",
      optionId: "default"
    },
    { kind: "reject_once", name: "No, keep planning", optionId: "plan" }
  ];
  if (canBypassPermissions()) {
    options.unshift({
      kind: "allow_always",
      name: "Yes, and bypass permissions",
      optionId: "bypassPermissions"
    });
  }
  return options;
}

export function isAllowOption(optionId: string): boolean {
  return [
    "allow",
    "allow_always",
    "accept",
    "acceptEdits",
    "default",
    "auto",
    "bypassPermissions"
  ].includes(optionId);
}

export function isExitPlanAllowOption(optionId: string): boolean {
  if (optionId === "bypassPermissions") {
    return canBypassPermissions();
  }
  return ["default", "acceptEdits", "auto"].includes(optionId);
}

export function canBypassPermissions(): boolean {
  const isRoot = (process.geteuid?.() ?? process.getuid?.()) === 0;
  return !isRoot || !!process.env.IS_SANDBOX;
}
