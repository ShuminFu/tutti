import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import {
  createProviderIdentityPatterns,
  findProviderIdentityViolations,
  isExemptPath,
  scanWorkspace
} from "./check-agent-provider-strategy-boundaries.mjs";
import {
  repositoryCheckArgs,
  selectRepositoryChecks
} from "./repository-checks.mjs";

const providerIds = [
  "claude-code",
  "codex",
  "cursor",
  "nexight",
  "openclaw",
  "opencode",
  "tutti-agent"
];

test("detects every registered provider through constants and literal branches", () => {
  for (const providerId of providerIds) {
    const stem = providerId
      .split("-")
      .map((part) => part[0].toUpperCase() + part.slice(1))
      .join("");
    const source = [
      `if provider == providerregistry.${stem}ProviderID {}`,
      `if p != "${providerId}" {}`,
      "switch kind {",
      "if nested {",
      `case "${providerId}":`,
      "return true",
      "}",
      "}"
    ].join("\n");
    assert.equal(
      findProviderIdentityViolations(
        "services/tuttid/service/example.go",
        source,
        providerIds
      ).length,
      3,
      providerId
    );
  }
});

test("detects legacy provider identity constant forms", () => {
  const source = [
    "return agentprovider.ClaudeCode",
    "return agentproviderbiz.Nexight",
    "return ProviderOpenClaw"
  ].join("\n");
  assert.equal(
    findProviderIdentityViolations(
      "services/tuttid/service/example.go",
      source,
      providerIds
    ).length,
    3
  );
});

test("detects provider literal branches in desktop TypeScript", () => {
  const source = [
    'if (p === "codex") { return true; }',
    "switch (kind) {",
    'case "opencode":',
    "return false;",
    "}"
  ].join("\n");
  assert.equal(
    findProviderIdentityViolations(
      "apps/desktop/src/renderer/providerPolicy.ts",
      source,
      providerIds
    ).length,
    2
  );
});

test("detects provider identity dispatched through Set membership", () => {
  const source = [
    "const runtimeProbeFallbackProviders = new Set<Provider>([",
    '  "cursor"',
    "]);",
    "return runtimeProbeFallbackProviders.has(provider);"
  ].join("\n");
  assert.deepEqual(
    findProviderIdentityViolations(
      "apps/desktop/src/renderer/providerPolicy.ts",
      source,
      providerIds
    ),
    ['apps/desktop/src/renderer/providerPolicy.ts:2: "cursor"']
  );
});

test("detects provider identity dispatched through array membership", () => {
  const source = [
    'const providersWithFallback = ["codex", "opencode"] as const;',
    "return providersWithFallback.includes(provider);"
  ].join("\n");
  assert.equal(
    findProviderIdentityViolations(
      "apps/desktop/src/renderer/providerPolicy.ts",
      source,
      providerIds
    ).length,
    2
  );
});

test("allows provider identity catalogs that do not dispatch through membership", () => {
  const source = [
    'const providerLabels = ["codex", "opencode"] as const;',
    "return providerLabels.map(renderProviderLabel);"
  ].join("\n");
  assert.deepEqual(
    findProviderIdentityViolations(
      "apps/desktop/src/renderer/providerLabels.ts",
      source,
      providerIds
    ),
    []
  );
});

test("allows provider enum validation without treating it as behavior dispatch", () => {
  const source = [
    'const desktopAgentProviders = ["codex", "opencode"] as const;',
    "return desktopAgentProviders.includes(provider);"
  ].join("\n");
  assert.deepEqual(
    findProviderIdentityViolations(
      "apps/desktop/src/shared/preferences/core.ts",
      source,
      providerIds
    ),
    []
  );
});

test("skips desktop TypeScript tests but scans adjacent production files", () => {
  const source = 'if (p === "codex") {}';
  assert.deepEqual(
    findProviderIdentityViolations(
      "apps/desktop/src/renderer/providerPolicy.test.ts",
      source,
      providerIds
    ),
    []
  );
  assert.equal(
    findProviderIdentityViolations(
      "apps/desktop/src/renderer/providerPolicy.ts",
      source,
      providerIds
    ).length,
    1
  );
});

test("does not treat descriptor lookup or unrelated literals as identity branches", () => {
  const source = [
    "descriptor, ok := providerregistry.Find(provider)",
    'name := "codex"',
    'values := map[string]bool{"opencode": true}'
  ].join("\n");
  assert.deepEqual(
    findProviderIdentityViolations(
      "services/tuttid/service/example.go",
      source,
      providerIds
    ),
    []
  );
});

test("keeps exemptions exact", () => {
  assert.equal(
    isExemptPath("packages/agent/daemon/runtime/codex_appserver_adapter.go"),
    true
  );
  assert.equal(
    isExemptPath("packages/agent/daemon/runtime/codex_appserver_policy.go"),
    false
  );
  assert.equal(
    isExemptPath("services/tuttid/service/agent/external_import_parse.go"),
    true
  );
  assert.equal(
    isExemptPath(
      "services/tuttid/service/agent/external_import_claude_export.go"
    ),
    true
  );
  assert.equal(
    isExemptPath("services/tuttid/service/agent/external_import_policy.go"),
    false
  );
});

test("rejects an invalid provider catalog", () => {
  assert.throws(() => createProviderIdentityPatterns(["codex", "codex"]), {
    name: "TypeError"
  });
});

test("incremental provider checks isolate changed sources and retain full-scan triggers", (t) => {
  const root = mkdtempSync(join(tmpdir(), "provider-scope-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const computer = "services/tuttid/service/computer/permissions.go";
  const existing = "packages/agent/daemon/runtime/policy.go";
  const desktop = "apps/desktop/src/policy.ts";
  for (const [file, source] of [
    [computer, "return receipt != nil"],
    [existing, 'if provider == "codex" {}'],
    [desktop, 'if (provider === "cursor") {}']
  ]) {
    mkdirSync(dirname(join(root, file)), { recursive: true });
    writeFileSync(join(root, file), source);
  }
  const check = selectRepositoryChecks([computer]).find(
    (item) => item.key === "boundary:agent-provider-strategy"
  );
  const args = repositoryCheckArgs(check, [computer]);
  assert.deepEqual(scanWorkspace(providerIds, JSON.parse(args[2]), root), []);
  assert.equal(scanWorkspace(providerIds, [existing, desktop], root).length, 2);
  assert.equal(scanWorkspace(providerIds, null, root).length, 2);
  assert.deepEqual(
    scanWorkspace(
      providerIds,
      [`${computer}_test.go`, "services/tuttid/deleted.go"],
      root
    ),
    []
  );
  for (const file of [
    "packages/agent/daemon/providerregistry/registry.go",
    "tools/scripts/check-agent-provider-strategy-boundaries.mjs",
    "tools/scripts/repository-checks.mjs"
  ]) {
    assert.deepEqual(repositoryCheckArgs(check, [file]), [], file);
  }
});
