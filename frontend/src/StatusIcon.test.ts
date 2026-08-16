import { describe, expect, it } from "vitest";
import { iconStatuses, rowStatuses } from "./StatusIcon";
import { statusLabel } from "./format";

// A status with no icon renders nothing at all, which reads as a rendering fault
// rather than as a state — and that is exactly how it shipped: two statuses had
// marks and the rest silently had none.

describe("the marks beside a verdict", () => {
  it("covers every status a row can carry", () => {
    // "detecting" is the one exception: it is drawn as the spinner, because it is
    // the only state whose meaning is that something is still happening.
    const needsAnIcon = rowStatuses.filter((s) => s !== "detecting");
    expect([...iconStatuses].sort()).toEqual([...needsAnIcon].sort());
  });

  it("names the same statuses the labels do", () => {
    // The two lists are written separately and must not drift: a status with a
    // word but no mark is the bug this file exists to prevent.
    expect([...rowStatuses].sort()).toEqual(Object.keys(statusLabel).sort());
  });

  it("gives each status its own mark", () => {
    // Two statuses sharing a glyph would be worse than none: it would state a
    // distinction and then contradict it.
    expect(new Set(iconStatuses).size).toBe(iconStatuses.length);
  });
});
