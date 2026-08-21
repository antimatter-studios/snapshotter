import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { Health } from "./Health";
import { Schedule } from "./Schedule";
import { Browser } from "./Browser";
import { Status, Schedule as ScheduleAPI, Browse, Config } from "./api";
import type { SnapshotView } from "./api";
import "./i18n";

// What the markup is, rather than what it does.
//
// A behaviour test passes against almost any shape: a table built from divs
// answers "is the name shown" exactly as a real one does, and then a screen
// reader announces nothing, the columns do not align, and the stylesheet targets
// elements that are not there. These assert the structure the CSS and the
// platform both assume.

const health = {
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
} as never;

afterEach(() => vi.restoreAllMocks());

describe("the health screen's markup", () => {
  // The figures are a description list, which is what they are: a term and the
  // value belonging to it. Divs with classes would read as an undifferentiated
  // run of text.
  it("states the figures as a description list", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(health);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);
    const { container } = render(<Health onStatus={() => {}} />);

    await screen.findByText(/covered/i);

    const list = container.querySelector("dl.facts");
    expect(list).not.toBeNull();
    const terms = list!.querySelectorAll("dt");
    const values = list!.querySelectorAll("dd");
    expect(terms.length).toBe(values.length);

    // Eight, and the number is load-bearing: the grid is four columns or eight,
    // the only counts that divide eight evenly. A ninth figure added without
    // reconsidering the layout leaves a row with one thing in it.
    expect(terms.length).toBe(8);
  });

  it("gives every figure a term and a value, neither of them empty", async () => {
    vi.spyOn(Status, "Check").mockResolvedValue(health);
    vi.spyOn(Config, "Get").mockResolvedValue({ config: { tripwire: { ignore: [] } } } as never);
    const { container } = render(<Health onStatus={() => {}} />);

    await screen.findByText(/covered/i);

    const pairs = container.querySelectorAll("dl.facts > div");
    expect(pairs.length).toBe(8);
    for (const pair of pairs) {
      expect(within(pair as HTMLElement).getByRole("term").textContent?.trim()).toBeTruthy();
      const value = pair.querySelector("dd");
      expect(value?.textContent?.trim()).toBeTruthy();
    }
  });
});

describe("the schedule screen's markup", () => {
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
  } as never;

  const policies = [
    { id: "flat", name: "Flat window", summary: "Everything for 14 days.", tiers: [], retained: 113, reachDays: 14 },
    { id: "tiered-daily-weekly", name: "Tiered — daily, then weekly", summary: "One every 3 hours for 14 days.", tiers: [], retained: 175, reachDays: 182 },
  ] as never;

  // A set of mutually exclusive choices is a radiogroup, and each option a radio.
  // Divs with click handlers cannot be reached by keyboard and announce nothing.
  it("offers the policies as a radiogroup", async () => {
    vi.spyOn(ScheduleAPI, "Status").mockResolvedValue(view);
    vi.spyOn(ScheduleAPI, "Policies").mockResolvedValue(policies);
    render(<Schedule onStatus={() => {}} />);

    const group = await screen.findByRole("radiogroup");
    expect(group).toBeTruthy();

    const options = within(group).getAllByRole("radio");
    expect(options.length).toBe(2);
    // Exactly one is chosen, which is what makes it a choice rather than a list.
    expect(options.filter((o) => (o as HTMLInputElement).checked).length).toBe(1);
  });

  // The sections are headings, so the screen can be navigated by them rather than
  // read start to finish.
  it("divides itself with headings", async () => {
    vi.spyOn(ScheduleAPI, "Status").mockResolvedValue(view);
    vi.spyOn(ScheduleAPI, "Policies").mockResolvedValue(policies);
    render(<Schedule onStatus={() => {}} />);

    await screen.findByRole("radiogroup");
    expect(screen.getAllByRole("heading").length).toBeGreaterThanOrEqual(2);
  });
});

describe("the browser's markup", () => {
  const snapshot = {
    name: "com.apple.TimeMachine.2026-08-20-120000.local",
    stamp: "2026-08-20-120000",
    taken: "2026-08-20T12:00:00Z",
    mounted: true,
    mountPoint: "/tmp/mnt",
  } as SnapshotView;

  // A table of files is a table: rows that line up in columns, with headers
  // naming them. Built from divs it looks the same until the text is long.
  it("lists files in a table with a header for every column", async () => {
    vi.spyOn(Browse, "Merged").mockResolvedValue({
      rows: [
        {
          relPath: "notes.md",
          absLive: "/Users/someone/notes.md",
          absSnapshot: "/tmp/mnt/notes.md",
          isDir: false,
          status: "modified",
          snapSize: 10,
          liveSize: 20,
          snapModTime: "2026-08-20T11:00:00Z",
          liveModTime: "2026-08-20T11:30:00Z",
        },
      ],
      note: "",
    } as never);
    vi.spyOn(Browse, "DirectoryStatus").mockResolvedValue({ status: "same", why: "" } as never);

    render(<Browser snapshot={snapshot} path="/Users/someone" onPathChange={() => {}} onMount={() => {}} onDiff={() => {}} onStatus={() => {}} />);

    const table = await screen.findByRole("table");
    const headers = within(table).getAllByRole("columnheader");
    // Five named columns and one for the row's actions, which is deliberately
    // unnamed: a header over a column of buttons is a label for nothing.
    expect(headers.length).toBe(6);
    expect(headers.filter((h) => h.textContent?.trim()).length).toBe(5);

    const rows = within(table).getAllByRole("row");
    expect(rows.length).toBe(2); // the header row and the one file
  });
});
