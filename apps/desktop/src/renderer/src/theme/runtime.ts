import {
  applyHostThemeId,
  applyHostThemeTokens,
  normalizeHostThemeId,
  normalizeHostThemeTokens,
  readHostThemeFromLocation,
  type HostThemeTokens
} from "./hostThemeTokens.ts";
import {
  defaultDesktopThemeSource,
  type DesktopThemeSource,
  type DesktopThemeAppearance,
  type DesktopThemeState
} from "../../../shared/theme/index.ts";

function readInitialThemeAppearance(): DesktopThemeAppearance {
  return resolveWindowThemeAppearance();
}

function resolveWindowThemeAppearance(): DesktopThemeAppearance {
  if (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
  ) {
    return "dark";
  }

  return "light";
}

// Embedded host theme pin.
//
// When the web build runs inside the RnDMaster shell the appearance is owned by
// the host: the embedded settings hide the Appearance section, so nothing in
// this renderer should be able to move it. The host passes the resolved
// appearance as `tuttiHostTheme` on the iframe URL (applied here at module load
// so the very first paint already matches) and pushes later changes over the
// secured host bridge. While the pin is set every theme write — the daemon
// preference hydration included — keeps its source but renders the host's
// appearance.
let hostThemeAppearance: DesktopThemeAppearance | null =
  readHostThemeAppearanceFromLocation();

// The host also owns the palette (issue 01): its stable role tokens arrive as
// resolved colours, first on the iframe URL — applied here at module load, so
// the very first paint is already the host's theme and not a stale one — then
// over the same secured bridge. `EmbeddedHostTheme.css` maps them onto this
// renderer's variables; provider brand colours are deliberately not mapped.
applyHostThemeFromLocation();

let requestedTheme: DesktopThemeState = {
  appearance: readInitialThemeAppearanceFromLocation(),
  source: readInitialThemeSourceFromLocation()
};

let activeTheme: DesktopThemeState = effectiveTheme(requestedTheme);

syncDocumentTheme(activeTheme);

if (typeof window !== "undefined" && typeof window.matchMedia === "function") {
  const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
  const handleChange = () => {
    if (activeTheme.source !== "system") {
      return;
    }

    setActiveTheme(resolveDesktopThemeState("system"));
  };

  if (typeof mediaQuery.addEventListener === "function") {
    mediaQuery.addEventListener("change", handleChange);
  } else if (typeof mediaQuery.addListener === "function") {
    mediaQuery.addListener(handleChange);
  }
}

export interface DesktopThemeSourceApi {
  getTheme(): Promise<DesktopThemeState>;
  onThemeChanged(listener: (theme: DesktopThemeState) => void): () => void;
}

export function syncDocumentTheme(theme: DesktopThemeState): void {
  if (typeof document === "undefined") {
    return;
  }

  document.documentElement.dataset.theme = theme.appearance;
  document.documentElement.dataset.themeSource = theme.source;
  document.documentElement.style.colorScheme = theme.appearance;
}

function effectiveTheme(theme: DesktopThemeState): DesktopThemeState {
  return hostThemeAppearance
    ? { appearance: hostThemeAppearance, source: theme.source }
    : theme;
}

function commitTheme(theme: DesktopThemeState): void {
  if (
    activeTheme.source === theme.source &&
    activeTheme.appearance === theme.appearance
  ) {
    return;
  }

  activeTheme = theme;
  syncDocumentTheme(theme);
}

function setActiveTheme(theme: DesktopThemeState): void {
  requestedTheme = theme;
  commitTheme(effectiveTheme(theme));
}

// Pins the appearance to the embedding host. `null` releases the pin and falls
// back to the last theme this renderer requested.
export function setHostThemeAppearance(
  appearance: DesktopThemeAppearance | null
): void {
  if (hostThemeAppearance === appearance) {
    return;
  }

  hostThemeAppearance = appearance;
  commitTheme(effectiveTheme(requestedTheme));
}

// Replaces the host role tokens pushed over the bridge. An empty/invalid set
// clears them and the renderer falls back to its own palette.
export function setHostThemeTokens(
  tokens: unknown,
  themeId?: unknown
): void {
  const normalized: HostThemeTokens = normalizeHostThemeTokens(tokens);
  applyHostThemeTokens(normalized);
  if (themeId !== undefined) {
    applyHostThemeId(normalizeHostThemeId(themeId));
  }
}

function applyHostThemeFromLocation(): void {
  if (typeof window === "undefined") {
    return;
  }

  // Same gate as the appearance pin: readHostThemeFromLocation returns nothing
  // without the secured bridge coordinates, so the standalone build is inert.
  const { themeId, tokens } = readHostThemeFromLocation(window.location.search);
  applyHostThemeTokens(tokens);
  applyHostThemeId(themeId);
}

export function getActiveTheme(): DesktopThemeState {
  return activeTheme;
}

export function resolveDesktopThemeState(
  source: DesktopThemeSource
): DesktopThemeState {
  return {
    appearance:
      source === "dark"
        ? "dark"
        : source === "light"
          ? "light"
          : resolveWindowThemeAppearance(),
    source
  };
}

function readHostThemeAppearanceFromLocation(): DesktopThemeAppearance | null {
  if (typeof window === "undefined") {
    return null;
  }

  const params = new URLSearchParams(window.location.search);
  // The pin only exists for the secured embedded bridge; without its
  // coordinates the parameter is ignored.
  if (!params.get("tuttiBootstrap") || !params.get("tuttiHostOrigin")) {
    return null;
  }

  const appearance = params.get("tuttiHostTheme");
  return isThemeAppearance(appearance) ? appearance : null;
}

function readInitialThemeSourceFromLocation(): DesktopThemeSource {
  if (typeof window === "undefined") {
    return defaultDesktopThemeSource;
  }

  const source = new URLSearchParams(window.location.search).get("themeSource");
  return isThemeSource(source) ? source : defaultDesktopThemeSource;
}

function readInitialThemeAppearanceFromLocation(): DesktopThemeAppearance {
  const source = readInitialThemeSourceFromLocation();
  if (source === "dark" || source === "light") {
    return source;
  }

  if (typeof window !== "undefined") {
    const appearance = new URLSearchParams(window.location.search).get("theme");
    if (isThemeAppearance(appearance)) {
      return appearance;
    }
  }

  return readInitialThemeAppearance();
}

function isThemeSource(value: string | null): value is DesktopThemeSource {
  return value === "system" || value === "dark" || value === "light";
}

function isThemeAppearance(
  value: string | null
): value is DesktopThemeAppearance {
  return value === "dark" || value === "light";
}

export function applyTheme(theme: DesktopThemeState): void {
  setActiveTheme(theme);
}

export function connectDesktopThemeSource(
  themeSource: DesktopThemeSourceApi
): () => void {
  let disposed = false;
  let themeEventVersion = 0;
  const unsubscribe = themeSource.onThemeChanged((theme) => {
    if (!disposed) {
      themeEventVersion += 1;
      applyTheme(theme);
    }
  });
  const initialThemeEventVersion = themeEventVersion;

  void themeSource.getTheme().then(
    (theme) => {
      if (!disposed && themeEventVersion === initialThemeEventVersion) {
        applyTheme(theme);
      }
    },
    () => {}
  );

  return () => {
    disposed = true;
    unsubscribe();
  };
}
