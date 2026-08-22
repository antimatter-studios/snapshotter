import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Schedule } from "./Schedule";
import { Schedule as ScheduleAPI, Config, type ScheduleView } from "./api";
import "./i18n";

// Choosing what is kept, which is the one screen that decides what gets deleted.
//
// The numbers on it are computed by planning a history through the same function
// that does the deleting, so what is promised here is what happens. That makes
// the screen worth testing for a reason the others are not: a wrong choice sent
// to the service is a policy that quietly destroys more than was asked for.

const view = {
  installed: true,
  running: true,
  intervalHours: 3,
  retentionDays: 14,
  policyId: "flat",
  policySummary: "Everything for 14 days.",
  reachDays: 14,
  retained: 113,
  conflicts: [],
  logPath: "/tmp/log",
  // Typed rather than cast to never: the cases below spread it to vary one
  // field, and never cannot be spread.
} as unknown as ScheduleView;

const policies = [
  { id: "flat", name: "Flat window", summary: "Everything for 14 days.", tiers: [], retained: 113, reachDays: 14 },
  { id: "tiered-daily-weekly", name: "Tiered — daily, then weekly", summary: "One every 3 hours for 14 days, then one a day out to 8 weeks.", tiers: [], retained: 175, reachDays: 182 },
] as never;

function stub() {
  vi.spyOn(ScheduleAPI, "Status").mockResolvedValue(view as never);
  vi.spyOn(ScheduleAPI, "Policies").mockResolvedValue(policies);
  vi.spyOn(ScheduleAPI, "Log").mockResolvedValue("" as never);
  // The screen reads these on load. Without them the real binding is called,
  // which rejects under jsdom and puts an error banner over every test here.
  vi.spyOn(Config, "TripwireSensitivities").mockResolvedValue([
    [
      { id: "cautious", deletions: 500, windowSeconds: 5 },
      { id: "balanced", deletions: 200, windowSeconds: 5 },
      { id: "sensitive", deletions: 75, windowSeconds: 5 },
      { id: "very-sensitive", deletions: 25, windowSeconds: 5 },
    ],
    "balanced",
  ] as never);
}

afterEach(() => vi.restoreAllMocks());

describe("choosing what is kept", () => {
  // Both figures come from planning a real history through the pruner, so they
  // are the strongest thing on the screen. Showing one without the other invites
  // choosing a policy on reach alone and discovering the count later.
  it("shows how many each policy holds and how far it reaches", async () => {
    stub();
    render(<Schedule onStatus={() => {}} />);

    const group = await screen.findByRole("radiogroup");
    expect(within(group).getByText("175")).toBeTruthy();
    expect(within(group).getByText(/113/)).toBeTruthy();
  });

  // The description is derived from the policy's own bands, so it cannot claim
  // something the policy does not do. The line that used to sit above it was
  // written by hand and was false at two of five intervals.
  it("describes each policy by its bands", async () => {
    stub();
    render(<Schedule onStatus={() => {}} />);

    await screen.findByRole("radiogroup");
    expect(screen.getByText(/One every 3 hours for 14 days/)).toBeTruthy();
  });

  it("sends the chosen policy, interval and retention to be installed", async () => {
    stub();
    const install = vi.spyOn(ScheduleAPI, "InstallPolicy").mockResolvedValue(view as never);
    render(<Schedule onStatus={() => {}} />);

    const group = await screen.findByRole("radiogroup");
    await userEvent.click(within(group).getByRole("radio", { name: /daily, then weekly/i }));
    // Named precisely: /schedule/ also matches the button that removes one, and
    // clicking that in a test asserting an install is a test that passes for the
    // wrong reason.
    await userEvent.click(screen.getByRole("button", { name: /^update schedule$/i }));

    await waitFor(() => expect(install).toHaveBeenCalledOnce());
    const [interval, retention, policy] = install.mock.calls[0];
    expect(policy).toBe("tiered-daily-weekly");
    expect(interval).toBe(3);
    expect(retention).toBe(14);
  });

  // Removing a schedule stops snapshots being taken; it does not delete the ones
  // already there. Saying so is the difference between a button somebody presses
  // and one they avoid.
  it("removes the schedule without touching existing snapshots", async () => {
    stub();
    const uninstall = vi.spyOn(ScheduleAPI, "Uninstall").mockResolvedValue({ ...view, installed: false } as never);
    render(<Schedule onStatus={() => {}} />);

    await screen.findByRole("radiogroup");
    await userEvent.click(screen.getByRole("button", { name: /remove/i }));

    await waitFor(() => expect(uninstall).toHaveBeenCalledOnce());
  });

  // A second agent taking snapshots doubles the rate and applies two retention
  // windows to one shared set, which is worth saying loudly rather than leaving
  // somebody to notice their history thinning twice as fast.
  it("warns when something else is also taking snapshots", async () => {
    vi.spyOn(ScheduleAPI, "Status").mockResolvedValue({ ...view, conflicts: ["com.example.backup"] } as never);
    vi.spyOn(ScheduleAPI, "Policies").mockResolvedValue(policies);
    vi.spyOn(ScheduleAPI, "Log").mockResolvedValue("" as never);
    render(<Schedule onStatus={() => {}} />);

    expect(await screen.findByText(/com\.example\.backup/)).toBeTruthy();
  });
});

// Two agents write two logs, and the watcher's had nowhere to be read. The
// scheduled task's log answers "why is my history thinner than I asked for"; the
// watcher's answers the harder one — why a bulk deletion went by without a
// snapshot being taken — and that was reachable only by knowing the path and
// opening a terminal.
describe("the two logs", () => {
  it("shows the scheduled task's log to begin with", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Log").mockResolvedValue("12:00 took snapshot" as never);
    const watcherLog = vi.spyOn(ScheduleAPI, "TripwireLog").mockResolvedValue("" as never);

    render(<Schedule onStatus={() => {}} />);

    expect(await screen.findByText(/took snapshot/)).toBeTruthy();
    // The watcher's log is not fetched until asked for: reading a file nobody is
    // looking at costs a read every time this screen polls.
    expect(watcherLog).not.toHaveBeenCalled();
  });

  it("shows the watcher's log when asked, and says where it is written", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Log").mockResolvedValue("12:00 took snapshot" as never);
    vi.spyOn(ScheduleAPI, "TripwireStatus").mockResolvedValue({
      installed: true, running: true, plistPath: "/p.plist", logPath: "/tmp/tripwire.log",
    } as never);
    vi.spyOn(ScheduleAPI, "TripwireLog").mockResolvedValue("03:10 caught 900 deletions" as never);

    render(<Schedule onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /watcher|deletion/i }));

    expect(await screen.findByText(/900 deletions/)).toBeTruthy();
    // The path, because the next thing someone does with a log is open it.
    expect(screen.getByText(/tripwire\.log/)).toBeTruthy();
    // And the other log is out of the way rather than stacked beneath it.
    expect(screen.queryByText(/took snapshot/)).toBeNull();
  });

  it("says the watcher is not installed rather than that it logged nothing", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Log").mockResolvedValue("" as never);
    vi.spyOn(ScheduleAPI, "TripwireStatus").mockResolvedValue({
      installed: false, running: false, plistPath: "", logPath: "",
    } as never);
    vi.spyOn(ScheduleAPI, "TripwireLog").mockResolvedValue("" as never);

    render(<Schedule onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /watcher|deletion/i }));

    // An empty log reads as "nothing has happened", which is reassuring and
    // wrong: nothing is watching. The two states need different words, and this
    // one needs to say where to fix it.
    // Scoped to the log itself: the sensitivity section says the same thing about
    // the same watcher, and this test is about what the log shows.
    await waitFor(() => expect(document.querySelector("pre.log")?.textContent).toMatch(/not installed/i));
    expect(screen.queryByText(/nothing logged/i)).toBeNull();
  });

  it("goes back to the scheduled task's log", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Log").mockResolvedValue("12:00 took snapshot" as never);
    vi.spyOn(ScheduleAPI, "TripwireStatus").mockResolvedValue({
      installed: true, running: true, plistPath: "", logPath: "/tmp/t.log",
    } as never);
    vi.spyOn(ScheduleAPI, "TripwireLog").mockResolvedValue("03:10 caught deletions" as never);

    render(<Schedule onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /watcher|deletion/i }));
    await screen.findByText(/caught deletions/);

    await userEvent.click(screen.getByRole("button", { name: /scheduled task/i }));

    expect(await screen.findByText(/took snapshot/)).toBeTruthy();
  });

  it("says why the watcher's log could not be read", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Log").mockResolvedValue("" as never);
    vi.spyOn(ScheduleAPI, "TripwireStatus").mockRejectedValue(new Error("launchctl is not answering"));

    render(<Schedule onStatus={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /watcher|deletion/i }));

    expect(await screen.findByText(/not answering/)).toBeTruthy();
  });
});

// Four blocks of English were written into this screen's markup, two of them with
// catalogue entries that nothing called — translated once and then never wired,
// which no test catches because an unused message is not a missing one.
describe("what it says about the installed schedule", () => {
  it("states the policy from the service rather than wording it again", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Status").mockResolvedValue({
      ...view,
      installed: true,
      loaded: true,
      policySummary: "One every 3 hours for 14 days, then one a day out to 8 weeks.",
    } as never);

    render(<Schedule onStatus={() => {}} />);

    // Verbatim, in the line that reports what is installed: this screen and the
    // menu bar read the same sentence from the same place, which is the point of
    // it being carried rather than built. It appears in the policy list too,
    // hence the scoping.
    await waitFor(() => expect(document.querySelector(".note")).not.toBeNull());
    expect(document.querySelector(".note")?.textContent).toContain(
      "One every 3 hours for 14 days, then one a day out to 8 weeks.",
    );
  });

  it("says a schedule is installed but not loaded", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Status").mockResolvedValue({
      ...view,
      installed: true,
      loaded: false,
    } as never);

    render(<Schedule onStatus={() => {}} />);

    // Installed and not loaded takes no snapshots, and reads as working unless
    // it is said.
    expect(await screen.findByText(/not loaded/i)).toBeTruthy();
  });

  it("warns about a hand-edited policy without discarding what it says", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Status").mockResolvedValue({
      ...view,
      installed: true,
      policyId: "custom",
      policySummary: "One every 2 hours for 5 days.",
    } as never);

    render(<Schedule onStatus={() => {}} />);

    const warning = await screen.findByText(/not one of these/i);
    // The policy it actually carries is quoted, so someone can decide whether to
    // replace it. A warning that hides what it is warning about cannot be acted on.
    expect(warning.textContent).toContain("One every 2 hours for 5 days.");
  });

  it("names a conflicting task", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "Status").mockResolvedValue({
      ...view,
      installed: true,
      conflicts: ["com.example.backup", "com.other.snapshots"],
    } as never);

    render(<Schedule onStatus={() => {}} />);

    // Both of them, because "another task is also taking snapshots" is
    // unactionable without knowing which.
    const warning = await screen.findByText(/double/i);
    expect(warning.textContent).toContain("com.example.backup");
    expect(warning.textContent).toContain("com.other.snapshots");
  });

  it("explains that a snapshot cannot be recreated", async () => {
    stub();

    render(<Schedule onStatus={() => {}} />);

    // The sentence that stops the figures below reading as reservations.
    expect(await screen.findByText(/cannot be recreated/i)).toBeTruthy();
  });
});

// How readily the wire trips. The count alone is unanswerable — whether 200 files
// in five seconds is a lot depends entirely on what the machine does all day —
// so the setting is a name, and the count is shown beside it.
describe("how sensitive the bulk-deletion watcher is", () => {
  it("offers each setting with the count it stands for", async () => {
    stub();

    render(<Schedule onStatus={() => {}} />);

    const select = await screen.findByLabelText(/readily/i);
    const options = Array.from(select.querySelectorAll("option")).map((o) => o.textContent ?? "");
    expect(options).toHaveLength(4);
    // Both halves: the name says what it is for, the number says what it means.
    expect(options[0]).toMatch(/Cautious/);
    expect(options[0]).toMatch(/500/);
    expect(options[3]).toMatch(/Very sensitive/);
    expect(options[3]).toMatch(/25/);
    // And the window, so "25 files" is not read as "25 files ever".
    expect(options[3]).toMatch(/5 seconds/);
  });

  it("shows the setting that is actually in force", async () => {
    stub();
    vi.spyOn(Config, "TripwireSensitivities").mockResolvedValue([
      [
        { id: "cautious", deletions: 500, windowSeconds: 5 },
        { id: "sensitive", deletions: 75, windowSeconds: 5 },
      ],
      "sensitive",
    ] as never);

    render(<Schedule onStatus={() => {}} />);

    // Resolved by the service the same way the watcher resolves it, so the
    // dropdown cannot show a different answer from the one being used.
    await waitFor(() =>
      expect((screen.getByLabelText(/readily/i) as HTMLSelectElement).value).toBe("sensitive"),
    );
  });

  it("saves as soon as one is chosen", async () => {
    stub();
    const saved = vi.spyOn(Config, "SetTripwireSensitivity").mockResolvedValue(undefined as never);

    render(<Schedule onStatus={() => {}} />);
    await userEvent.selectOptions(await screen.findByLabelText(/readily/i), "very-sensitive");

    // No Apply button: a settings dropdown with one invites someone to change it
    // and walk away.
    await waitFor(() => expect(saved).toHaveBeenCalledWith("very-sensitive"));
  });

  it("puts the dropdown back if the setting could not be saved", async () => {
    stub();
    vi.spyOn(Config, "SetTripwireSensitivity").mockRejectedValue(new Error("the settings file is read-only"));

    render(<Schedule onStatus={() => {}} />);
    const select = await screen.findByLabelText(/readily/i);
    await userEvent.selectOptions(select, "cautious");

    expect(await screen.findByText(/read-only/)).toBeTruthy();
    // Otherwise it shows a setting that is not in force, which is the one thing a
    // settings screen must never do.
    await waitFor(() => expect((select as HTMLSelectElement).value).toBe("balanced"));
  });

  it("says the setting will not take effect until the watcher restarts", async () => {
    stub();

    render(<Schedule onStatus={() => {}} />);

    // The watcher is a separate process that reads this at startup. A setting that
    // appears to apply and does not is worse than one that says when it will.
    expect(await screen.findByText(/next time the watcher starts/i)).toBeTruthy();
  });

  it("says so when there is no watcher for the setting to apply to", async () => {
    stub();
    vi.spyOn(ScheduleAPI, "TripwireStatus").mockResolvedValue({
      installed: false, running: false, plistPath: "", logPath: "",
    } as never);
    vi.spyOn(ScheduleAPI, "TripwireLog").mockResolvedValue("" as never);

    render(<Schedule onStatus={() => {}} />);
    // The status is fetched when the watcher's log is opened, which is what puts
    // it on screen.
    await userEvent.click(await screen.findByRole("button", { name: /watcher|deletion/i }));

    // Choosing a sensitivity for a watcher that is not running is a setting with
    // nothing to apply to, and it says so above the dropdown rather than below it.
    await waitFor(() => expect(screen.getByText(/nothing is being watched for/i)).toBeTruthy());
  });
});
