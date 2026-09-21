import assert from "node:assert/strict";
import test from "node:test";
import { embeddedHostCreatedSessionId } from "./embeddedHostCreatedSessionId.ts";

test("embedded host session identity matches the backend ManagedTuttiSessionID contract", () => {
  const clientSubmitId = "11111111-1111-4111-8111-111111111111";
  assert.equal(
    embeddedHostCreatedSessionId(`  ${clientSubmitId}  `),
    "8953c2d2-b502-575b-9b48-e90bc5d2fe96"
  );
});
