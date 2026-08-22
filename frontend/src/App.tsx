import { useCallback, useEffect, useState } from "react";
import { Snapshots, Browse, Status, message, type SnapshotView, type Overview } from "./api";
import { age, bytes, stamp } from "./format";
import { Browser } from "./Browser";
import { FileDiff } from "./FileDiff";
import { Schedule } from "./Schedule";
import { Health } from "./Health";
import { Search } from "./Search";
import { ThemeToggle } from "./ThemeToggle";
import { LanguagePicker } from "./LanguagePicker";
import { useTranslation } from "react-i18next";
import { useLiveRefresh } from "./live";
import { useAction } from "./useAction";
// The same file the application icon and the favicon are built from, reached out
// of the project's assets/ rather than copied into public/, so the mark has one
// home. Vite resolves and emits it at build time.
import iconUrl from "../../assets/icons/icon.svg";

/** The tabs that read a snapshot. Every one of them is about the snapshot
 *  selected in the sidebar, which is why health is no longer among them: it
 *  describes the machine and ignored the selection entirely, so as a tab it
 *  claimed to be about a snapshot and was not. It is the home view instead. */
// The compare tab is gone. It walked a tree and produced a list of paths that
// had changed, which says where to look and nothing about what is there — the
// question people actually had is answered per file, from the row itself.
type Tab = "browse" | "search";

/** Which of the three things the window can be showing. home and schedule are
 *  whole-machine; snapshots is the one that depends on what is selected. */
type View = "home" | "snapshots" | "schedule";

/**
 * The phrase that identifies a mount refused for want of Full Disk Access.
 *
 * Matched literally, and deliberately not through the catalogue. It was a
 * translation key, which meant the check compared a German phrase against an
 * English error message and never matched: for every language but English the
 * instructions and the settings button silently never appeared, and the reader
 * got the raw refusal with nothing to do about it.
 *
 * The error is produced by mountmgr.ErrNeedsFullDiskAccess and is English
 * whatever the window is set to, because it names a permission macOS itself only
 * names in English. That package has a test asserting the message still contains
 * this, so rewording it there fails there rather than silently here.
 */
const refusalMarker = "Full Disk Access";

export default function App() {
  const { t } = useTranslation();
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
  const { busy, error, setError, run } = useAction(setStatus);
  const [faking, setFaking] = useState(false);
  const [scenario, setScenario] = useState("");
  // mountHelp is fetched lazily, because it only means anything once mounting
  // has actually been refused.
  const [mountHelp, setMountHelp] = useState("");
  // The snapshot whose deletion has been asked about and not yet confirmed.
  const [confirming, setConfirming] = useState("");
  // The file whose contents are being compared, or none. Held here rather than
  // in the browser because the panel covers the whole view.
  const [diffFile, setDiffFile] = useState("");

  // A refusal to mount is not an ordinary error: it names a permission, and the
  // place to grant it is four levels into System Settings. Recognising it here
  // is what turns a dead end into something with a next step.
  const mountRefused = error.includes(refusalMarker);

  const refresh = useCallback(async () => {
    try {
      const next = await Snapshots.Overview();
      setOverview(next);
      setSelected((current) => current || next.snapshots[0]?.name || "");
      // Deliberately does NOT clear the error.
      //
      // This runs on a timer and whenever the window is looked at again, and
      // mounting raises an authorization dialog — so dismissing that dialog hands
      // focus back, this succeeds, and the reason the mount failed was wiped
      // before it could be read. It flashed red and vanished.
      //
      // A background read succeeding says nothing about whether the thing the
      // user asked for worked. The error stays until they start another action or
      // dismiss it.
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

  const act = (fn: () => Promise<unknown>, done: string) => run(fn, done, refresh);


  const mount = (snapshot: SnapshotView) =>
    act(() => Snapshots.Mount([snapshot.name]), `Opened the snapshot from ${stamp(snapshot.taken)}`);

  // Which snapshot has been asked about but not yet confirmed. One at a time, so
  // pressing Delete on a second row puts the first question away rather than
  // leaving two of them open.
  const remove = (snapshot: SnapshotView) => {
    setConfirming("");
    // By stamp, which is what the service deletes by. And the selection has to go
    // with it: leaving it pointing at a snapshot that no longer exists shows an
    // empty browser under a heading naming something that is gone.
    if (snapshot.name === selected) {
      setSelected("");
      setView("home");
    }
    return act(
      () => Snapshots.Delete(snapshot.stamp),
      t("app.deleted", { when: stamp(snapshot.taken) }),
    );
  };

  const mountAll = () =>
    act(
      () => Snapshots.Mount(snapshots.filter((s) => !s.mounted).map((s) => s.name)),
      t("app.openedEvery"),
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
            <span className="subtitle">{t("app.subtitle")}</span>
          </div>
        </div>
        <div className="header-actions">
          {overview && <DiskSpace free={overview.volumeFreeBytes} total={overview.volumeTotalBytes} />}
          <LanguagePicker />
            <ThemeToggle />
        </div>
      </header>

      {/* First banner, and the most important one: every figure below it is
          invented. Worded flatly rather than cheerfully — a reader who misses
          this draws conclusions about a machine that does not exist. */}
      {scenario && (
        <p className="banner error">
          <strong>{t("app.simulatedLead")}</strong> {t("app.scenario")} <code>{scenario}</code> is loaded. Every
          snapshot, schedule and figure on this screen was invented to drive the interface, and
          none of it describes this Mac.
        </p>
      )}
      {faking && (
        <p className="banner warning">
          <strong>{t("app.mountsSimulatedLead")}</strong> Everything shown inside a snapshot was invented for
          development, and Replace restores are refused. Unset SNAPSHOTTER_FAKE_MOUNTS for the real
          thing.
        </p>
      )}
      {overview?.timeMachineWarning && <p className="banner warning">{overview.timeMachineWarning}</p>}

      {mountRefused ? (
        <div className="banner error">
          <p>
            <strong>{t("app.macosRefused")}</strong> {mountHelp}
          </p>
          <button onClick={() => void Status.OpenPrivacySettings()}>
            {t("app.openFdaSettings")}
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
            {t("nav.home")}
          </button>

          <div className="aside-head">
            <h2>{t("nav.snapshots")}</h2>
            <span className="count">{snapshots.length}</span>
          </div>

          {snapshots.length === 0 && (
            <p className="aside-empty">
              {t("app.noneYet")}
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
                  <span className="dot" title={snapshot.mounted ? t("app.isOpen") : t("app.notOpen")} />
                  <span>{stamp(snapshot.taken)}</span>
                </div>
                <div className="age">{age(snapshot.taken, t)}</div>
                <div className="row-actions">
                  {/* Asked twice, because a snapshot cannot be recreated: it
                      records a state of the disk that has passed. Inline rather
                      than in a dialog — the row being asked about stays visible,
                      which is the whole question. */}
                  {confirming === snapshot.name ? (
                    <>
                      <button
                        className="destructive"
                        onClick={(e) => (e.stopPropagation(), remove(snapshot))}
                        disabled={busy}
                      >
                        {t("app.deleteConfirm")}
                      </button>
                      <button onClick={(e) => (e.stopPropagation(), setConfirming(""))}>
                        {t("app.deleteKeep")}
                      </button>
                    </>
                  ) : (
                    <>
                      {snapshot.mounted ? (
                        <button onClick={(e) => (e.stopPropagation(), act(() => Snapshots.Unmount([snapshot.name]), "Closed"))}>
                          {t("app.close")}
                        </button>
                      ) : (
                        <button onClick={(e) => (e.stopPropagation(), mount(snapshot))}>{t("app.open")}</button>
                      )}
                      {/* Offered but refused while it is open, rather than hidden:
                          the service will not delete a mounted snapshot, and a
                          button that is missing leaves the reader wondering
                          whether deleting is possible at all. */}
                      <button
                        title={snapshot.mounted ? t("app.closeToDelete") : t("app.deleteTitle")}
                        disabled={snapshot.mounted}
                        onClick={(e) => (e.stopPropagation(), setConfirming(snapshot.name))}
                      >
                        {t("app.delete")}
                      </button>
                    </>
                  )}
                </div>
              </li>
            ))}
          </ul>

          {snapshots.some((s) => !s.mounted) && (
            <button className="wide" onClick={mountAll} disabled={busy}>
              {t("app.openAll", { count: snapshots.filter((s) => !s.mounted).length })}
            </button>
          )}
          {snapshots.some((s) => s.mounted) && (
            <button className="wide" onClick={() => act(() => Snapshots.UnmountAll(), t("app.closedEvery"))} disabled={busy}>
              {t("app.closeAll")}
            </button>
          )}

          {/* Pushed to the bottom: these act on the machine, not on whichever
              snapshot happens to be selected above. */}
          <div className="aside-footer">
            <button
              className="wide primary"
              onClick={() => act(() => Snapshots.TakeNow(), t("app.snapshotTaken"))}
              disabled={busy}
            >
              {t("app.takeSnapshotNow")}
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
              {/* t("app.options") rather than "Schedule": the panel already holds how often
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
              {t("nav.browse")}
            </button>
            <button className={tab === "search" ? "active" : ""} onClick={() => setTab("search")}>
              {t("nav.search")}
            </button>
          </nav>

          {tab === "browse" && (
            <Browser
              snapshot={current}
              path={path}
              onPathChange={setPath}
              onMount={() => current && mount(current)}
              onDiff={(livePath) => setDiffFile(livePath)}
              onStatus={setStatus}
            />
          )}

          {/* Over the browser rather than beside it: a diff wants the width, and
              it is opened for one file and closed again. */}
          {diffFile && current && (
            <FileDiff
              snapshot={current.name}
              livePath={diffFile}
              snapshots={snapshots}
              dark={darkNow()}
              onClose={() => setDiffFile("")}
            />
          )}
          {tab === "search" && (
            <Search onStatus={setStatus} snapshot={current} path={path} />
          )}
          </>
          )}
        </main>
      </div>
    </div>
  );
}

/**
 * Free space, as a bar and a number.
 *
 * Snapshots are purgeable: macOS reclaims the oldest under space pressure rather
 * than failing a write, so a disk filling up is the quiet way a retention setting
 * stops being kept. That is worth seeing at a glance rather than reading, which a
 * number alone cannot do.
 *
 * The colour is the glance and the number is the detail. The thresholds match the
 * health screen's — below a tenth is what it calls low — so the two cannot
 * disagree about whether this Mac is running out.
 */
function DiskSpace({ free, total }: { free: number; total: number }) {
  if (!total) return null;

  const freeRatio = free / total;
  const level = freeRatio < 0.1 ? "bad" : freeRatio < 0.2 ? "warn" : "ok";

  return (
    <span
      className={`disk ${level}`}
      title={`${bytes(free)} free of ${bytes(total)} — ${Math.round(freeRatio * 100)}% free`}
    >
      {/* The bar fills with what is USED, because one that empties as things get
          worse reads backwards: a full bar looks like a full disk. */}
      <span className="disk-bar" aria-hidden="true">
        <span className="disk-used" style={{ width: `${Math.min(100, (1 - freeRatio) * 100)}%` }} />
      </span>
      <span className="disk-text">{bytes(free)} free</span>
    </span>
  );
}

/**
 * Whether the window is currently dark, for anything that has to be told rather
 * than inheriting it from CSS.
 *
 * The diff viewer takes a boolean, so the answer is read off the root element —
 * which is where applyTheme stamps it, and which follows the system when no
 * choice has been made.
 */
function darkNow(): boolean {
  const chosen = document.documentElement.getAttribute("data-theme");
  if (chosen) return chosen === "dark";
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? true;
}
