import { useCallback, useEffect, useState } from "react";
import { useLiveRefresh } from "./live";
import {
  Status,
  Snapshots,
  Schedule,
  message,
  serviceChosenTail,
  type Health as HealthState,
  type Finding,
} from "./api";
import { bytes, stamp } from "./format";
import { FindingIcon } from "./FindingIcon";
import { useAction } from "./useAction";

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
      setError("");
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
