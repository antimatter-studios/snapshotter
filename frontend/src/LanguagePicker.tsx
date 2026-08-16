import { useTranslation } from "react-i18next";
import { Config } from "./api";
import { flags, languages, rememberLanguage, type Language } from "./i18n";

/**
 * Choosing which language the application speaks.
 *
 * A native select rather than a built menu: it is four items that never change,
 * and the platform's own control is keyboard-navigable, screen-reader-legible and
 * correctly positioned on every display without any of that being written here.
 *
 * The flag and the language's own name are shown together. The flag alone would
 * be a poor control — a flag is a country rather than a language, and the ones
 * for Spanish and French claim for a single country something spoken across
 * dozens — so the name is what identifies it and the flag is what makes it quick
 * to find. Each language is named in itself, never translated, because someone
 * who has landed in a language they cannot read needs to recognise their own.
 */
export function LanguagePicker() {
  const { t, i18n } = useTranslation();

  // i18next holds the live language; the settings file is what the menu bar
  // reads, so both are written.
  const choose = (next: Language) => {
    void i18n.changeLanguage(next);
    rememberLanguage(next);
    Config.SetLanguage(next).catch(() => {});
  };

  // Written in each language rather than in the current one, so "Deutsch" is
  // findable by a German speaker looking at a Spanish interface.
  const endonym: Record<Language, string> = {
    en: "English",
    de: "Deutsch",
    es: "Español",
    fr: "Français",
  };

  return (
    <label className="language-picker" title={t("language.label")}>
      <span className="visually-hidden">{t("language.label")}</span>
      <select value={i18n.language} onChange={(e) => choose(e.target.value as Language)}>
        {languages.map((code) => (
          <option key={code} value={code}>
            {flags[code]} {endonym[code]}
          </option>
        ))}
      </select>
    </label>
  );
}
