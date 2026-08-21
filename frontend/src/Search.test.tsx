import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Search } from "./Search";
import { Search as SearchAPI, Restore } from "./api";
import "./i18n";

// Finding a file across every open snapshot.
//
// The thing worth pinning is what it says when it found nothing, because there
// are two quite different nothings: no snapshot was open so nothing was searched,
// and snapshots were searched and the file is not in them. Collapsing those into
// one message sends somebody looking for a file that was never looked for.

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

    render(<Search onStatus={() => {}} />);
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

    render(<Search onStatus={() => {}} />);
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

    render(<Search onStatus={() => {}} />);
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
    render(<Search onStatus={() => {}} />);

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

    render(<Search onStatus={said} />);
    await userEvent.type(screen.getByPlaceholderText(/file name/i), "id_rsa");
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }));
    await userEvent.click(await screen.findByRole("button", { name: /restore/i }));

    await waitFor(() => expect(restore).toHaveBeenCalledOnce());
    await waitFor(() => expect(said).toHaveBeenCalledWith(expect.stringContaining("id_rsa.restored")));
  });
});
