import { useCallback, useEffect, useState } from "react";
import { Schedule as ScheduleAPI, message, serviceChosenTail, type ScheduleView, type TripwireView } from "./api";
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
  const { busy, error, setError, run } = useAction(onStatus);

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

  return (
    <div className="schedule">
      <section>
        <h2>{t("schedule.automatic")}</h2>
        <p className="explain">
          macOS only takes local snapshots on its own when Time Machine has a backup disk configured. This schedule takes
          them without one. It needs no password: creating and deleting snapshots goes through Time Machine's own
          background service.
        </p>

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
            {t("schedule.flatWindow")}
            <select value={retention} onChange={(e) => setRetention(Number(e.target.value))}>
              {RETENTIONS.map((r) => (
                <option key={r.days} value={r.days}>
                  {t(r.key)}
                </option>
              ))}
            </select>
          </label>
        </div>
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

        {view?.installed && view.policyId === "custom" && (
          <p className="warning">
            The installed schedule carries a policy that is not one of these — {view.policySummary} Choosing one above
            and updating the schedule replaces it.
          </p>
        )}

        <p className="explain">
          A snapshot cannot be recreated: it records a state of the disk that has passed. Anything outside the policy is
          deleted on the next scheduled run, and macOS may reclaim more than that under disk pressure — so these figures
          are upper bounds, not reservations.
        </p>

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
              ? `A snapshot every ${view.intervalHours} hours. ${view.policySummary} About ${
                  view.maxSnapshots
                } snapshots, reaching back ${reach(view.reachDays)} — ${
                  view.loaded ? "running" : "installed but not loaded"
                }.`
              : t("schedule.none")}
          </p>
        )}

        {!!view?.conflicts?.length && (
          <p className="warning">
            Another scheduled task is also taking snapshots: {view.conflicts.join(", ")}. Two of them will double the
            snapshot rate and apply two different retention windows to the same set. Remove one.
          </p>
        )}
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
