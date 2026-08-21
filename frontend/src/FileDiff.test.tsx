import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FileDiff } from "./FileDiff";
import { Diff } from "./api";
import type { FileVersions, SnapshotView } from "./api";
import "./i18n";

// One file, as two chosen versions hold it.
//
// The left side is fixed on the snapshot being browsed; the right is a choice,
// and that choice is the part worth testing. It defaults to the live disk, which
// is the usual question, but pointing it at another snapshot turns the panel into
// "what happened to this file between these two dates" — something the disk alone
// cannot answer, and something no other test covers.

const snapshots = [
  { name: "snap-a", stamp: "2026-08-20-120000", taken: "2026-08-20T12:00:00Z", mounted: true, mountPoint: "/tmp/a" },
  { name: "snap-b", stamp: "2026-08-18-120000", taken: "2026-08-18T12:00:00Z", mounted: true, mountPoint: "/tmp/b" },
  { name: "snap-c", stamp: "2026-08-16-120000", taken: "2026-08-16T12:00:00Z", mounted: false, mountPoint: "" },
] as unknown as SnapshotView[];

function text(over: Partial<FileVersions> = {}): FileVersions {
  return {
    kind: "text",
    readable: true,
    note: "",
    left: "one\ntwo\n",
    right: "one\ntwo CHANGED\n",
    leftSize: 8,
    rightSize: 16,
    leftExists: true,
    rightExists: true,
    rightLabel: "the live disk",
    identical: false,
    ...over,
  } as FileVersions;
}

afterEach(() => vi.restoreAllMocks());

describe("comparing one file", () => {
  it("compares against the live disk to begin with", async () => {
    const read = vi.spyOn(Diff, "FileVersions").mockResolvedValue(text() as never);
    render(<FileDiff snapshot="snap-a" livePath="/Users/someone/notes.md" snapshots={snapshots} dark={false} onClose={() => {}} />);

    await waitFor(() => expect(read).toHaveBeenCalledOnce());
    // An empty target means the disk, which is the default rather than a special
    // case.
    expect(read).toHaveBeenCalledWith("snap-a", "/Users/someone/notes.md", "");
  });

  // Only mounted snapshots can be read from, and comparing the left side with
  // itself has no answer to give.
  it("offers only the other mounted snapshots as targets", async () => {
    vi.spyOn(Diff, "FileVersions").mockResolvedValue(text() as never);
    render(<FileDiff snapshot="snap-a" livePath="/Users/someone/notes.md" snapshots={snapshots} dark={false} onClose={() => {}} />);

    await screen.findByRole("combobox");
    const offered = screen.getAllByRole("option").map((o) => (o as HTMLOptionElement).value);

    expect(offered).toContain(""); // the live disk
    expect(offered).toContain("snap-b");
    expect(offered).not.toContain("snap-a"); // itself
    expect(offered).not.toContain("snap-c"); // not mounted
  });

  it("reads both versions again when the target changes", async () => {
    const read = vi.spyOn(Diff, "FileVersions").mockResolvedValue(text() as never);
    render(<FileDiff snapshot="snap-a" livePath="/Users/someone/notes.md" snapshots={snapshots} dark={false} onClose={() => {}} />);

    await waitFor(() => expect(read).toHaveBeenCalledOnce());
    await userEvent.selectOptions(screen.getByRole("combobox"), "snap-b");

    await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
    expect(read).toHaveBeenLastCalledWith("snap-a", "/Users/someone/notes.md", "snap-b");
  });

  // A full-width panel with only a mouse target to leave by is a panel people
  // feel stuck in.
  it("closes on Escape", async () => {
    vi.spyOn(Diff, "FileVersions").mockResolvedValue(text() as never);
    const closed = vi.fn();
    render(<FileDiff snapshot="snap-a" livePath="/Users/someone/notes.md" snapshots={snapshots} dark={false} onClose={closed} />);

    await screen.findByRole("combobox");
    await userEvent.keyboard("{Escape}");

    expect(closed).toHaveBeenCalledOnce();
  });

  // A file in neither version has nothing to show, which is not the same as
  // something having gone wrong. It used to be returned as an error and put a red
  // banner over a perfectly reasonable question.
  it("says there is nothing to compare rather than reporting a failure", async () => {
    vi.spyOn(Diff, "FileVersions").mockResolvedValue(
      text({ kind: "absent", readable: false, leftExists: false, rightExists: false }) as never,
    );
    render(<FileDiff snapshot="snap-a" livePath="/Users/someone/gone.md" snapshots={snapshots} dark={false} onClose={() => {}} />);

    expect(await screen.findByText(/nothing to compare/i)).toBeTruthy();
  });

  // Declined rather than attempted, and both ordinary rather than errors — but
  // the sizes are still given, because "2.1 MB became 2.4 MB" is a real answer
  // about a file that cannot be diffed.
  it("gives the sizes for a file it cannot compare line by line", async () => {
    vi.spyOn(Diff, "FileVersions").mockResolvedValue(
      text({ kind: "binary", readable: false, note: "this looks like a binary file", leftSize: 2100000, rightSize: 2400000 }) as never,
    );
    render(<FileDiff snapshot="snap-a" livePath="/Users/someone/thing.bin" snapshots={snapshots} dark={false} onClose={() => {}} />);

    expect(await screen.findByText(/binary file/i)).toBeTruthy();
    expect(screen.getByText(/2\.0 MB/)).toBeTruthy();
  });
});
