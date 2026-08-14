import { useEffect, useState } from "react";
import { applyTheme, nextTheme, storedTheme, systemIsDark, type Theme } from "./theme";

/**
 * Cycles system → light → dark.
 *
 * The glyph shows what you would get, not what you have: a control that
 * displays the current state gives you nothing to predict from, and this one
 * has three states rather than two so a plain sun/moon pair could not describe
 * it honestly.
 */
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(storedTheme);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  const label: Record<Theme, string> = {
    system: `Following the system (${systemIsDark() ? "dark" : "light"}) — click for light`,
    light: "Light — click for dark",
    dark: "Dark — click to follow the system",
  };

  return (
    <button
      className="theme-toggle"
      onClick={() => setTheme(nextTheme(theme))}
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
