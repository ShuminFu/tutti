import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  HOST_WINDOW_INSET_PREFIX,
  HOST_WINDOW_PLATFORM_ATTRIBUTE,
  applyHostWindowInsets,
  decodeHostWindowInsets,
  normalizeHostWindowInsetPx,
  normalizeHostWindowInsets,
  normalizeHostWindowPlatform,
  readHostWindowInsetsFromLocation
} from "./hostWindowInsets.ts";

test("window inset values accept non-negative pixels and reject everything else", () => {
  assert.equal(normalizeHostWindowInsetPx(62), 62);
  assert.equal(normalizeHostWindowInsetPx("90"), 90);
  assert.equal(normalizeHostWindowInsetPx(0), 0);
  assert.equal(normalizeHostWindowInsetPx("61.6"), 62);

  for (const rejected of [
    "",
    "   ",
    "62px",
    "not-a-number",
    -1,
    9999,
    Number.NaN,
    Number.POSITIVE_INFINITY,
    null,
    undefined,
    {},
    []
  ] as unknown[]) {
    assert.equal(normalizeHostWindowInsetPx(rejected), null);
  }
});

test("host platform normalizes to the four values the stylesheet keys on", () => {
  assert.equal(normalizeHostWindowPlatform("mac"), "mac");
  assert.equal(normalizeHostWindowPlatform("darwin"), "mac");
  assert.equal(normalizeHostWindowPlatform("windows"), "windows");
  assert.equal(normalizeHostWindowPlatform("linux"), "linux");
  assert.equal(normalizeHostWindowPlatform("plan9"), "other");
  assert.equal(normalizeHostWindowPlatform(42), "other");
  assert.equal(normalizeHostWindowPlatform(undefined), "other");
});

test("the bridge payload is accepted only as a plain record of numbers", () => {
  assert.deepEqual(
    normalizeHostWindowInsets({
      platform: "mac",
      controlsTop: 62,
      controlsLeft: 90,
      controlsGap: 8,
      captionRight: 0
    }),
    {
      platform: "mac",
      controlsTop: 62,
      controlsLeft: 90,
      controlsGap: 8,
      captionRight: 0
    }
  );

  // A single bad field is dropped; the rest of the payload survives.
  assert.deepEqual(
    normalizeHostWindowInsets({
      platform: "darwin",
      controlsTop: 62,
      controlsLeft: -5,
      controlsGap: "8px"
    }),
    { platform: "mac", controlsTop: 62 }
  );

  for (const rejected of [null, undefined, "mac", 42, [62, 90]] as unknown[]) {
    assert.equal(normalizeHostWindowInsets(rejected), null);
  }
});

test("the first-frame payload rides the iframe URL behind the secured gate", () => {
  const search =
    "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost" +
    "&tuttiHostInsets=platform%3Amac%2Ccontrols-top%3A62%2Ccontrols-left%3A90" +
    "%2Ccontrols-gap%3A8%2Ccaption-right%3A0";
  assert.deepEqual(readHostWindowInsetsFromLocation(search), {
    platform: "mac",
    controlsTop: 62,
    controlsLeft: 90,
    controlsGap: 8,
    captionRight: 0
  });

  // Without the secured bridge coordinates — the standalone desktop build —
  // the parameter is ignored entirely.
  assert.equal(
    readHostWindowInsetsFromLocation(
      "?tuttiHostInsets=platform%3Amac%2Ccontrols-top%3A62"
    ),
    null
  );
  assert.equal(readHostWindowInsetsFromLocation(""), null);
  assert.equal(
    readHostWindowInsetsFromLocation(
      "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost"
    ),
    null
  );

  // A malformed entry is skipped rather than voiding the whole payload.
  assert.deepEqual(
    decodeHostWindowInsets("platform:mac,controls-top:oops,controls-left:90"),
    { platform: "mac", controlsLeft: 90 }
  );
  assert.deepEqual(decodeHostWindowInsets("Controls-Top:62"), {
    platform: "other"
  });
});

test("host window insets paint onto the document root and clear again", () => {
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

  applyHostWindowInsets(
    {
      platform: "mac",
      controlsTop: 62,
      controlsLeft: 90,
      controlsGap: 8,
      captionRight: 0
    },
    documentRef
  );
  assert.equal(
    properties.get(`${HOST_WINDOW_INSET_PREFIX}controls-top`),
    "62px"
  );
  assert.equal(
    properties.get(`${HOST_WINDOW_INSET_PREFIX}controls-left`),
    "90px"
  );
  assert.equal(
    properties.get(`${HOST_WINDOW_INSET_PREFIX}caption-right`),
    "0px"
  );
  assert.equal(attributes.get(HOST_WINDOW_PLATFORM_ATTRIBUTE), "mac");

  // A later push that no longer carries a name must not leave the old value.
  applyHostWindowInsets({ platform: "windows", controlsGap: 12 }, documentRef);
  assert.equal(properties.has(`${HOST_WINDOW_INSET_PREFIX}controls-top`), false);
  assert.equal(
    properties.get(`${HOST_WINDOW_INSET_PREFIX}controls-gap`),
    "12px"
  );
  assert.equal(attributes.get(HOST_WINDOW_PLATFORM_ATTRIBUTE), "windows");

  applyHostWindowInsets(null, documentRef);
  assert.equal(properties.size, 0);
  assert.equal(attributes.has(HOST_WINDOW_PLATFORM_ATTRIBUTE), false);
});

test("host window insets stay inert without a document", () => {
  applyHostWindowInsets({ platform: "mac", controlsTop: 62 }, undefined);
});

// The stylesheet is the other half of the contract: without it the painted
// `--rndmaster-host-controls-*` properties reach nothing.
const embeddedDintalDockCss = readFileSync(
  new URL(
    "../features/workspace-workbench/ui/EmbeddedDintalDock.css",
    import.meta.url
  ),
  "utf8"
);

test("the mac safe area is yielded only while the embedded window is fullscreen", () => {
  // One variable moves all three columns: the provider rail, the conversation
  // rail's toolbar row and the detail column all reserve this height.
  const reservation = embeddedDintalDockCss.match(
    /:root\[data-rndmaster-host-platform="mac"\]\s*\n\s*\.rndmaster-dintaldock-embedded\s*\n\s*\.workbench-window-shell\[data-display-mode="fullscreen"\]\s*\n\s*\.workbench-window\[data-window-header-layout="overlay"\] \{([^}]*)\}/
  );
  assert.notEqual(reservation, null);
  assert.match(
    String(reservation?.[1] ?? ""),
    /--agent-gui-workbench-header-height:\s*var\(--rndmaster-host-controls-top, 0px\);/
  );

  // The collapsed rail's floating toggle is the first click target in that
  // state; it clears the light cluster horizontally too, and keeps upstream 8px
  // when the host published no insets.
  const floating = embeddedDintalDockCss.match(
    /\.rndmaster-dintaldock-embedded\s*\n\s*\.workbench-window-shell\[data-display-mode="fullscreen"\]\s*\n\s*\.agent-gui-node__rail-toggle-button--floating \{([^}]*)\}/
  );
  assert.notEqual(floating, null);
  assert.match(String(floating?.[1] ?? ""), /left:\s*max\(\s*8px,/);
  assert.match(
    String(floating?.[1] ?? ""),
    /var\(--rndmaster-host-controls-left, 0px\)/
  );
  assert.match(
    String(floating?.[1] ?? ""),
    /var\(--rndmaster-host-controls-gap, 0px\)/
  );

  // Windows yields width at the top-right instead; 0 today, non-zero only for a
  // frameless host that overlays its caption buttons.
  const caption = embeddedDintalDockCss.match(
    /:root\[data-rndmaster-host-platform="windows"\]\s*\n\s*\.rndmaster-dintaldock-embedded\s*\n\s*\.workbench-window-shell\[data-display-mode="fullscreen"\]\s*\n\s*\.agent-gui-node__detail \{([^}]*)\}/
  );
  assert.notEqual(caption, null);
  assert.match(
    String(caption?.[1] ?? ""),
    /padding-right:\s*var\(--rndmaster-host-caption-right, 0px\);/
  );

  // Every safe-area rule is gated on fullscreen: a windowed embedded Agent sits
  // under the master toolbar and must not yield anything.
  for (const selector of embeddedDintalDockCss
    .split("}")
    .filter((block) => block.includes("--rndmaster-host-"))
    .map((block) => block.split("{")[0])) {
    assert.match(selector, /\[data-display-mode="fullscreen"\]/);
  }
});
