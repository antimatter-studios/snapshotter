import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Browser } from "./Browser";
import { Browse, Restore } from "./api";
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
  it("offers a way in when no snapshot is chosen yet", () => {
    render(<Browser snapshot={null} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    expect(screen.getByRole("heading")).toHaveTextContent(/snapshot/i);
  });

  // A snapshot that exists but is not mounted has no files to show. The screen
  // has to say so and offer the one action that changes it, because otherwise an
  // empty table reads as a snapshot that captured nothing.
  it("offers to open a snapshot that is not mounted", async () => {
    const open = vi.fn();
    render(<Browser snapshot={{ ...snapshot, mounted: false }} path="/Users/someone" onPathChange={() => {}} onMount={open} onDiff={() => {}} onStatus={() => {}} />);

    await userEvent.click(screen.getByRole("button", { name: /open/i }));
    expect(open).toHaveBeenCalled();
  });
});

describe("putting a file back", () => {
  const file = row("notes.md", false, "modified");

  it("says where the file went, and that the old one was kept", async () => {
    mount([file]);
    vi.spyOn(Restore, "Restore").mockResolvedValue({
      destination: "/Users/someone/notes.md",
      backedUp: "/Users/someone/notes.md.before-restore",
    } as never);
    const status = vi.fn();

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={status} />);
    await userEvent.click(await screen.findByRole("button", { name: /copy/i }));

    // Both halves matter: where it landed, and that nothing was destroyed to put
    // it there. A restore that silently overwrote would be reported the same way.
    await waitFor(() => expect(status).toHaveBeenCalled());
    const said = status.mock.calls[0][0] as string;
    expect(said).toContain("/Users/someone/notes.md");
    expect(said).toContain("notes.md.before-restore");
  });

  it("replaces in place only for a file that differs", async () => {
    mount([file, row("photo.jpg", false, "onlyInSnapshot")]);
    const restore = vi.spyOn(Restore, "Restore").mockResolvedValue({ destination: "/x", backedUp: "" } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);
    await screen.findByText("notes.md");

    // Only the changed file offers Replace: for a file that exists on one side
    // alone there is nothing to replace, and offering it would imply otherwise.
    const replace = screen.getAllByRole("button", { name: /replace/i });
    expect(replace).toHaveLength(1);

    await userEvent.click(replace[0]);
    await waitFor(() => expect(restore).toHaveBeenCalledWith(expect.objectContaining({ replace: true })));
  });

  it("shows why a restore failed rather than reporting success", async () => {
    mount([file]);
    vi.spyOn(Restore, "Restore").mockRejectedValue(new Error("the destination is read-only"));
    const status = vi.fn();

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={status} />);
    await userEvent.click(await screen.findByRole("button", { name: /copy/i }));

    expect(await screen.findByText(/read-only/)).toBeTruthy();
    expect(status).not.toHaveBeenCalled();
  });

  // A file that exists only on disk was never in the snapshot, so there is
  // nothing to put back and the button would fail if pressed.
  it("offers nothing to put back for a file the snapshot never held", async () => {
    mount([row("draft.txt", false, "onlyOnDisk")]);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);
    await screen.findByText("draft.txt");

    expect(screen.queryByRole("button", { name: /copy/i })).toBeNull();
    // Comparing it is still offered: one whole side added is worth seeing.
    expect(screen.getByRole("button", { name: /compare/i })).toBeTruthy();
  });
});

describe("moving around", () => {
  it("walks into a folder by its name", async () => {
    mount([row("projects", true)]);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "modified", why: "" } as never);
    const went = vi.fn();

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={went} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);
    await userEvent.click(await screen.findByText("projects/"));

    expect(went).toHaveBeenCalledWith("/Users/someone/projects");
  });

  it("walks back out through a breadcrumb", async () => {
    mount([]);
    const went = vi.fn();

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={went} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Users" }));

    expect(went).toHaveBeenCalledWith("/Users");
  });

  it("says why Finder could not be opened instead of appearing to work", async () => {
    mount([]);
    vi.spyOn(Browse, "RevealInFinder").mockRejectedValue(new Error("no such folder on disk"));

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: /finder/i }));

    expect(await screen.findByText(/no such folder/)).toBeTruthy();
  });

  it("says the folder is empty rather than showing a bare table", async () => {
    mount([]);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    expect(await screen.findByText(/empty|nothing/i)).toBeTruthy();
  });
});

describe("when the folder cannot be listed", () => {
  it("says why, and shows no table rather than an empty one", async () => {
    vi.spyOn(Browse, "Merged").mockRejectedValue(new Error("no snapshot covers this folder"));

    render(<Browser snapshot={snapshot} path="/Volumes/other" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    expect(await screen.findByText(/no snapshot covers/)).toBeTruthy();
    // An empty table under a heading reads as a folder with nothing in it, which
    // is a different and much more alarming fact than a folder that could not be
    // read.
    expect(document.querySelector("tbody tr")).toBeNull();
  });

  it("passes on a note from the service without treating it as a failure", async () => {
    vi.spyOn(Browse, "Merged").mockResolvedValue({
      rows: [],
      note: "Only the first 5,000 entries are shown.",
    } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    const note = await screen.findByText(/first 5,000/);
    // As a note, not an error: the listing is correct as far as it goes, and
    // colouring it red would say something went wrong.
    expect(note.className).toContain("note");
    expect(note.className).not.toContain("error");
  });
});
