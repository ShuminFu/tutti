import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const inlineAppBodySource = readFileSync(
  new URL("./workspaceAppCenterInlineAppBody.tsx", import.meta.url),
  "utf8"
);
const contributionSource = readFileSync(
  new URL("./workspaceAppCenterContribution.tsx", import.meta.url),
  "utf8"
);

test("app-center body renders every persisted app tab and hides inactive guests", () => {
  assert.match(inlineAppBodySource, /readWorkspaceAppTabIds\(/);
  assert.match(inlineAppBodySource, /openAppIds\.map\(\(openAppId\)\s*=>/);
  assert.match(
    inlineAppBodySource,
    /hidden=\{context\.node\.isMinimized \|\| !isActive\}/
  );
  assert.doesNotMatch(inlineAppBodySource, /useState/);
});

test("app-center header exposes a fixed catalog tab and closeable app tabs", () => {
  assert.match(contributionSource, /role="tablist"/);
  assert.match(contributionSource, /data-workspace-app-center-tab=/);
  assert.match(contributionSource, /selectWorkspaceAppTab/);
  assert.match(contributionSource, /closeWorkspaceAppTab/);
  assert.match(contributionSource, /<AddIcon/);
});
