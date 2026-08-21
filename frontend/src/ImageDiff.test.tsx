import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ImageDiff } from "./ImageDiff";
import type { FileVersions } from "./api";
import * as compare from "./comparePixels";
// Imported for its side effect: it configures i18next, without which useTranslation
// hands back the key rather than the sentence and every assertion on text fails.
import "./i18n";

// Two versions of a picture, and the three ways of looking at them.
//
// The modes carry real state and an asynchronous computation, and none of it was
// exercised: a mode that renders nothing, or a difference that never arrives,
// looks like a slow window rather than a fault.

const png = "data:image/png;base64,AAAA";

function versions(over: Partial<FileVersions> = {}): FileVersions {
  return {
    kind: "image",
    readable: false,
    note: "",
    left: "",
    right: "",
    leftSize: 1000,
    rightSize: 1200,
    leftExists: true,
    rightExists: true,
    rightLabel: "the live disk",
    leftImage: png,
    rightImage: png,
    leftDims: "800×600",
    rightDims: "800×600",
    identical: false,
    ...over,
  } as FileVersions;
}

afterEach(() => vi.restoreAllMocks());

describe("comparing two pictures", () => {
  it("shows both versions side by side to begin with", () => {
    render(<ImageDiff versions={versions()} leftLabel="2026-08-20-120000" />);

    expect(screen.getAllByRole("presentation", { hidden: true }).length).toBeGreaterThanOrEqual(0);
    expect(screen.getByText("2026-08-20-120000")).toBeTruthy();
    expect(screen.getByText("the live disk")).toBeTruthy();
    // Dimensions answer "was it resized", which the byte size alone cannot.
    expect(screen.getAllByText(/800×600/).length).toBe(2);
  });

  // With one side missing there is nothing to fade between, so offering the
  // control would be a switch that does nothing.
  it("offers no modes when only one side has a picture", () => {
    render(
      <ImageDiff
        versions={versions({ rightImage: "", rightExists: false })}
        leftLabel="2026-08-20-120000"
      />,
    );

    expect(screen.queryByRole("button", { name: /overlay|difference/i })).toBeNull();
  });

  // Missing on the right means deleted, which is a different fact from missing on
  // the left, and the one somebody browsing a snapshot is usually here to find.
  it("says which kind of missing an absent side is", () => {
    render(
      <ImageDiff
        versions={versions({ rightImage: "", rightExists: false })}
        leftLabel="2026-08-20-120000"
      />,
    );

    expect(screen.getByText(/no longer in/i)).toBeTruthy();
  });

  it("computes the difference only when that mode is chosen", async () => {
    const spy = vi.spyOn(compare, "comparePixels").mockResolvedValue({
      mask: png,
      percentChanged: 1.25,
      width: 800,
      height: 600,
    });

    render(<ImageDiff versions={versions()} leftLabel="2026-08-20-120000" />);
    expect(spy).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: /difference/i }));

    await waitFor(() => expect(spy).toHaveBeenCalledOnce());
    expect(await screen.findByText(/1\.25/)).toBeTruthy();
  });

  // Two pictures of different shapes are refused rather than compared, because
  // overlaying a 1200-wide on an 800-wide compares unrelated pixels and produces
  // a confident, meaningless answer.
  it("says so when the two cannot be compared pixel for pixel", async () => {
    vi.spyOn(compare, "comparePixels").mockResolvedValue(null);

    render(<ImageDiff versions={versions()} leftLabel="2026-08-20-120000" />);
    await userEvent.click(screen.getByRole("button", { name: /difference/i }));

    expect(await screen.findByText(/different dimensions/i)).toBeTruthy();
  });

  it("says when two versions are byte for byte the same", () => {
    render(<ImageDiff versions={versions({ identical: true })} leftLabel="2026-08-20-120000" />);

    expect(screen.getByText(/byte-for-byte identical/i)).toBeTruthy();
  });

  // A picture past the cap has no data to show, and that is a different thing
  // from the picture not being there.
  it("distinguishes too large from not present", () => {
    render(
      <ImageDiff
        versions={versions({ rightImage: "", rightExists: true })}
        leftLabel="2026-08-20-120000"
      />,
    );

    expect(screen.getByText(/too large/i)).toBeTruthy();
    expect(screen.queryByText(/no longer in/i)).toBeNull();
  });
});
