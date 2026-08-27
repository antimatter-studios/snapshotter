import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Search } from "./Search";
import { Search as SearchAPI, Restore, type SnapshotView } from "./api";
import "./i18n";

// Finding a file across every open snapshot.
//
// The thing worth pinning is what it says when it found nothing, because there
// are two quite different nothings: no snapshot was open so nothing was searched,
// and snapshots were searched and the file is not in them. Collapsing those into
// one message sends somebody looking for a file that was never looked for.

const snapshot = {
  name: "com.apple.TimeMachine.2026-08-20-120000.local",
  stamp: "2026-08-20-120000",
  // The startup disk, which is what an empty volume means. The real service
  // always sends one, so a fixture without it tests a shape that never occurs.
  device: "",
  taken: "2026-08-20T12:00:00Z",
  mounted: true,
  mountPoint: "/tmp/mnt",
} as SnapshotView;

afterEach(() => vi.restoreAllMocks());

describe("searching the snapshots", () => {
  it("searches for what was typed, under where it was told", async () => {
    const search = vi.spyOn(SearchAPI, "Search").mockResolvedValue({
      term: "id_rsa",
      hits: [],
      searched: ["snap-a"],
      skipped: [],
      note: "",
    } as never);

    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);
    await userEvent.type(screen.getByPlaceholderText(/file name/i), "id_rsa");
    await userEvent.type(screen.getByPlaceholderText(/only under/i), "/Users/someone");
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }));

    await waitFor(() => expect(search).toHaveBeenCalledOnce());
    expect(search).toHaveBeenCalledWith("id_rsa", "/Users/someone");
  });

  // Nothing to search is not the same as nothing found, and the service says
  // which by naming the snapshots it skipped.
  it("distinguishes nothing searched from nothing found", async () => {
    vi.spyOn(SearchAPI, "Search").mockResolvedValue({
      term: "vault",
      hits: [],
      searched: [],
      skipped: ["snap-a", "snap-b"],
      note: "No snapshot is open, so nothing was searched. Open one of the 2 snapshots to search inside them.",
    } as never);

    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);
    await userEvent.type(screen.getByPlaceholderText(/file name/i), "vault");
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }));

    expect(await screen.findByText(/nothing was searched/i)).toBeTruthy();
  });

  it("lists what it found, with where it was and which snapshot holds it", async () => {
    vi.spyOn(SearchAPI, "Search").mockResolvedValue({
      term: "id_rsa",
      hits: [
        {
          snapshot: "com.apple.TimeMachine.2026-08-20-120000.local",
          stamp: "2026-08-20-120000",
          path: "/tmp/a/Users/someone/.ssh/id_rsa",
          livePath: "/Users/someone/.ssh/id_rsa",
          name: "id_rsa",
          isDir: false,
          size: 2048,
          modTime: "2026-08-20T11:00:00Z",
        },
      ],
      searched: ["snap-a"],
      skipped: [],
      note: "",
    } as never);

    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);
    await userEvent.type(screen.getByPlaceholderText(/file name/i), "id_rsa");
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }));

    const table = await screen.findByRole("table");
    // The name, and the path it was at — which is what somebody searches by and
    // what tells them whether this is the one they meant.
    expect(table.textContent).toContain("id_rsa");
    expect(table.textContent).toContain("/Users/someone/.ssh/id_rsa");
  });

  // A search cannot start without something to search for, or it walks every
  // snapshot to match everything.
  it("will not search for nothing", () => {
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);

    expect(screen.getByRole("button", { name: /^search$/i })).toBeDisabled();
  });

  it("restores a hit to where it says it restored it", async () => {
    vi.spyOn(SearchAPI, "Search").mockResolvedValue({
      term: "id_rsa",
      hits: [
        {
          snapshot: "com.apple.TimeMachine.2026-08-20-120000.local",
          stamp: "2026-08-20-120000",
          path: "/tmp/a/Users/someone/.ssh/id_rsa",
          livePath: "/Users/someone/.ssh/id_rsa",
          name: "id_rsa",
          isDir: false,
          size: 2048,
          modTime: "2026-08-20T11:00:00Z",
        },
      ],
      searched: ["snap-a"],
      skipped: [],
      note: "",
    } as never);
    const restore = vi.spyOn(Restore, "Restore").mockResolvedValue({ destination: "/Users/someone/.ssh/id_rsa.restored" } as never);
    const said = vi.fn();

    render(<Search onStatus={said} snapshot={snapshot} path="/Users/someone" />);
    await userEvent.type(screen.getByPlaceholderText(/file name/i), "id_rsa");
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }));
    await userEvent.click(await screen.findByRole("button", { name: /restore/i }));

    await waitFor(() => expect(restore).toHaveBeenCalledOnce());
    await waitFor(() => expect(said).toHaveBeenCalledWith(expect.stringContaining("id_rsa.restored")));
  });
});

// The other question, and the one that had no answer here: not "where is the file
// called this", but "what was in this folder that is no longer there". Someone
// who knows what they lost can search by name. Someone who only knows something
// is missing could not ask at all — while the service could answer the whole time.
describe("what has gone since the snapshot", () => {
  function gone(changes: unknown[]) {
    return vi.spyOn(SearchAPI, "DeletedSince").mockResolvedValue({
      changes,
      scanned: 120,
      truncated: false,
    } as never);
  }

  function change(relPath: string, isDir = false) {
    return {
      relPath,
      absLive: "/Users/someone/" + relPath,
      absSnapshot: "/tmp/mnt/Users/someone/" + relPath,
      status: "onlyInSnapshot",
      isDir,
      snapSize: 4096,
      liveSize: 0,
      snapModTime: "2026-08-19T09:00:00Z",
      liveModTime: "0001-01-01T00:00:00Z",
    };
  }

  async function ask() {
    await userEvent.click(screen.getByRole("button", { name: /gone/i }));
    await userEvent.click(screen.getByRole("button", { name: /^look$/i }));
  }

  it("asks about the folder the browser is in", async () => {
    const asked = gone([]);
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone/projects" />);

    await ask();

    // Seeded from where they already are, because that is the folder someone
    // noticing an absence has just been looking at.
    await waitFor(() => expect(asked).toHaveBeenCalledWith(snapshot.device, snapshot.name, "/Users/someone/projects", false));
  });

  it("lists what the folder held and holds no longer", async () => {
    gone([change("notes.md"), change("archive", true)]);
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);

    await ask();

    expect(await screen.findByText("notes.md")).toBeTruthy();
    // A folder reads as one, because losing a folder and losing a file in it are
    // different sizes of problem.
    expect(screen.getByText("archive/")).toBeTruthy();
  });

  it("says nothing is gone rather than showing an empty table", async () => {
    gone([]);
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);

    await ask();

    // The reassuring answer, and it has to be said: an empty table under a
    // heading reads as a question that failed.
    expect(await screen.findByText(/nothing has gone/i)).toBeTruthy();
  });

  it("puts a file back where it was", async () => {
    gone([change("notes.md")]);
    const restored = vi.spyOn(Restore, "Restore").mockResolvedValue({
      destination: "/Users/someone/notes.md",
      backedUp: "",
    } as never);
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);
    await ask();
    await screen.findByText("notes.md");

    await userEvent.click(screen.getByRole("button", { name: /restore/i }));

    // Finding it is half the point. The other half is getting it back without
    // having to go and browse to it.
    await waitFor(() =>
      expect(restored).toHaveBeenCalledWith(
        expect.objectContaining({ snapshot: snapshot.name, livePath: "/Users/someone/notes.md" }),
      ),
    );
  });

  it("compares contents when asked to", async () => {
    const asked = gone([]);
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);

    await userEvent.click(screen.getByRole("button", { name: /gone/i }));
    await userEvent.click(screen.getByRole("checkbox"));
    await userEvent.click(screen.getByRole("button", { name: /^look$/i }));

    await waitFor(() => expect(asked).toHaveBeenCalledWith(snapshot.device, snapshot.name, "/Users/someone", true));
  });

  it("says a snapshot is needed rather than offering to look in none", async () => {
    render(<Search onStatus={() => {}} snapshot={null} path="/Users/someone" />);

    await userEvent.click(screen.getByRole("button", { name: /gone/i }));

    expect(screen.getByText(/pick a snapshot/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: /^look$/i })).toBeDisabled();
  });

  it("says why it could not look", async () => {
    vi.spyOn(SearchAPI, "DeletedSince").mockRejectedValue(new Error("the snapshot is not mounted"));
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);

    await ask();

    expect(await screen.findByText(/not mounted/)).toBeTruthy();
  });

  it("keeps the two questions' answers apart", async () => {
    vi.spyOn(SearchAPI, "Search").mockResolvedValue({
      term: "id_rsa",
      hits: [{ name: "id_rsa", livePath: "/Users/someone/.ssh/id_rsa", snapshot: snapshot.name, stamp: "2026-08-20-120000", modTime: "2026-08-19T09:00:00Z", size: 100, isDir: false }],
      searched: [snapshot.name],
      skipped: [],
      note: "",
      truncated: false,
      incomplete: false,
    } as never);
    gone([change("notes.md")]);
    render(<Search onStatus={() => {}} snapshot={snapshot} path="/Users/someone" />);

    await userEvent.type(screen.getByPlaceholderText(/name/i), "id_rsa");
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }));
    await screen.findByText("id_rsa");

    await ask();

    // One table at a time. Two sets of results stacked under one screen leaves
    // no way to tell which question either of them answered.
    expect(await screen.findByText("notes.md")).toBeTruthy();
    expect(screen.queryByText("id_rsa")).toBeNull();
  });
});
