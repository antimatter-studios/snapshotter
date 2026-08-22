import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Schedule } from "./Schedule";
import { Schedule as ScheduleAPI, type ScheduleView } from "./api";
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
    expect(await screen.findByText(/not installed/i)).toBeTruthy();
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
