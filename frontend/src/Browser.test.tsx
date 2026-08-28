import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor, cleanup } from "@testing-library/react";
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
  // The startup disk, which is what an empty volume means. The real service
  // always sends one, so a fixture without it tests a shape that never occurs.
  device: "",
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

// Every listing calls this before it resolves a single folder, to give up on the
// checks still running for the listing being left. Stubbed for the whole file
// rather than per test: unstubbed, the real binding rejects, load() gives up
// before it renders, and the failure lands on whatever the test was actually
// about.
beforeEach(() => {
  vi.spyOn(Browse, "AbandonFolderChecks").mockResolvedValue(undefined as never);
  // How many folders to check at once now comes from the disk, so it has to be
  // stubbed for the same reason: unstubbed, the real binding rejects and no
  // listing gets as far as checking anything.
  vi.spyOn(Browse, "Lanes").mockResolvedValue(3 as never);
  // The cheap pass that runs before any walking. Stubbed for the same reason as
  // the two above: unstubbed, the real binding rejects and nothing lists.
  vi.spyOn(Browse, "ScanEventLog").mockResolvedValue({ offered: 0, found: 0, usable: false } as never);
  // The lookup pass that runs before everything. Stubbed to "nothing known" so
  // the tests below exercise the walk they are about — and stubbed at all because
  // unstubbed it rejects, which the loop treats as nothing known and would have
  // hidden the fact that the pass was never really running.
  vi.spyOn(Browse, "KnownDirectoryStatus").mockResolvedValue({ status: "notExamined", why: "" } as never);
});

afterEach(() => {
  // Unmounted BEFORE the stubs are taken away, not after.
  //
  // vitest runs afterEach hooks in stack order, so this file's teardown runs
  // ahead of the cleanup registered in test-setup — which meant every component
  // was unmounted with its bindings already restored to the real ones. Anything
  // still in flight then landed on a real call during teardown, and when that
  // went wrong the unmount went with it: the next test started with the previous
  // test's screen still in the document, and a query that should find one button
  // found a page that no longer had it.
  //
  // That is what made one Health test fail three runs in five with nothing wrong
  // in its own file. Cleaning up first costs nothing and removes the ordering
  // from the picture entirely.
  cleanup();
  vi.restoreAllMocks();
});

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
  // 642% CPU. A bounded queue is the fix, and nothing pinned it.
  //
  // The bound is now the disk's, not a constant — an SD card has one slow channel
  // and internal storage wants a deep queue — so what is asserted is that the
  // window uses the number the service gives it. Stubbed to three above.
  it("resolves no more at a time than the disk says", async () => {
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
    expect(asked).toHaveBeenCalledWith(snapshot.device, snapshot.name, "/Users/someone/projects");
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
    // The volume comes first now: a snapshot name does not identify a copy, since
    // the same date exists on every volume that was mounted when it was taken.
    vi.spyOn(Browse, "DirectoryStatus").mockImplementation((async (_device: string, _n: string, path: string) =>
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

// Slow work, counted.
//
// A folder's verdict can be a walk of everything beneath it, so a listing of
// source trees sits on a column of "detecting…" that does not change. Until it
// said how far along it was, the only evidence it was working was that nothing
// had happened yet — which reads as frozen.
describe("reporting how far the folder checks have got", () => {
  const folders = [
    { relPath: "projects", absLive: "/Users/someone/projects", isDir: true, status: "modified", snapSize: 0, liveSize: 0 },
    { relPath: "archive", absLive: "/Users/someone/archive", isDir: true, status: "modified", snapSize: 0, liveSize: 0 },
    { relPath: "notes.md", absLive: "/Users/someone/notes.md", isDir: false, status: "modified", snapSize: 1, liveSize: 2 },
  ];

  it("counts the folders, and only the folders", async () => {
    vi.spyOn(Browse, "Merged").mockResolvedValue({ rows: folders, note: "" } as never);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);
    const seen: Array<{ done: number; total: number } | null> = [];

    render(
      <Browser
        snapshot={snapshot}
        path="/Users/someone"
        onPathChange={() => {}}
        onMount={() => {}}
        onDiff={() => {}}
        onStatus={() => {}}
        onProgress={(p) => seen.push(p && { done: p.done, total: p.total })}
      />,
    );

    // Two folders and a file: the file is not walked, so it is not counted.
    await waitFor(() => expect(seen.some((p) => p?.total === 2)).toBe(true));
    // The counted phase starts at nothing done, so the bar does not appear
    // already part-full. It is not the first report any more — the event-log pass
    // runs first and has nothing to count.
    expect(seen.find((p) => p?.total === 2)).toEqual({ done: 0, total: 2 });
    // And it reaches the end.
    await waitFor(() => expect(seen.some((p) => p?.done === 2 && p.total === 2)).toBe(true));
  });

  // Cleared when there is nothing in flight, or the bar sits at 100% for ever
  // and stops meaning anything.
  it("says when there is nothing left to do", async () => {
    vi.spyOn(Browse, "Merged").mockResolvedValue({ rows: folders, note: "" } as never);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);
    const seen: Array<unknown> = [];

    render(
      <Browser
        snapshot={snapshot}
        path="/Users/someone"
        onPathChange={() => {}}
        onMount={() => {}}
        onDiff={() => {}}
        onStatus={() => {}}
        onProgress={(p) => seen.push(p)}
      />,
    );

    await waitFor(() => expect(seen[seen.length - 1]).toBeNull());
  });

  // A folder that cannot be checked still moves the count, or the bar stops short
  // of the end and looks stuck at the very moment it has finished.
  it("counts a folder that could not be checked", async () => {
    vi.spyOn(Browse, "Merged").mockResolvedValue({ rows: folders, note: "" } as never);
    vi.spyOn(Browse, "DirectoryStatus").mockRejectedValue(new Error("cannot walk it"));
    const seen: Array<{ done: number; total: number } | null> = [];

    render(
      <Browser
        snapshot={snapshot}
        path="/Users/someone"
        onPathChange={() => {}}
        onMount={() => {}}
        onDiff={() => {}}
        onStatus={() => {}}
        onProgress={(p) => seen.push(p && { done: p.done, total: p.total })}
      />,
    );

    await waitFor(() => expect(seen.some((p) => p?.done === 2 && p.total === 2)).toBe(true));
  });
});

// Navigating away has to stop the work, not just discard its results. Proving a
// folder unchanged means reading everything under it, so a listing that keeps
// asking after somebody has clicked elsewhere keeps a slow disk busy answering
// about rows nobody will see again — and the folder they DID click on waits
// behind it.
describe("giving up on a listing that has been left", () => {
  it("stops asking about folders once the listing is superseded", async () => {
    const folders = Array.from({ length: 30 }, (_, i) =>
      row(`folder-${i}`, true, "modified"),
    );
    mount(folders);

    let asked = 0;
    const release: Array<() => void> = [];
    vi.spyOn(Browse, "DirectoryStatus").mockImplementation((() => {
      asked++;
      return new Promise((resolve) => release.push(() => resolve({ status: "same", why: "" })));
    }) as never);

    const view = render(
      <Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />,
    );

    // Three in flight, which is the queue width.
    await waitFor(() => expect(release.length).toBe(3));

    // The listing goes away. Whatever was in flight resolves afterwards, the way
    // a real call would.
    view.unmount();
    release.forEach((r) => r());

    // No further folders are asked about: the workers stop where they are rather
    // than working through all thirty for a listing nobody is looking at.
    const askedWhenLeft = asked;
    await waitFor(() => expect(asked).toBe(askedWhenLeft));
    expect(asked).toBeLessThan(folders.length);
  });

  // And the service is told, because the walks already running cannot be stopped
  // from the window: they are inside a read that has to be interrupted where it
  // is happening.
  it("tells the service to give up on the walks already running", async () => {
    mount([row("projects", true, "modified")]);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    const view = render(
      <Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />,
    );

    // Once when the listing starts.
    await waitFor(() => expect(Browse.AbandonFolderChecks).toHaveBeenCalled());
    const onStart = vi.mocked(Browse.AbandonFolderChecks).mock.calls.length;

    // And again when it is left.
    view.unmount();
    await waitFor(() =>
      expect(vi.mocked(Browse.AbandonFolderChecks).mock.calls.length).toBeGreaterThan(onStart),
    );
  });
});

// The queue width is a property of the disk, so a volume that can take more must
// actually get more. Hardcoding three left internal storage mostly idle.
describe("how many folders are checked at once", () => {
  it("opens as many lanes as the volume says it can take", async () => {
    vi.mocked(Browse.Lanes).mockResolvedValue(8 as never);
    mount(Array.from({ length: 20 }, (_, i) => row(`folder-${i}`, true)));

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

    await waitFor(() => expect(release.length).toBe(8));
    expect(mostAtOnce).toBe(8);
  });

  // A disk that will not say how it is attached still has to be browsable, and a
  // rejected call must not stop the listing being checked at all.
  it("still checks folders when the disk will not say", async () => {
    vi.mocked(Browse.Lanes).mockRejectedValue(new Error("no such volume") as never);
    mount([row("projects", true)]);
    const asked = vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(asked).toHaveBeenCalled());
  });
});

// Two stages, and the window says which one it is in. They cost wildly different
// amounts — the log pass reads no trees at all, and the disk pass reads
// everything under every folder it cannot answer any other way — so a bar that
// called them both "checking folders" was hiding the only distinction that
// explains why one is instant and the other is not.
describe("saying which way the folders are being checked", () => {
  it("says the event log first, then the disk", async () => {
    mount([row("projects", true)]);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);
    const labels: string[] = [];

    render(
      <Browser
        snapshot={snapshot}
        path="/Users/someone"
        onPathChange={() => {}}
        onMount={() => {}}
        onDiff={() => {}}
        onStatus={() => {}}
        onProgress={(p) => p && labels.push(p.label)}
      />,
    );

    await waitFor(() => expect(labels.some((l) => /by disk/i.test(l))).toBe(true));
    // The first thing said is that the folder is being read, which is the stretch
    // that used to look like nothing happening at all.
    expect(labels[0]).toMatch(/reading/i);
    // Then the event log, then the disk. In that order: the cheap pass is what
    // makes the expensive one shorter, so running it second would be pointless.
    expect(labels.findIndex((l) => /event log/i.test(l)))
      .toBeLessThan(labels.findIndex((l) => /by disk/i.test(l)));
  });

  // The log is a hint and nothing more. A volume that keeps none, or one whose
  // log this process cannot read, must still be browsable.
  it("still checks the disk when the event log tells it nothing", async () => {
    vi.mocked(Browse.ScanEventLog).mockRejectedValue(new Error("no log here") as never);
    mount([row("projects", true)]);
    const asked = vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(asked).toHaveBeenCalled());
  });
});

// Three sources of an answer, costing wildly different amounts: a lookup, then
// the event log, then reading the tree. They have to run in that order, and each
// one must remove work from the next.
describe("answering from what is already known", () => {
  it("asks the lookup before the event log or the disk", async () => {
    mount([row("projects", true)]);
    const order: string[] = [];
    vi.mocked(Browse.KnownDirectoryStatus).mockImplementation((async () => {
      order.push("known");
      return { status: "notExamined", why: "" };
    }) as never);
    vi.mocked(Browse.ScanEventLog).mockImplementation((async () => {
      order.push("log");
      return { offered: 0, found: 0, usable: false };
    }) as never);
    vi.spyOn(Browse, "DirectoryStatus").mockImplementation((async () => {
      order.push("disk");
      return { status: "same", why: "" };
    }) as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(order).toContain("disk"));
    expect(order).toEqual(["known", "log", "disk"]);
  });

  // A folder the lookup settles is not walked, which is the entire point: reading
  // a tree to reach a conclusion already recorded is the work being avoided.
  it("does not walk a folder the lookup already answered", async () => {
    mount([row("projects", true), row("archive", true)]);
    vi.mocked(Browse.KnownDirectoryStatus).mockImplementation((async (_d: string, _n: string, path: string) =>
      path.endsWith("archive") ? { status: "modified", why: "" } : { status: "notExamined", why: "" }) as never);
    const walked = vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(walked).toHaveBeenCalled());
    for (const call of walked.mock.calls) {
      expect(call[2]).not.toMatch(/archive$/);
    }
    // And its answer is on screen without anything having been read for it.
    expect(await screen.findByText(/changed/i)).toBeTruthy();
  });

  // With every folder already known there is nothing for the other two passes to
  // find, so neither should run at all.
  it("reads nothing when the lookup answers everything", async () => {
    mount([row("projects", true)]);
    vi.mocked(Browse.KnownDirectoryStatus).mockResolvedValue({ status: "same", why: "" } as never);
    const scanned = vi.mocked(Browse.ScanEventLog);
    const walked = vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(screen.getByText("projects/")).toBeTruthy());
    await waitFor(() => expect(vi.mocked(Browse.KnownDirectoryStatus)).toHaveBeenCalled());
    expect(scanned).not.toHaveBeenCalled();
    expect(walked).not.toHaveBeenCalled();
  });
});

// Reacting to the click is the first job; filling the window is the second.
//
// This used to react only when the new listing arrived — eight to ten seconds on
// a slow disk, with the folder you had just left still on screen the whole time.
// Nothing said the click had landed, so it read as hung rather than as busy.
describe("reacting to a click before anything is read", () => {
  it("clears the folder you left before the new one has been read", async () => {
    mount([row("projects", true), row("notes.md", false, "modified")]);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    const { rerender } = render(
      <Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />,
    );
    expect(await screen.findByText("notes.md")).toBeTruthy();

    // The next folder's listing never arrives, which is the slow disk this is
    // about. The rows from the old one must go anyway.
    vi.spyOn(Browse, "Merged").mockReturnValue(new Promise(() => {}) as never);
    rerender(
      <Browser snapshot={snapshot} path="/Users/someone/projects" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />,
    );

    await waitFor(() => expect(screen.queryByText("notes.md")).toBeNull());
  });

  // And the walks for the folder being left are stopped BEFORE the new listing is
  // asked for. This was the other half of those eight seconds, and it was not
  // perception: a dozen walks were still running and the two directory reads for
  // the new folder queued behind them at the disk.
  it("gives up on the old folder's walks before asking for the new listing", async () => {
    const order: string[] = [];
    vi.mocked(Browse.AbandonFolderChecks).mockImplementation((async () => {
      order.push("abandon");
    }) as never);
    vi.spyOn(Browse, "Merged").mockImplementation((async () => {
      order.push("merged");
      return { rows: [], note: "" };
    }) as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    await waitFor(() => expect(order).toContain("merged"));
    expect(order).toEqual(["abandon", "merged"]);
  });

  // A listing that arrives after the reader has moved on again is discarded. It
  // describes a folder nobody is looking at, and drawing it would put the wrong
  // rows under the right breadcrumbs.
  it("throws away a listing that arrives after the reader has moved on", async () => {
    let settle: (v: unknown) => void = () => {};
    vi.spyOn(Browse, "Merged").mockReturnValue(new Promise((resolve) => { settle = resolve; }) as never);

    const { rerender } = render(
      <Browser snapshot={snapshot} path="/Users/someone/slow" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />,
    );

    // Moved on before the first listing lands.
    vi.mocked(Browse.Merged).mockResolvedValue({ rows: [row("elsewhere", true)], note: "" } as never);
    rerender(
      <Browser snapshot={snapshot} path="/Users/someone/elsewhere" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />,
    );
    settle({ rows: [row("stale", true)], note: "" });

    await waitFor(() => expect(screen.getByText("elsewhere/")).toBeTruthy());
    expect(screen.queryByText("stale/")).toBeNull();
  });
});

// The window sitting still with no rows is the moment the bar is most wanted, and
// it used to say nothing at all — it only appeared once there was something to
// count, which is after the listing has already arrived.
it("says it is reading the folder before the listing arrives", async () => {
  mount([row("projects", true)]);
  vi.spyOn(Browse, "Merged").mockReturnValue(new Promise(() => {}) as never);
  const labels: string[] = [];

  render(
    <Browser
      snapshot={snapshot}
      path="/Users/someone"
      onPathChange={() => {}}
      onMount={() => {}}
      onDiff={() => {}}
      onStatus={() => {}}
      onProgress={(p) => p && labels.push(p.label)}
    />,
  );

  await waitFor(() => expect(labels.length).toBeGreaterThan(0));
  expect(labels[0]).toMatch(/reading/i);
});

// A listing that fails must not leave the bar saying it is still reading. The
// error is shown above it; the bar has to stop claiming work is in progress.
it("stops saying it is reading when the listing fails", async () => {
  mount([]);
  vi.spyOn(Browse, "Merged").mockRejectedValue(new Error("no such folder") as never);
  const reports: ({ label: string } | null)[] = [];

  render(
    <Browser
      snapshot={snapshot}
      path="/Users/someone"
      onPathChange={() => {}}
      onMount={() => {}}
      onDiff={() => {}}
      onStatus={() => {}}
      onProgress={(p) => reports.push(p)}
    />,
  );

  await waitFor(() => expect(reports[reports.length - 1]).toBeNull());
});
