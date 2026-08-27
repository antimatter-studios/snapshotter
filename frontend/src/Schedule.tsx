import { useCallback, useEffect, useState } from "react";
import { Schedule as ScheduleAPI, Config, message, serviceChosenTail, type ScheduleView, type TripwireView, type TripwireSensitivity, type WatchedDirectory } from "./api";
import "./Schedule.css";
import { useAction } from "./useAction";
import { useTranslation } from "react-i18next";

// Keys rather than text: these are module-level, where no translation is in
// scope, and holding the key defers the lookup to the render that shows it —
// which is also what makes the list re-read when the language changes.
const INTERVALS = [
  { hours: 1, key: "schedule.everyHour" },
  { hours: 3, key: "schedule.every3" },
  { hours: 6, key: "schedule.every6" },
  { hours: 12, key: "schedule.every12" },
  { hours: 24, key: "schedule.daily" },
] as const;

const RETENTIONS = [
  { days: 3, key: "schedule.days3" },
  { days: 7, key: "schedule.days7" },
  { days: 14, key: "schedule.days14" },
  { days: 30, key: "schedule.days30" },
] as const;

/** The identifier the Go side gives the flat window. */
const FLAT = "flat";

/**
 * One retention policy on offer, as the binding hands it over.
 *
 * Derived from the call rather than restated, so it cannot drift from
 * services.PolicyOption. api.ts is where binding types are named for the views,
 * and this one is not among them yet.
 */
type PolicyOption = Awaited<ReturnType<typeof ScheduleAPI.Policies>>[number];

/**
 * Schedule configures the timer that takes snapshots.
 *
 * Without one, nothing takes local snapshots at all: macOS only schedules them
 * when Time Machine has a destination configured.
 */
export function Schedule({ onStatus }: { onStatus: (text: string) => void }) {
  const { t } = useTranslation();
  const [view, setView] = useState<ScheduleView | null>(null);
  const [interval, setInterval] = useState(6);
  const [retention, setRetention] = useState(14);
  const [policy, setPolicy] = useState(FLAT);
  const [options, setOptions] = useState<PolicyOption[]>([]);
  const [log, setLog] = useState("");
  // Which log is showing, and what the watcher has written. Fetched when its tab
  // is first opened rather than on every refresh: reading a log nobody is looking
  // at costs a file read each time the screen polls.
  const [which, setWhich] = useState<"scheduled" | "tripwire">("scheduled");
  const [tripwire, setTripwire] = useState<TripwireView | null>(null);
  const [tripwireLog, setTripwireLog] = useState("");
  // The settings on offer and the one in force, both from the service so the
  // dropdown cannot show a different answer from the one the watcher uses.
  const [sensitivities, setSensitivities] = useState<TripwireSensitivity[]>([]);
  const [sensitivity, setSensitivity] = useState("");
  // What the watcher watches, which is the whole of what it watches. It used to
  // watch the entire home directory with an ignore list chasing whatever had most
  // recently made a noise; ~/Library alone tripped it all day and each trip cost a
  // whole-volume snapshot.
  const [watched, setWatched] = useState<WatchedDirectory[]>([]);
  const [candidate, setCandidate] = useState("");
  const { busy, error, setError, run } = useAction(onStatus);

  // Saved as soon as it is chosen, like the theme and the language: a settings
  // dropdown with its own Apply button invites someone to change it and walk away.
  const chooseSensitivity = async (id: string) => {
    const previous = sensitivity;
    setSensitivity(id);
    await run(async () => {
      try {
        await Config.SetTripwireSensitivity(id);
      } catch (err) {
        // Put back, so the dropdown never shows a setting that was not saved.
        setSensitivity(previous);
        throw err;
      }
    });
  };

  // Added and removed one at a time, saved immediately: this is the same kind of
  // setting as the sensitivity below it, and a list with its own Apply button
  // invites someone to type a directory and walk away from it.
  const addDirectory = async () => {
    const dir = candidate.trim();
    if (!dir) return;
    await run(async () => {
      const view = await Config.WatchDirectory(dir);
      setWatched(await Config.WatchedDirectories());
      // Cleared only once it was accepted, so a rejected path stays in the box to
      // be corrected rather than having to be typed again.
      if (view) setCandidate("");
    });
  };

  const removeDirectory = async (dir: string) => {
    await run(async () => {
      await Config.UnwatchDirectory(dir);
      setWatched(await Config.WatchedDirectories());
    });
  };

  const refresh = useCallback(async () => {
    try {
      const status = await ScheduleAPI.Status();
      setView(status);
      if (status.installed) {
        setInterval(status.intervalHours);
        setPolicy(status.policyId);
        // Only a flat window's reach is the flat window: a tiered policy's is
        // its horizon, which is not one of the choices in this select and would
        // leave it showing nothing.
        if (status.policyId === FLAT) setRetention(status.retentionDays);
      }
      setLog(await ScheduleAPI.Log(serviceChosenTail));

      // Which sensitivities exist and which is chosen. Read here rather than
      // computed in the window: the counts belong to the code that decides with
      // them, and a second copy of "sensitive means 75" would drift.
      const [offered, current] = await Config.TripwireSensitivities();
      setSensitivities(offered);
      setSensitivity(current);
      setWatched(await Config.WatchedDirectories());
    } catch (err) {
      setError(message(err));
    }
  }, []);

  useEffect(() => {
    if (which !== "tripwire") return;
    let live = true;
    void (async () => {
      try {
        const [status, body] = await Promise.all([
          ScheduleAPI.TripwireStatus(),
          ScheduleAPI.TripwireLog(serviceChosenTail),
        ]);
        if (!live) return;
        setTripwire(status);
        setTripwireLog(body);
      } catch (err) {
        if (live) setError(message(err));
      }
    })();
    return () => {
      live = false;
    };
  }, [which, setError]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Re-asked as the interval and the window change, because what a policy
  // retains depends on both and a stale number here is a number someone acts on.
  // Any failure lands in the error banner rather than in the console: with no
  // options the install button is disabled, and a screen that is inert for an
  // unstated reason is worse than one that says why.
  useEffect(() => {
    void (async () => {
      try {
        setOptions(await ScheduleAPI.Policies(interval, retention));
      } catch (err) {
        setError(message(err));
      }
    })();
  }, [interval, retention]);

  const install = () =>
    run(async () => {
      setView(await ScheduleAPI.InstallPolicy(interval, retention, policy));
    }, t("schedule.installed"));

  const uninstall = () =>
    run(async () => {
      setView(await ScheduleAPI.Uninstall());
    }, t("schedule.removed"));

  const chosen = options.find((o) => o.id === policy);
  // Flat keeps everything for the span; every other shape keeps everything at the
  // chosen rate for it and then thins. The two labels below differ because the
  // promise does.
  const tiered = policy !== FLAT;

  return (
    <div className="schedule">
      <section>
        <h2>{t("schedule.automatic")}</h2>
        <p className="explain">{t("schedule.whyNeeded")}</p>

      </section>

      <section>
        <h2>{t("schedule.whatIsKept")}</h2>
          <p className="explain">{t("schedule.flatOrThinned")}</p>

          {/* Said here rather than discovered later: a snapshot taken between
              periods is removed, and one of those is the snapshot the tripwire
              takes when files start disappearing — which the application also
              sends a notification about. Being told a snapshot exists and then
              finding it gone is the surprise worth heading off. */}
          <p className="explain">{t("schedule.onePerPeriod")}</p>

        {/* Both numbers are computed by planning a history through the same
            function that does the deleting, so what is promised here is what
            happens. */}
        <div className="policies" role="radiogroup" aria-label={t("schedule.whatIsKept")}>
          {options.map((option) => (
            <label key={option.id} className={`policy ${option.id === policy ? "chosen" : ""}`}>
              <input
                type="radio"
                name="retention-policy"
                value={option.id}
                checked={option.id === policy}
                onChange={() => setPolicy(option.id)}
              />
              <div className="policy-body">
                <div className="policy-name">{option.name}</div>
                <p className="policy-bands">{option.summary}</p>
                <div className="policy-numbers">
                  <span>
                    <strong>{option.retained}</strong> snapshots
                  </span>
                  <span title={`${Math.round(option.reachDays)} days`}>
                    {t("schedule.reachingBack")} <strong>{reach(option.reachDays)}</strong>
                  </span>
                </div>
              </div>
            </label>
          ))}
        </div>

        {/* Below the choice, not above it: these two numbers mean different things
            depending on the shape chosen, so the shape has to be settled first.
            They were above it, labelled "Flat window", which read as belonging to
            the flat profile alone — when in fact a tiered profile's first band IS
            this rate for this span, and its later bands are multiples of the span.
            The same number, a different promise, under a name that named only one
            of them. */}
        <h3>{t("schedule.theseNumbers")}</h3>
        <div className="fields">
          <label>
            {t("schedule.howOften")}
            <select value={interval} onChange={(e) => setInterval(Number(e.target.value))}>
              {INTERVALS.map((i) => (
                <option key={i.hours} value={i.hours}>
                  {t(i.key)}
                </option>
              ))}
            </select>
          </label>

          <label>
            {tiered ? t("schedule.keepEverySnapshotFor") : t("schedule.keepEverythingFor")}
            <select value={retention} onChange={(e) => setRetention(Number(e.target.value))}>
              {RETENTIONS.map((r) => (
                <option key={r.days} value={r.days}>
                  {t(r.key)}
                </option>
              ))}
            </select>
          </label>
        </div>

        {/* What the chosen shape does with them. For a flat window that is the
            whole story; for a tiered one the span is multiplied, which is a large
            lever and was previously unexplained. */}
        <p className="explain">
          {tiered ? t("schedule.windowFeedsBands") : t("schedule.everythingKept")}
        </p>

        {view?.installed && view.policyId === "custom" && (
          <p className="warning">{t("schedule.customPolicy", { summary: view.policySummary })}</p>
        )}

        {/* This key existed and nothing called it: translated once, then never
            wired, which no test catches because an unused message is not a
            missing one. */}
        <p className="explain">{t("schedule.cannotRecreate")}</p>

        <div className="buttons">
          <button className="primary" onClick={install} disabled={busy || !chosen}>
            {view?.installed ? t("schedule.update") : t("schedule.install")}
          </button>
          {view?.installed && (
            <button onClick={uninstall} disabled={busy}>
              {t("schedule.remove")}
            </button>
          )}
        </div>

        {error && <p className="error">{error}</p>}

        {view && (
          <p className={view.loaded ? "note ok" : "note"}>
            {view.installed
              ? // The policy sentence comes from the service, which words it in
                // one place; only the count and the state are added here.
                t("schedule.installedState", {
                  every: t("count.hours", { count: view.intervalHours }),
                  summary: view.policySummary,
                  count: view.maxSnapshots,
                  reach: reach(view.reachDays),
                  state: view.loaded ? t("schedule.stateRunning") : t("schedule.stateNotLoaded"),
                })
              : t("schedule.none")}
          </p>
        )}

        {!!view?.conflicts?.length && (
          <p className="warning">{t("schedule.conflict", { tasks: view.conflicts.join(", ") })}</p>
        )}
      </section>

      <section>
        <h2>{t("tripwire.heading")}</h2>
        <p className="explain">{t("tripwire.explain")}</p>

        {/* The list comes first because it is the setting that decides whether
            anything is watched at all. A sensitivity above an empty list is a
            dial on a machine that is switched off. */}
        <div className="watched">
          <h4>{t("tripwire.watching")}</h4>
          <p className="explain">{t("tripwire.watchingExplain")}</p>

          {watched.length === 0 ? (
            <p className="warning">{t("tripwire.nothingWatched")}</p>
          ) : (
            <ul>
              {watched.map((d) => (
                <li key={d.configured}>
                  {/* Both spellings, because they differ and the difference is
                      the whole of some confusions: "~/projects" is what was
                      typed, and the resolved path is what is actually watched. */}
                  <code>
                    {d.configured}
                    {d.resolved !== d.configured && <span className="resolved">{d.resolved}</span>}
                    {/* A directory that is not there is not being watched, and
                        nothing else on this screen would say so. */}
                    {d.missing && <span className="warning">{t("tripwire.directoryMissing")}</span>}
                  </code>
                  <button
                    title={t("tripwire.stopWatchingTitle", { directory: d.configured })}
                    disabled={busy}
                    onClick={() => void removeDirectory(d.configured)}
                  >
                    {t("tripwire.stopWatching")}
                  </button>
                </li>
              ))}
            </ul>
          )}

          <div className="add-directory">
            <label>
              {t("tripwire.addDirectory")}
              <input
                type="text"
                value={candidate}
                placeholder={t("tripwire.directoryPlaceholder")}
                disabled={busy}
                onChange={(e) => setCandidate(e.target.value)}
                // Enter is what someone types after a path; making them reach for
                // the button is how a list ends up with one entry in it.
                onKeyDown={(e) => {
                  if (e.key === "Enter") void addDirectory();
                }}
              />
            </label>
            <button disabled={busy || candidate.trim() === ""} onClick={() => void addDirectory()}>
              {t("tripwire.add")}
            </button>
          </div>
        </div>

        {tripwire && !tripwire.installed && watched.length > 0 && (
          // Said before the dropdown, not after: choosing a sensitivity for a
          // watcher that is not running is a setting with nothing to apply to.
          // Only once something is watched, though — with an empty list the line
          // above already says why nothing is happening, and two of them would
          // read as two problems.
          <p className="warning">{t("tripwire.notInstalled")}</p>
        )}

        <div className="fields">
          <label>
            {t("tripwire.sensitivity")}
            <select
              value={sensitivity}
              onChange={(e) => void chooseSensitivity(e.target.value)}
              disabled={busy || sensitivities.length === 0}
            >
              {sensitivities.map((s) => (
                <option key={s.id} value={s.id}>
                  {t("tripwire.option", {
                    name: t(`tripwire.level.${s.id}` as never),
                    count: s.deletions,
                    seconds: s.windowSeconds,
                  })}
                </option>
              ))}
            </select>
          </label>
        </div>

        {/* A setting that appears to apply and does not is worse than one that
            says when it will. The watcher is its own process; launchd restarts
            it, and it reads this at startup. */}
        <p className="explain">{t("tripwire.appliesNextRun")}</p>
      </section>

      <section>
        <h2>{t("schedule.log")}</h2>

        {/* Two agents write two logs, and the second one had nowhere to be read.
            The scheduled task's log answers "why is my history thinner than I
            asked for"; the watcher's answers the harder question — why a bulk
            deletion went by without a snapshot being taken — and that one was
            only reachable by knowing the path and opening a terminal. */}
        <div className="segmented">
          <button className={which === "scheduled" ? "on" : ""} onClick={() => setWhich("scheduled")}>
            {t("schedule.logScheduled")}
          </button>
          <button className={which === "tripwire" ? "on" : ""} onClick={() => setWhich("tripwire")}>
            {t("schedule.logTripwire")}
          </button>
        </div>

        {which === "scheduled" ? (
          <>
            <p className="explain">
              {t("schedule.writtenBy")} {view?.logPath}
            </p>
            <pre className="log">{log || t("schedule.nothingLogged")}</pre>
          </>
        ) : (
          <>
            <p className="explain">
              {t("schedule.writtenByTripwire")} {tripwire?.logPath}
            </p>
            {/* Not installed is a different answer from nothing logged, and the
                one that says what to do about it. */}
            <pre className="log">
              {tripwire && !tripwire.installed
                ? t("schedule.watcherNotInstalled")
                : tripwireLog || t("schedule.nothingLogged")}
            </pre>
          </>
        )}
      </section>
    </div>
  );
}

// Whole weeks rather than calendar units. A reach is a rolling window, so
// "52 weeks" is the honest year here and 365 would make the weeks and the years
// disagree at the boundary — 52 weeks would round to "a year" while 364 days
// still read as "52 weeks".
const daysPerWeek = 7;
const weeksPerYear = 52;
const daysPerYear = daysPerWeek * weeksPerYear;

// Below four weeks, weeks are a coarser answer than the days themselves.
const daysBeforeWeeksAreClearer = 4 * daysPerWeek;

/** Words a reach in the largest unit that stays honest; exact days sit in the title. */
function reach(days: number): string {
  if (days < 1) return "under a day";
  if (days < daysBeforeWeeksAreClearer) return `${Math.round(days)} days`;
  if (days < daysPerYear) return `${Math.round(days / daysPerWeek)} weeks`;
  const years = days / daysPerYear;
  return years < 1.5 ? "a year" : `${years.toFixed(1)} years`;
}
