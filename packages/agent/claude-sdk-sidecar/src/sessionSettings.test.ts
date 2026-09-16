import assert from "node:assert/strict";
import test from "node:test";
import {
  queryCreateCarriesEffort,
  queryEffortOverride,
  querySettingsFromSessionSettings,
  sidecarSessionSettings
} from "./sessionSettings.ts";

test("plansDirectory can be configured by the host environment", () => {
  const settings = sidecarSessionSettings({
    env: {
      TUTTI_CLAUDE_PLANS_DIRECTORY: "."
    },
    settings: {}
  });

  assert.equal(settings.plansDirectory, ".");
  assert.deepEqual(querySettingsFromSessionSettings(settings), {
    plansDirectory: "."
  });
});

test("blank host plansDirectory keeps the Claude SDK default", () => {
  const settings = sidecarSessionSettings({
    env: {
      TUTTI_CLAUDE_PLANS_DIRECTORY: "   "
    }
  });

  assert.equal(settings.plansDirectory, "");
  assert.deepEqual(querySettingsFromSessionSettings(settings), {});
});

test("query create settings include effortLevel for the next resumed query", () => {
  assert.deepEqual(
    querySettingsFromSessionSettings({
      model: "",
      permissionModeId: "default",
      planMode: false,
      effort: "high",
      speed: "standard"
    }),
    {
      effortLevel: "high",
      fastMode: false
    }
  );
  assert.deepEqual(
    querySettingsFromSessionSettings({
      model: "",
      permissionModeId: "default",
      planMode: false,
      effort: "",
      speed: ""
    }),
    {}
  );
});

// `max` is the one level the SDK's persisted settings type excludes, so it must
// not be written into create-time settings; it rides the query's own
// `Options.effort` field instead. Both vehicles count as "already carried", so
// live applyFlagSettings does not have to deliver the level again.
test("max effort rides query options instead of create-time settings", () => {
  const settings = {
    model: "",
    permissionModeId: "default",
    planMode: false,
    effort: "max",
    speed: "standard"
  };

  assert.equal(
    querySettingsFromSessionSettings(settings).effortLevel,
    undefined
  );
  assert.equal(queryEffortOverride("max"), "max");
  assert.equal(queryCreateCarriesEffort("max"), true);

  assert.equal(queryEffortOverride("high"), undefined);
  assert.equal(queryCreateCarriesEffort("high"), true);
  assert.equal(queryCreateCarriesEffort(""), false);
  assert.equal(queryCreateCarriesEffort("ultra"), false);
});
