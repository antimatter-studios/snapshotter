import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeToggle } from "./ThemeToggle";
import { Config } from "./api";
import "./i18n";

// Three states, cycled by one button.
//
// The glyph shows what you would get, not what you have — a control that displays
// the current state gives nothing to predict from — and that inversion is exactly
// the kind of thing that gets "corrected" by somebody who reads the code without
// the reasoning. This pins it.

afterEach(() => {
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("data-theme");
  localStorage.clear();
});

describe("the theme toggle", () => {
  it("cycles system, light, dark and back", async () => {
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { appearance: { theme: "system" } } } as never);
    const saved = vi.spyOn(Config, "SetTheme").mockResolvedValue(undefined as never);

    render(<ThemeToggle />);
    const button = screen.getByRole("button");

    await userEvent.click(button);
    expect(saved).toHaveBeenLastCalledWith("light");
    await userEvent.click(button);
    expect(saved).toHaveBeenLastCalledWith("dark");
    await userEvent.click(button);
    expect(saved).toHaveBeenLastCalledWith("system");
  });

  // Written to the file rather than only to this webview's storage, so the same
  // choice greets the next version installed.
  it("writes the choice to the settings", async () => {
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { appearance: { theme: "system" } } } as never);
    const saved = vi.spyOn(Config, "SetTheme").mockResolvedValue(undefined as never);

    render(<ThemeToggle />);
    await userEvent.click(screen.getByRole("button"));

    expect(saved).toHaveBeenCalledOnce();
  });

  // A settings file that cannot be written must not stop the theme changing. The
  // choice is still right for this session.
  it("still changes the theme when the settings cannot be written", async () => {
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { appearance: { theme: "light" } } } as never);
    vi.spyOn(Config, "SetTheme").mockRejectedValue(new Error("read-only disk"));

    render(<ThemeToggle />);
    await userEvent.click(screen.getByRole("button"));

    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  // The button says what it would do, which is the only thing it can usefully
  // say: with three states a sun and a moon cannot describe it honestly.
  it("is named for what pressing it would do", async () => {
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { appearance: { theme: "light" } } } as never);
    vi.spyOn(Config, "SetTheme").mockResolvedValue(undefined as never);

    render(<ThemeToggle />);

    expect(await screen.findByRole("button", { name: /click for dark/i })).toBeTruthy();
  });
});
