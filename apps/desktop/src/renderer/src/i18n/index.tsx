import type { ReactNode } from "react";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import {
  type DesktopI18nKey,
  type DesktopLocale,
  type I18nParams
} from "@shared/i18n";
import { getAppI18nRuntime, type AppI18nRuntime } from "./appRuntime.ts";
import {
  applyLocale,
  getActiveLocale,
  setHostLocale,
  subscribeLocale,
  syncDocumentLanguage
} from "./runtime.ts";

export { applyLocale, getActiveLocale, setHostLocale };
export { connectDesktopLocaleSource } from "./runtime.ts";
export { translate } from "./appRuntime.ts";

export type TranslateFn = (key: DesktopI18nKey, params?: I18nParams) => string;

const I18nContext = createContext<{
  i18n: AppI18nRuntime;
  locale: DesktopLocale;
  t: TranslateFn;
} | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<DesktopLocale>(() => getActiveLocale());

  useEffect(() => {
    syncDocumentLanguage(getActiveLocale());

    return subscribeLocale((nextLocale: DesktopLocale) => {
      setLocale((current) => (current === nextLocale ? current : nextLocale));
    });
  }, []);

  const value = useMemo(() => {
    const i18n = getAppI18nRuntime(locale);

    return {
      i18n,
      locale,
      t: (key: DesktopI18nKey, params?: I18nParams) => i18n.t(key, params)
    };
  }, [locale]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useTranslation(): {
  i18n: AppI18nRuntime;
  locale: DesktopLocale;
  t: TranslateFn;
} {
  const context = useContext(I18nContext);

  if (!context) {
    const locale = getActiveLocale();
    const i18n = getAppI18nRuntime(locale);

    return {
      i18n,
      locale,
      t: (key: DesktopI18nKey, params?: I18nParams) => i18n.t(key, params)
    };
  }

  return context;
}
