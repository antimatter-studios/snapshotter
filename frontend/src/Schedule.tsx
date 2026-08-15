import { useCallback, useEffect, useState } from "react";
import { Schedule as ScheduleAPI, message, serviceChosenTail, type ScheduleView } from "./api";
import "./Schedule.css";

const INTERVALS = [
  { hours: 1, label: "Every hour" },
  { hours: 3, label: "Every 3 hours" },
  { hours: 6, label: "Every 6 hours" },
  { hours: 12, label: "Every 12 hours" },
  { hours: 24, label: "Once a day" },
];

const RETENTIONS = [
  { days: 3, label: "3 days" },
  { days: 7, label: "1 week" },
  { days: 14, label: "2 weeks" },
  { days: 30, label: "30 days" },
];

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
  const [view, setView] = useState<ScheduleView | null>(null);
  const [interval, setInterval] = useState(6);
  const [retention, setRetention] = useState(14);
  const [policy, setPolicy] = useState(FLAT);
  const [options, setOptions] = useState<PolicyOption[]>([]);
  const [log, setLog] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

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

  const install = async () => {
    setBusy(true);
    setError("");
    try {
      setView(await ScheduleAPI.InstallPolicy(interval, retention, policy));
      onStatus("Schedule installed. The first snapshot is taken immediately.");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  const uninstall = async () => {
    setBusy(true);
    setError("");
    try {
      setView(await ScheduleAPI.Uninstall());
      onStatus("Schedule removed. Existing snapshots were left alone.");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  const chosen = options.find((o) => o.id === policy);

  return (
    <div className="schedule">
      <section>
        <h2>Automatic snapshots</h2>
        <p className="explain">
          macOS only takes local snapshots on its own when Time Machine has a backup disk configured. This schedule takes
          them without one. It needs no password: creating and deleting snapshots goes through Time Machine's own
          background service.
        </p>

        <div className="fields">
          <label>
            How often
            <select value={interval} onChange={(e) => setInterval(Number(e.target.value))}>
              {INTERVALS.map((i) => (
                <option key={i.hours} value={i.hours}>
                  {i.label}
                </option>
              ))}
            </select>
          </label>

          <label>
            Flat window
            <select value={retention} onChange={(e) => setRetention(Number(e.target.value))}>
              {RETENTIONS.map((r) => (
                <option key={r.days} value={r.days}>
                  {r.label}
                </option>
              ))}
            </select>
          </label>
        </div>
      </section>

      <section>
        <h2>What is kept</h2>
        <p className="explain">
          Snapshots can be kept flat — everything inside the window set above — or thinned as they age: everything for
          the last day or two, then one a day, then one a week. Thinning reaches months back for about the count a flat
          fortnight already costs, because an old snapshot is wanted for the day it covers rather than for the hour.
        </p>

        {/* Both numbers are computed by planning a history through the same
            function that does the deleting, so what is promised here is what
            happens. */}
        <div className="policies" role="radiogroup" aria-label="What is kept">
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
                <p className="policy-why">{option.why}</p>
                <p className="policy-bands">{option.summary}</p>
                <div className="policy-numbers">
                  <span>
                    <strong>{option.retained}</strong> snapshots
                  </span>
                  <span title={`${Math.round(option.reachDays)} days`}>
                    reaching back <strong>{reach(option.reachDays)}</strong>
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
            {view?.installed ? "Update schedule" : "Install schedule"}
          </button>
          {view?.installed && (
            <button onClick={uninstall} disabled={busy}>
              Remove schedule
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
              : "No schedule installed. Nothing is taking snapshots automatically."}
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
        <h2>Log</h2>
        <p className="explain">Written by the scheduled task at {view?.logPath}</p>
        <pre className="log">{log || "Nothing logged yet."}</pre>
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
