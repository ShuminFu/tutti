import { describe, expect, it } from "vitest";
import {
  composerDefaultsRejectionTranslationOptions,
  composerDefaultsTouchedFields,
  composerDefaultsWholePatchRejections,
  createAgentGUIComposerDefaultsFailureReporter,
  formatComposerDefaultsFields,
  composerDefaultsReasonMessageKey
} from "./agentGuiComposerDefaultsFailureNotice";

describe("agentGuiComposerDefaultsFailureNotice", () => {
  it("maps every documented reason code to dedicated copy", () => {
    expect(composerDefaultsReasonMessageKey("invalid_value")).toBe(
      "messages.agentComposerDefaultsReasonInvalidValue"
    );
    expect(composerDefaultsReasonMessageKey("unsupported_value")).toBe(
      "messages.agentComposerDefaultsReasonUnsupportedValue"
    );
    expect(composerDefaultsReasonMessageKey("not_configurable")).toBe(
      "messages.agentComposerDefaultsReasonNotConfigurable"
    );
    expect(composerDefaultsReasonMessageKey("unsupported_field")).toBe(
      "messages.agentComposerDefaultsReasonUnsupportedField"
    );
    expect(composerDefaultsReasonMessageKey("internal_error")).toBe(
      "messages.agentComposerDefaultsReasonInternalError"
    );
  });

  // A newer daemon may add reason codes this client predates. Falling back to a
  // generic sentence keeps the warning visible and still names the field.
  it("falls back to the generic reason copy for an unknown code", () => {
    expect(composerDefaultsReasonMessageKey("brand_new_code")).toBe(
      "messages.agentComposerDefaultsReasonUnknown"
    );
    expect(composerDefaultsReasonMessageKey(undefined)).toBe(
      "messages.agentComposerDefaultsReasonUnknown"
    );
  });

  it("names every affected field exactly once", () => {
    expect(
      formatComposerDefaultsFields([
        { field: "model", reasonCode: "invalid_value" },
        { field: "model", reasonCode: "unsupported_value" },
        { field: "reasoningEffort", reasonCode: "not_configurable" }
      ])
    ).toBe("Model, Reasoning effort");
  });

  it("never surfaces the daemon diagnostic message", () => {
    const options = composerDefaultsRejectionTranslationOptions([
      {
        field: "permissionModeId",
        reasonCode: "internal_error",
        // Diagnostic prose from the daemon must not reach the user.
        message: "composer defaults: persist failed: sqlite busy"
      } as never
    ]);

    expect(options.fields).toBe("Permission mode");
    expect(composerDefaultsReasonMessageKey("internal_error")).toBe(
      "messages.agentComposerDefaultsReasonInternalError"
    );
    expect(JSON.stringify(options)).not.toContain("sqlite");
    expect(JSON.stringify(options)).not.toContain("persist failed");
  });

  // One user action can settle several waiters and the coordinator retries a
  // failed publish, so the same rejection arrives repeatedly. Only the first
  // report of each (field, reason) pair is fresh.
  it("reports each field and reason at most once per action", () => {
    const reporter = createAgentGUIComposerDefaultsFailureReporter();
    const rejection = {
      field: "reasoningEffort" as const,
      reasonCode: "unsupported_value"
    };

    expect(reporter.report([rejection])).toBe(true);
    expect(reporter.report([rejection])).toBe(false);
    expect(reporter.report([rejection, rejection])).toBe(false);
    // A different reason for the same field is genuinely new information.
    expect(
      reporter.report([
        { field: "reasoningEffort", reasonCode: "invalid_value" }
      ])
    ).toBe(true);
  });

  it("reports again after a new action resets the reporter", () => {
    const reporter = createAgentGUIComposerDefaultsFailureReporter();
    const rejection = {
      field: "model" as const,
      reasonCode: "invalid_value"
    };

    expect(reporter.report([rejection])).toBe(true);
    reporter.reset();
    expect(reporter.report([rejection])).toBe(true);
  });

  it("reports nothing when no field was rejected", () => {
    const reporter = createAgentGUIComposerDefaultsFailureReporter();
    expect(reporter.report([])).toBe(false);
  });

  // The warning lists fields in the canonical order rather than the order the
  // patch object happened to be built in, so the same failure reads the same way
  // whichever control the user touched first.
  it("lists touched fields in canonical order", () => {
    expect(
      composerDefaultsTouchedFields({ reasoningEffort: "high", model: "x" })
    ).toEqual(["model", "reasoningEffort"]);
    expect(composerDefaultsTouchedFields({})).toEqual([]);
    // Presence decides, not truthiness: a cleared saver mode and an empty speed
    // are still fields the patch carries.
    expect(
      composerDefaultsTouchedFields({ speed: "", codexSaverMode: false })
    ).toEqual(["codexSaverMode", "speed"]);
  });

  // A publish that produced no verdict at all leaves nothing per-field to
  // report, so every field the mutation carried is reported as an internal
  // error instead of dropping the warning.
  it("represents a missing verdict as an internal error per field", () => {
    expect(composerDefaultsWholePatchRejections(["model", "speed"])).toEqual([
      { field: "model", reasonCode: "internal_error" },
      { field: "speed", reasonCode: "internal_error" }
    ]);
    expect(composerDefaultsWholePatchRejections([])).toEqual([]);
  });
});
