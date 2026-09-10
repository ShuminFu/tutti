// Host window safe area for the RnDMaster embedded build (issue 04).
//
// While the embedded workbench sits under the master shell's canvas toolbar the
// window's own controls — the macOS traffic lights, or a frameless Windows
// caption — overlap that toolbar, not this iframe. The moment the user puts the
// embedded Agent window into its own fullscreen (see
// `installHostWorkbenchLayoutNotifications`) the master hides its toolbar and
// this renderer's top row becomes the topmost row of the whole window. It then
// has to yield the same safe area the master shell was yielding.
//
// The host owns those numbers: they live on its `--window-controls-*` tokens.
// It reads them as resolved CSS pixels and hands them over on the iframe URL
// (`tuttiHostInsets`) for the first paint, then over the same secured
// `tutti-host-theme` bridge as the palette. This module only decodes, validates
// and paints them as `--rndmaster-host-controls-*` custom properties plus a
// platform attribute; `EmbeddedDintalDock.css` is what consumes them, and only
// while the window shell reports `data-display-mode="fullscreen"`.
//
// Nothing here runs for the standalone desktop build: the URL gate is the
// secured embedded bridge coordinates, and without the painted properties the
// stylesheet's `var(..., 0px)` fallbacks leave the layout exactly as upstream.

export interface HostWindowInsets {
  platform: HostWindowPlatform;
  controlsTop?: number;
  controlsLeft?: number;
  controlsGap?: number;
  captionRight?: number;
}

export type HostWindowPlatform = "mac" | "windows" | "linux" | "other";

export const HOST_WINDOW_INSET_PREFIX = "--rndmaster-host-";
export const HOST_WINDOW_PLATFORM_ATTRIBUTE = "data-rndmaster-host-platform";

// URL/CSS name -> message field. The CSS custom property is the prefix plus the
// URL name, so `controls-left` paints `--rndmaster-host-controls-left`.
export const HOST_WINDOW_INSET_FIELDS: Readonly<
  Record<string, keyof HostWindowInsets>
> = Object.freeze({
  "controls-top": "controlsTop",
  "controls-left": "controlsLeft",
  "controls-gap": "controlsGap",
  "caption-right": "captionRight"
});

const NAME_PATTERN = /^[a-z][a-z0-9-]*$/;
// A window safe area is never this large; anything bigger is a bad payload.
const MAX_INSET_PX = 1000;

export function normalizeHostWindowPlatform(value: unknown): HostWindowPlatform {
  if (typeof value !== "string") {
    return "other";
  }

  const raw = value.trim().toLowerCase();
  if (raw === "mac" || raw === "darwin") {
    return "mac";
  }
  if (raw === "windows") {
    return "windows";
  }
  if (raw === "linux") {
    return "linux";
  }
  return "other";
}

// Finite, non-negative CSS pixels only. Accepts a number or a bare numeric
// string; everything else — NaN, Infinity, negatives, units, objects — is
// rejected so a malformed field is dropped instead of painting garbage.
export function normalizeHostWindowInsetPx(value: unknown): number | null {
  const px =
    typeof value === "number"
      ? value
      : typeof value === "string" && value.trim() !== ""
        ? Number(value.trim())
        : Number.NaN;
  if (!Number.isFinite(px) || px < 0 || px > MAX_INSET_PX) {
    return null;
  }
  return Math.round(px);
}

// Accepts the bridge payload shape only: a plain record with a platform string
// and numeric insets. Arrays and non-objects are rejected outright (the caller
// then drops just the insets field and keeps the rest of the theme message).
export function normalizeHostWindowInsets(
  input: unknown
): HostWindowInsets | null {
  if (!input || typeof input !== "object" || Array.isArray(input)) {
    return null;
  }

  const source = input as Record<string, unknown>;
  const insets: HostWindowInsets = {
    platform: normalizeHostWindowPlatform(source.platform)
  };
  for (const field of Object.values(HOST_WINDOW_INSET_FIELDS)) {
    if (field === "platform") {
      continue;
    }
    const px = normalizeHostWindowInsetPx(source[field]);
    if (px !== null) {
      insets[field] = px;
    }
  }

  return insets;
}

// `platform:mac,controls-top:62,…` — see the host's hostWindowInsets.js, which
// owns the encoder.
export function decodeHostWindowInsets(
  encoded: string | null
): HostWindowInsets {
  const insets: HostWindowInsets = { platform: "other" };
  if (!encoded) {
    return insets;
  }

  for (const entry of encoded.split(",")) {
    const at = entry.indexOf(":");
    if (at <= 0) {
      continue;
    }

    const name = entry.slice(0, at);
    const value = entry.slice(at + 1);
    if (!NAME_PATTERN.test(name)) {
      continue;
    }
    if (name === "platform") {
      insets.platform = normalizeHostWindowPlatform(value);
      continue;
    }

    const field = HOST_WINDOW_INSET_FIELDS[name];
    if (!field || field === "platform") {
      continue;
    }
    const px = normalizeHostWindowInsetPx(value);
    if (px !== null) {
      insets[field] = px;
    }
  }

  return insets;
}

// Reads the first-frame payload off the iframe URL, behind the same secured
// bridge gate as the palette: the standalone build has no coordinates, gets
// nothing, and never yields any safe area.
export function readHostWindowInsetsFromLocation(
  search: string
): HostWindowInsets | null {
  const params = new URLSearchParams(search || "");
  if (!params.get("tuttiBootstrap") || !params.get("tuttiHostOrigin")) {
    return null;
  }

  const encoded = params.get("tuttiHostInsets");
  if (!encoded) {
    return null;
  }
  return decodeHostWindowInsets(encoded);
}

let paintedNames: string[] = [];

// Paints the insets onto the document root as `--rndmaster-host-<name>` and
// stamps the platform. `null` clears both, so the layout returns to upstream.
export function applyHostWindowInsets(
  insets: HostWindowInsets | null,
  documentRef: Document | undefined = typeof document === "undefined"
    ? undefined
    : document
): void {
  if (!documentRef) {
    return;
  }

  const root = documentRef.documentElement;
  const next: string[] = [];
  if (insets) {
    for (const [name, field] of Object.entries(HOST_WINDOW_INSET_FIELDS)) {
      const px = insets[field];
      if (typeof px === "number") {
        next.push(name);
      }
    }
  }

  for (const name of paintedNames) {
    if (!next.includes(name)) {
      root.style.removeProperty(`${HOST_WINDOW_INSET_PREFIX}${name}`);
    }
  }
  for (const name of next) {
    const field = HOST_WINDOW_INSET_FIELDS[name];
    root.style.setProperty(
      `${HOST_WINDOW_INSET_PREFIX}${name}`,
      `${insets?.[field] ?? 0}px`
    );
  }
  paintedNames = next;

  if (insets) {
    root.setAttribute(HOST_WINDOW_PLATFORM_ATTRIBUTE, insets.platform);
  } else {
    root.removeAttribute(HOST_WINDOW_PLATFORM_ATTRIBUTE);
  }
}
