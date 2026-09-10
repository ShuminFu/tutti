import assert from "node:assert/strict";
import test from "node:test";
import {
  isEmbeddedDintalDock,
  reconcileEmbeddedDintalDock
} from "./embeddedDintalDock.ts";

test("detects only the protected host bootstrap query", () => {
  assert.equal(isEmbeddedDintalDock("?tuttiBootstrap=nonce-1"), true);
  assert.equal(isEmbeddedDintalDock("?workspace=one"), false);
});

test("keeps the frontmost agent and removes the rest of the desktop", async () => {
  const closed: string[] = [];
  const exited: string[] = [];
  const focused: string[] = [];
  const result = await reconcileEmbeddedDintalDock({
    closeNode: (id) => closed.push(id),
    exitFullscreenNode: (id) => exited.push(id),
    focusNode: (id) => focused.push(id),
    getSnapshot: () => ({
      nodeStack: ["agent-old", "files", "agent-current"],
      nodes: [
        {
          data: { typeId: "agent-gui" },
          displayMode: "floating",
          id: "agent-old"
        },
        { data: { typeId: "files" }, displayMode: "floating", id: "files" },
        {
          data: { typeId: "agent-gui" },
          displayMode: "fullscreen",
          id: "agent-current"
        }
      ]
    }),
    launchNode: async () => null,
    load: async () => undefined
  });

  assert.equal(result, "agent-current");
  assert.deepEqual(closed, ["agent-old", "files"]);
  assert.deepEqual(exited, ["agent-current"]);
  assert.deepEqual(focused, ["agent-current"]);
});

test("opens an agent when the persisted desktop has none", async () => {
  const launched: { reason: "host"; typeId: string }[] = [];
  const result = await reconcileEmbeddedDintalDock({
    closeNode: () => undefined,
    exitFullscreenNode: () => undefined,
    focusNode: () => undefined,
    getSnapshot: () => ({ nodeStack: [], nodes: [] }),
    launchNode: async (input) => {
      launched.push(input);
      return "agent-new";
    },
    load: async () => undefined
  });

  assert.equal(result, "agent-new");
  assert.deepEqual(launched, [{ reason: "host", typeId: "agent-gui" }]);
});
