import { describe, expect, it } from "vitest";
import en from "../locales/en.json";
import de from "../locales/de.json";
import es from "../locales/es.json";
import fr from "../locales/fr.json";

// i18next owns lookup, fallback, plurals and interpolation now, so none of that
// is tested here — testing a library's own behaviour is how a suite grows without
// getting safer.
//
// What remains is the translations themselves, which no library can check: a
// dropped clause, a placeholder that did not survive, a per-cent sign written the
// English way in a German string. Every one of these has been a real fault here.

type Catalogue = Record<string, string>;
const english = en as Catalogue;
const catalogues: Record<string, Catalogue> = { de, es, fr };

const placeholders = (text: string) => (text.match(/\{\{(\w+)\}\}/g) ?? []).sort();
const ending = (text: string) => (/[.…:]$/.test(text) ? text.slice(-1) : "");

describe("the translation catalogues", () => {
  for (const [code, catalogue] of Object.entries(catalogues)) {
    describe(code, () => {
      it("carries every key English does, and no others", () => {
        expect(Object.keys(catalogue).sort()).toEqual(Object.keys(english).sort());
      });

      it("leaves nothing blank or padded", () => {
        const wrong = Object.entries(catalogue)
          .filter(([, text]) => text.trim() === "" || text !== text.trim())
          .map(([key]) => key);
        expect(wrong).toEqual([]);
      });

      it("keeps the same placeholders as English", () => {
        // Order is not checked — a translator must be free to move {{version}} —
        // but a dropped one silently loses the value it carried.
        const wrong = Object.keys(english)
          .filter((key) => placeholders(catalogue[key]).join() !== placeholders(english[key]).join())
          .map((key) => `${key}: expected ${placeholders(english[key]).join(" ")}`);
        expect(wrong).toEqual([]);
      });

      it("keeps the sentence-ending punctuation English has", () => {
        // A translation stopping short of the full stop has usually stopped short
        // of a clause. Three languages once lost "so they are taken for you" and
        // nobody could see it, because what remained was a grammatical sentence.
        const wrong = Object.keys(english)
          .filter((key) => ending(catalogue[key]) !== ending(english[key]))
          .map((key) => `${key}: English ends ${ending(english[key]) || "(none)"}`);
        expect(wrong).toEqual([]);
      });

      it("is not obviously truncated", () => {
        // German runs longer than English rather than shorter, so a translation
        // under half the length of a sentence is nearly always a lost clause.
        const wrong = Object.keys(english)
          .filter((key) => english[key].length > 25 && catalogue[key].length < english[key].length * 0.55)
          .map((key) => `${key}: ${catalogue[key].length} vs ${english[key].length}`);
        expect(wrong).toEqual([]);
      });

      it("puts a space before a per-cent sign", () => {
        // German, French and Spanish all take one; English does not.
        const wrong = Object.keys(english)
          .filter((key) => english[key].includes("%"))
          .filter((key) => /\S%/.test(catalogue[key]) && !/\d\s%/.test(catalogue[key]))
          .map((key) => `${key}: ${catalogue[key]}`);
        expect(wrong).toEqual([]);
      });

      it("writes an ellipsis as one character", () => {
        const wrong = Object.keys(english)
          .filter(
            (key) =>
              catalogue[key].includes("...") ||
              (english[key].includes("…") && !catalogue[key].includes("…")),
          )
          .map((key) => key);
        expect(wrong).toEqual([]);
      });
    });
  }
});
