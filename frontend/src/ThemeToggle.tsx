import { useEffect, useState } from "react";
import { applyTheme, nextTheme, storedTheme, systemIsDark, type Theme } from "./theme";
import { Config } from "./api";
import { useTranslation } from "./i18n";

/**
 * Cycles system → light → dark.
 *
 * The glyph shows what you would get, not what you have: a control that
 * displays the current state gives you nothing to predict from, and this one
 * has three states rather than two so a plain sun/moon pair could not describe
 * it honestly.
 */
export function ThemeToggle() {
  const { t } = useTranslation();
  // Seeded from the cache so the first paint is right, then corrected from the
  // configuration file, which is what every installation shares.
  const [theme, setTheme] = useState<Theme>(storedTheme);

  useEffect(() => {
    Config.Get()
      .then((view) => {
        const stored = view.config?.appearance?.theme;
        if (stored === "system" || stored === "light" || stored === "dark") setTheme(stored);
      })
      .catch(() => {
        // The cached value is already applied; a failure here is not worth a banner.
      });
  }, []);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  const choose = (next: Theme) => {
    setTheme(next);
    // Written to the file rather than only to this webview's storage, so the same
    // choice greets the next version installed.
    Config.SetTheme(next).catch(() => {});
  };

  const label: Record<Theme, string> = {
    system: t("theme.followingSystem", { mode: systemIsDark() ? t("theme.modeDark") : t("theme.modeLight") }),
    light: t("theme.light"),
    dark: t("theme.dark"),
  };

  return (
    <button
      className="theme-toggle"
      onClick={() => choose(nextTheme(theme))}
      title={label[theme]}
      aria-label={label[theme]}
    >
      {theme === "light" ? <SunIcon /> : theme === "dark" ? <MoonIcon /> : <AutoIcon />}
    </button>
  );
}

/* Drawn rather than typed: an emoji sun renders at a different weight from the
   rest of the interface and picks up its own colour, which a currentColor SVG
   does not. */

function SunIcon() {
  return (
    <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
      <circle cx="8" cy="8" r="3.2" fill="currentColor" />
      {[0, 45, 90, 135, 180, 225, 270, 315].map((deg) => (
        <line
          key={deg}
          x1="8"
          y1="1.4"
          x2="8"
          y2="3.1"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          transform={`rotate(${deg} 8 8)`}
        />
      ))}
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
      {/* A crescent as one path: two overlapping circles would show a seam
          wherever the button background is not opaque. */}
      <path
        d="M10.6 1.9a6.2 6.2 0 1 0 3.5 9.9A6.9 6.9 0 0 1 10.6 1.9Z"
        fill="currentColor"
      />
    </svg>
  );
}

function AutoIcon() {
  return (
    <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
      {/* Half sun, half moon: the system is deciding, and this says so without
          committing to either. */}
      <circle cx="8" cy="8" r="5.4" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path d="M8 2.6a5.4 5.4 0 0 0 0 10.8Z" fill="currentColor" />
    </svg>
  );
}
