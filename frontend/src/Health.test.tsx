import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Health } from "./Health";
import { Status, Config, Schedule, Snapshots } from "./api";
import "./i18n";

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

afterEach(() => vi.restoreAllMocks());

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
