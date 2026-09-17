import assert from "node:assert/strict";
import test from "node:test";
import { translate } from "./appRuntime.ts";
import {
  applyLocale,
  connectDesktopLocaleSource,
  getActiveLocale,
  readHostLocaleFromSearch,
  setHostLocale
} from "./runtime.ts";
import {
  applyTheme,
  connectDesktopThemeSource,
  getActiveTheme
} from "../theme/runtime.ts";

test("host locale query is ignored without secured bridge coordinates", () => {
  assert.equal(readHostLocaleFromSearch("?tuttiHostLang=zh-CN"), null);
  assert.equal(
    readHostLocaleFromSearch(
      "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost&tuttiHostLang=fr"
    ),
    null
  );
});

test("host locale query pins zh-CN and en when the bridge coordinates are present", () => {
  assert.equal(
    readHostLocaleFromSearch(
      "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost&tuttiHostLang=zh"
    ),
    "zh-CN"
  );
  assert.equal(
    readHostLocaleFromSearch(
      "?tuttiBootstrap=nonce-1&tuttiHostOrigin=http%3A%2F%2Fwails.localhost&tuttiHostLang=en-US"
    ),
    "en"
  );
});

test("host locale pin keeps daemon preference writes from moving the rendered language", () => {
  const originalLocale = getActiveLocale();

  try {
    setHostLocale("zh-CN");
    applyLocale("en");
    assert.equal(getActiveLocale(), "zh-CN");

    setHostLocale("en");
    assert.equal(getActiveLocale(), "en");
  } finally {
    setHostLocale(null);
    applyLocale(originalLocale);
  }
});

test("translate follows locale changes through the app i18n runtime", () => {
  const originalLocale = getActiveLocale();

  try {
    applyLocale("en");
    assert.equal(translate("common.neverOpened"), "Never opened");

    applyLocale("zh-CN");
    assert.equal(translate("common.neverOpened"), "从未打开");
  } finally {
    applyLocale(originalLocale);
  }
});

test("renderer locale source applies initial and broadcast locale changes", async () => {
  const originalLocale = getActiveLocale();
  let listener: ((locale: "en" | "zh-CN") => void) | null = null;

  try {
    applyLocale("en");

    const disconnect = connectDesktopLocaleSource({
      async getLocale() {
        return "zh-CN";
      },
      onLocaleChanged(nextListener) {
        listener = nextListener;
        return () => {
          listener = null;
        };
      }
    });

    await Promise.resolve();
    assert.equal(getActiveLocale(), "zh-CN");

    const emitLocaleChange = listener as
      | ((locale: "en" | "zh-CN") => void)
      | null;
    if (typeof emitLocaleChange === "function") {
      emitLocaleChange("en");
    }
    assert.equal(getActiveLocale(), "en");

    disconnect();
    const emitAfterDisconnect = listener as
      | ((locale: "en" | "zh-CN") => void)
      | null;
    if (typeof emitAfterDisconnect === "function") {
      emitAfterDisconnect("zh-CN");
    }
    assert.equal(getActiveLocale(), "en");
  } finally {
    applyLocale(originalLocale);
  }
});

test("renderer locale source ignores stale initial locale after a newer event", async () => {
  const originalLocale = getActiveLocale();
  let listener: ((locale: "en" | "zh-CN") => void) | null = null;
  let resolveLocale: ((locale: "en" | "zh-CN") => void) | null = null;

  try {
    applyLocale("en");

    const disconnect = connectDesktopLocaleSource({
      getLocale() {
        return new Promise<"en" | "zh-CN">((resolve) => {
          resolveLocale = resolve;
        });
      },
      onLocaleChanged(nextListener) {
        listener = nextListener;
        return () => {
          listener = null;
        };
      }
    });

    const emitLocaleChange = listener as
      | ((locale: "en" | "zh-CN") => void)
      | null;
    emitLocaleChange?.("zh-CN");
    assert.equal(getActiveLocale(), "zh-CN");

    if (!resolveLocale) {
      throw new Error("Expected pending locale resolver.");
    }
    const resolvePendingLocale = resolveLocale as (
      locale: "en" | "zh-CN"
    ) => void;
    resolvePendingLocale("en");
    await Promise.resolve();
    assert.equal(getActiveLocale(), "zh-CN");

    disconnect();
  } finally {
    applyLocale(originalLocale);
  }
});

test("renderer theme source applies initial and broadcast theme changes", async () => {
  const originalTheme = getActiveTheme();
  let listener:
    | ((theme: {
        appearance: "dark" | "light";
        source: "dark" | "light" | "system";
      }) => void)
    | null = null;

  try {
    applyTheme({
      appearance: "light",
      source: "system"
    });

    const disconnect = connectDesktopThemeSource({
      async getTheme() {
        return {
          appearance: "dark",
          source: "dark"
        };
      },
      onThemeChanged(nextListener) {
        listener = nextListener;
        return () => {
          listener = null;
        };
      }
    });

    await Promise.resolve();
    assert.deepEqual(getActiveTheme(), {
      appearance: "dark",
      source: "dark"
    });

    const emitThemeChange = listener as
      | ((theme: {
          appearance: "dark" | "light";
          source: "dark" | "light" | "system";
        }) => void)
      | null;
    emitThemeChange?.({
      appearance: "light",
      source: "system"
    });
    assert.deepEqual(getActiveTheme(), {
      appearance: "light",
      source: "system"
    });

    disconnect();
    const emitAfterDisconnect = listener as
      | ((theme: {
          appearance: "dark" | "light";
          source: "dark" | "light" | "system";
        }) => void)
      | null;
    emitAfterDisconnect?.({
      appearance: "dark",
      source: "dark"
    });
    assert.deepEqual(getActiveTheme(), {
      appearance: "light",
      source: "system"
    });
  } finally {
    applyTheme(originalTheme);
  }
});
