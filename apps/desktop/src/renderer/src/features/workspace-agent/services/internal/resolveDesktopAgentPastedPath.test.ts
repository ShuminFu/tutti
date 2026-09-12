import assert from "node:assert/strict";
import test from "node:test";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import { createDesktopAgentPastedPathResolver } from "./resolveDesktopAgentPastedPath.ts";

function createResolver(
  check: (input: { path: string }) => Promise<{
    exists: boolean;
    isDirectory: boolean;
    path: string;
  }>
) {
  const requestedPaths: string[] = [];
  return {
    requestedPaths,
    resolve: createDesktopAgentPastedPathResolver({
      tuttidClient: {
        async checkUserProjectPath(input: { path: string }) {
          requestedPaths.push(input.path);
          return check(input);
        }
      } as unknown as Pick<TuttidClient, "checkUserProjectPath">
    })
  };
}

test("resolves a pasted directory path into a folder reference", async () => {
  const { requestedPaths, resolve } = createResolver(async () => ({
    exists: true,
    isDirectory: true,
    path: "/Users/me/My Folder"
  }));

  assert.deepEqual(await resolve("  /Users/me/My Folder  "), {
    hostPath: "/Users/me/My Folder",
    kind: "folder",
    path: "/Users/me/My Folder"
  });
  assert.deepEqual(requestedPaths, ["/Users/me/My Folder"]);
});

test("resolves a pasted file path into a file reference", async () => {
  const { resolve } = createResolver(async () => ({
    exists: true,
    isDirectory: false,
    path: "/Users/me/notes.md"
  }));

  assert.deepEqual(await resolve("/Users/me/notes.md"), {
    hostPath: "/Users/me/notes.md",
    kind: "file",
    path: "/Users/me/notes.md"
  });
});

test("uses the canonical path the daemon reports", async () => {
  const { resolve } = createResolver(async () => ({
    exists: true,
    isDirectory: true,
    path: "/private/tmp/scratch"
  }));

  assert.equal((await resolve("/tmp/scratch"))?.path, "/private/tmp/scratch");
});

test("returns null for missing paths so the composer keeps plain text", async () => {
  const { resolve } = createResolver(async () => ({
    exists: false,
    isDirectory: false,
    path: "/Users/me/nope"
  }));

  assert.equal(await resolve("/Users/me/nope"), null);
});

test("returns null for root so the chip always has a name", async () => {
  const { resolve } = createResolver(async ({ path }) => ({
    exists: true,
    isDirectory: true,
    path
  }));

  // `/` names nothing, and the mention name comes from the last path segment.
  assert.equal(await resolve("/"), null);
  assert.equal(await resolve("//"), null);
  assert.equal(await resolve("///  "), null);
});

test("falls back to the pasted path when the daemon reports no path", async () => {
  const { resolve } = createResolver(
    async () =>
      ({
        exists: true,
        isDirectory: false,
        path: undefined
      }) as unknown as { exists: boolean; isDirectory: boolean; path: string }
  );

  assert.equal(
    (await resolve("/Users/me/notes.md"))?.path,
    "/Users/me/notes.md"
  );
});

test("returns null for relative paths without asking the daemon", async () => {
  const { requestedPaths, resolve } = createResolver(async () => ({
    exists: true,
    isDirectory: false,
    path: ""
  }));

  assert.equal(await resolve("workspace/a.txt"), null);
  assert.equal(await resolve(""), null);
  assert.deepEqual(requestedPaths, []);
});

test("returns null when the path check fails", async () => {
  const { resolve } = createResolver(async () => {
    throw new Error("daemon unreachable");
  });

  assert.equal(await resolve("/Users/me/a.txt"), null);
});
