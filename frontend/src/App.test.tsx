import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { Snapshots, Status, Config, Browse, Schedule } from "./api";
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

/** The nth snapshot row, once the list has arrived.
 *
 *  By position in the list rather than by the date it shows: the row renders a
 *  localised timestamp and a relative age, and asserting on either would make
 *  these fail when the clock moves rather than when the list breaks. */
async function snapshotRow(n = 0) {
  return await waitFor(() => {
    const rows = document.querySelectorAll("ul.snapshot-list li");
    expect(rows.length).toBeGreaterThan(n);
    return rows[n] as HTMLElement;
  });
}

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

// The one failure that needs a different answer from every other: macOS refusing
// the mount for want of Full Disk Access. It is not a fault to report — nothing
// this application can do will fix it — so the banner carries the instructions
// and the way to the settings pane instead of an error message.
describe("when macOS refuses", () => {
  it("explains what to grant and offers the settings pane", async () => {
    stub();
    vi.spyOn(Snapshots, "Mount").mockRejectedValue(
      new Error("mounting was refused: grant Full Disk Access to snapshotter"),
    );
    vi.spyOn(Status, "MountHelp").mockResolvedValue("Grant Full Disk Access in System Settings." as never);
    const settings = vi.spyOn(Status, "OpenPrivacySettings").mockResolvedValue(undefined as never);

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: /^open$/i }));

    expect(await screen.findByText(/Grant Full Disk Access in System Settings/)).toBeTruthy();
    // The raw error is not shown alongside it: two messages read as two problems,
    // and the one from Go says nothing the instructions do not.
    expect(screen.queryByText(/mounting was refused/)).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: /settings/i }));
    expect(settings).toHaveBeenCalled();
  });

  it("reports an ordinary failure as itself", async () => {
    stub();
    vi.spyOn(Snapshots, "Mount").mockRejectedValue(new Error("the snapshot has gone"));

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: /^open$/i }));

    expect(await screen.findByText(/the snapshot has gone/)).toBeTruthy();
  });
});

describe("choosing a snapshot", () => {
  it("shows what is in it as soon as it is picked", async () => {
    stub();

    render(<App />);
    // Clicking the row, not a button in it: picking a snapshot from the list is
    // how the browser is reached, and it was the only route never exercised.
    await userEvent.click(await snapshotRow());

    expect(await screen.findByRole("button", { name: /browse/i })).toBeTruthy();
  });

  it("closes a snapshot without also selecting it", async () => {
    stub();
    const closed = vi.spyOn(Snapshots, "Unmount").mockResolvedValue(undefined as never);

    render(<App />);
    // Scoped to the row: the screen has more than one Close, and the one that
    // matters is the one sitting inside the snapshot it closes.
    await userEvent.click(within(await snapshotRow()).getByRole("button", { name: /close/i }));

    // The button stops the click reaching the row. Without that, closing a
    // snapshot also navigated into the one just closed.
    await waitFor(() => expect(closed).toHaveBeenCalledWith(["snap-a"]));
    expect(screen.queryByRole("button", { name: /browse/i })).toBeNull();
  });

  it("opens every closed snapshot at once, and only those", async () => {
    stub();
    const opened = vi.spyOn(Snapshots, "Mount").mockResolvedValue(undefined as never);

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: /^open all/i }));

    // snap-a is already open. Asking for it again is a second authorization
    // prompt for no gain.
    await waitFor(() => expect(opened).toHaveBeenCalledWith(["snap-b"]));
  });
});

describe("the options panel", () => {
  it("returns to where it was opened from", async () => {
    stub();
    vi.spyOn(Schedule, "Status").mockResolvedValue({
      installed: true, running: true, intervalHours: 3, retentionDays: 14, retentionPolicy: "",
    } as never);
    vi.spyOn(Schedule, "Policies").mockResolvedValue([] as never);

    render(<App />);
    await userEvent.click(await snapshotRow());
    await screen.findByRole("button", { name: /browse/i });

    const options = screen.getByRole("button", { name: /options/i });
    await userEvent.click(options);
    // Pressing it again is how it is closed, and closing it has to land back on
    // the snapshot rather than on the home screen — which is what happened when
    // the previous view was not remembered.
    await userEvent.click(options);

    expect(await screen.findByRole("button", { name: /browse/i })).toBeTruthy();
  });
});

describe("searching", () => {
  it("switches between browsing and searching within a snapshot", async () => {
    stub();

    render(<App />);
    await userEvent.click(await snapshotRow());
    await userEvent.click(await screen.findByRole("button", { name: /search/i }));

    // By its placeholder: the field carries no label, and the panel it sits in is
    // the only thing on screen that asks for a name.
    expect(await screen.findByPlaceholderText(/name/i)).toBeTruthy();
  });
});
