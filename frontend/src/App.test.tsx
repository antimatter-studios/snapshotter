import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { Snapshots, Status, Config, Browse } from "./api";
import "./i18n";

// The shell: the snapshot list, which one is selected, and the actions that act
// on the machine rather than on a snapshot.
//
// Worth covering because it is where a snapshot is opened, and opening one is the
// only step that needs a password. A button that silently does nothing here reads
// as macOS refusing rather than as the application failing.

const snapshots = [
  { name: "snap-a", stamp: "2026-08-20-120000", taken: "2026-08-20T12:00:00Z", mounted: true, mountPoint: "/tmp/a" },
  { name: "snap-b", stamp: "2026-08-18-120000", taken: "2026-08-18T12:00:00Z", mounted: false, mountPoint: "" },
];

function overview(over: Record<string, unknown> = {}) {
  return {
    snapshots,
    volumeTotalBytes: 1000,
    volumeFreeBytes: 400,
    timeMachineWarning: "",
    ...over,
  };
}

function stub(over: Record<string, unknown> = {}) {
  vi.spyOn(Snapshots, "Overview").mockResolvedValue(overview(over) as never);
  vi.spyOn(Status, "Check").mockResolvedValue({
    level: "ok", headline: "You are covered", findings: [],
    snapshotCount: 2, coverageHours: 48, scheduleInstalled: true, scheduleRunning: true,
    intervalHours: 3, retentionDays: 14, volumeTotalBytes: 1000, volumeFreeBytes: 400,
    freePercent: 40, purgeableBytes: 0, pinningContainer: false,
    tripwireInstalled: true, tripwireRunning: true, version: "0.53.0",
  } as never);
  vi.spyOn(Status, "RecentWarnings").mockResolvedValue([] as never);
  vi.spyOn(Config, "Get").mockResolvedValue({ config: { appearance: { theme: "system", language: "en" }, tripwire: { ignore: [] } } } as never);
  vi.spyOn(Browse, "Merged").mockResolvedValue({ rows: [], note: "" } as never);
}

afterEach(() => vi.restoreAllMocks());

describe("the application shell", () => {
  it("lists every snapshot on the machine", async () => {
    stub();
    render(<App />);

    // By count rather than by the text of a date: the row shows a formatted,
    // localised timestamp, and asserting on its exact wording would make this
    // test fail whenever the format improves rather than when the list breaks.
    // Exactly the snapshots given, in the list that holds them — found by its
    // own element rather than by role, since the screen has more than one list.
    const list = await waitFor(() => {
      const found = document.querySelector("ul.snapshot-list");
      expect(found).not.toBeNull();
      return found!;
    });
    expect(list.querySelectorAll("li").length).toBe(2);
  });

  // Opening attaches the snapshot read-only, which macOS requires a password for.
  // The button has to reach the service or it looks like macOS refused.
  it("opens an unmounted snapshot", async () => {
    stub();
    const mount = vi.spyOn(Snapshots, "Mount").mockResolvedValue(undefined as never);
    render(<App />);

    const open = await screen.findByRole("button", { name: /^open$/i });
    await userEvent.click(open);

    await waitFor(() => expect(mount).toHaveBeenCalled());
  });

  // A machine with none is the state this application exists to change, so it
  // must say so and offer the way out rather than showing an empty list.
  it("says what to do when there are no snapshots", async () => {
    stub({ snapshots: [] });
    render(<App />);

    expect(await screen.findByText(/none yet/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: /take a snapshot now/i })).toBeTruthy();
  });

  it("takes a snapshot when asked", async () => {
    stub({ snapshots: [] });
    const take = vi.spyOn(Snapshots, "TakeNow").mockResolvedValue({} as never);
    render(<App />);

    await userEvent.click(await screen.findByRole("button", { name: /take a snapshot now/i }));

    await waitFor(() => expect(take).toHaveBeenCalledOnce());
  });

  // Free space is shown against the total, because "400 MB free" means nothing
  // without knowing whether that is of 500 or of 5000.
  it("shows the free space in the header", async () => {
    stub();
    render(<App />);

    await waitFor(() => expect(document.querySelector("ul.snapshot-list")).not.toBeNull());

    // The bar itself is decoration; the figures live in its title, which is where
    // "400 B free of 1000 B" can be read. A bar with no numbers says a proportion
    // and never says of what.
    const disk = document.querySelector("[title*='free of']");
    expect(disk).not.toBeNull();
    expect(disk!.getAttribute("title")).toMatch(/free of/);
  });

  // Time Machine thins local snapshots to roughly a day whatever retention says,
  // so a configured destination has to be said out loud.
  it("warns when Time Machine will thin the history", async () => {
    stub({ timeMachineWarning: "Time Machine has a backup destination configured" });
    render(<App />);

    expect(await screen.findByText(/backup destination configured/i)).toBeTruthy();
  });
});
