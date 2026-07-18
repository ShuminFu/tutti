import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import test from "node:test";
import reactCompiler from "babel-plugin-react-compiler";

const require = createRequire(import.meta.url);
const { transformAsync } = require("@babel/core") as {
  transformAsync: (
    source: string,
    options: Record<string, unknown>
  ) => Promise<{ code?: string | null } | null>;
};

const sourceUrl = new URL(
  "./useStableDesktopAgentGUIHostProps.ts",
  import.meta.url
);

test("React Compiler preserves field-keyed Agent GUI host projections", async () => {
  const source = await readFile(sourceUrl, "utf8");
  const result = await transformAsync(source, {
    babelrc: false,
    configFile: false,
    filename: sourceUrl.pathname,
    parserOpts: { plugins: ["typescript"] },
    plugins: [
      [
        reactCompiler,
        {
          compilationMode: "infer",
          panicThreshold: "none"
        }
      ]
    ]
  });
  const compiled = result?.code ?? "";

  assert.doesNotMatch(compiled, /const identity\w* = nextIdentity;/);
  assert.match(compiled, /nextIdentity\.nodeId/);
  assert.match(compiled, /nextIdentity\.workspaceId/);
  assert.match(compiled, /nextWorkspace\.fileReferenceAdapter/);
  assert.match(compiled, /nextHostActions\.onOpenConversationWindow/);
});
