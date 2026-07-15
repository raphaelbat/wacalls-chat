import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

import ptBR from "./locales/pt-BR/common.json";
import en from "./locales/en/common.json";
import es from "./locales/es/common.json";
import ar from "./locales/ar/common.json";

export const SUPPORTED_LANGS = [
  { code: "pt-BR", label: "Português", flag: "🇧🇷", short: "BR", country: "br" },
  { code: "en", label: "English", flag: "🇺🇸", short: "EN", country: "us" },
  { code: "es", label: "Español", flag: "🇪🇸", short: "ES", country: "es" },
  { code: "ar", label: "العربية", flag: "🇸🇦", short: "AR", country: "sa" },
] as const;

export type LangCode = typeof SUPPORTED_LANGS[number]["code"];

const applyDir = (lng: string) => {
  if (typeof document === "undefined") return;
  document.documentElement.lang = lng;
  document.documentElement.dir = lng.startsWith("ar") ? "rtl" : "ltr";
};

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      "pt-BR": { common: ptBR },
      en: { common: en },
      es: { common: es },
      ar: { common: ar },
    },
    lng: "pt-BR",
    fallbackLng: "pt-BR",
    supportedLngs: ["pt-BR", "en", "es", "ar"],
    ns: ["common"],
    defaultNS: "common",
    interpolation: { escapeValue: false },
    detection: {
      order: ["localStorage"],
      lookupLocalStorage: "vozzap.lang",
      caches: ["localStorage"],
    },
  });

applyDir(i18n.language || "pt-BR");
i18n.on("languageChanged", (lng) => applyDir(lng));

export const changeLanguage = async (code: LangCode) => {
  await i18n.changeLanguage(code);
  try {
    localStorage.setItem("vozzap.lang", code);
  } catch {
    /* noop */
  }
  // Best-effort persist to the user profile; ignored if endpoint not present.
  try {
    await fetch("/api/me/language", {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ language: code }),
    });
  } catch {
    /* offline / unsupported — localStorage already covers persistence */
  }
};

export default i18n;