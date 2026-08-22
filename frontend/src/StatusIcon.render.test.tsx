import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { StatusIcon, iconStatuses } from "./StatusIcon";

// StatusIcon.test.ts pins which statuses have a mark. This pins that the mark is
// actually drawn, which is a separate thing: the registry can be complete and the
// component still render nothing.

describe("drawing a verdict's mark", () => {
  it("draws one for every status in the registry", () => {
    for (const status of iconStatuses) {
      const { container } = render(<StatusIcon status={status} />);
      expect(container.firstElementChild, status).not.toBeNull();
    }
  });

  it("draws nothing at all for a status it does not know", () => {
    // Rather than a fallback glyph: a status arriving from a newer service is
    // shown by its word, which is already beside this, and inventing a mark for
    // it would state a meaning nobody chose.
    const { container } = render(<StatusIcon status="somethingNewer" />);
    expect(container.firstElementChild).toBeNull();
  });
});
