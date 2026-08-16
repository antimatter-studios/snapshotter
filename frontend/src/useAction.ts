import { useState } from "react";
import { message } from "./api";

/**
 * Running one thing the user asked for, and saying what happened.
 *
 * Every view had its own copy of this: set busy, clear the error, await the
 * call, report it, catch, unset busy. Five copies that drifted — some cleared
 * the previous error before starting and some left it on screen under a
 * succeeding action, which is how a stale "authorization was cancelled" could
 * sit beneath a snapshot that had just been taken.
 *
 * The busy flag is what disables the buttons. Without it a second click starts a
 * second mount, and mounting raises an authorization prompt, so the user gets
 * two password dialogs for one intention.
 */
export function useAction(onStatus: (text: string) => void) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  /**
   * Runs `fn`, reports `done` if it succeeds and one was given, and shows why if
   * it does not.
   *
   * `after` runs only on success — it is the refresh that pulls in whatever the
   * action changed, and running it after a failure would replace the error with
   * a screen that looks fine.
   */
  const run = async (fn: () => Promise<unknown>, done?: string, after?: () => unknown) => {
    setBusy(true);
    // Cleared on the way in rather than on the way out: an error from a previous
    // attempt is not evidence about this one.
    setError("");
    try {
      await fn();
      // Loading something is not worth a status line — only doing something is.
      // The same busy-and-error handling applies to both, which is why one
      // function serves them with the message optional.
      if (done) onStatus(done);
      if (after) await after();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  return { busy, error, setError, run };
}
