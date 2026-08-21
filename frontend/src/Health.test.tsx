import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Health } from "./Health";
import { Status, Config } from "./api";
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
