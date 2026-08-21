import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { LanguagePicker } from "./LanguagePicker";
import { Config } from "./api";
import i18next, { languages } from "./i18n";

// The one control that changes both surfaces.
//
// The window switches language from its own state, so a failure here is nearly
// invisible in the window and total in the menu bar — which only follows because
// the choice reaches the settings file and the watcher notices. That asymmetry is
// why this is worth testing rather than clicking.

beforeEach(() => {
  vi.spyOn(Config, "SetLanguage").mockResolvedValue(undefined as never);
});

afterEach(async () => {
  vi.restoreAllMocks();
  await i18next.changeLanguage("en");
});

describe("the language picker", () => {
  it("offers every language the build carries", () => {
    render(<LanguagePicker />);

    // By value rather than by label: the label carries a flag and an endonym,
    // and what the rest of the application acts on is the code.
    const offered = screen
      .getAllByRole("option")
      .map((o) => (o as HTMLOptionElement).value)
      .sort();
    expect(offered).toEqual([...languages].sort());
  });

  // Each language is written in its own name rather than translated, so somebody
  // who has landed in one they cannot read can still find their own.
  it("names each language in itself", () => {
    render(<LanguagePicker />);

    for (const name of ["English", "Deutsch", "Español", "Français"]) {
      expect(screen.getByRole("option", { name: new RegExp(name) })).toBeTruthy();
    }
  });

  it("shows the language currently in force", async () => {
    await i18next.changeLanguage("de");
    render(<LanguagePicker />);

    expect(screen.getByRole("combobox")).toHaveValue("de");
  });

  // The part that reaches the menu bar. Changing i18next alone would look correct
  // in the window and leave the menu bar in the old language for ever.
  it("writes the choice to the settings, which is what the menu bar reads", async () => {
    render(<LanguagePicker />);

    await userEvent.selectOptions(screen.getByRole("combobox"), "fr");

    expect(Config.SetLanguage).toHaveBeenCalledWith("fr");
    expect(i18next.language).toBe("fr");
  });

  // A settings file that cannot be written must not stop the window changing
  // language. The choice is still right for this session, and refusing it would
  // trade a working window for a failed write nobody can act on.
  it("still changes the window when the settings cannot be written", async () => {
    vi.spyOn(Config, "SetLanguage").mockRejectedValue(new Error("read-only disk"));
    render(<LanguagePicker />);

    await userEvent.selectOptions(screen.getByRole("combobox"), "es");

    expect(i18next.language).toBe("es");
  });

  // Named for anything reading the page aloud. The control is a flag and a word
  // with no visible label, so without this it announces as an unlabelled select.
  it("has a name for a screen reader", () => {
    render(<LanguagePicker />);

    expect(screen.getByRole("combobox", { name: /language/i })).toBeTruthy();
  });
});
