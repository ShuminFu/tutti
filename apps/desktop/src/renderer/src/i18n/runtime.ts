import {
  defaultDesktopLocale,
  normalizeDesktopLocale,
  resolveDesktopLocaleFromCandidates,
  toDocumentLanguage,
  type DesktopLocale
} from "../../../shared/i18n/index.ts";

const localeListeners = new Set<(locale: DesktopLocale) => void>();

// Embedded host locale pin.
//
// When the web build runs inside the RnDMaster shell the interface language is
// owned by the host: the embedded settings hide the General/Language section,
// so nothing in this renderer should be able to move it. The host passes the
// resolved locale as `tuttiHostLang` on the iframe URL (applied here at module
// load so the very first paint already matches) and pushes later changes over
// the secured host bridge. While the pin is set every locale write — daemon
// preference hydration included — keeps the requested value but renders the
// host's language. Otherwise an English OS / English daemon preference would
// paint the first frame (and every restart) in English while the host is
// Chinese.
export function readHostLocaleFromSearch(search: string): DesktopLocale | null {
  const params = new URLSearchParams(search || "");
  if (!params.get("tuttiBootstrap") || !params.get("tuttiHostOrigin")) {
    return null;
  }

  return normalizeDesktopLocale(params.get("tuttiHostLang"));
}

function readHostLocaleFromLocation(): DesktopLocale | null {
  if (typeof window === "undefined") {
    return null;
  }

  return readHostLocaleFromSearch(window.location.search);
}

function readInitialLocale(): DesktopLocale {
  if (typeof window === "undefined") {
    return defaultDesktopLocale;
  }

  const searchParams = new URLSearchParams(window.location.search);
  const queryLocale = searchParams.get("lang");

  return resolveDesktopLocaleFromCandidates([
    readHostLocaleFromLocation(),
    queryLocale,
    ...(navigator.languages ?? []),
    navigator.language
  ]);
}

let hostLocale: DesktopLocale | null = readHostLocaleFromLocation();
let requestedLocale: DesktopLocale = readInitialLocale();
let activeLocale: DesktopLocale = hostLocale ?? requestedLocale;

export interface DesktopLocaleSource {
  getLocale(): Promise<DesktopLocale>;
  onLocaleChanged(listener: (locale: DesktopLocale) => void): () => void;
}

export function syncDocumentLanguage(locale: DesktopLocale): void {
  if (typeof document === "undefined") {
    return;
  }

  document.documentElement.lang = toDocumentLanguage(locale);
}

function effectiveLocale(locale: DesktopLocale): DesktopLocale {
  return hostLocale ?? locale;
}

function setActiveLocale(locale: DesktopLocale): void {
  if (activeLocale === locale) {
    return;
  }

  activeLocale = locale;
  syncDocumentLanguage(locale);
  localeListeners.forEach((listener) => {
    listener(locale);
  });
}

export function subscribeLocale(
  listener: (locale: DesktopLocale) => void
): () => void {
  localeListeners.add(listener);
  return () => {
    localeListeners.delete(listener);
  };
}

export function getActiveLocale(): DesktopLocale {
  return activeLocale;
}

export function applyLocale(locale: DesktopLocale): void {
  requestedLocale = locale;
  setActiveLocale(effectiveLocale(locale));
}

// Pins the rendered locale to the embedding host. `null` releases the pin and
// falls back to the last locale this renderer requested.
export function setHostLocale(locale: DesktopLocale | null): void {
  if (hostLocale === locale) {
    return;
  }

  hostLocale = locale;
  setActiveLocale(effectiveLocale(requestedLocale));
}

export function connectDesktopLocaleSource(
  localeSource: DesktopLocaleSource
): () => void {
  let disposed = false;
  let localeEventVersion = 0;
  const unsubscribe = localeSource.onLocaleChanged((locale) => {
    if (!disposed) {
      localeEventVersion += 1;
      applyLocale(locale);
    }
  });
  const initialLocaleEventVersion = localeEventVersion;

  void localeSource.getLocale().then(
    (locale) => {
      if (!disposed && localeEventVersion === initialLocaleEventVersion) {
        applyLocale(locale);
      }
    },
    () => {}
  );

  return () => {
    disposed = true;
    unsubscribe();
  };
}
