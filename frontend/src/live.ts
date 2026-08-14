import { useEffect } from "react";

/** How often an open window re-reads state it did not change itself.
 *
 *  The same reasoning as the menu bar's own timer: the underlying state moves on
 *  the schedule's interval — hours, not seconds — so this is about noticing a
 *  snapshot taken elsewhere rather than about being live. Half a minute is short
 *  enough that a disagreement is never left on screen long enough to be believed.
 */
const refreshEvery = 30_000;

/** Re-runs a fetch periodically, and immediately whenever the window is looked at
 *  again.
 *
 *  Without this the window has no way to learn about a snapshot it did not take.
 *  Three things change that state from outside this window — the menu bar's *Take
 *  a snapshot now*, the scheduled agent, and the bulk-deletion tripwire — and two
 *  of them are separate processes, so nothing can push the change in. The menu bar
 *  polls and the window did not, which is why they could disagree: the tray said
 *  four snapshots while the window still said three, and the window is the one
 *  that looks broken.
 *
 *  The focus listener matters more than the timer. Noticing within thirty seconds
 *  is fine for a window in the background; a window you have just turned back to
 *  should already be right.
 */
export function useLiveRefresh(refresh: () => unknown) {
  useEffect(() => {
    const tick = () => void refresh();
    const id = window.setInterval(tick, refreshEvery);
    // Both events: focus fires when the window is activated, visibilitychange
    // when it is uncovered or the app is switched back to. Either can happen
    // without the other.
    const onVisible = () => {
      if (!document.hidden) tick();
    };
    window.addEventListener("focus", tick);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(id);
      window.removeEventListener("focus", tick);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [refresh]);
}
