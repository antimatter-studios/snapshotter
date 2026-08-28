import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Health } from "./Health";
import { Status, Config, Schedule, Snapshots } from "./api";
import "./i18n";

// Config.Get for every test in this file, stubbed whether the test cares or not.
//
// The screens here refresh themselves on a timer, and the interval is a setting —
// so useLiveRefresh reads it with a binding call on mount and on every tick.
// Unstubbed, that is a real HTTP request: the bindings post to a relative URL and
// jsdom resolves it against its own origin, so the test's behaviour depends on
// what happens to be listening on port 80 of whichever machine is running it.
// Docker answers 503 here in seven milliseconds and nothing answers on the build
// machine, which is how one test in this suite failed three runs in five locally
// and how another pair failed continuous integration alone.
//
// Tests that care about the settings still stub it themselves; this only means
// none of them can reach the network by forgetting to.
beforeEach(() => {
  vi.spyOn(Config, "Get").mockResolvedValue({
    config: { appearance: { theme: "system", language: "en" }, tripwire: { ignore: [] } },
  } as never);
});


// Whether this Mac is actually protected, and what to do if not.
//
// The findings carry actions, and an action that does not reach the service is
// a button that looks like it worked. That is the failure worth catching here:
// the screen goes on saying the same thing, and somebody concludes the problem
// cannot be fixed rather than that the button is broken.

// Typed loosely on purpose: the cases below vary one field at a time, and the
// findings array is empty in the base, so its element type would be inferred as
// never and reject every finding added later.
const base: Record<string, unknown> = {
  level: "ok",
  headline: "You are covered",
  findings: [],
  snapshotCount: 21,
  coverageHours: 168,
  scheduleInstalled: true,
  scheduleRunning: true,
  intervalHours: 3,
  retentionDays: 14,
  volumeTotalBytes: 1000,
  volumeFreeBytes: 500,
  freePercent: 50,
  purgeableBytes: 0,
  pinningContainer: false,
  tripwireInstalled: true,
  tripwireRunning: true,
  version: "0.53.0",
};

function check(over: Record<string, unknown> = {}) {
  vi.spyOn(Status, "Check").mockResolvedValue({ ...base, ...over } as never);
  vi.spyOn(Status, "RecentWarnings").mockResolvedValue([] as never);
  vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);
}

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

describe("the health screen", () => {
  it("leads with the verdict", async () => {
    check();
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText("You are covered")).toBeTruthy();
  });

  // Nothing to act on is a statement, not an empty screen. A blank space where
  // findings would be reads as something that failed to load.
  it("says so when there is nothing to act on", async () => {
    check();
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText(/nothing to act on/i)).toBeTruthy();
  });

  it("shows each finding with what it is about", async () => {
    check({
      level: "bad",
      headline: "No snapshots — nothing to roll back to",
      findings: [
        { level: "bad", title: "There are no snapshots", detail: "Nothing can be rolled back to.", kind: "snapshots", action: "take-snapshot" },
      ],
    });
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText("There are no snapshots")).toBeTruthy();
    expect(screen.queryByText(/nothing to act on/i)).toBeNull();
  });

  // The bulk-deletion warnings are the record of the tripwire firing, and the
  // location is the point of them: "something is deleting a lot of files" tells
  // nobody anything they can act on.
  it("names where files were being deleted from", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(base as never);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);
    vi.spyOn(Status, "RecentWarnings").mockResolvedValue([
      {
        at: "2026-08-20T03:10:00Z",
        kind: "bulk-deletion",
        where: ["/Users/someone/projects"],
        labels: ["~/projects"],
        snapshot: "com.apple.TimeMachine.2026-08-20-031000.local",
        note: "",
      },
    ] as never);

    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText("~/projects")).toBeTruthy();
  });

  // Silencing a folder is what stops a browser cache tripping the wire every
  // afternoon, and it has to reach the settings or the warning returns tomorrow.
  it("silences a folder through the settings", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(base as never);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);
    vi.spyOn(Status, "RecentWarnings").mockResolvedValue([
      {
        at: "2026-08-20T03:10:00Z",
        kind: "bulk-deletion",
        where: ["/Users/someone/Library/Caches"],
        labels: ["~/Library/Caches"],
        snapshot: "",
        note: "",
      },
    ] as never);
    const ignored = vi.spyOn(Config, "IgnoreFolder").mockResolvedValue({ config: { tripwire: { ignore: ["/Users/someone/Library/Caches"] } } } as never);

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /ignore/i }));

    await waitFor(() => expect(ignored).toHaveBeenCalledWith("/Users/someone/Library/Caches"));
  });

  // The response failing is the one outcome worth surfacing: the tripwire fired
  // and no snapshot came of it, which is the protection not working.
  it("says when the tripwire fired and took nothing", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(base as never);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);
    vi.spyOn(Status, "RecentWarnings").mockResolvedValue([
      {
        at: "2026-08-20T03:10:00Z",
        kind: "bulk-deletion",
        where: ["/Users/someone/projects"],
        labels: ["~/projects"],
        snapshot: "",
        note: "",
      },
    ] as never);

    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText(/no snapshot was taken in response/i)).toBeTruthy();
  });
});

// Every finding that carries an action gets one of these. The failure they guard
// against is the quiet one: the button reaches nothing, the screen goes on saying
// the same thing, and the reader concludes the problem cannot be fixed rather
// than that the button is broken.
describe("acting on a finding", () => {
  function finding(action: string) {
    return {
      level: "warning",
      title: "Something needs doing",
      detail: "And here is why.",
      action,
    };
  }

  it("takes a snapshot when the finding is that there are none", async () => {
    check({ findings: [finding("take-snapshot")] });
    const taken = vi.spyOn(Snapshots, "TakeNow").mockResolvedValue(undefined as never);

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /now/i }));

    await waitFor(() => expect(taken).toHaveBeenCalled());
  });

  it("installs the schedule with the interval the button names", async () => {
    check({ findings: [finding("install-schedule")], scheduleInstalled: false });
    const installed = vi.spyOn(Schedule, "Install").mockResolvedValue(undefined as never);

    render(<Health onStatus={() => {}} />);
    const button = await screen.findByRole("button", { name: /every/i });
    // What it promises and what it does have to be the same number, or the
    // schedule silently differs from the one that was agreed to.
    const promised = Number(button.textContent?.match(/\d+/)?.[0]);
    await userEvent.click(button);

    await waitFor(() => expect(installed).toHaveBeenCalled());
    expect(installed.mock.calls[0][0]).toBe(promised);
  });

  it("offers to start a schedule that is installed but stopped", async () => {
    check({ findings: [finding("install-schedule")], scheduleInstalled: true, scheduleRunning: false });

    render(<Health onStatus={() => {}} />);

    // Not "take one every 3 hours" — that reads as setting it up again, when what
    // is wrong is that the agent it already has is not running.
    expect(await screen.findByRole("button", { name: /start/i })).toBeTruthy();
  });

  it("installs the tripwire", async () => {
    check({ findings: [finding("install-tripwire")], tripwireInstalled: false });
    const installed = vi.spyOn(Schedule, "InstallTripwire").mockResolvedValue(undefined as never);

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /watch/i }));

    await waitFor(() => expect(installed).toHaveBeenCalled());
  });

  it("shows the log the agent wrote", async () => {
    check({ findings: [finding("show-log")] });
    vi.spyOn(Schedule, "Log").mockResolvedValue("12:00 took snapshot\n15:00 took snapshot" as never);

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /log/i }));

    expect(await screen.findByText(/took snapshot/)).toBeTruthy();
  });

  it("says why the log could not be read rather than showing an empty box", async () => {
    check({ findings: [finding("show-log")] });
    vi.spyOn(Schedule, "Log").mockRejectedValue(new Error("no log file yet"));

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /log/i }));

    expect(await screen.findByText(/no log file yet/)).toBeTruthy();
  });

  // A failed action used to replace the whole screen with its own sentence: the
  // verdict went, the findings went, and the button that failed went with them.
  // The reason has to be readable next to the thing it failed to do.
  it("keeps the findings on screen when an action fails", async () => {
    check({ findings: [finding("take-snapshot")] });
    vi.spyOn(Snapshots, "TakeNow").mockRejectedValue(new Error("authorization was declined"));

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /now/i }));

    expect(await screen.findByText(/declined/)).toBeTruthy();
    expect(screen.getByText("Something needs doing")).toBeTruthy();
    expect(screen.getByText("You are covered")).toBeTruthy();
    // And the action can be tried again, which was the point of saying why.
    expect(screen.getByRole("button", { name: /now/i })).toBeTruthy();
  });
});

describe("watching a folder again", () => {
  // A silenced folder that cannot be un-silenced means the tripwire quietly
  // watches less and less, and nothing ever says so.
  it("removes a folder from the silenced list", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(base as never);
    vi.spyOn(Status, "RecentWarnings").mockResolvedValue([] as never);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: ["/Users/someone/Library/Caches"] } } } as never);
    const watched = vi.spyOn(Config, "StopIgnoringFolder").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /watch again/i }));

    await waitFor(() => expect(watched).toHaveBeenCalledWith("/Users/someone/Library/Caches"));
    // And the row goes, so the list is what the settings now say rather than what
    // they said when the screen opened.
    await waitFor(() => expect(screen.queryByText("/Users/someone/Library/Caches")).toBeNull());
  });

  it("says why the folder could not be watched again", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(base as never);
    vi.spyOn(Status, "RecentWarnings").mockResolvedValue([] as never);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: ["/Users/someone/Library/Caches"] } } } as never);
    vi.spyOn(Config, "StopIgnoringFolder").mockRejectedValue(new Error("the settings file is read-only"));

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /watch again/i }));

    expect(await screen.findByText(/read-only/)).toBeTruthy();
  });
});

describe("when the check itself fails", () => {
  it("shows the reason rather than checking for ever", async () => {
    vi.spyOn(Status, "Check").mockRejectedValue(new Error("the volume could not be read"));

    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText(/volume could not be read/)).toBeTruthy();
    expect(screen.queryByText(/checking/i)).toBeNull();
  });
});

describe("the verdict line", () => {
  it("checks again when asked", async () => {
    check();
    render(<Health onStatus={() => {}} />);
    await screen.findByText("You are covered");

    // Called once on mount; pressing it has to actually ask again, or a fix made
    // elsewhere never shows up here.
    await userEvent.click(screen.getByRole("button", { name: /check/i }));

    await waitFor(() => expect(vi.mocked(Status.Check).mock.calls.length).toBeGreaterThan(1));
  });

  it("says nothing has been recorded rather than showing an empty date", async () => {
    check({ snapshotCount: 0, newest: "" });
    render(<Health onStatus={() => {}} />);

    // Exactly, not by fragment: "nothing" appears elsewhere on a machine with no
    // snapshots, and matching loosely would pass on the wrong sentence.
    expect(await screen.findByText("Nothing has been recorded.")).toBeTruthy();
  });

  it("says when the next one is due, once there is a schedule to be due under", async () => {
    check({ newest: "2026-08-20T12:00:00Z", nextDue: "2026-08-20T18:00:00Z" });
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText(/next/i)).toBeTruthy();
  });

  // How far back the history reaches, which is the number that answers "can I get
  // Tuesday's version back". Its wording changes scale twice, and each scale is a
  // separate plural rule in every language.
  it("gives the depth in days once there is more than two of them", async () => {
    check({ coverageHours: 168 });
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText("7 days")).toBeTruthy();
  });

  it("gives it in hours below two days", async () => {
    check({ coverageHours: 6 });
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText("6 hours")).toBeTruthy();
  });

  it("says under an hour rather than rounding to zero", async () => {
    check({ coverageHours: 0.4 });
    render(<Health onStatus={() => {}} />);

    // "0 hours" of history reads as no history at all, when what is true is that
    // the first snapshot was taken minutes ago.
    expect(await screen.findByText(/under an hour/i)).toBeTruthy();
    expect(screen.queryByText("0 hours")).toBeNull();
  });
});

describe("silencing a folder that will not silence", () => {
  it("says why instead of appearing to have worked", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(base as never);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);
    vi.spyOn(Status, "RecentWarnings").mockResolvedValue([
      {
        at: "2026-08-20T03:10:00Z",
        kind: "bulk-deletion",
        where: ["/Users/someone/Library/Caches"],
        labels: ["~/Library/Caches"],
        snapshot: "",
        note: "",
      },
    ] as never);
    vi.spyOn(Config, "IgnoreFolder").mockRejectedValue(new Error("the settings file is read-only"));

    render(<Health onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /ignore/i }));

    // Otherwise the row stays, the warning returns tomorrow, and nothing ever
    // said the setting was not saved.
    expect(await screen.findByText(/read-only/)).toBeTruthy();
  });
});

// The schedule figure used to be built here from two numbers — "Every 3h, kept
// 14d" — in English, and wrong for every tiered policy: it read the horizon as the
// retention, which is true of a flat window and of nothing else. The words come
// from the service now.
describe("the schedule figure", () => {
  function withSchedule(over: Record<string, unknown>) {
    check({
      scheduleInstalled: true,
      scheduleRunning: true,
      intervalHours: 3,
      retentionDays: 14,
      ...over,
    });
  }

  it("names the retention mode rather than restating the numbers", async () => {
    withSchedule({
      retentionMode: "Tiered — daily, then weekly",
      scheduleHeadline: "Tiered — daily, then weekly: every 3 hours, thinning out to 26 weeks",
    });

    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText(/Tiered — daily, then weekly/)).toBeTruthy();
    // Not the old claim. On a tiered policy only one snapshot every four weeks
    // survives past the twenty-sixth, so "kept 14d" was never true of it.
    expect(screen.queryByText(/kept 14d/)).toBeNull();
  });

  it("carries the whole line where there is room for it", async () => {
    const headline = "Flat window: every 3 hours, kept 14 days";
    withSchedule({ retentionMode: "Flat window", scheduleHeadline: headline });

    render(<Health onStatus={() => {}} />);

    // The grid is four columns wide, so the cell shows the label and the tooltip
    // carries the sentence.
    const cell = await screen.findByText("Flat window");
    expect(cell.getAttribute("title")).toBe(headline);
  });

  it("says a schedule is not running, in the reader's language", async () => {
    withSchedule({
      retentionMode: "Flat window",
      scheduleHeadline: "Flat window: every 3 hours, kept 14 days",
      scheduleRunning: false,
    });

    render(<Health onStatus={() => {}} />);

    // An installed schedule that is not running takes no snapshots, which is the
    // failure this application exists to prevent — so the cell must not read as
    // though it were working.
    expect(await screen.findByText(/not running/i)).toBeTruthy();
  });

  it("falls back to the whole line if only that arrived", async () => {
    withSchedule({ retentionMode: "", scheduleHeadline: "Flat window: every 3 hours, kept 14 days" });

    render(<Health onStatus={() => {}} />);

    // Rather than an empty cell, and rather than wording it here again.
    expect(await screen.findByText(/Flat window/)).toBeTruthy();
  });

  it("says there is none when nothing is scheduled", async () => {
    check({ scheduleInstalled: false });

    render(<Health onStatus={() => {}} />);

    // Scoped to the schedule's own cell: "None" appears in more than one figure
    // on a machine with nothing set up.
    const label = await screen.findByText(/^Schedule$/i);
    const cell = label.parentElement?.querySelector("dd");
    expect(cell?.textContent).toBe("None");
  });
});

// Every APFS volume holding snapshots, with its own numbers.
//
// The figures grid answers for the startup disk, and that used to be all there
// was — which was wrong rather than incomplete. `tmutil localsnapshot` takes no
// arguments and snapshots every mounted APFS volume at once, so an external disk
// fills with snapshots this application took while the screen reports a boot
// volume that is fine. The one that found it was at 98% full with its own pinning
// snapshot, and nothing anywhere said so.
describe("the volumes being snapshotted", () => {
  const twoVolumes = [
    {
      mountPoint: "/System/Volumes/Data",
      device: "disk3s1",
      snapshotCount: 7,
      purgeableCount: 7,
      pinningStamp: "",
      totalBytes: 1000,
      freeBytes: 500,
      freePercent: 50,
    },
    {
      mountPoint: "/Volumes/sdcard256gb",
      device: "disk8s1",
      snapshotCount: 14,
      purgeableCount: 14,
      pinningStamp: "2026-08-26-145013",
      totalBytes: 1000,
      freeBytes: 20,
      freePercent: 2,
    },
  ];

  it("names every volume, not just the startup disk", async () => {
    check({ volumes: twoVolumes });
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText("/Volumes/sdcard256gb")).toBeTruthy();
    expect(screen.getByText("/System/Volumes/Data")).toBeTruthy();
    // The device too, because two mount points can name one volume and the row
    // has to stay identifiable when they do.
    expect(screen.getByText("disk8s1")).toBeTruthy();
  });

  it("gives each volume its own snapshot count and free space", async () => {
    check({ volumes: twoVolumes });
    render(<Health onStatus={() => {}} />);

    await screen.findByText("/Volumes/sdcard256gb");
    // 14 on the external disk against 7 on the startup disk: the divergence is
    // the whole point, and a single figure cannot show it.
    expect(screen.getByText(/14 \(14 purgeable\)/)).toBeTruthy();
    expect(screen.getByText(/7 \(7 purgeable\)/)).toBeTruthy();
    expect(screen.getByText(/2%/)).toBeTruthy();
  });

  it("names the snapshot holding a container open", async () => {
    check({ volumes: twoVolumes });
    render(<Health onStatus={() => {}} />);

    // The one whose deletion actually returns space, and the reason a volume can
    // hold fourteen purgeable snapshots and still be full.
    expect(await screen.findByText("2026-08-26-145013")).toBeTruthy();
  });

  // A row that looked comfortable beside a warning saying otherwise would be
  // worse than either alone, so the threshold is the one the finding uses.
  it("marks a volume that is short of space", async () => {
    check({ volumes: twoVolumes });
    const { container } = render(<Health onStatus={() => {}} />);

    await screen.findByText("/Volumes/sdcard256gb");
    const low = container.querySelectorAll("td.low");
    expect(low.length).toBe(1);
    expect(low[0].textContent).toContain("2%");
  });

  // One volume is what the grid above already describes, and a table repeating it
  // invites the reader to look for the difference between them.
  it("says nothing when there is only the startup disk", async () => {
    check({ volumes: [twoVolumes[0]] });
    render(<Health onStatus={() => {}} />);

    await screen.findByText("You are covered");
    expect(screen.queryByText(/volumes being snapshotted/i)).toBeNull();
  });

  // An older service, or one that could not enumerate them, sends no list at all.
  // The screen is otherwise correct and must still render.
  it("survives a service that reports no volumes", async () => {
    check({ volumes: undefined });
    render(<Health onStatus={() => {}} />);

    expect(await screen.findByText("You are covered")).toBeTruthy();
  });
});

// Where the varying-length sections live, which is a layout rule with teeth.
//
// The figures at the foot of this screen are pinned: on a healthy Mac the middle
// is nearly empty and on a sick one it is a list that outruns the window, and
// either way the numbers should stay where the eye last found them. So anything
// whose length varies has to sit in the part that scrolls — and the volumes table
// did not. It was placed after the figures, which put it past the pinned panel,
// where a machine with two volumes had a table it could never reach.
describe("what scrolls on the health screen", () => {
  const twoVolumes = [
    { mountPoint: "/System/Volumes/Data", device: "disk3s1", snapshotCount: 7, purgeableCount: 7, pinningStamp: "", totalBytes: 1000, freeBytes: 500, freePercent: 50 },
    { mountPoint: "/Volumes/sdcard256gb", device: "disk8s1", snapshotCount: 14, purgeableCount: 14, pinningStamp: "2026-08-26-145013", totalBytes: 1000, freeBytes: 20, freePercent: 2 },
  ];

  it("keeps the volumes table inside the scrolling body", async () => {
    check({ volumes: twoVolumes });
    render(<Health onStatus={() => {}} />);

    await screen.findByText("/Volumes/sdcard256gb");
    const body = document.querySelector(".health-body")!;
    const volumes = document.querySelector(".volumes")!;
    expect(body.contains(volumes)).toBe(true);
  });

  // And the figures stay out of it, or they scroll away with everything else and
  // the reason they are pinned is lost.
  it("keeps the figures outside the scrolling body", async () => {
    check({ volumes: twoVolumes });
    render(<Health onStatus={() => {}} />);

    await screen.findByText("/Volumes/sdcard256gb");
    const body = document.querySelector(".health-body")!;
    const facts = document.querySelector(".facts")!;
    expect(body.contains(facts)).toBe(false);
  });

  // Every section in the body is a direct child of it, so one gap rule governs
  // the space between all of them. Nesting one inside another would take it out
  // of that rhythm, which is how this screen came to have several spacings.
  it("makes every varying section a direct child of the body", async () => {
    check({ volumes: twoVolumes, findings: [{ level: "warn", kind: "space", title: "A", detail: "a" }, { level: "warn", kind: "schedule", title: "B", detail: "b" }] });
    render(<Health onStatus={() => {}} />);

    await screen.findByText("/Volumes/sdcard256gb");
    const body = document.querySelector(".health-body")!;
    for (const section of document.querySelectorAll(".finding, .volumes, .warnings")) {
      expect(section.parentElement).toBe(body);
    }
  });
});
