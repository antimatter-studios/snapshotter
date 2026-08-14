import { useCallback, useEffect, useState } from "react";
import { Snapshots, Browse, Status, message, type SnapshotView, type Overview } from "./api";
import { age, bytes, stamp } from "./format";
import { Browser } from "./Browser";
import { Compare } from "./Compare";
import { Schedule } from "./Schedule";
import { Health } from "./Health";
import { Search } from "./Search";
import { ThemeToggle } from "./ThemeToggle";
import { useLiveRefresh } from "./live";
// The same file the application icon and the favicon are built from, reached out
// of the project's assets/ rather than copied into public/, so the mark has one
// home. Vite resolves and emits it at build time.
import iconUrl from "../../assets/icons/icon.svg";

/** The tabs that read a snapshot. Every one of them is about the snapshot
 *  selected in the sidebar, which is why health is no longer among them: it
 *  describes the machine and ignored the selection entirely, so as a tab it
 *  claimed to be about a snapshot and was not. It is the home view instead. */
type Tab = "browse" | "compare" | "search";

/** Which of the three things the window can be showing. home and schedule are
 *  whole-machine; snapshots is the one that depends on what is selected. */
type View = "home" | "snapshots" | "schedule";

export default function App() {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [selected, setSelected] = useState<string>("");
  const [path, setPath] = useState<string>("");
  const [tab, setTab] = useState<Tab>("browse");
  // Opening on home rather than on a snapshot: the first question is whether this
  // machine has usable restore points at all, not what is inside one of them.
  const [view, setView] = useState<View>("home");
  // Where Options was opened from, so leaving it returns you there rather than
  // guessing. Without this, closing Options from a snapshot would either drop you
  // on home or pretend a snapshot was selected when none was.
  const [beforeOptions, setBeforeOptions] = useState<View>("home");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [faking, setFaking] = useState(false);
  const [scenario, setScenario] = useState("");
  // mountHelp is fetched lazily, because it only means anything once mounting
  // has actually been refused.
  const [mountHelp, setMountHelp] = useState("");

  // A refusal to mount is not an ordinary error: it names a permission, and the
  // place to grant it is four levels into System Settings. Recognising it here
  // is what turns a dead end into something with a next step.
  const mountRefused = error.includes("Full Disk Access");

  const refresh = useCallback(async () => {
    try {
      const next = await Snapshots.Overview();
      setOverview(next);
      setSelected((current) => current || next.snapshots[0]?.name || "");
      setError("");
    } catch (err) {
      setError(message(err));
    }
  }, []);

  useEffect(() => {
    void refresh();
    // Browsing starts in the home folder, which is where the things worth
    // recovering usually live.
    Browse.Home().then(setPath).catch(() => setPath("/Users"));
    Status.Check()
      .then((h) => {
        setFaking(h.faking);
        setScenario(h.scenario ?? "");
      })
      .catch(() => setFaking(false));
  }, [refresh]);

  useEffect(() => {
    if (mountRefused && !mountHelp) Status.MountHelp().then(setMountHelp).catch(() => {});
  }, [mountRefused, mountHelp]);

  // The sidebar counts snapshots this window did not necessarily take.
  useLiveRefresh(refresh);

  const snapshots = overview?.snapshots ?? [];
  const current = snapshots.find((s) => s.name === selected) ?? null;

  const act = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      setStatus(done);
      await refresh();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  const mount = (snapshot: SnapshotView) =>
    act(() => Snapshots.Mount([snapshot.name]), `Opened the snapshot from ${stamp(snapshot.taken)}`);

  const mountAll = () =>
    act(
      () => Snapshots.Mount(snapshots.filter((s) => !s.mounted).map((s) => s.name)),
      "Opened every snapshot — one password, and now any of them can be searched",
    );

  return (
    <div className="app">
      <header>
        <div className="brand">
          {/* Decorative: the heading beside it already says the name, so
              announcing it twice would only slow a screen reader down. */}
          <img className="mark" src={iconUrl} alt="" width={40} height={40} />
          <div className="title">
            <h1>Snapshotter</h1>
            <span className="subtitle">APFS local snapshots on this Mac</span>
          </div>
        </div>
        <div className="header-actions">
          {overview && (
            <span className="disk">
              {bytes(overview.volumeFreeBytes)} free of {bytes(overview.volumeTotalBytes)}
            </span>
          )}
          <ThemeToggle />
        </div>
      </header>

      {/* First banner, and the most important one: every figure below it is
          invented. Worded flatly rather than cheerfully — a reader who misses
          this draws conclusions about a machine that does not exist. */}
      {scenario && (
        <p className="banner error">
          <strong>Simulated machine.</strong> Scenario <code>{scenario}</code> is loaded. Every
          snapshot, schedule and figure on this screen was invented to drive the interface, and
          none of it describes this Mac.
        </p>
      )}
      {faking && (
        <p className="banner warning">
          <strong>Mounts are simulated.</strong> Everything shown inside a snapshot was invented for
          development, and Replace restores are refused. Unset SNAPSHOTTER_FAKE_MOUNTS for the real
          thing.
        </p>
      )}
      {overview?.timeMachineWarning && <p className="banner warning">{overview.timeMachineWarning}</p>}

      {mountRefused ? (
        <div className="banner error">
          <p>
            <strong>macOS would not mount the snapshot.</strong> {mountHelp}
          </p>
          <button onClick={() => void Status.OpenPrivacySettings()}>
            Open Full Disk Access settings
          </button>
        </div>
      ) : (
        error && <p className="banner error">{error}</p>
      )}
      {status && (
        <p className="banner ok" onClick={() => setStatus("")}>
          {status}
        </p>
      )}

      <div className="body">
        <aside>
          {/* The way back to the machine itself. Once a snapshot is selected every
              tab reads as being about that snapshot, so leaving has to be as
              explicit as arriving was — the Health tab was already whole-machine,
              but nothing said so. */}
          <button
            className={`aside-home ${view === "home" ? "active" : ""}`}
            onClick={() => setView("home")}
          >
            Home
          </button>

          <div className="aside-head">
            <h2>Snapshots</h2>
            <span className="count">{snapshots.length}</span>
          </div>

          {snapshots.length === 0 && (
            <p className="aside-empty">
              None yet. Take one now, or set up a schedule so they are taken for you.
            </p>
          )}

          <ul className="snapshot-list">
            {snapshots.map((snapshot) => (
              <li
                key={snapshot.name}
                className={`${snapshot.name === selected ? "selected" : ""} ${snapshot.mounted ? "mounted" : ""}`}
                onClick={() => (setSelected(snapshot.name), setView("snapshots"))}
              >
                <div className="when">
                  <span className="dot" title={snapshot.mounted ? "Open" : "Not open"} />
                  <span>{stamp(snapshot.taken)}</span>
                </div>
                <div className="age">{age(snapshot.taken)}</div>
                <div className="row-actions">
                  {snapshot.mounted ? (
                    <button onClick={(e) => (e.stopPropagation(), act(() => Snapshots.Unmount([snapshot.name]), "Closed"))}>
                      Close
                    </button>
                  ) : (
                    <button onClick={(e) => (e.stopPropagation(), mount(snapshot))}>Open</button>
                  )}
                </div>
              </li>
            ))}
          </ul>

          {snapshots.some((s) => !s.mounted) && (
            <button className="wide" onClick={mountAll} disabled={busy}>
              Open all ({snapshots.filter((s) => !s.mounted).length})
            </button>
          )}
          {snapshots.some((s) => s.mounted) && (
            <button className="wide" onClick={() => act(() => Snapshots.UnmountAll(), "Closed every snapshot")} disabled={busy}>
              Close all
            </button>
          )}

          {/* Pushed to the bottom: these act on the machine, not on whichever
              snapshot happens to be selected above. */}
          <div className="aside-footer">
            <button
              className="wide primary"
              onClick={() => act(() => Snapshots.TakeNow(), "Snapshot taken")}
              disabled={busy}
            >
              Take a snapshot now
            </button>
            <button
              className={`wide ${view === "schedule" ? "active" : ""}`}
              onClick={() => {
                if (view === "schedule") {
                  setView(beforeOptions);
                } else {
                  setBeforeOptions(view);
                  setView("schedule");
                }
              }}
            >
              {/* "Options" rather than "Schedule": the panel already holds how often
                  snapshots are taken, what is kept, and the log, which is every
                  choice this application has. Naming it after one of them made the
                  other two hard to find. */}
              {view === "schedule" ? "Back" : "Options"}
            </button>
          </div>
        </aside>

        <main>
          {view === "schedule" ? (
            <Schedule onStatus={setStatus} />
          ) : view === "home" ? (
            /* The machine, not a snapshot: whether anything is taking them, how
               far back they reach, and what is wrong. It answers the question you
               open the application with. */
            <Health onStatus={setStatus} />
          ) : (
          <>
          <nav className="tabs">
            <button className={tab === "browse" ? "active" : ""} onClick={() => setTab("browse")}>
              Browse
            </button>
            <button className={tab === "compare" ? "active" : ""} onClick={() => setTab("compare")}>
              Compare
            </button>
            <button className={tab === "search" ? "active" : ""} onClick={() => setTab("search")}>
              Search
            </button>
          </nav>

          {tab === "browse" && (
            <Browser
              snapshot={current}
              path={path}
              onPathChange={setPath}
              onMount={() => current && mount(current)}
              onCompare={() => setTab("compare")}
              onStatus={setStatus}
            />
          )}
          {tab === "compare" && <Compare snapshot={current} path={path} onStatus={setStatus} />}
          {tab === "search" && <Search onStatus={setStatus} />}
          </>
          )}
        </main>
      </div>
    </div>
  );
}
