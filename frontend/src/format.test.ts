import { describe, expect, it, vi, afterEach } from "vitest";
import { age, breadcrumbs, bytes, stamp, statusLabel } from "./format";

// These four decide almost every piece of text on screen. Nothing here can crash
// the application, which is exactly why they are worth testing: a wrong answer
// does not throw, it just quietly tells someone their snapshot is from yesterday
// when it is from last month.

describe("bytes", () => {
  it("climbs through the units", () => {
    expect(bytes(512)).toBe("512 B");
    expect(bytes(1024)).toBe("1.0 KB");
    expect(bytes(1024 * 1024)).toBe("1.0 MB");
    expect(bytes(1024 ** 3)).toBe("1.0 GB");
    expect(bytes(1024 ** 4)).toBe("1.0 TB");
  });

  // The sidebar shows free space beside used space; without the decimal, 1.9 GB
  // and 1.1 GB both read "2 GB" and the bar beside them appears to disagree.
  it("keeps a decimal place while the number is small", () => {
    expect(bytes(1536)).toBe("1.5 KB");
    expect(bytes(1024 * 20)).toBe("20 KB");
  });

  // Nothing at all is a dash rather than "0 B": a snapshot whose size has not
  // been measured is not a snapshot of nothing.
  it("shows a dash for nothing", () => {
    expect(bytes(0)).toBe("—");
  });

  // Beyond the last unit it must keep counting rather than wrapping to undefined.
  it("does not run off the end of the units", () => {
    expect(bytes(1024 ** 6)).toContain("TB");
  });
});

describe("age", () => {
  afterEach(() => vi.useRealTimers());

  function at(iso: string, now: string) {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(now));
    return age(iso);
  }

  it("names each span the way someone would say it", () => {
    expect(at("2026-08-15T12:00:00Z", "2026-08-15T12:00:10Z")).toBe("just now");
    expect(at("2026-08-15T11:30:00Z", "2026-08-15T12:00:00Z")).toBe("30 min ago");
    expect(at("2026-08-15T09:00:00Z", "2026-08-15T12:00:00Z")).toBe("3 hr ago");
    expect(at("2026-08-14T12:00:00Z", "2026-08-15T12:00:00Z")).toBe("yesterday");
    expect(at("2026-08-10T12:00:00Z", "2026-08-15T12:00:00Z")).toBe("5 days ago");
  });

  // Clocks move backwards — a machine waking from sleep, a corrected NTP drift.
  // "in -3 minutes" beside a restore button is alarming and means nothing.
  it("never counts into the future", () => {
    expect(at("2026-08-15T12:05:00Z", "2026-08-15T12:00:00Z")).toBe("just now");
  });
});

describe("stamp", () => {
  // A date that will not parse must show a dash. "Invalid Date" printed beside a
  // snapshot reads as a corrupted snapshot rather than as a formatting slip.
  it("shows a dash rather than Invalid Date", () => {
    expect(stamp("not a date")).toBe("—");
  });

  it("renders a real date", () => {
    const got = stamp("2026-08-15T12:00:00Z");
    expect(got).not.toBe("—");
    expect(got.length).toBeGreaterThan(0);
  });
});

describe("breadcrumbs", () => {
  // Every crumb is clickable and navigates to its own path, so the paths have to
  // be cumulative. A crumb that navigates to the wrong folder is worse than one
  // that does nothing.
  it("builds a cumulative trail from the root", () => {
    expect(breadcrumbs("/Users/someone/Documents")).toEqual([
      { label: "/", path: "/" },
      { label: "Users", path: "/Users" },
      { label: "someone", path: "/Users/someone" },
      { label: "Documents", path: "/Users/someone/Documents" },
    ]);
  });

  it("always keeps a way back to the root", () => {
    expect(breadcrumbs("/")).toEqual([{ label: "/", path: "/" }]);
    expect(breadcrumbs("")).toEqual([{ label: "/", path: "/" }]);
  });

  // Trailing and doubled separators come from string concatenation upstream and
  // must not produce empty crumbs.
  it("ignores empty segments", () => {
    expect(breadcrumbs("//Users//someone/")).toEqual([
      { label: "/", path: "/" },
      { label: "Users", path: "/Users" },
      { label: "someone", path: "/Users/someone" },
    ]);
  });
});

describe("statusLabel", () => {
  // These are the badges on the comparison screen. "onlyInSnapshot" means the
  // file is gone from the disk, which is the single most important row there —
  // showing a raw enum name, or nothing, loses it.
  it("names every status the comparison can return", () => {
    for (const key of ["same", "modified", "onlyInSnapshot", "onlyOnDisk", "typeChanged"]) {
      expect(statusLabel[key]).toBeTruthy();
      expect(statusLabel[key]).not.toBe(key);
    }
  });

  it("says deleted plainly, because that is the row people are looking for", () => {
    expect(statusLabel.onlyInSnapshot).toContain("deleted");
  });
});
