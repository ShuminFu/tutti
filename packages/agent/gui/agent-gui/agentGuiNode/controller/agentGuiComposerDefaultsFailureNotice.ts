import { translate } from "../../../i18n/index";
import type { TranslateOptions } from "../../../i18n/index";
import {
  rememberComposerDefaultsFields,
  type AgentGUIComposerDefaultsField,
  type AgentGUIRememberComposerDefaultsRejection
} from "./agentGuiController.providerHelpers";

// Composer-defaults persistence is a background side effect of changing a
// composer setting: the live session already holds the new value, so a refused
// default never blocks the user. The one thing it must not do is fail in
// silence — the user has to learn that the choice will not survive a restart.
//
// This module turns the daemon's machine-readable per-field verdict into the
// single localized warning the composer shows. The daemon's diagnostic
// `message` is deliberately never rendered: it is engineer-facing prose with no
// i18n contract, so only the stable `reasonCode` reaches the UI.
//
// i18n keys are listed here rather than inlined so the vocabulary stays
// auditable in one place: `agentComposerDefaultsNotSaved` names the affected
// fields, and each reason gets a sentence explaining why the value was refused.
const composerDefaultsFieldMessageKeys: Record<
  AgentGUIComposerDefaultsField,
  string
> = {
  codexSaverMode: "messages.agentComposerDefaultsFieldCodexSaverMode",
  model: "messages.agentComposerDefaultsFieldModel",
  permissionModeId: "messages.agentComposerDefaultsFieldPermissionModeId",
  reasoningEffort: "messages.agentComposerDefaultsFieldReasoningEffort",
  speed: "messages.agentComposerDefaultsFieldSpeed"
};

// A rejected field may carry a reason the client version predates. Unknown
// codes fall through to the generic sentence, which still names the field, so
// the warning never degrades into silence.
const composerDefaultsReasonMessageKeys: Record<string, string> = {
  invalid_value: "messages.agentComposerDefaultsReasonInvalidValue",
  unsupported_value: "messages.agentComposerDefaultsReasonUnsupportedValue",
  not_configurable: "messages.agentComposerDefaultsReasonNotConfigurable",
  unsupported_field: "messages.agentComposerDefaultsReasonUnsupportedField",
  internal_error: "messages.agentComposerDefaultsReasonInternalError"
};

export interface AgentGUIComposerDefaultsFailureNotice {
  message: string;
  tone: "warning";
}

// Translation options for the aggregate warning: the field list plus the reason
// sentence of the first rejected field. Reporting one reason keeps the toast a
// single readable line; the field list still names everything that was refused.
export function composerDefaultsRejectionTranslationOptions(
  rejections: readonly AgentGUIRememberComposerDefaultsRejection[]
): TranslateOptions {
  return {
    fields: formatComposerDefaultsFields(rejections),
    reason: translate(
      composerDefaultsReasonMessageKey(rejections[0]?.reasonCode)
    )
  };
}

export function composerDefaultsReasonMessageKey(
  reasonCode: string | undefined
): string {
  return (
    composerDefaultsReasonMessageKeys[reasonCode ?? ""] ??
    "messages.agentComposerDefaultsReasonUnknown"
  );
}

export function formatComposerDefaultsFields(
  rejections: readonly AgentGUIRememberComposerDefaultsRejection[]
): string {
  const labels: string[] = [];
  for (const rejection of rejections) {
    const key = composerDefaultsFieldMessageKeys[rejection.field];
    if (!key) continue;
    const label = translate(key);
    if (!labels.includes(label)) {
      labels.push(label);
    }
  }
  return labels.join(translate("messages.agentComposerDefaultsFieldSeparator"));
}

export function createComposerDefaultsFailureNotice(
  rejections: readonly AgentGUIRememberComposerDefaultsRejection[]
): AgentGUIComposerDefaultsFailureNotice | null {
  if (rejections.length === 0) return null;
  return {
    message: translate(
      "messages.agentComposerDefaultsNotSaved",
      composerDefaultsRejectionTranslationOptions(rejections)
    ),
    tone: "warning"
  };
}

// Per-action dedupe. The coordinator retries a failed publish up to three
// times, and a single user edit can settle more than one waiter, so the same
// field can be rejected repeatedly for one user action. The reporter collapses
// that into one toast per (field, reasonCode) pair while still letting a later,
// genuinely different failure through.
export interface AgentGUIComposerDefaultsFailureReporter {
  report(
    rejections: readonly AgentGUIRememberComposerDefaultsRejection[]
  ): boolean;
  reset(): void;
}

export function createAgentGUIComposerDefaultsFailureReporter(): AgentGUIComposerDefaultsFailureReporter {
  let reported = new Set<string>();
  return {
    report(rejections) {
      const fresh = rejections.filter((rejection) => {
        const key = `${rejection.field}\u0000${rejection.reasonCode}`;
        if (reported.has(key)) return false;
        reported.add(key);
        return true;
      });
      return fresh.length > 0;
    },
    reset() {
      reported = new Set();
    }
  };
}

// The fields a patch or mutation actually carries, in the order the daemon
// evaluates and reports them. Object key order is an accident of whichever
// object happens to hold the values; the order the user reads in the warning is
// not, so it is derived from the canonical field list instead.
export function composerDefaultsTouchedFields(
  values: Partial<Record<AgentGUIComposerDefaultsField, unknown>>
): AgentGUIComposerDefaultsField[] {
  return rememberComposerDefaultsFields.filter(
    (field) => values[field] !== undefined
  );
}

// A publish can fail without producing any per-field verdict: the transport
// refused the intent, the target is unknown, or the coordinator gave up after
// its retries. The fields the mutation tried to change are then the only thing
// left to report, and reporting them is the point — a default the user believes
// was saved, but was not, resets silently on the next launch.
export function composerDefaultsWholePatchRejections(
  fields: readonly AgentGUIComposerDefaultsField[]
): AgentGUIRememberComposerDefaultsRejection[] {
  return fields.map((field) => ({ field, reasonCode: "internal_error" }));
}
