/**
 * Light, dark, or whatever the system is doing.
 *
 * Three states rather than two. A plain toggle would have to guess an initial
 * value, and guessing wrong means the application is the one window on the
 * machine that ignores the system setting — so "system" is the default and is
 * kept as a real option to return to.
 */
export type Theme = "system" | "light" | "dark";

const KEY = "snapshotter.theme";

/** Reads the stored choice, defaulting to following the system. */
export function storedTheme(): Theme {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw === "light" || raw === "dark" || raw === "system") return raw;
  } catch {
    // Private browsing or a locked-down webview: not a reason to fail.
  }
  return "system";
}

/**
 * Applies a theme by stamping the root element, which is what the palette in
 * styles.css keys off. "system" removes the attribute entirely rather than
 * resolving it here, so the CSS media query stays in charge and the window
 * follows a change made while it is open.
 */
export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  if (theme === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", theme);

  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // Losing the preference is survivable; failing to change theme is not.
  }
}

/** What the system currently prefers, for labelling the "system" state. */
export function systemIsDark(): boolean {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? true;
}

/** The order the toggle cycles in: system → light → dark → system. */
export function nextTheme(current: Theme): Theme {
  return current === "system" ? "light" : current === "light" ? "dark" : "system";
}
