import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { Config } from "../api";
import { en, type Key } from "./en";
import { de } from "./de";
import { es } from "./es";
import { fr } from "./fr";

/**
 * Which language the window speaks.
 *
 * The catalogues are compiled in rather than fetched: text is needed for the
 * first paint, and a round trip to the service would mean either a flash of
 * English or an empty window while it arrives. Four languages of interface text
 * is a few kilobytes.
 *
 * The choice is written to the settings file rather than kept here, because the
 * menu bar is drawn in Go and reads the same file. The settings watcher redraws
 * it, so switching language in the window changes the menu bar too, without a
 * relaunch.
 */

export const languages = ["en", "de", "es", "fr"] as const;
export type Language = (typeof languages)[number];

const catalogues: Record<Language, Record<Key, string>> = { en, de, es, fr };

/**
 * The flag for each language.
 *
 * A flag is a country and a language is not, which makes this a compromise
 * rather than a correct mapping — Spanish is spoken in twenty countries and
 * Spain's flag claims it for one of them. It is what was asked for, and with the
 * language's own name beside it in the menu the flag is decoration rather than
 * the only way to tell them apart.
 *
 * English takes the Union Jack rather than the Stars and Stripes, as requested.
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

/** The lookup itself, for helpers that live outside a component. */
export type Translate = (key: Key, values?: Record<string, string | number>) => string;

interface Translation {
  language: Language;
  setLanguage: (next: Language) => void;
  /** Looks up a key, substituting any {placeholders}. */
  t: Translate;
}

const TranslationContext = createContext<Translation | null>(null);

export function TranslationProvider({ children }: { children: ReactNode }) {
  // Seeded from the cache so the first paint is right, then corrected from the
  // configuration file — the same order the theme uses, and for the same reason.
  const [language, setLanguageState] = useState<Language>(storedLanguage);

  useEffect(() => {
    Config.Get()
      .then((view) => {
        const stored = view.config?.appearance?.language;
        if (stored && (languages as readonly string[]).includes(stored)) {
          setLanguageState(stored as Language);
        }
      })
      .catch(() => {
        // The cached choice is already applied; a failure here is not worth a banner.
      });
  }, []);

  const setLanguage = useCallback((next: Language) => {
    setLanguageState(next);
    try {
      localStorage.setItem(cacheKey, next);
    } catch {
      // Not being able to remember it is survivable: the settings file below is
      // the durable copy, and this is only about the next first paint.
    }
    // Written to the file so the menu bar, which is drawn in Go, follows.
    Config.SetLanguage(next).catch(() => {});
  }, []);

  const t = useCallback(
    (key: Key, values?: Record<string, string | number>) => {
      // English is the fallback for a key a catalogue somehow lacks. The types
      // make that unreachable at build time, but a catalogue could still hold an
      // empty string, and an English word beats a blank space.
      const text = catalogues[language][key] || en[key];
      if (!values) return text;
      // By name, never by position: German and French routinely need a different
      // word order, and a translator has to be free to move a placeholder.
      return text.replace(/\{(\w+)\}/g, (whole, name: string) =>
        name in values ? String(values[name]) : whole,
      );
    },
    [language],
  );

  const value = useMemo(() => ({ language, setLanguage, t }), [language, setLanguage, t]);

  // The document's own language, so a screen reader and the browser's spell
  // checker are told what they are reading.
  useEffect(() => {
    document.documentElement.lang = language;
  }, [language]);

  return <TranslationContext.Provider value={value}>{children}</TranslationContext.Provider>;
}

export function useTranslation(): Translation {
  const value = useContext(TranslationContext);
  if (!value) throw new Error("useTranslation was called outside the TranslationProvider");
  return value;
}
