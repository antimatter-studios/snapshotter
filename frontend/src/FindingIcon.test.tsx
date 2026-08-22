import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { FindingIcon, findingKinds } from "./FindingIcon";

// The menu bar draws these same shapes in Go, because macOS wants PNG bytes for
// a menu item. Two implementations of one vocabulary cannot share code, so what
// holds them together is this list — and a kind added to the service reaches the
// window only if it is added here too.
//
// The Go side has the matching assertion: every kind must draw something, and no
// two kinds may draw the same thing.

describe("the kinds the window knows", () => {
  it("matches the kinds the status service assigns", () => {
    // Mirrors the const block in services/status.go. A kind added there and not
    // here renders the fallback dot, which is survivable but silently wrong.
    const fromGo = [
      "snapshots",
      "schedule",
      "overdue",
      "tripwire",
      "thinning",
      "conflict",
      "space",
      "simulated",
      "stale",
    ];
    expect([...findingKinds].sort()).toEqual([...fromGo].sort());
  });

  it("has no duplicates", () => {
    expect(new Set(findingKinds).size).toBe(findingKinds.length);
  });
  it("draws an icon for a kind this build has not heard of", () => {
    // Findings are added in the service. A blank where the icon should be reads
    // as a rendering fault rather than as a new kind of finding.
    const { container } = render(<FindingIcon kind="somethingNewer" />);
    expect(container.firstElementChild).not.toBeNull();
  });

  it("marks the tripwire in red whatever the level", () => {
    // The one place a finding's icon overrides the card's colour. It marks
    // something absent, which is worth breaking a palette for.
    const { container } = render(<FindingIcon kind="tripwire" />);
    expect((container.firstElementChild as HTMLElement).style.color).toContain("--deleted");
  });
});
