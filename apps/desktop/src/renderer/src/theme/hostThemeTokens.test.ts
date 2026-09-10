import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  HOST_THEME_ID_ATTRIBUTE,
  HOST_THEME_ROOT_ATTRIBUTE,
  HOST_THEME_TOKEN_PREFIX,
  applyHostThemeId,
  applyHostThemeTokens,
  decodeHostThemeTokens,
  normalizeHostThemeId,
  normalizeHostThemeTokenValue,
  normalizeHostThemeTokens,
  readHostThemeFromLocation
} from "./hostThemeTokens.ts";

test("host theme token values accept colours and reject everything else", () => {
  assert.equal(normalizeHostThemeTokenValue("#74ADE8"), "#74ADE8");
  assert.equal(
    normalizeHostThemeTokenValue("  rgb(116, 173, 232) "),
    "rgb(116, 173, 232)"
  );
  assert.equal(
    normalizeHostThemeTokenValue("oklch(0.972369 0.005497 157.15)"),
    "oklch(0.972369 0.005497 157.15)"
  );

  for (const rejected of [
    "",
    "   ",
    "12px",
    "not-a-color",
    "var(--background)",
    "rgba(0, 0, 0, 0)",
    "#fff; background: url(http://evil)",
    "url(http://evil/x.png)",
    "#fff}",
    42,
    null,
    undefined,
    { toString: () => "#fff" }
  ] as unknown[]) {
    assert.equal(normalizeHostThemeTokenValue(rejected), "");
  }
});

test("host theme token records drop bad roles, bad values and non-records", () => {
  assert.deepEqual(
    normalizeHostThemeTokens({
      accent: "#74ADE8",
      "Bad Role": "#000000",
      border: "not-a-color",
      canvas: 7
    }),
    { accent: "#74ADE8" }
  );

  for (const rejected of [null, undefined, "tokens", 7, ["#fff"]] as unknown[]) {
    assert.deepEqual(normalizeHostThemeTokens(rejected), {});
  }
});

test("host theme tokens decode from the compact URL encoding", () => {
  const encoded = "accent:rgb(1%2C%202%2C%203),canvas:%23FFFFFF";
  assert.deepEqual(decodeHostThemeTokens(encoded), {
    accent: "rgb(1, 2, 3)",
    canvas: "#FFFFFF"
  });

  assert.deepEqual(decodeHostThemeTokens(null), {});
  assert.deepEqual(decodeHostThemeTokens(""), {});
  assert.deepEqual(
    decodeHostThemeTokens("accent:%23000000,,garbage,x:%ZZ,:%23fff,B:%23fff"),
    { accent: "#000000" }
  );
});

test("host theme ids are shape checked", () => {
  assert.equal(normalizeHostThemeId("rnd-ocean"), "rnd-ocean");
  assert.equal(normalizeHostThemeId("RND-Ocean"), "rnd-ocean");
  assert.equal(normalizeHostThemeId("rnd ocean; drop"), "");
  assert.equal(normalizeHostThemeId(7), "");
});

test("the first frame reads host tokens off the secured iframe URL", () => {
  const search =
    "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost" +
    "&tuttiHostThemeId=rnd-ocean" +
    "&tuttiHostTokens=" +
    encodeURIComponent("accent:rgb(1%2C%202%2C%203),canvas:%23FFFFFF");

  assert.deepEqual(readHostThemeFromLocation(search), {
    themeId: "rnd-ocean",
    tokens: { accent: "rgb(1, 2, 3)", canvas: "#FFFFFF" }
  });

  // Without the secured bridge coordinates — the standalone desktop build —
  // the parameters are ignored entirely.
  assert.deepEqual(
    readHostThemeFromLocation(
      "?tuttiHostThemeId=rnd-ocean&tuttiHostTokens=canvas:%23FFFFFF"
    ),
    { themeId: "", tokens: {} }
  );
  assert.deepEqual(readHostThemeFromLocation(""), { themeId: "", tokens: {} });
});

test("host theme tokens paint onto the document root and clear again", () => {
  const properties = new Map<string, string>();
  const attributes = new Map<string, string>();
  const documentRef = {
    documentElement: {
      style: {
        setProperty(name: string, value: string) {
          properties.set(name, value);
        },
        removeProperty(name: string) {
          properties.delete(name);
        }
      },
      setAttribute(name: string, value: string) {
        attributes.set(name, value);
      },
      removeAttribute(name: string) {
        attributes.delete(name);
      }
    }
  } as unknown as Document;

  applyHostThemeTokens({ accent: "#74ADE8", canvas: "#FFFFFF" }, documentRef);
  assert.equal(properties.get(`${HOST_THEME_TOKEN_PREFIX}accent`), "#74ADE8");
  assert.equal(properties.get(`${HOST_THEME_TOKEN_PREFIX}canvas`), "#FFFFFF");
  assert.equal(attributes.has(HOST_THEME_ROOT_ATTRIBUTE), true);

  // A later push that no longer carries `canvas` must not leave the old colour.
  applyHostThemeTokens({ accent: "#000000" }, documentRef);
  assert.equal(properties.get(`${HOST_THEME_TOKEN_PREFIX}accent`), "#000000");
  assert.equal(properties.has(`${HOST_THEME_TOKEN_PREFIX}canvas`), false);

  applyHostThemeTokens({}, documentRef);
  assert.equal(properties.size, 0);
  assert.equal(attributes.has(HOST_THEME_ROOT_ATTRIBUTE), false);

  applyHostThemeId("rnd-ember", documentRef);
  assert.equal(attributes.get(HOST_THEME_ID_ATTRIBUTE), "rnd-ember");
  applyHostThemeId("not a theme id", documentRef);
  assert.equal(attributes.has(HOST_THEME_ID_ATTRIBUTE), false);
});

test("host theme tokens stay inert without a document", () => {
  applyHostThemeTokens({ accent: "#74ADE8" }, undefined);
  applyHostThemeId("rnd-ocean", undefined);
});

// The mapping table itself is the other half of the contract: without it the
// painted `--rndmaster-host-*` properties reach nothing.
const embeddedHostThemeCss = readFileSync(
  new URL(
    "../features/workspace-workbench/ui/EmbeddedHostTheme.css",
    import.meta.url
  ),
  "utf8"
);

test("the embedded mapping table only applies to the embedded root", () => {
  const selectors = embeddedHostThemeCss
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("{")[0]
    .split(",")
    .map((selector) => selector.trim())
    .filter(Boolean);

  assert.deepEqual(selectors, [
    ":root[data-rndmaster-host-theme]",
    ".rndmaster-dintaldock-embedded"
  ]);
  // A bare `:root { … }` block would leak into the standalone desktop build.
  assert.equal(/(^|\})\s*:root\s*\{/.test(embeddedHostThemeCss), false);
});

test("the embedded mapping table wires tutti variables to host roles", () => {
  for (const [variable, role] of [
    ["--background", "canvas"],
    ["--background-panel", "sidebar"],
    ["--background-fronted", "surface"],
    ["--muted", "surface-muted"],
    ["--border", "border"],
    ["--text-primary", "text"],
    ["--text-secondary", "text-muted"],
    ["--primary", "accent"],
    ["--accent", "accent"],
    ["--primary-foreground", "on-accent"],
    ["--accent-bg", "accent-surface"],
    ["--border-focus", "accent-soft"]
  ] as Array<[string, string]>) {
    const declaration = new RegExp(
      `\\n\\s*${variable}:\\s*var\\(\\s*\\n?\\s*--rndmaster-host-${role}\\b`
    );
    assert.equal(
      declaration.test(embeddedHostThemeCss),
      true,
      `${variable} must resolve through --rndmaster-host-${role}`
    );
  }
});

test("the embedded mapping table leaves provider brand colours alone", () => {
  const declared = [
    ...embeddedHostThemeCss.matchAll(/\n\s*(--[a-z0-9-]+):/g)
  ].map((match) => match[1]);
  assert.equal(declared.includes("--accent-claude"), false);
  assert.equal(declared.includes("--accent-codex"), false);
  assert.equal(
    declared.some((name) => name.startsWith("--rich-text-mention")),
    false
  );
});

// Issue 02: the top 44px of the embedded renderer has to read as the same
// chrome band as the master's canvas toolbar, so the Agent header row must take
// its background from the host `tabbar` role instead of staying transparent.
test("the embedded Agent header row paints the host toolbar band", () => {
  const band = embeddedHostThemeCss.match(
    /\n\.rndmaster-dintaldock-embedded \.agent-gui-workbench-header \{([^}]*)\}/
  );
  assert.notEqual(band, null);
  const declarations = String(band?.[1] ?? "");
  assert.match(
    declarations,
    /background:\s*var\(\s*--rndmaster-host-tabbar\b/,
    "the header band must resolve through --rndmaster-host-tabbar"
  );
  // A seam is a border, a radius or a shadow; none may survive on the band.
  assert.match(declarations, /\n\s*border:\s*0;/);
  assert.match(declarations, /\n\s*border-radius:\s*0;/);
  assert.match(declarations, /\n\s*box-shadow:\s*none;/);
});

test("the embedded Agent window keeps no focus ring at the iframe edge", () => {
  const focused = embeddedHostThemeCss.match(
    /\.workbench-window\[data-focused="true"\] \{([^}]*)\}/
  );
  assert.notEqual(focused, null);
  assert.match(String(focused?.[1] ?? ""), /\n\s*border:\s*0;/);
  assert.match(String(focused?.[1] ?? ""), /\n\s*outline:\s*none;/);
});
