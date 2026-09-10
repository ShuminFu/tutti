// Host theme tokens for the RnDMaster embedded build.
//
// The embedding host owns the palette, not just dark/light: it reads its own
// stable role tokens (`--rnd-theme-*` plus the canvas toolbar surface) as
// already-resolved colour strings and hands them over — on the iframe URL for
// the very first paint, then over the secured `tutti-host-theme` bridge. This
// module only decodes, validates and paints them onto the document root as
// `--rndmaster-host-<role>` custom properties; `EmbeddedHostTheme.css` is what
// maps them onto this renderer's own variables.
//
// Nothing here runs for the standalone desktop build: the caller gates on the
// embedded bridge coordinates, and without the root attribute the mapping
// stylesheet matches nothing.

export type HostThemeTokens = Record<string, string>;

export const HOST_THEME_TOKEN_PREFIX = "--rndmaster-host-";
export const HOST_THEME_ROOT_ATTRIBUTE = "data-rndmaster-host-theme";
export const HOST_THEME_ID_ATTRIBUTE = "data-rndmaster-host-theme-id";

const ROLE_PATTERN = /^[a-z][a-z0-9-]*$/;
const THEME_ID_PATTERN = /^[a-z][a-z0-9-]{0,63}$/;
// Anything that could escape the single declaration, or pull in a remote
// resource, is rejected outright.
const UNSAFE_VALUE_PATTERN = /[;{}<>\\"']|\/\*|url\(|@import|expression/i;
const COLOR_FUNCTIONS = "rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix";
const COLOR_PATTERN = new RegExp(
  `^(?:#[0-9a-f]{3,8}|(?:${COLOR_FUNCTIONS})\\([^;{}]*\\)|transparent|currentcolor)$`,
  "i"
);
// A fully transparent colour means the host could not resolve the role; treat
// it as missing rather than painting the workbench see-through.
const FULLY_TRANSPARENT = /^rgba?\(\s*0\s*,\s*0\s*,\s*0\s*,\s*0\s*\)$/i;

export function normalizeHostThemeTokenValue(value: unknown): string {
  if (typeof value !== "string") {
    return "";
  }

  const raw = value.replace(/\s+/g, " ").trim();
  if (!raw || raw.length > 200) {
    return "";
  }
  if (UNSAFE_VALUE_PATTERN.test(raw) || /\bvar\(/i.test(raw)) {
    return "";
  }
  if (FULLY_TRANSPARENT.test(raw)) {
    return "";
  }

  return COLOR_PATTERN.test(raw) ? raw : "";
}

export function normalizeHostThemeId(value: unknown): string {
  if (typeof value !== "string") {
    return "";
  }

  const id = value.trim().toLowerCase();
  return THEME_ID_PATTERN.test(id) ? id : "";
}

// Accepts the bridge payload shape only: a plain record of role -> colour.
// Arrays, class instances and everything else are rejected silently.
export function normalizeHostThemeTokens(input: unknown): HostThemeTokens {
  if (!input || typeof input !== "object" || Array.isArray(input)) {
    return {};
  }

  const tokens: HostThemeTokens = {};
  for (const [role, value] of Object.entries(input as Record<string, unknown>)) {
    if (!ROLE_PATTERN.test(role)) {
      continue;
    }
    const colour = normalizeHostThemeTokenValue(value);
    if (colour) {
      tokens[role] = colour;
    }
  }

  return tokens;
}

// `role:encodeURIComponent(value)` joined by commas — see the host's
// hostThemeTokens.js, which owns the encoder.
export function decodeHostThemeTokens(encoded: string | null): HostThemeTokens {
  if (!encoded) {
    return {};
  }

  const tokens: HostThemeTokens = {};
  for (const entry of encoded.split(",")) {
    const at = entry.indexOf(":");
    if (at <= 0) {
      continue;
    }

    const role = entry.slice(0, at);
    if (!ROLE_PATTERN.test(role)) {
      continue;
    }

    let decoded = "";
    try {
      decoded = decodeURIComponent(entry.slice(at + 1));
    } catch {
      continue;
    }

    const colour = normalizeHostThemeTokenValue(decoded);
    if (colour) {
      tokens[role] = colour;
    }
  }

  return tokens;
}

// Reads the first-frame payload off the iframe URL. The gate is the secured
// embedded bridge coordinates: the standalone desktop build has none, so it
// gets an empty result and never touches its own palette.
export function readHostThemeFromLocation(search: string): {
  themeId: string;
  tokens: HostThemeTokens;
} {
  const params = new URLSearchParams(search || "");
  if (!params.get("tuttiBootstrap") || !params.get("tuttiHostOrigin")) {
    return { themeId: "", tokens: {} };
  }

  return {
    themeId: normalizeHostThemeId(params.get("tuttiHostThemeId")),
    tokens: decodeHostThemeTokens(params.get("tuttiHostTokens"))
  };
}

let paintedRoles: string[] = [];

// Paints the roles onto the document root and marks it so the embedded mapping
// stylesheet applies. An empty set clears both.
export function applyHostThemeTokens(
  tokens: HostThemeTokens,
  documentRef: Document | undefined = typeof document === "undefined"
    ? undefined
    : document
): void {
  if (!documentRef) {
    return;
  }

  const root = documentRef.documentElement;
  const roles = Object.keys(tokens);
  for (const role of paintedRoles) {
    if (!(role in tokens)) {
      root.style.removeProperty(`${HOST_THEME_TOKEN_PREFIX}${role}`);
    }
  }
  for (const role of roles) {
    root.style.setProperty(`${HOST_THEME_TOKEN_PREFIX}${role}`, tokens[role]);
  }
  paintedRoles = roles;

  if (roles.length > 0) {
    root.setAttribute(HOST_THEME_ROOT_ATTRIBUTE, "");
  } else {
    root.removeAttribute(HOST_THEME_ROOT_ATTRIBUTE);
  }
}

export function applyHostThemeId(
  themeId: string,
  documentRef: Document | undefined = typeof document === "undefined"
    ? undefined
    : document
): void {
  if (!documentRef) {
    return;
  }

  const root = documentRef.documentElement;
  const id = normalizeHostThemeId(themeId);
  if (id) {
    root.setAttribute(HOST_THEME_ID_ATTRIBUTE, id);
  } else {
    root.removeAttribute(HOST_THEME_ID_ATTRIBUTE);
  }
}
