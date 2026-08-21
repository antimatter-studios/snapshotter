import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useLiveRefresh } from "./live";
import { Config } from "./api";

// How an open window learns about a snapshot it did not take.
//
// Three things change that state from outside the window — the menu bar's *Take
// a snapshot now*, the scheduled agent, and the bulk-deletion tripwire — and two
// of them are separate processes, so nothing can push the change in. The menu bar
// polled and the window did not, which is how the tray could say four snapshots
// while the window still said three. The window is the one that looks broken.
//
// Every failure here is silent: a listener that is never attached, an interval
// that is never cleared, a settings value that is read once and then ignored.

const halfAMinute = 30_000;

function settings(windowSeconds?: number) {
  vi.spyOn(Config, "Get").mockResolvedValue({
    config: { refresh: windowSeconds === undefined ? {} : { window_seconds: windowSeconds } },
  } as never);
}

/** Mounts the hook and lets the settings read that happens on mount settle. */
async function start(refresh: () => unknown) {
  const view = renderHook(() => useLiveRefresh(refresh));
  await act(async () => {});
  return view;
}

beforeEach(() => vi.useFakeTimers());

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("keeping an open window current", () => {
  it("refreshes on its own after half a minute", async () => {
    settings();
    const refresh = vi.fn();
    await start(refresh);

    expect(refresh).not.toHaveBeenCalled();
    await act(async () => void vi.advanceTimersByTime(halfAMinute));
    expect(refresh).toHaveBeenCalledOnce();
  });

  // This matters more than the timer. Noticing within thirty seconds is fine for
  // a window in the background; a window you have just turned back to should
  // already be right.
  it("refreshes at once when the window is looked at again", async () => {
    settings();
    const refresh = vi.fn();
    await start(refresh);

    await act(async () => void window.dispatchEvent(new Event("focus")));

    expect(refresh).toHaveBeenCalledOnce();
  });

  it("refreshes when the window is uncovered", async () => {
    settings();
    const refresh = vi.fn();
    await start(refresh);

    // focus and visibilitychange are separate events and either can happen
    // without the other: switching apps raises focus, uncovering a window that
    // already had focus raises only visibilitychange.
    await act(async () => void document.dispatchEvent(new Event("visibilitychange")));

    expect(refresh).toHaveBeenCalledOnce();
  });

  it("does not refresh when the window is covered up", async () => {
    settings();
    const refresh = vi.fn();
    await start(refresh);

    const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
    await act(async () => void document.dispatchEvent(new Event("visibilitychange")));

    // Work done for a window nobody can see is work done for nothing, and the
    // refresh costs a walk of every snapshot on the volume.
    expect(refresh).not.toHaveBeenCalled();
    hidden.mockRestore();
  });

  it("stops when the window closes", async () => {
    settings();
    const refresh = vi.fn();
    const view = await start(refresh);

    view.unmount();
    await act(async () => void vi.advanceTimersByTime(halfAMinute * 4));
    window.dispatchEvent(new Event("focus"));

    // A timer left running calls into a component that no longer exists, and the
    // listeners accumulate one set per screen the user has visited.
    expect(refresh).not.toHaveBeenCalled();
  });
});

describe("the interval as a setting", () => {
  it("follows the settings file rather than the built-in default", async () => {
    settings(5);
    const refresh = vi.fn();
    await start(refresh);

    await act(async () => void vi.advanceTimersByTime(5_000));
    expect(refresh).toHaveBeenCalledOnce();
  });

  it("re-reads the setting as it goes, so a change takes effect without relaunching", async () => {
    settings(5);
    const refresh = vi.fn();
    await start(refresh);

    // The file changes under a running window, which is what editing it looks
    // like from here.
    settings(60);
    await act(async () => void vi.advanceTimersByTime(5_000));
    expect(refresh).toHaveBeenCalledOnce();

    // The old interval is gone: five more seconds is no longer enough.
    await act(async () => void vi.advanceTimersByTime(5_000));
    expect(refresh).toHaveBeenCalledOnce();

    await act(async () => void vi.advanceTimersByTime(55_000));
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it("ignores a value that would refresh continuously or never", async () => {
    settings(0);
    const refresh = vi.fn();
    await start(refresh);

    // Zero and negative both mean the default rather than a tight loop or a
    // window that silently never updates again.
    await act(async () => void vi.advanceTimersByTime(halfAMinute - 1));
    expect(refresh).not.toHaveBeenCalled();
    await act(async () => void vi.advanceTimersByTime(1));
    expect(refresh).toHaveBeenCalledOnce();
  });

  it("keeps refreshing when the settings cannot be read", async () => {
    vi.spyOn(Config, "Get").mockRejectedValue(new Error("the settings file is unreadable"));
    const refresh = vi.fn();
    await start(refresh);

    // A settings file that cannot be read is no reason to stop noticing
    // snapshots — the window would go stale with nothing to say why.
    await act(async () => void vi.advanceTimersByTime(halfAMinute));
    expect(refresh).toHaveBeenCalledOnce();
  });
});
