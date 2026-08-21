import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Browser } from "./Browser";
import { Browse } from "./api";
import type { SnapshotView } from "./api";
import "./i18n";

// Browsing one folder from a snapshot and from the disk at once.
//
// The intricate part is not the table: it is how folder verdicts arrive. Each
// costs a walk, so they are resolved a few at a time, after the rows are already
// on screen, and an answer that arrives for a folder nobody is looking at any
// more has to be thrown away. None of that was exercised, and all of it fails
// quietly — a verdict that never lands leaves a row saying "detecting" for ever,
// which reads as a slow disk.

const snapshot: SnapshotView = {
  name: "com.apple.TimeMachine.2026-08-20-120000.local",
  stamp: "2026-08-20-120000",
  taken: "2026-08-20T12:00:00Z",
  mounted: true,
  mountPoint: "/tmp/mnt",
} as SnapshotView;

function row(name: string, isDir: boolean, status = "notExamined") {
  return {
    relPath: name,
    absLive: "/Users/someone/" + name,
    absSnapshot: "/tmp/mnt/Users/someone/" + name,
    isDir,
    status,
    snapSize: 10,
    liveSize: 10,
    snapModTime: "2026-08-20T11:00:00Z",
    liveModTime: "2026-08-20T11:00:00Z",
  };
}

function mount(rows: ReturnType<typeof row>[]) {
  vi.spyOn(Browse, "Merged").mockResolvedValue({ rows, note: "" } as never);
}

afterEach(() => vi.restoreAllMocks());

describe("browsing a folder", () => {
  it("shows a folder as detecting until its verdict arrives", async () => {
    mount([row("projects", true)]);
    let settle: (v: unknown) => void = () => {};
    vi.spyOn(Browse, "DirectoryStatus").mockReturnValue(
      new Promise((resolve) => {
        settle = resolve;
      }) as never,
    );

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    expect(await screen.findByText(/detecting/i)).toBeTruthy();

    settle({ status: "modified", why: "" });
    await waitFor(() => expect(screen.queryByText(/detecting/i)).toBeNull());
  });

  // The reason this exists: every folder's walk fired at once took the machine to
  // 642% CPU. Three at a time is the fix, and nothing pinned it.
  it("resolves at most three folders at a time", async () => {
    mount([1, 2, 3, 4, 5, 6].map((n) => row("folder" + n, true)));

    let inFlight = 0;
    let mostAtOnce = 0;
    const release: Array<() => void> = [];
    vi.spyOn(Browse, "DirectoryStatus").mockImplementation((() => {
      inFlight++;
      mostAtOnce = Math.max(mostAtOnce, inFlight);
      return new Promise((resolve) => {
        release.push(() => {
          inFlight--;
          resolve({ status: "same", why: "" });
        });
      });
    }) as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(release.length).toBe(3));
    expect(mostAtOnce).toBe(3);

    // Releasing one lets exactly one more start, never more.
    release.shift()!();
    await waitFor(() => expect(release.length).toBe(3));
    expect(mostAtOnce).toBe(3);
  });

  // A file's verdict comes with the listing; only folders are resolved after the
  // fact. Asking about a file would be a walk nobody needed.
  it("asks about folders and not about files", async () => {
    mount([row("notes.md", false, "modified"), row("projects", true)]);
    const asked = vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(asked).toHaveBeenCalledOnce());
    expect(asked).toHaveBeenCalledWith(snapshot.name, "/Users/someone/projects");
  });

  // Every file row offers it. The panel and the service shipped in v0.22.0 with
  // nothing to open them, because this callback was threaded through and dropped.
  it("offers a compare button on every file", async () => {
    mount([row("notes.md", false, "modified")]);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);
    const opened = vi.fn();

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={opened} onStatus={() => {}} />);

    await userEvent.click(await screen.findByRole("button", { name: /compare/i }));
    expect(opened).toHaveBeenCalledWith("/Users/someone/notes.md");
  });

  // A folder that resolves to identical is hidden by the toggle. It cannot be
  // filtered when the listing is built, because a folder arrives unexamined and
  // only becomes identical once its own walk answers.
  it("hides folders that turn out identical once the toggle is off", async () => {
    mount([row("projects", true), row("archive", true)]);
    vi.spyOn(Browse, "DirectoryStatus").mockImplementation((async (_n: string, path: string) =>
      path.endsWith("archive") ? { status: "same", why: "" } : { status: "modified", why: "" }) as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    expect(await screen.findByText("archive/")).toBeTruthy();

    await userEvent.click(screen.getByRole("checkbox"));
    await waitFor(() => expect(screen.queryByText("archive/")).toBeNull());
    expect(screen.getByText("projects/")).toBeTruthy();
  });
});
