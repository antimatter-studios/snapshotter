import { describe, expect, it } from "vitest";
import { en, type Key } from "./en";
import { de } from "./de";
import { es } from "./es";
import { fr } from "./fr";

// TypeScript already requires every catalogue to carry every key. What it cannot
// check is what is inside the strings, and the failures there are silent: a
// translation that drops a {placeholder} renders a sentence with the value
// missing, which reads as finished text rather than as a fault.

const catalogues = { de, es, fr };
const placeholders = (text: string) => (text.match(/\{(\w+)\}/g) ?? []).sort();

describe("the translation catalogues", () => {
  for (const [code, catalogue] of Object.entries(catalogues)) {
    describe(code, () => {
      it("carries every key English does", () => {
        expect(Object.keys(catalogue).sort()).toEqual(Object.keys(en).sort());
      });

      it("leaves nothing blank", () => {
        // An empty string falls back to English at runtime, which would hide a
        // missing translation behind text that looks deliberate.
        const blank = Object.entries(catalogue)
          .filter(([, text]) => text.trim() === "")
          .map(([key]) => key);
        expect(blank).toEqual([]);
      });

      it("keeps the same placeholders as English", () => {
        // Order is not checked — a translator has to be free to move {version}
        // to wherever the sentence needs it — but the set must match, because a
        // dropped placeholder silently loses the value it carried.
        const wrong = (Object.keys(en) as Key[])
          .filter((key) => placeholders(catalogue[key]).join() !== placeholders(en[key]).join())
          .map((key) => `${key}: expected ${placeholders(en[key]).join(" ")}`);
        expect(wrong).toEqual([]);
      });
    });
  }
});
