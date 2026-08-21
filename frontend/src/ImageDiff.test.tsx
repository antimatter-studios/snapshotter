import { describe, expect, it, vi, afterEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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
    rightLabel: "",
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
    render(<ImageDiff versions={versions()} leftLabel="2026-08-20-120000" rightLabel="The live disk" />);

    expect(screen.getAllByRole("presentation", { hidden: true }).length).toBeGreaterThanOrEqual(0);
    expect(screen.getByText("2026-08-20-120000")).toBeTruthy();
    // Both labels are given by the caller. The right one used to be read off the
    // data, where it arrived as English prose and stayed English in every language.
    expect(screen.getByText("The live disk")).toBeTruthy();
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
        rightLabel="The live disk"
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
        rightLabel="The live disk"
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

    render(<ImageDiff versions={versions()} leftLabel="2026-08-20-120000" rightLabel="The live disk" />);
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

    render(<ImageDiff versions={versions()} leftLabel="2026-08-20-120000" rightLabel="The live disk" />);
    await userEvent.click(screen.getByRole("button", { name: /difference/i }));

    expect(await screen.findByText(/different dimensions/i)).toBeTruthy();
  });

  it("says when two versions are byte for byte the same", () => {
    render(<ImageDiff versions={versions({ identical: true })} leftLabel="2026-08-20-120000" rightLabel="The live disk" />);

    expect(screen.getByText(/byte-for-byte identical/i)).toBeTruthy();
  });

  // A picture past the cap has no data to show, and that is a different thing
  // from the picture not being there.
  it("distinguishes too large from not present", () => {
    render(
      <ImageDiff
        versions={versions({ rightImage: "", rightExists: true })}
        leftLabel="2026-08-20-120000"
        rightLabel="The live disk"
      />,
    );

    expect(screen.getByText(/too large/i)).toBeTruthy();
    expect(screen.queryByText(/no longer in/i)).toBeNull();
  });
  // Fade is the mode that earns this component its existence: two screenshots
  // that differ by a shifted button look identical side by side, because the eye
  // has to carry a memory across the gap. Cross-dissolving them makes the shift
  // obvious.
  it("stacks the two versions so they can be cross-dissolved", async () => {
    render(<ImageDiff versions={versions()} leftLabel="Snapshot" rightLabel="The live disk" />);
    await userEvent.click(screen.getByRole("button", { name: /overlay|fade/i }));

    const stacked = document.querySelectorAll(".image-stack img");
    expect(stacked).toHaveLength(2);
    // Halfway to begin with, which is where a difference shows most.
    expect((stacked[1] as HTMLElement).style.opacity).toBe("0.5");
  });

  it("fades all the way to either version", async () => {
    render(<ImageDiff versions={versions()} leftLabel="Snapshot" rightLabel="The live disk" />);
    await userEvent.click(screen.getByRole("button", { name: /overlay|fade/i }));

    const slider = screen.getByRole("slider");
    fireEvent.change(slider, { target: { value: "1" } });
    // Entirely the right-hand version: without reaching the ends, a difference at
    // the edge of the picture can hide under the other copy.
    expect((document.querySelectorAll(".image-stack img")[1] as HTMLElement).style.opacity).toBe("1");

    fireEvent.change(slider, { target: { value: "0" } });
    expect((document.querySelectorAll(".image-stack img")[1] as HTMLElement).style.opacity).toBe("0");
  });

  it("goes back to side by side", async () => {
    render(<ImageDiff versions={versions()} leftLabel="Snapshot" rightLabel="The live disk" />);
    await userEvent.click(screen.getByRole("button", { name: /overlay|fade/i }));
    expect(document.querySelector(".image-pair")).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: /side/i }));

    // The mode that never misleads, and so the one to fall back to.
    expect(document.querySelector(".image-pair")).not.toBeNull();
  });

  it("says the comparison failed rather than showing an empty frame", async () => {
    vi.spyOn(compare, "comparePixels").mockRejectedValue(new Error("the picture could not be decoded"));

    render(<ImageDiff versions={versions()} leftLabel="Snapshot" rightLabel="The live disk" />);
    await userEvent.click(screen.getByRole("button", { name: /difference/i }));

    // The same message as a mismatched pair: from here both mean "no pixel answer
    // is available", and an empty black frame would read as no differences.
    await waitFor(() => expect(document.querySelector(".image-identical")).not.toBeNull());
    expect(document.querySelector(".pixel-mask")).toBeNull();
  });
});
