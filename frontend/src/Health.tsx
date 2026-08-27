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
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

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
  const { t } = useTranslation();
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

  // Only when there is nothing else to show. An action failing used to take this
  // branch too, which replaced the whole screen — verdict, findings and all —
  // with one sentence, and the reader lost both the reason they came and any
  // other button they might have pressed. The banner belongs above the findings,
  // which are all still true.
  if (error && !health) return <p className="banner error">{error}</p>;
  if (!health) return <p className="empty">{t("health.checking")}</p>;

  const act = (fn: () => Promise<unknown>, done: string) => run(fn, done, refresh);

  return (
    <div className="health">
      {/* Above the content, not instead of it: what failed was one action, and
          every other thing this screen says is still worth reading. Left up
          until something else happens, because refresh runs on a timer and would
          otherwise clear the reason before it could be read. */}
      {error && <p className="banner error" onClick={() => setError("")}>{error}</p>}

      <div className={`verdict ${health.level}`}>
        <span className="verdict-mark" aria-hidden="true" />
        <div>
          <h3>{health.headline}</h3>
          <p>
            {health.snapshotCount > 0 && health.newest
              ? t("health.newest", { when: stamp(health.newest) })
              : t("health.nothingRecorded")}
            {health.scheduleInstalled && health.nextDue
              ? " " + t("health.nextDue", { when: stamp(health.nextDue) })
              : ""}
          </p>
        </div>
        <button onClick={() => void refresh()} disabled={busy}>
          {t("health.recheck")}
        </button>
      </div>

      {/* Everything whose length varies scrolls. The eight figures below it do
          not: on a healthy Mac this area is nearly empty, on a sick one it is a
          list that outruns the window, and either way the numbers should stay
          where the eye last found them. */}
      <div className="health-body">

        {health.findings.length === 0 && (
          // This had a catalogue entry all along while the English sat here.
          <p className="empty">{t("health.nothingToActOn")}</p>
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
                {t("health.takeOneNow")}
              </button>
            )}

            {f.action === "install-schedule" && (
              <button
                onClick={() =>
                  act(
                    () => Schedule.Install(DEFAULT_INTERVAL_HOURS, DEFAULT_RETENTION_DAYS),
                    t("health.willTakeEvery", { hours: DEFAULT_INTERVAL_HOURS }),
                  )
                }
                disabled={busy}
              >
                {health.scheduleInstalled
                  ? t("health.startIt")
                  : t("health.takeEvery", { hours: DEFAULT_INTERVAL_HOURS })}
              </button>
            )}

            {f.action === "install-tripwire" && (
              <button
                onClick={() =>
                  act(
                    () => Schedule.InstallTripwire(),
                    t("health.watchingFor"),
                  )
                }
                disabled={busy}
              >
                {health.tripwireInstalled ? t("health.startWatching") : t("health.watchForBulk")}
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
                  {log ? t("health.refreshLog") : t("health.showLog")}
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
          <dt>{t("health.restorePoints")}</dt>
          <dd>{health.snapshotCount}</dd>
        </div>
        <div>
          <dt>{t("health.cover")}</dt>
          <dd>{coverage(health.coverageHours, t)}</dd>
        </div>
        <div>
          <dt>{t("health.schedule")}</dt>
          {/* The mode's name, with the whole line in the tooltip. This cell used
              to say "Every 3h, kept 14d" — built here from two numbers, in
              English, and wrong for every tiered policy: it read the horizon as
              the retention, which is true of a flat window and nothing else. The
              words come from the service now, which gets them from one place. */}
          <dd title={health.scheduleHeadline}>
            {health.scheduleInstalled
              ? `${health.retentionMode || health.scheduleHeadline}${
                  health.scheduleRunning ? "" : ` (${t("health.scheduleNotRunning")})`
                }`
              : t("health.scheduleNone")}
          </dd>
        </div>
        <div>
          <dt>{t("health.freeSpace")}</dt>
          <dd>
            {bytes(health.volumeFreeBytes)} of {bytes(health.volumeTotalBytes)} (
            {health.freePercent.toFixed(0)}%)
          </dd>
        </div>
        <div>
          {/* APFS reports no size per snapshot and cannot: a snapshot shares
              blocks with the live volume and with its neighbours. Purgeable is
              the honest substitute — how many macOS may delete on its own. */}
          <dt>{t("health.purgeable")}</dt>
          <dd>
            {health.purgeableCount} of {health.snapshotCount}
          </dd>
        </div>
        <div>
          <dt>{t("health.pinning")}</dt>
          <dd>{health.pinningStamp || "None"}</dd>
        </div>
        <div>
          <dt>{t("health.bulkWatch")}</dt>
          <dd>
            {health.tripwireRunning
              ? t("health.watching")
              : health.tripwireInstalled
                ? t("health.installedNotRunning")
                : "Off"}
          </dd>
        </div>
        {/* Fills the eighth cell of a four-wide grid that was showing seven, and
            earns it: a copy in /Applications and a working build share a bundle
            identifier, so "which one am I looking at" is a real question. */}
        <div>
          <dt>{t("health.version")}</dt>
          <dd>{health.version}</dd>
        </div>
      </dl>

      <Volumes health={health} />
    </div>
  );
}

/**
 * Every APFS volume holding local snapshots, with its own numbers.
 *
 * The figures above describe the startup disk. They used to be the only ones,
 * and that was wrong rather than incomplete: `tmutil localsnapshot` takes no
 * arguments and snapshots every mounted APFS volume at once, so an external disk
 * fills with snapshots this application took while the screen reported a boot
 * volume that was fine. The one that found it was at 98% full.
 *
 * Free space and the pinning snapshot are per volume because a container is per
 * volume — neither is knowable from another disk's numbers.
 *
 * Absent when there is only the startup disk, which is what the grid above
 * already says.
 */
function Volumes({ health }: { health: HealthState }) {
  const { t } = useTranslation();
  const volumes = health.volumes ?? [];
  if (volumes.length < 2) return null;

  return (
    <section className="volumes">
      <h3>{t("health.volumes")}</h3>
      <p className="explain">{t("health.volumesExplain")}</p>
      <table>
        <thead>
          <tr>
            <th>{t("health.colVolume")}</th>
            <th>{t("health.colSnapshots")}</th>
            <th>{t("health.colFree")}</th>
            <th>{t("health.colPinning")}</th>
          </tr>
        </thead>
        <tbody>
          {volumes.map((v) => (
            <tr key={v.device}>
              <td>
                <span className="volume-mount">{v.mountPoint}</span>
                {/* The device, because two mount points can name one volume and
                    the row has to be identifiable when they do. */}
                <span className="volume-device">{v.device}</span>
              </td>
              <td>
                {t("health.ofWhichPurgeable", {
                  count: v.snapshotCount,
                  purgeable: v.purgeableCount,
                })}
              </td>
              {/* Marked low at the same threshold the finding uses, so the row
                  and the warning above it cannot disagree. */}
              <td className={v.freePercent > 0 && v.freePercent < lowFreePercent ? "low" : ""}>
                {bytes(v.freeBytes)} ({v.freePercent.toFixed(0)}%)
              </td>
              <td>{v.pinningStamp || t("health.none")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

/** The threshold services/status.go warns at. Stated twice because Go cannot
 *  call TypeScript; a row that looked fine beside a warning that said otherwise
 *  would be worse than either alone. */
const lowFreePercent = 10;

/** Words a span in the largest unit that stays honest. */
// The same rule and the same keys as i18n.Span on the Go side. Go cannot call
// TypeScript, so the thresholds are stated twice — but the keys are shared, so a
// correction to a translation lands in both, and the constants are named rather
// than being two bare numbers a reader has to match up by eye.
const hoursPerDay = 24;
const hoursBeforeDays = 48;

function coverage(hours: number, t: TFunction): string {
  // count is i18next's own plural selector, so German and Spanish pick their form
  // by CLDR rule rather than by an "s" appended in English.
  if (hours >= hoursBeforeDays) return t("count.days", { count: Math.round(hours / hoursPerDay) });
  if (hours >= 1) return t("count.hours", { count: Math.round(hours) });
  return t("count.underAnHour");
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
  const { t } = useTranslation();
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
      const view = await Config.StopIgnoringFolder(fragment);
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
      <h3>{t("health.bulkWarnings")}</h3>
      {/* A row per event rather than a card each: these are a log, and a log is
          read by scanning down one column. The folder is the column that gets
          scanned, so it is the one given the room. */}
      <table>
        <tbody>
          {warnings.map((w, i) => (
            <tr key={i}>
              <td className="warning-when">{stamp(w.at)}</td>
              {/* One folder per line, each with its own button.

                  They were joined with commas, which wrapped into a block nobody
                  could read — and the single Ignore button silenced whichever
                  happened to be first, which is not a choice anyone made. A burst
                  usually spans two or three folders and only one of them is the
                  noisy one. */}
              <td className="warning-where">
                {/* The response failing is the one outcome worth surfacing. The
                    column that used to sit here read "snapshot taken" on every
                    healthy row — the expected case, stated at length — and its
                    absence was the only informative part. */}
                {!w.snapshot && <p className="warning-failed">{w.note || t("health.responseFailed")}</p>}
                {w.where?.length ? (
                  <ul>
                    {w.where.map((folder, n) => (
                      <li key={folder}>
                        {/* Shown short, ignored long: "~" is for reading and
                            means nothing to a path comparison. */}
                        <span className="folder">{w.labels?.[n] ?? folder}</span>
                        <button
                          title={`Stop warning about ${folder}`}
                          onClick={() => void ignore(folder)}
                        >
                          {t("health.ignore")}
                        </button>
                      </li>
                    ))}
                  </ul>
                ) : (
                  t("health.unknownLocation")
                )}
              </td>
              {/* No snapshot is the row worth seeing from across the room: the
                  deletion happened and nothing was captured. */}
            </tr>
          ))}
        </tbody>
      </table>

      {problem && <p className="error">{problem}</p>}

      {ignored.length > 0 && (
        // Shown, and removable. A list nobody can see or shorten grows until the
        // tripwire watches nothing, and that failure is silent by construction.
        <div className="ignored">
          <h4>{t("health.notWarningAbout")}</h4>
          <ul>
            {ignored.map((fragment) => (
              <li key={fragment}>
                <code>{fragment}</code>
                <button title={t("health.watchAgainTitle")} onClick={() => void watchAgain(fragment)}>
                  {t("health.watchAgain")}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
