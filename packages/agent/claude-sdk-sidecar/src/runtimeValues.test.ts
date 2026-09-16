import assert from "node:assert/strict";
import test from "node:test";
import { claudeSessionInfoTitle } from "./runtimeValues.ts";

test("claudeSessionInfoTitle prefers customTitle over aiTitle", () => {
  assert.equal(
    claudeSessionInfoTitle({
      customTitle: "Renamed title",
      aiTitle: "Generated title",
      summary: "Legacy summary"
    }),
    "Renamed title"
  );
});

test("claudeSessionInfoTitle uses aiTitle when the session was not renamed", () => {
  assert.equal(
    claudeSessionInfoTitle({
      aiTitle: "修复会话标题取源",
      summary: "Legacy summary"
    }),
    "修复会话标题取源"
  );
});

test("claudeSessionInfoTitle falls back to summary when aiTitle is absent", () => {
  assert.equal(
    claudeSessionInfoTitle({ summary: "Legacy summary" }),
    "Legacy summary"
  );
});
