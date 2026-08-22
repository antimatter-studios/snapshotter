import { describe, expect, it } from "vitest";
import { message } from "./api";

// Errors arriving from Go are the user's main feedback channel here — an
// authorization prompt that was dismissed, a snapshot that is not mounted, a
// folder no snapshot covers — so they are shown rather than logged. What arrives
// is whatever the bindings rejected with, which is not always an Error.

describe("turning a rejection into a sentence", () => {
  it("uses the message of an Error, not its name", () => {
    expect(message(new Error("the snapshot is not mounted"))).toBe("the snapshot is not mounted");
  });

  it("passes a plain string through untouched", () => {
    // The bindings reject with a bare string for a failure raised in Go rather
    // than thrown in the runtime, and wrapping it would show the user quotes.
    expect(message("diskutil exited with status 1")).toBe("diskutil exited with status 1");
  });

  it("says something rather than nothing for anything else", () => {
    // Whatever this is, it ends up on screen. An empty banner is the one outcome
    // that leaves the reader with no idea anything went wrong.
    for (const thrown of [{ code: 42 }, 42, null, undefined]) {
      expect(message(thrown)).not.toBe("");
    }
  });
});
