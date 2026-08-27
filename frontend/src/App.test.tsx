import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { Snapshots, Status, Config, Browse, Schedule, Diff } from "./api";
import i18n from "./i18n";

// The shell: the snapshot list, which one is selected, and the actions that act
// on the machine rather than on a snapshot.
//
// Worth covering because it is where a snapshot is opened, and opening one is the
// only step that needs a password. A button that silently does nothing here reads
// as macOS refusing rather than as the application failing.

// Every row carries the volume it is on and its identifier there. The same date
// exists on every volume mounted when it was taken, so a row is one copy of a
// date rather than the date, and deleting it must say which copy it means.
const snapshots = [
  { name: "snap-a", stamp: "2026-08-20-120000", taken: "2026-08-20T12:00:00Z", mounted: true, mountPoint: "/tmp/a", device: "disk3s1", uuid: "AAAAAAAA-0000-0000-0000-000000000001" },
  { name: "snap-b", stamp: "2026-08-18-120000", taken: "2026-08-18T12:00:00Z", mounted: false, mountPoint: "", device: "disk3s1", uuid: "AAAAAAAA-0000-0000-0000-000000000002" },
];

function overview(over: Record<string, unknown> = {}) {
  return {
    snapshots,
    // One volume unless a case says otherwise, which is what a Mac with nothing
    // plugged in looks like: the grouping is then invisible, as it should be.
    volumes: [
      { name: "Macintosh HD", mountPoint: "/System/Volumes/Data", device: "disk3s1", isStartupDisk: true, snapshots, freeBytes: 400, totalBytes: 1000 },
    ],
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
    // Exactly the snapshots given, in the lists that hold them — found by their
    // own element rather than by role, since the screen has more than one list.
    //
    // The rows are waited for, not the list. The list is rendered before the
    // overview arrives — empty, and once per volume — so waiting for the element
    // and counting afterwards is a race that passes or fails on how much work
    // happens to sit between the two.
    await waitFor(() => {
      expect(document.querySelectorAll("ul.snapshot-list li").length).toBe(2);
    });
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
    await waitFor(() => expect(closed).toHaveBeenCalledWith("disk3s1", ["snap-a"]));
    expect(screen.queryByRole("button", { name: /browse/i })).toBeNull();
  });

  it("opens every closed snapshot at once, and only those", async () => {
    stub();
    const opened = vi.spyOn(Snapshots, "Mount").mockResolvedValue(undefined as never);

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: /^open all/i }));

    // snap-a is already open. Asking for it again is a second authorization
    // prompt for no gain. Named by volume too, since a date alone would not say
    // which disk's copy of it to attach.
    await waitFor(() => expect(opened).toHaveBeenCalledWith("disk3s1", ["snap-b"]));
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

describe("comparing a file from the browser", () => {
  it("opens the comparison over the listing, and closes it again", async () => {
    stub();
    vi.spyOn(Browse, "Merged").mockResolvedValue({
      rows: [{
        relPath: "notes.md",
        absLive: "/Users/someone/notes.md",
        absSnapshot: "/tmp/a/Users/someone/notes.md",
        isDir: false,
        status: "modified",
        snapSize: 10,
        liveSize: 12,
        snapModTime: "2026-08-20T11:00:00Z",
        liveModTime: "2026-08-20T11:30:00Z",
      }],
      note: "",
    } as never);
    vi.spyOn(Diff, "FileVersions").mockResolvedValue({
      kind: "text", readable: true, left: "before", right: "after",
      rightLabel: "Live disk", leftExists: true, rightExists: true,
      leftSize: 10, rightSize: 12, note: "",
    } as never);

    render(<App />);
    await userEvent.click(await snapshotRow());
    await userEvent.click(await screen.findByRole("button", { name: /compare/i }));

    // Over the listing rather than beside it: a comparison wants the width, and
    // it is opened for one file and closed again — by Escape, since the reader's
    // hands are on the keyboard and the thing is a temporary overlay.
    expect(await screen.findByText("before")).toBeTruthy();
    await userEvent.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByText("before")).toBeNull());
    // And the listing is still there underneath, not reloaded from nothing.
    expect(screen.getByText("notes.md")).toBeTruthy();
  });
});

describe("getting back out", () => {
  it("leaves a snapshot for the machine itself", async () => {
    stub();

    render(<App />);
    await userEvent.click(await snapshotRow());
    await screen.findByRole("button", { name: /browse/i });

    // Once a snapshot is selected every tab reads as being about that snapshot,
    // so leaving has to be as explicit as arriving was.
    await userEvent.click(screen.getByRole("button", { name: /home/i }));

    await waitFor(() => expect(screen.queryByRole("button", { name: /browse/i })).toBeNull());
  });

  it("closes every open snapshot", async () => {
    stub();
    const closed = vi.spyOn(Snapshots, "UnmountAll").mockResolvedValue(undefined as never);

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: /close all/i }));

    await waitFor(() => expect(closed).toHaveBeenCalled());
  });

  it("dismisses a status line when it is clicked", async () => {
    stub();
    vi.spyOn(Snapshots, "TakeNow").mockResolvedValue({} as never);

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: /take a snapshot now/i }));
    const said = await screen.findByText(/snapshot/i, { selector: ".banner.ok" });

    await userEvent.click(said);

    // Dismissible because it stays until something else happens: a line reporting
    // a snapshot taken ten minutes ago is no longer news.
    await waitFor(() => expect(document.querySelector(".banner.ok")).toBeNull());
  });
});

describe("when the machine cannot be read", () => {
  it("says why instead of showing an empty list", async () => {
    stub();
    vi.spyOn(Snapshots, "Overview").mockRejectedValue(new Error("the volume is not mounted"));

    render(<App />);

    expect(await screen.findByText(/the volume is not mounted/)).toBeTruthy();
  });

  it("shows no disk gauge when the size is unknown", async () => {
    stub({ volumeTotalBytes: 0, volumeFreeBytes: 0 });

    render(<App />);
    await waitFor(() => expect(document.querySelector("ul.snapshot-list")).not.toBeNull());

    // A gauge of an unknown total would draw a bar at some arbitrary fraction,
    // which states a fact nobody knows.
    expect(document.querySelector(".disk-text")).toBeNull();
  });
});

// The bug this guards: the phrase was looked up in the catalogue, so the check
// compared a German phrase against an English error and never matched. Every
// language but English lost the instructions and the settings button, and got the
// raw refusal instead — with nothing to do about it.
describe("the refusal is recognised whatever the window speaks", () => {
  for (const language of ["de", "es", "fr"]) {
    it(`shows the instructions in ${language}`, async () => {
      stub();
      vi.spyOn(Snapshots, "Mount").mockRejectedValue(
        new Error("macOS refused to mount the snapshot. Mounting needs Full Disk Access as well as an administrator password"),
      );
      vi.spyOn(Status, "MountHelp").mockResolvedValue("Wie man den Zugriff erteilt." as never);
      await i18n.changeLanguage(language);

      render(<App />);
      // By position, not by the word on it: the button says Öffnen, Abrir or
      // Ouvrir here. snap-b is the closed one, and Open is the first of its two
      // actions — Delete is the other.
      await userEvent.click(within(await snapshotRow(1)).getAllByRole("button")[0]);

      expect(await screen.findByText("Wie man den Zugriff erteilt.")).toBeTruthy();
      // The raw English refusal is not what is shown, which is the whole point of
      // recognising it.
      expect(screen.queryByText(/administrator password/)).toBeNull();
    });
  }

  afterEach(async () => {
    await i18n.changeLanguage("en");
  });
});

// Retention deletes on a schedule. Nothing deleted one now, so somebody looking
// at a low-space warning had no lever at all — and the service could do it the
// whole time.
describe("deleting a snapshot", () => {
  it("asks before doing it", async () => {
    stub();
    const deleted = vi.spyOn(Snapshots, "Delete").mockResolvedValue(undefined as never);

    render(<App />);
    // The closed one: an open snapshot is attached to the filesystem and the
    // service refuses to delete it.
    const row = within(await snapshotRow(1));
    await userEvent.click(row.getByRole("button", { name: /delete/i }));

    // Nothing has happened yet. A snapshot cannot be recreated — it records a
    // state of the disk that has passed — so one press must not be enough.
    expect(deleted).not.toHaveBeenCalled();
    expect(row.getByRole("button", { name: /for good/i })).toBeTruthy();
  });

  it("deletes it by stamp once confirmed", async () => {
    stub();
    const deleted = vi.spyOn(Snapshots, "Delete").mockResolvedValue(undefined as never);

    render(<App />);
    const row = within(await snapshotRow(1));
    await userEvent.click(row.getByRole("button", { name: /delete/i }));
    await userEvent.click(row.getByRole("button", { name: /for good/i }));

    // The volume and the identifier on it, then the stamp. Deleting by stamp
    // alone removes the date from every volume holding it, which would take an
    // external disk's snapshot of the same moment along with this one — silently,
    // because until now that snapshot was not on screen at all.
    await waitFor(() =>
      expect(deleted).toHaveBeenCalledWith("disk3s1", "AAAAAAAA-0000-0000-0000-000000000002", "2026-08-18-120000"),
    );
  });

  it("puts the question away when the answer is no", async () => {
    stub();
    const deleted = vi.spyOn(Snapshots, "Delete").mockResolvedValue(undefined as never);

    render(<App />);
    const row = within(await snapshotRow(1));
    await userEvent.click(row.getByRole("button", { name: /delete/i }));
    await userEvent.click(row.getByRole("button", { name: /keep/i }));

    expect(deleted).not.toHaveBeenCalled();
    expect(row.getByRole("button", { name: /delete/i })).toBeTruthy();
    expect(row.queryByRole("button", { name: /for good/i })).toBeNull();
  });

  it("will not delete a snapshot that is open, and says why", async () => {
    stub();

    render(<App />);
    // snap-a is mounted. Offered but refused, rather than hidden: a missing
    // button leaves the reader wondering whether deleting is possible at all.
    const open = within(await snapshotRow(0)).getByRole("button", { name: /delete/i });

    expect(open).toBeDisabled();
    expect(open.getAttribute("title")).toMatch(/close/i);
  });

  it("asks about one snapshot at a time", async () => {
    stub();
    vi.spyOn(Snapshots, "Delete").mockResolvedValue(undefined as never);
    // Both closed, so both offer to be deleted.
    vi.spyOn(Snapshots, "Overview").mockResolvedValue(
      overview({ snapshots: snapshots.map((s) => ({ ...s, mounted: false })) }) as never,
    );

    render(<App />);
    await userEvent.click(within(await snapshotRow(0)).getByRole("button", { name: /delete/i }));
    await userEvent.click(within(await snapshotRow(1)).getByRole("button", { name: /delete/i }));

    // Two open questions with the same wording, one row apart, is how the wrong
    // one gets answered.
    expect(screen.getAllByRole("button", { name: /for good/i })).toHaveLength(1);
  });

  it("leaves the snapshot it was showing", async () => {
    stub();
    vi.spyOn(Snapshots, "Delete").mockResolvedValue(undefined as never);

    render(<App />);
    await userEvent.click(await snapshotRow(1));
    await screen.findByRole("button", { name: /browse/i });

    const row = within(await snapshotRow(1));
    await userEvent.click(row.getByRole("button", { name: /delete/i }));
    await userEvent.click(row.getByRole("button", { name: /for good/i }));

    // Otherwise the browser sits empty under a heading naming something that is
    // gone, which reads as a snapshot that captured nothing.
    await waitFor(() => expect(screen.queryByRole("button", { name: /browse/i })).toBeNull());
  });

  it("says why a deletion failed", async () => {
    stub();
    vi.spyOn(Snapshots, "Delete").mockRejectedValue(new Error("the snapshot is in use"));

    render(<App />);
    const row = within(await snapshotRow(1));
    await userEvent.click(row.getByRole("button", { name: /delete/i }));
    await userEvent.click(row.getByRole("button", { name: /for good/i }));

    expect(await screen.findByText(/in use/)).toBeTruthy();
  });
});

// Snapshots grouped by the disk they are on.
//
// `tmutil localsnapshot` takes no arguments and writes to every mounted APFS
// volume at once, so a flat list showed one volume's copies and gave no sign the
// others existed. There was no way to see the snapshots on an external disk at
// all — including, on the machine that found this, the one holding the source of
// this application.
describe("snapshots grouped by volume", () => {
  const external = [
    { name: "snap-x", stamp: "2026-08-19-090000", taken: "2026-08-19T09:00:00Z", mounted: false, mountPoint: "", device: "disk8s1", uuid: "BBBBBBBB-0000-0000-0000-000000000001" },
  ];
  const twoVolumes = {
    volumes: [
      { name: "Macintosh HD", mountPoint: "/System/Volumes/Data", device: "disk3s1", isStartupDisk: true, snapshots, freeBytes: 400, totalBytes: 1000 },
      { name: "sdcard256gb", mountPoint: "/Volumes/sdcard256gb", device: "disk8s1", isStartupDisk: false, snapshots: external, freeBytes: 20, totalBytes: 1000 },
    ],
  };

  it("shows every volume's snapshots, not just the startup disk's", async () => {
    stub(twoVolumes);
    render(<App />);

    await waitFor(() => {
      expect(document.querySelectorAll("ul.snapshot-list li").length).toBe(3);
    });
    expect(screen.getByText("sdcard256gb")).toBeTruthy();
    expect(screen.getByText("Macintosh HD")).toBeTruthy();
  });

  // The heading is the disk's name. Without it the rows are dates with no
  // indication of which machine part they belong to, which is worse than the flat
  // list it replaced: it looks like duplicates.
  it("heads each group with the name of the disk", async () => {
    stub(twoVolumes);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const heads = [...document.querySelectorAll(".volume-name")].map((n) => n.textContent);
    expect(heads).toEqual(["Macintosh HD", "sdcard256gb"]);
  });

  // One disk needs no label saying which disk. Every Mac would otherwise look as
  // though it had something to disambiguate.
  it("says nothing about volumes when there is only one", async () => {
    stub();
    render(<App />);

    await waitFor(() => {
      expect(document.querySelectorAll("ul.snapshot-list li").length).toBe(2);
    });
    expect(document.querySelectorAll(".volume-head").length).toBe(0);
  });

  // Opening works on every volume now that the privileged helper builds its
  // allowlist from the machine rather than from a constant.
  it("opens a snapshot on a volume that is not the startup disk", async () => {
    stub(twoVolumes);
    const mounted = vi.spyOn(Snapshots, "Mount").mockResolvedValue(undefined as never);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const group = within(document.querySelectorAll(".volume-group")[1] as HTMLElement);
    await userEvent.click(group.getByRole("button", { name: /^open$/i }));

    // Named by volume as well as by snapshot: a date is not an identity, and the
    // data volume has one of the same date that must not be attached instead.
    await waitFor(() => expect(mounted).toHaveBeenCalledWith("disk8s1", ["snap-x"]));
  });

  it("closes a snapshot on the volume it was opened on", async () => {
    const open = [{ name: "snap-x", stamp: "2026-08-19-090000", taken: "2026-08-19T09:00:00Z", mounted: true, mountPoint: "/tmp/x", device: "disk8s1", uuid: "BBBBBBBB-0000-0000-0000-000000000001" }];
    stub({
      volumes: [
        { name: "Macintosh HD", mountPoint: "/System/Volumes/Data", device: "disk3s1", isStartupDisk: true, snapshots, freeBytes: 400, totalBytes: 1000 },
        { name: "sdcard256gb", mountPoint: "/Volumes/sdcard256gb", device: "disk8s1", isStartupDisk: false, snapshots: open, freeBytes: 20, totalBytes: 1000 },
      ],
    });
    const unmounted = vi.spyOn(Snapshots, "Unmount").mockResolvedValue(undefined as never);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const group = within(document.querySelectorAll(".volume-group")[1] as HTMLElement);
    await userEvent.click(group.getByRole("button", { name: /^close$/i }));

    await waitFor(() => expect(unmounted).toHaveBeenCalledWith("disk8s1", ["snap-x"]));
  });

  // Opening everything is one call per volume: each raises its own authorization
  // prompt and each attaches into its own directory.
  it("opens everything one volume at a time", async () => {
    stub(twoVolumes);
    const mounted = vi.spyOn(Snapshots, "Mount").mockResolvedValue(undefined as never);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    await userEvent.click(screen.getByRole("button", { name: /open all|open every/i }));

    await waitFor(() => expect(mounted).toHaveBeenCalledWith("disk3s1", ["snap-b"]));
    expect(mounted).toHaveBeenCalledWith("disk8s1", ["snap-x"]);
  });

  // The row is one copy of a date, not the date. Deleting it must name the volume
  // it is on, or tmutil removes that date from every volume holding it — taking
  // snapshots that were never on screen.
  it("deletes only the copy on the volume whose row was pressed", async () => {
    stub(twoVolumes);
    const deleted = vi.spyOn(Snapshots, "Delete").mockResolvedValue(undefined as never);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const group = within(document.querySelectorAll(".volume-group")[1] as HTMLElement);
    await userEvent.click(group.getByRole("button", { name: /delete/i }));
    await userEvent.click(group.getByRole("button", { name: /for good/i }));

    await waitFor(() =>
      expect(deleted).toHaveBeenCalledWith("disk8s1", "BBBBBBBB-0000-0000-0000-000000000001", "2026-08-19-090000"),
    );
  });

  // Confirming is per copy too. The same date on two volumes is two rows, and
  // arming one must not arm the other — a second click would then delete a
  // snapshot the user never looked at.
  it("arms only the row that was pressed", async () => {
    const sameDate = [{ ...snapshots[1], device: "disk8s1", uuid: "CCCCCCCC-0000-0000-0000-000000000001" }];
    stub({
      volumes: [
        { name: "Macintosh HD", mountPoint: "/System/Volumes/Data", device: "disk3s1", isStartupDisk: true, snapshots, freeBytes: 400, totalBytes: 1000 },
        { name: "sdcard256gb", mountPoint: "/Volumes/sdcard256gb", device: "disk8s1", isStartupDisk: false, snapshots: sameDate, freeBytes: 20, totalBytes: 1000 },
      ],
    });
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const externalGroup = within(document.querySelectorAll(".volume-group")[1] as HTMLElement);
    await userEvent.click(externalGroup.getByRole("button", { name: /delete/i }));

    // Exactly one row is asking, though both hold the same date.
    expect(screen.getAllByRole("button", { name: /for good/i }).length).toBe(1);
  });

  // A service that cannot enumerate the volumes still has the startup disk's
  // list, and showing that is better than showing nothing.
  it("falls back to a flat list when no grouping arrives", async () => {
    stub({ volumes: [] });
    render(<App />);

    await waitFor(() => {
      expect(document.querySelectorAll("ul.snapshot-list li").length).toBe(2);
    });
    expect(document.querySelectorAll(".volume-head").length).toBe(0);
  });
});

// The sidebar's shape, which is load-bearing and not decorative.
//
// The column does not scroll; one region inside it does, and the footer is held
// below that region by margin-top:auto. Grouping the list broke this by putting a
// wrapper between the column and the scrolling element — every group then sized
// to its own content, the column grew to fit them all, and the footer was pushed
// past the sidebar's overflow:hidden where it could not be reached at all.
//
// jsdom has no layout engine, so this asserts the structure that layout depends
// on rather than the pixels: one scrolling region, every group inside it, and the
// controls outside it.
describe("the sidebar's scrolling region", () => {
  const twoVolumes = {
    volumes: [
      { name: "Macintosh HD", mountPoint: "/System/Volumes/Data", device: "disk3s1", isStartupDisk: true, snapshots, freeBytes: 400, totalBytes: 1000 },
      { name: "sdcard256gb", mountPoint: "/Volumes/sdcard256gb", device: "disk8s1", isStartupDisk: false, snapshots: [{ name: "snap-x", stamp: "2026-08-19-090000", taken: "2026-08-19T09:00:00Z", mounted: false, mountPoint: "", device: "disk8s1", uuid: "BBBBBBBB-0000-0000-0000-000000000001" }], freeBytes: 20, totalBytes: 1000 },
    ],
  };

  it("holds every volume's group in one scrolling region", async () => {
    stub(twoVolumes);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));

    // One, or the sidebar has two things competing to be the part that shrinks.
    expect(document.querySelectorAll(".snapshot-scroll").length).toBe(1);
    const scroll = document.querySelector(".snapshot-scroll")!;
    for (const group of document.querySelectorAll(".volume-group")) {
      expect(scroll.contains(group)).toBe(true);
    }
  });

  // The footer is what goes missing when this is wrong, and it holds the controls
  // that act on the machine — the ones that must stay reachable however long the
  // list gets.
  it("keeps the footer outside the scrolling region", async () => {
    stub(twoVolumes);
    render(<App />);

    await waitFor(() => expect(document.querySelector(".aside-footer")).not.toBeNull());
    const scroll = document.querySelector(".snapshot-scroll")!;
    const footer = document.querySelector(".aside-footer")!;
    expect(scroll.contains(footer)).toBe(false);
    // And it is a later sibling, so margin-top:auto pins it to the bottom rather
    // than floating it above the list.
    expect(scroll.compareDocumentPosition(footer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  // Open all and Close all act on the whole machine too, so they scroll away with
  // the list only if they are inside the region. They are not.
  it("keeps the whole-machine buttons outside the scrolling region", async () => {
    stub(twoVolumes);
    render(<App />);

    const openAll = await screen.findByRole("button", { name: /open all|open every/i });
    expect(document.querySelector(".snapshot-scroll")!.contains(openAll)).toBe(false);
  });
});

// Selecting a snapshot on another volume, which is the whole point of listing
// them.
//
// They mounted and could not be opened: the row was not selectable, because the
// browser roots at a home directory and another volume has none. So a snapshot
// could be attached and then not looked at, which is most of the way to not
// working.
describe("browsing a snapshot on another volume", () => {
  const external = [
    { name: "snap-x", stamp: "2026-08-19-090000", taken: "2026-08-19T09:00:00Z", mounted: true, mountPoint: "/tmp/x", device: "disk8s1", uuid: "BBBBBBBB-0000-0000-0000-000000000001" },
  ];
  const twoVolumes = {
    volumes: [
      { name: "Macintosh HD", mountPoint: "/System/Volumes/Data", device: "disk3s1", isStartupDisk: true, snapshots, freeBytes: 400, totalBytes: 1000 },
      { name: "sdcard256gb", mountPoint: "/Volumes/sdcard256gb", device: "disk8s1", isStartupDisk: false, snapshots: external, freeBytes: 20, totalBytes: 1000 },
    ],
  };

  it("asks where browsing starts on the volume that was selected", async () => {
    stub(twoVolumes);
    const home = vi.spyOn(Browse, "Home").mockResolvedValue("/Volumes/sdcard256gb" as never);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const group = within(document.querySelectorAll(".volume-group")[1] as HTMLElement);
    await userEvent.click(group.getByText(/2026|Aug/));

    // The volume's own root, not a home directory: another disk has none, and
    // starting at one would open an empty listing that reads as an empty snapshot.
    await waitFor(() => expect(home).toHaveBeenCalledWith("disk8s1"));
  });

  // Every row is selectable now. One that is not reads as broken, since it is
  // sitting in a list beside rows that are.
  it("selects a row on any volume", async () => {
    stub(twoVolumes);
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const group = document.querySelectorAll(".volume-group")[1] as HTMLElement;
    await userEvent.click(within(group).getByText(/2026|Aug/));

    await waitFor(() => expect(group.querySelector("li.selected")).not.toBeNull());
  });

  // The same date exists on both volumes and both can be open at once, so
  // selecting one must not light up the other.
  it("selects one copy of a date, not every copy", async () => {
    const sameDate = [{ ...snapshots[0], device: "disk8s1", uuid: "CCCCCCCC-0000-0000-0000-000000000001" }];
    stub({
      volumes: [
        { name: "Macintosh HD", mountPoint: "/System/Volumes/Data", device: "disk3s1", isStartupDisk: true, snapshots, freeBytes: 400, totalBytes: 1000 },
        { name: "sdcard256gb", mountPoint: "/Volumes/sdcard256gb", device: "disk8s1", isStartupDisk: false, snapshots: sameDate, freeBytes: 20, totalBytes: 1000 },
      ],
    });
    render(<App />);

    await waitFor(() => expect(document.querySelectorAll(".volume-group").length).toBe(2));
    const group = document.querySelectorAll(".volume-group")[1] as HTMLElement;
    await userEvent.click(within(group).getAllByText(/2026|Aug/)[0]);

    await waitFor(() => expect(document.querySelectorAll("li.selected").length).toBe(1));
  });
});
