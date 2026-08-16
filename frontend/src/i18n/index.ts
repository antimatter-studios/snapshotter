import i18next from "i18next";
import { initReactI18next } from "react-i18next";
import en from "../locales/en.json";
import de from "../locales/de.json";
import es from "../locales/es.json";
import fr from "../locales/fr.json";

/**
 * i18next, configured once and imported for its side effect from main.tsx.
 *
 * The catalogues are bundled rather than fetched over a backend: text is needed
 * for the first paint, and a round trip would mean either a flash of English or
 * an empty window. Four languages of interface text is a few kilobytes.
 *
 * The choice is written to the settings file rather than kept here, because the
 * menu bar is drawn in Go and reads the same file. The settings watcher redraws
 * it, so switching language in the window changes both surfaces without a
 * relaunch.
 */

export const languages = ["en", "de", "es", "fr"] as const;
export type Language = (typeof languages)[number];

/**
 * The flag for each language.
 *
 * A flag is a country and a language is not, which makes this a compromise
 * rather than a correct mapping. With the language's own name beside it the flag
 * is decoration rather than the only way to tell them apart.
 */
export const flags: Record<Language, string> = {
  en: "🇬🇧",
  de: "🇩🇪",
  es: "🇪🇸",
  fr: "🇫🇷",
};

/** The last choice, so the first paint is in the right language. */
const cacheKey = "snapshotter.language";

export function storedLanguage(): Language {
  try {
    const saved = localStorage.getItem(cacheKey);
    if (saved && (languages as readonly string[]).includes(saved)) return saved as Language;
  } catch {
    // Private browsing, or a webview without storage. English is a fine default
    // and the configuration file corrects it a moment later.
  }
  return "en";
}

export function rememberLanguage(code: Language) {
  try {
    localStorage.setItem(cacheKey, code);
  } catch {
    // Not remembering it is survivable: the settings file is the durable copy,
    // and this only affects the next first paint.
  }
}

void i18next.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    de: { translation: de },
    es: { translation: es },
    fr: { translation: fr },
  },
  lng: storedLanguage(),
  fallbackLng: "en",
  // Keys carry dots as part of their name — "browser.colName" is one key, not a
  // path into nested objects — and the same for colons.
  keySeparator: false,
  nsSeparator: false,
  interpolation: {
    // React escapes everything it renders already, and doing it twice turns an
    // apostrophe in a path into &#39;.
    escapeValue: false,
  },
});

export default i18next;
