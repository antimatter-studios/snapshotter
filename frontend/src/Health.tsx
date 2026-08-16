import { useCallback, useEffect, useState } from "react";
import { useLiveRefresh } from "./live";
import {
  Status,
  Snapshots,
  Schedule,
  Config,
  message,
  serviceChosenTail,
  type Health as HealthState,
  type Finding,
} from "./api";
import { bytes, stamp } from "./format";
import { FindingIcon } from "./FindingIcon";
import { useAction } from "./useAction";
import type { Warning } from "./api";

/**
 * What the one-click fix installs. Six hours rather than hourly, kept a
 * fortnight: the aim is days of depth against an accidental deletion, not
 * intra-day granularity, and each snapshot pins another generation of every
 * large file rewritten between them. These mirror schedule.DefaultConfig.
 */
const DEFAULT_INTERVAL_HOURS = 6;
const DEFAULT_RETENTION_DAYS = 14;

/**
 * The answer to "am I protected right now", which no other screen gives.
 *
 * Every number here existed already, spread across the sidebar, the schedule
 * tab and a warning banner. Spread out is the problem: the failure this
 * application exists to prevent is believing you are covered when you are not,
 * and that belief survives any amount of information that has to be assembled
 * by hand.
 */
export function Health({ onStatus }: { onStatus: (s: string) => void }) {
  const [health, setHealth] = useState<HealthState | null>(null);
  const { busy, error, setError, run } = useAction(onStatus);
  // The scheduled task's log, fetched only when a finding offers it — a
  // schedule that is failing silently is the one case where the raw output is
  // the answer.
  const [log, setLog] = useState("");

  const refresh = useCallback(async () => {
    try {
      setHealth(await Status.Check());
      // Deliberately does not clear the error: this runs on a timer and on
      // focus, and clearing here wipes the reason an action failed before it can
      // be read. See the same note in App.tsx.
    } catch (err) {
      setError(message(err));
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // The verdict on this machine changes without anyone pressing anything here.
  useLiveRefresh(refresh);

  if (error) return <p className="banner error">{error}</p>;
  if (!health) return <p className="empty">Checking…</p>;

  const act = (fn: () => Promise<unknown>, done: string) => run(fn, done, refresh);

  return (
    <div className="health">
      <div className={`verdict ${health.level}`}>
        <span className="verdict-mark" aria-hidden="true" />
        <div>
          <h3>{health.headline}</h3>
          <p>
            {health.snapshotCount > 0 && health.newest
              ? `Newest ${stamp(health.newest)}.`
              : "Nothing has been recorded."}
            {health.scheduleInstalled && health.nextDue
              ? ` Next due ${stamp(health.nextDue)}.`
              : ""}
          </p>
        </div>
        <button onClick={() => void refresh()} disabled={busy}>
          Re-check
        </button>
      </div>

      {/* Everything whose length varies scrolls. The eight figures below it do
          not: on a healthy Mac this area is nearly empty, on a sick one it is a
          list that outruns the window, and either way the numbers should stay
          where the eye last found them. */}
      <div className="health-body">

        {health.findings.length === 0 && (
          <p className="empty">
            Nothing to act on. Snapshots exist, something is taking more, and the retention you set
            is the retention you will get.
          </p>
        )}

        {health.findings.map((f: Finding, i: number) => (
          <div key={i} className={`finding ${f.level}`}>
            {/* The shape says what the finding is about, the colour how bad it
                is — the same split the menu bar makes, so the two agree. */}
            <h4>
              <FindingIcon kind={f.kind} />
              {f.title}
            </h4>
            <p>{f.detail}</p>

            {/* A finding you cannot act on from where you read it is just
                anxiety, so the fix is offered here rather than named and left
                in another tab. */}
            {f.action === "take-snapshot" && (
              <button
                onClick={() => act(() => Snapshots.TakeNow(), "Snapshot taken")}
                disabled={busy}
              >
                Take one now
              </button>
            )}

            {f.action === "install-schedule" && (
              <button
                onClick={() =>
                  act(
                    () => Schedule.Install(DEFAULT_INTERVAL_HOURS, DEFAULT_RETENTION_DAYS),
                    `Snapshots will now be taken every ${DEFAULT_INTERVAL_HOURS} hours`,
                  )
                }
                disabled={busy}
              >
                {health.scheduleInstalled
                  ? "Start it"
                  : `Take one every ${DEFAULT_INTERVAL_HOURS} hours`}
              </button>
            )}

            {f.action === "install-tripwire" && (
              <button
                onClick={() =>
                  act(
                    () => Schedule.InstallTripwire(),
                    "Watching for bulk deletion — it will keep watching after this window closes",
                  )
                }
                disabled={busy}
              >
                {health.tripwireInstalled ? "Start watching" : "Watch for bulk deletion"}
              </button>
            )}

            {f.action === "show-log" && (
              <>
                <button
                  onClick={() =>
                    Schedule.Log(serviceChosenTail)
                      .then(setLog)
                      .catch((err) => setError(message(err)))
                  }
                  disabled={busy}
                >
                  {log ? "Refresh the log" : "Show the log"}
                </button>
                {log && <pre className="log">{log}</pre>}
              </>
            )}
          </div>
        ))}

        <BulkDeletionWarnings />
      </div>

      <dl className="facts">
        <div>
          <dt>Restore points</dt>
          <dd>{health.snapshotCount}</dd>
        </div>
        <div>
          <dt>Cover</dt>
          <dd>{coverage(health.coverageHours)}</dd>
        </div>
        <div>
          <dt>Schedule</dt>
          <dd>
            {health.scheduleInstalled
              ? `Every ${health.intervalHours}h, kept ${health.retentionDays}d${
                  health.scheduleRunning ? "" : " (not running)"
                }`
              : "None"}
          </dd>
        </div>
        <div>
          <dt>Free space</dt>
          <dd>
            {bytes(health.volumeFreeBytes)} of {bytes(health.volumeTotalBytes)} (
            {health.freePercent.toFixed(0)}%)
          </dd>
        </div>
        <div>
          {/* APFS reports no size per snapshot and cannot: a snapshot shares
              blocks with the live volume and with its neighbours. Purgeable is
              the honest substitute — how many macOS may delete on its own. */}
          <dt>Purgeable</dt>
          <dd>
            {health.purgeableCount} of {health.snapshotCount}
          </dd>
        </div>
        <div>
          <dt>Pinning the container</dt>
          <dd>{health.pinningStamp || "None"}</dd>
        </div>
        <div>
          <dt>Bulk-deletion watch</dt>
          <dd>
            {health.tripwireRunning
              ? "Watching"
              : health.tripwireInstalled
                ? "Installed, not running"
                : "Off"}
          </dd>
        </div>
        {/* Fills the eighth cell of a four-wide grid that was showing seven, and
            earns it: a copy in /Applications and a working build share a bundle
            identifier, so "which one am I looking at" is a real question. */}
        <div>
          <dt>Version</dt>
          <dd>{health.version}</dd>
        </div>
      </dl>
    </div>
  );
}

/** Words a span in the largest unit that stays honest. */
function coverage(hours: number): string {
  if (hours >= 48) return `${Math.round(hours / 24)} days`;
  if (hours >= 1) return `${Math.round(hours)} hours`;
  return "under an hour";
}

/**
 * The last few bulk deletions the tripwire saw.
 *
 * Read from a file rather than held in memory, because the tripwire is a
 * separate process: it runs under launchd, and by the time anyone opens this
 * window the process that saw the deletion has exited. The file is how the two
 * talk.
 *
 * Absent entirely when nothing has happened. An empty section headed "Bulk
 * Deletion Warnings" on a healthy Mac invites someone to wonder what is missing.
 */
function BulkDeletionWarnings() {
  const [warnings, setWarnings] = useState<Warning[]>([]);
  const [ignored, setIgnored] = useState<string[]>([]);
  const [problem, setProblem] = useState("");

  const loadIgnored = useCallback(async () => {
    try {
      const view = await Config.Get();
      setIgnored(view.config?.tripwire?.ignore ?? []);
    } catch {
      // The list is an aid, not the state of the machine.
    }
  }, []);

  const load = useCallback(async () => {
    try {
      setWarnings(await Status.RecentWarnings(5));
    } catch {
      // Nothing to say. This is history, not state: failing to read it is not
      // worth a banner over a screen that is otherwise correct.
    }
  }, []);
  useEffect(() => {
    void load();
    void loadIgnored();
  }, [load, loadIgnored]);
  useLiveRefresh(load);

  const ignore = async (folder: string) => {
    setProblem("");
    try {
      const view = await Config.IgnoreFolder(folder);
      setIgnored(view.config?.tripwire?.ignore ?? []);
    } catch (err) {
      setProblem(message(err));
    }
  };

  const watchAgain = async (fragment: string) => {
    setProblem("");
    try {
      const view = await Config.WatchFolder(fragment);
      setIgnored(view.config?.tripwire?.ignore ?? []);
    } catch (err) {
      setProblem(message(err));
    }
  };

  // Nothing has happened and nothing is silenced: an empty section under that
  // heading only invites someone to wonder what is missing.
  if (warnings.length === 0 && ignored.length === 0) return null;

  return (
    <section className="warnings">
      <h3>Bulk deletion warnings</h3>
      {/* A row per event rather than a card each: these are a log, and a log is
          read by scanning down one column. The folder is the column that gets
          scanned, so it is the one given the room. */}
      <table>
        <tbody>
          {warnings.map((w, i) => (
            <tr key={i}>
              <td className="warning-when">{stamp(w.at)}</td>
              <td className="warning-where" title={w.where?.join(", ")}>
                {w.where?.join(", ") || "an unknown location"}
              </td>
              {/* No snapshot is the row worth seeing from across the room: the
                  deletion happened and nothing was captured. */}
              <td className={w.snapshot ? "warning-outcome ok" : "warning-outcome bad"}>
                {w.snapshot ? w.snapshot : w.note || "no snapshot"}
              </td>
              {/* The moment someone wants this is while looking at a warning
                  they did not want, so the button is on the row rather than in a
                  settings screen they would have to go and find. */}
              <td className="warning-action">
                {w.where?.[0] && (
                  <button
                    className="link"
                    title={`Stop warning about ${w.where[0]}`}
                    onClick={() => void ignore(w.where[0])}
                  >
                    ignore
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {problem && <p className="error">{problem}</p>}

      {ignored.length > 0 && (
        // Shown, and removable. A list nobody can see or shorten grows until the
        // tripwire watches nothing, and that failure is silent by construction.
        <div className="ignored">
          <h4>Not warning about</h4>
          <ul>
            {ignored.map((fragment) => (
              <li key={fragment}>
                <code>{fragment}</code>
                <button className="link" title="Watch this again" onClick={() => void watchAgain(fragment)}>
                  watch again
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
