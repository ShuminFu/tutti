// Family grouping for provider model lists: the model dropdown collapses to
// one entry per family — only the latest version of each family is offered —
// and the collapsed entries are grouped by vendor (the id's first token).
//
// A family is the model id's name tokens with version and variant tokens
// removed. Tier words survive on either side of the version, while pure
// variant/effort tokens never split a family:
//
//   claude-opus-4-8         -> claude-opus      (tier before the version)
//   gemini-3.1-pro          -> gemini-pro       (tier after the version)
//   gemini-3-flash          -> gemini-flash     (pro/flash stay separate)
//   gemini-2.5-flash-lite   -> gemini-flash-lite
//   gpt-5.2 / gpt-5.3-codex-low -> gpt          (codex/effort are variants)
//   kimi-k2.7-code          -> kimi
//
// Unversioned entries (e.g. Cursor's "Auto" / "default[]") carry no version to
// compare and pass through the collapse untouched.

import type { AgentGUIComposerSettingOption } from "./agentGuiNodeTypes";

interface ParsedModelFamily {
  family: string;
  version: number[];
}

// Variant and effort tokens that describe how a model runs rather than which
// product tier it is; they never contribute to the family key. "codex" is
// here deliberately: gpt-5.3-codex is the gpt family, not a codex family.
const modelVariantTokens = new Set([
  "codex",
  "code",
  "thinking",
  "preview",
  "exp",
  "experimental",
  "latest",
  "fast",
  "turbo",
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
  "max"
]);

// A 1M context marker is identity rather than a run parameter: "X[1m]" and "X"
// are two rows the user must be able to tell apart, so the marker keys its own
// family slot instead of tying with the bare id and losing the tie.
const contextWindowMarkerPattern = /\[1m\]$/iu;

function normalizedModelName(
  option: Pick<AgentGUIComposerSettingOption, "value" | "label">
): string {
  return (option.label.trim() || option.value)
    .replace(/\[.*\]$/u, "")
    .trim()
    .toLowerCase();
}

// contextWindowFamilySuffix keeps the 1M marker in the family key. Every other
// bracketed suffix (Cursor's "[fast=true]") describes how a model runs and is
// stripped by normalizedModelName, so it collapses into the family it belongs
// to.
function contextWindowFamilySuffix(
  option: Pick<AgentGUIComposerSettingOption, "value" | "label">
): string {
  const source = (option.label.trim() || option.value).trim();
  return contextWindowMarkerPattern.test(source) ? "[1m]" : "";
}

// parseModelFamily derives the family key and comparable version from a model
// display name (falling back to the value with any parameterized "[...]"
// suffix stripped).
export function parseModelFamily(
  option: Pick<AgentGUIComposerSettingOption, "value" | "label">
): ParsedModelFamily | null {
  const source = normalizedModelName(option);
  if (!source) {
    return null;
  }
  const tokens = source.split("-").filter((token) => token !== "");
  const familyTokens: string[] = [];
  const version: number[] = [];
  for (const token of tokens) {
    if (/\d/u.test(token)) {
      for (const segment of token.match(/\d+/gu) ?? []) {
        version.push(Number.parseInt(segment, 10));
      }
      continue;
    }
    if (modelVariantTokens.has(token)) {
      continue;
    }
    familyTokens.push(token);
  }
  if (familyTokens.length === 0 || version.length === 0) {
    return null;
  }
  return {
    family: familyTokens.join("-") + contextWindowFamilySuffix(option),
    version
  };
}

// modelVendorLabel derives the manufacturer group label from the model name's
// first token ("claude-sonnet-5" -> "Claude", "gpt-5.2" -> "GPT"). Returns
// null for entries without a recognizable name (Auto etc. stay ungrouped).
export function modelVendorLabel(
  option: Pick<AgentGUIComposerSettingOption, "value" | "label">
): string | null {
  if (!parseModelFamily(option)) {
    return null;
  }
  const vendor = normalizedModelName(option).split("-", 1)[0] ?? "";
  if (!vendor) {
    return null;
  }
  // Short vendor names are acronyms (GPT, GLM); longer ones are proper names.
  return vendor.length <= 3
    ? vendor.toUpperCase()
    : vendor.charAt(0).toUpperCase() + vendor.slice(1);
}

function compareVersions(left: number[], right: number[]): number {
  const length = Math.max(left.length, right.length);
  for (let index = 0; index < length; index += 1) {
    const difference = (left[index] ?? 0) - (right[index] ?? 0);
    if (difference !== 0) {
      return difference;
    }
  }
  return 0;
}

// collapseModelOptionsToLatest reduces the list to one option per family —
// the highest version, first-advertised on ties — keeping each family at the
// position of its first appearance. Unversioned options (Auto etc.) pass
// through in place, so the collapsed list mirrors the agent's own ordering.
export function collapseModelOptionsToLatest(
  options: readonly AgentGUIComposerSettingOption[]
): AgentGUIComposerSettingOption[] {
  interface FamilySlot {
    family: string;
    option: AgentGUIComposerSettingOption;
    version: number[];
  }
  const slots: (FamilySlot | AgentGUIComposerSettingOption)[] = [];
  const slotByFamily = new Map<string, FamilySlot>();
  for (const option of options) {
    const parsed = parseModelFamily(option);
    if (!parsed) {
      slots.push(option);
      continue;
    }
    const slot = slotByFamily.get(parsed.family);
    if (!slot) {
      const next: FamilySlot = {
        family: parsed.family,
        option,
        version: parsed.version
      };
      slotByFamily.set(parsed.family, next);
      slots.push(next);
      continue;
    }
    if (compareVersions(parsed.version, slot.version) > 0) {
      slot.option = option;
      slot.version = parsed.version;
    }
  }
  return slots.map((slot) => ("family" in slot ? slot.option : slot));
}

export interface ModelVendorGroup<Option> {
  /** Vendor heading; null for the leading ungrouped entries (Auto etc.). */
  label: string | null;
  options: Option[];
}

// groupModelOptionsByVendor buckets options under their manufacturer, in
// first-appearance order. Entries without a vendor (Auto) form a leading
// unlabeled group so they keep their top position.
export function groupModelOptionsByVendor<
  Option extends Pick<AgentGUIComposerSettingOption, "value" | "label">
>(options: readonly Option[]): ModelVendorGroup<Option>[] {
  const groups: ModelVendorGroup<Option>[] = [];
  const groupByLabel = new Map<string | null, ModelVendorGroup<Option>>();
  for (const option of options) {
    const label = modelVendorLabel(option);
    let group = groupByLabel.get(label);
    if (!group) {
      group = { label, options: [] };
      groupByLabel.set(label, group);
      if (label === null) {
        // Ungrouped entries lead the menu regardless of where an injected
        // selected value happens to appear in the source list.
        groups.unshift(group);
      } else {
        groups.push(group);
      }
    }
    group.options.push(option);
  }
  return groups;
}
