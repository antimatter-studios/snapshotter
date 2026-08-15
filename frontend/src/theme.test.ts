import { beforeEach, describe, expect, it, vi } from "vitest";
import { applyTheme, nextTheme, storedTheme, systemIsDark } from "./theme";

// The theme is stamped on the root element before the first paint, from a cache,
// because the real value arrives from Go asynchronously. That makes the failure
// modes here visual and immediate: a wrong cached value flashes the wrong theme
// on every launch, and a thrown error from localStorage leaves no theme at all.

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

describe("storedTheme", () => {
  it("follows the system until told otherwise", () => {
    expect(storedTheme()).toBe("system");
  });

  it("reads back each choice", () => {
    for (const theme of ["light", "dark", "system"] as const) {
      applyTheme(theme);
      expect(storedTheme()).toBe(theme);
    }
  });

  // Anything else in that key is not a theme. Returning it would stamp a
  // nonsense attribute and leave the window with no palette at all.
  it("ignores a value that is not a theme", () => {
    localStorage.setItem("snapshotter.theme", "chartreuse");
    expect(storedTheme()).toBe("system");
  });

  // A locked-down webview throws on access rather than returning null.
  it("survives storage that refuses to be read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("storage is not available");
    });
    expect(storedTheme()).toBe("system");
    vi.restoreAllMocks();
  });
});

describe("applyTheme", () => {
  it("stamps the root element, which is what the palette keys off", () => {
    applyTheme("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    applyTheme("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  // "system" removes the attribute rather than resolving it, so the CSS media
  // query stays in charge and the window follows a change made while it is open.
  it("removes the attribute for system rather than resolving it", () => {
    applyTheme("dark");
    applyTheme("system");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  });

  // Losing the preference is survivable; failing to change theme is not.
  it("still changes the theme when the preference cannot be saved", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("storage is full");
    });
    expect(() => applyTheme("dark")).not.toThrow();
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    vi.restoreAllMocks();
  });
});

describe("nextTheme", () => {
  // The toggle is one button, so the cycle has to return to where it started or
  // a state becomes unreachable.
  it("cycles system to light to dark and back", () => {
    expect(nextTheme("system")).toBe("light");
    expect(nextTheme("light")).toBe("dark");
    expect(nextTheme("dark")).toBe("system");
  });

  it("visits all three and returns to the start", () => {
    const seen = new Set<string>();
    let theme = storedTheme();
    for (let i = 0; i < 3; i++) {
      seen.add(theme);
      theme = nextTheme(theme);
    }
    expect(seen).toEqual(new Set(["system", "light", "dark"]));
    expect(theme).toBe(storedTheme());
  });
});

describe("systemIsDark", () => {
  it("reports what the system prefers", () => {
    vi.stubGlobal("matchMedia", (q: string) => ({ matches: q.includes("dark") }));
    expect(systemIsDark()).toBe(true);
    vi.unstubAllGlobals();
  });

  // A webview without matchMedia must still produce a boolean rather than
  // undefined, which would render as an empty label beside the toggle.
  it("answers even where matchMedia does not exist", () => {
    vi.stubGlobal("matchMedia", undefined);
    expect(typeof systemIsDark()).toBe("boolean");
    vi.unstubAllGlobals();
  });
});
