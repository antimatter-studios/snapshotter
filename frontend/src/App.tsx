import { useCallback, useEffect, useRef, useState } from "react";
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
import type { TFunction } from "i18next";
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
  // Which volume the selected snapshot is on. Held beside the name because the
  // name does not identify a copy: the same date exists on every volume mounted
  // when it was taken, and both can be open at once.
  const [selectedDevice, setSelectedDevice] = useState<string>("");
  // Where the browser is, and the volume that path belongs to, held together.
  //
  // They were two states and could disagree for one render. Selecting a snapshot
  // on another volume set the device immediately while the path still pointed at
  // the last volume's home folder, so the browser asked for a home directory
  // inside an SD card's snapshot and was told — correctly — that it is not on
  // that volume. The listing then arrived a moment later and the error stayed on
  // screen, describing a question nobody had asked.
  //
  // An empty path means "not resolved yet", which is a state the browser can
  // show rather than a pairing it can get wrong.
  const [at, setAt] = useState<{ device: string; path: string }>({ device: "", path: "" });
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
  // Grouped by the disk they are on. `tmutil localsnapshot` takes no arguments
  // and writes to every mounted APFS volume at once, so a flat list of one
  // volume's copies was most of a machine's snapshots missing — with no way to
  // see the ones on an external disk at all.
  //
  // Falls back to a single unnamed group, so a service that cannot enumerate the
  // volumes still shows the startup disk's list rather than nothing.
  const groups =
    overview?.volumes?.length
      ? overview.volumes
      : [{ name: "", mountPoint: "", device: "", isStartupDisk: true, snapshots, freeBytes: 0, totalBytes: 0 }];
  const total = groups.reduce((n, g) => n + g.snapshots.length, 0);
  // Identity of one COPY, for the row key and the delete confirmation. The name
  // repeats across volumes, so confirming by name alone would arm every volume's
  // row for that date at once.
  const copyID = (device: string, name: string) => `${device}/${name}`;

  // What each row is doing, so waiting, success and failure are all visible.
  //
  // Mounting raises an authorization prompt and then attaches a filesystem, so it
  // is the slowest thing here by a wide margin — and it reported nothing at all
  // until it was over, when a grey dot quietly turned green. Silence during the
  // slow part reads as a click that did not register, which is how somebody comes
  // to press it twice and answer two password prompts for one intention.
  const [progress, setProgress] = useState<Record<string, "working" | "ok" | "failed">>({});
  // The timers that clear a finished mark. Held so a row asked about twice does
  // not keep an older timer that would wipe the newer mark early, and so nothing
  // fires into a component that has gone.
  const clearTimers = useRef<Record<string, number>>({});

  useEffect(() => {
    const timers = clearTimers.current;
    return () => {
      for (const id of Object.values(timers)) window.clearTimeout(id);
    };
  }, []);

  // Long enough to be read, short enough that the row goes back to saying what is
  // true rather than what just happened.
  const markFor = 5000;

  const settle = (id: string, outcome: "ok" | "failed") => {
    setProgress((p) => ({ ...p, [id]: outcome }));
    window.clearTimeout(clearTimers.current[id]);
    clearTimers.current[id] = window.setTimeout(() => {
      setProgress((p) => {
        const next = { ...p };
        delete next[id];
        return next;
      });
      delete clearTimers.current[id];
    }, markFor);
  };

  // Wraps one row's action so the row says what is happening to it. The outcome
  // is marked before anything is rethrown, so a failure shows on the row as well
  // as in the banner.
  const watched = async (ids: string[], fn: () => Promise<unknown>) => {
    setProgress((p) => {
      const next = { ...p };
      for (const id of ids) {
        window.clearTimeout(clearTimers.current[id]);
        delete clearTimers.current[id];
        next[id] = "working";
      }
      return next;
    });
    try {
      const out = await fn();
      for (const id of ids) settle(id, "ok");
      return out;
    } catch (err) {
      for (const id of ids) settle(id, "failed");
      throw err;
    }
  };

  const working = Object.values(progress).some((s) => s === "working");

  // Slow work, counted, along the bottom of the window.
  //
  // The application had one honest way to say it was busy — a disabled button —
  // and several kinds of work that take long enough to look like a freeze. A
  // folder's verdict can be a walk of everything beneath it, so a listing of
  // source trees sat on a column of "detecting…" that never changed. A meter that
  // says 245/567 is the difference between waiting and giving up: it does not
  // make the work faster, it makes it legible.
  //
  // A bar rather than a spinner, because the count is knowable. A spinner says
  // "something is happening"; this says how much is left.
  const [slowWork, setSlowWork] = useState<{ label: string; done: number; total: number } | null>(null);

  // Selecting a snapshot on another volume means browsing that volume, so where
  // browsing starts moves with it: a home directory on the startup disk, and the
  // volume's own root anywhere else, since another disk has no home directory and
  // starting at one would open an empty listing that reads as an empty snapshot.
  useEffect(() => {
    let live = true;
    Browse.Home(selectedDevice)
      .then((home) => live && setAt({ device: selectedDevice, path: home }))
      // Said, not swallowed. The service answers the startup disk's home folder
      // or the volume's own root, and nothing else — so an error here means the
      // volume could not be identified, and quietly leaving the browser where it
      // was would show one disk's files under another disk's heading.
      .catch((err) => live && setError(message(err)));
    return () => {
      live = false;
    };
  }, [selectedDevice, setError]);
  // Across every group, not the startup disk's list: a snapshot on another volume
  // is selectable too, and looking it up in one volume's list would find nothing
  // and leave the browser showing the last thing that was open.
  const current =
    groups.flatMap((g) => g.snapshots).find((s) => s.name === selected && s.device === selectedDevice) ??
    snapshots.find((s) => s.name === selected) ??
    null;

  const act = (fn: () => Promise<unknown>, done: string) => run(fn, done, refresh);


  // The volume as well as the name: a date is not an identity, since every volume
  // mounted when a snapshot was taken has one of that date, and each is a
  // separate thing to attach.
  const mount = (snapshot: SnapshotView) =>
    act(
      () => watched([copyID(snapshot.device, snapshot.name)], () => Snapshots.Mount(snapshot.device, [snapshot.name])),
      t("app.opened", { when: stamp(snapshot.taken) }),
    );

  // Which snapshot has been asked about but not yet confirmed. One at a time, so
  // pressing Delete on a second row puts the first question away rather than
  // leaving two of them open.
  const remove = (snapshot: SnapshotView) => {
    setConfirming("");
    // The selection has to go with it: leaving it pointing at a snapshot that no
    // longer exists shows an empty browser under a heading naming something that
    // is gone. Only the startup disk's rows are ever selected, so only they can
    // be the one being pointed at.
    if (snapshot.device === selectedDevice && snapshot.name === selected) {
      setSelected("");
      setSelectedDevice("");
      setView("home");
    }
    // By volume and identifier, not by date. The same date exists on every volume
    // mounted when it was taken, and deleting by date removes all of them — so a
    // button beside one row would silently take snapshots that were not on screen.
    return act(
      () => Snapshots.Delete(snapshot.device, snapshot.uuid, snapshot.stamp),
      t("app.deleted", { when: stamp(snapshot.taken) }),
    );
  };

  // One call per volume, because each raises its own authorization prompt and
  // each attaches into its own directory. Opening everything on a machine with
  // two disks is two prompts, which is honest: they are two mounts.
  const mountAll = () =>
    act(async () => {
      for (const group of groups) {
        const closed = group.snapshots.filter((s) => !s.mounted);
        if (!closed.length) continue;
        await watched(
          closed.map((s) => copyID(s.device, s.name)),
          () => Snapshots.Mount(group.device, closed.map((s) => s.name)),
        );
      }
    }, t("app.openedEvery"));

  return (
    <div className="app">
      {working && (
        // Over the window rather than in the sidebar, because the sidebar row is
        // small and this is the moment the application looks frozen. It names the
        // password prompt: the wait is mostly macOS asking, and somebody who does
        // not know that is watching a spinner for no stated reason.
        <div className="working-overlay" role="status" aria-live="polite">
          <span className="spinner" aria-hidden="true" />
          <div>
            <strong>{t("app.opening")}</strong>
            <span>{t("app.openingExplain")}</span>
          </div>
        </div>
      )}
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
            <span className="count">{total}</span>
          </div>

          {total === 0 && (
            <p className="aside-empty">
              {t("app.noneYet")}
            </p>
          )}

          {/* One scrolling region holding every group.

              It used to be the list itself, which worked while there was exactly
              one. Grouping put a wrapper between the column and the list, so the
              scrolling element was no longer the flex child that had to shrink —
              the column grew to fit every group instead, and the footer was
              pushed past the sidebar's own overflow:hidden and out of sight. */}
          <div className="snapshot-scroll">
          {groups.map((group) => (
            <div className="volume-group" key={group.device || "startup"}>
              {/* Headed only when there is more than one. A single disk needs no
                  label saying which disk, and adding one would make every machine
                  look like it had something to disambiguate. */}
              {groups.length > 1 && (
                <div className="volume-head" title={`${group.mountPoint} (${group.device})`}>
                  <span className="volume-name">{group.name || group.mountPoint}</span>
                  <span className="count">{group.snapshots.length}</span>
                </div>
              )}
              {/* Said once per group rather than per row: every row in it is the
                  same, and repeating it would bury the dates it sits beside. */}
              {!group.isStartupDisk && groups.length > 1 && (
                <p className="volume-note">{t("app.otherVolumeNote")}</p>
              )}

          <ul className="snapshot-list">
            {group.snapshots.map((snapshot) => (
              <li
                key={copyID(snapshot.device, snapshot.name)}
                className={`${snapshot.name === selected && snapshot.device === selectedDevice ? "selected" : ""} ${snapshot.mounted ? "mounted" : ""}`}
                onClick={() => (setSelected(snapshot.name), setSelectedDevice(snapshot.device), setView("snapshots"))}
              >
                <div className="when">
                  <RowState
                    state={progress[copyID(snapshot.device, snapshot.name)]}
                    mounted={snapshot.mounted}
                    t={t}
                  />
                  <span>{stamp(snapshot.taken)}</span>
                </div>
                <div className="age">{age(snapshot.taken, t)}</div>
                <div className="row-actions">
                  {/* Asked twice, because a snapshot cannot be recreated: it
                      records a state of the disk that has passed. Inline rather
                      than in a dialog — the row being asked about stays visible,
                      which is the whole question. */}
                  {confirming === copyID(snapshot.device, snapshot.name) ? (
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
                      {/* Every volume, now that the privileged helper builds its
                          allowlist from the machine rather than from a constant.
                          Browsing what is inside is still the startup disk's
                          alone — the browser is rooted at a home directory — so
                          another volume's snapshot opens and says where. */}
                      {(snapshot.mounted ? (
                          <button onClick={(e) => (e.stopPropagation(), act(() => watched([copyID(snapshot.device, snapshot.name)], () => Snapshots.Unmount(snapshot.device, [snapshot.name])), t("app.closed")))}>
                            {t("app.close")}
                          </button>
                        ) : (
                          <button onClick={(e) => (e.stopPropagation(), mount(snapshot))}>{t("app.open")}</button>
                        ))}
                      {/* Offered but refused while it is open, rather than hidden:
                          the service will not delete a mounted snapshot, and a
                          button that is missing leaves the reader wondering
                          whether deleting is possible at all. */}
                      <button
                        title={snapshot.mounted ? t("app.closeToDelete") : t("app.deleteTitle")}
                        disabled={snapshot.mounted}
                        onClick={(e) => (e.stopPropagation(), setConfirming(copyID(snapshot.device, snapshot.name)))}
                      >
                        {t("app.delete")}
                      </button>
                    </>
                  )}
                </div>
              </li>
            ))}
          </ul>
            </div>
          ))}
          </div>

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
              path={at.device === selectedDevice ? at.path : ""}
              onPathChange={(next) => setAt({ device: selectedDevice, path: next })}
              onMount={() => current && mount(current)}
              onDiff={(livePath) => setDiffFile(livePath)}
              onStatus={setStatus}
              onProgress={setSlowWork}
            />
          )}

          {/* Over the browser rather than beside it: a diff wants the width, and
              it is opened for one file and closed again. */}
          {diffFile && current && (
            <FileDiff
              device={selectedDevice}
              snapshot={current.name}
              livePath={diffFile}
              snapshots={snapshots}
              dark={darkNow()}
              onClose={() => setDiffFile("")}
            />
          )}
          {tab === "search" && (
            <Search onStatus={setStatus} snapshot={current} path={at.device === selectedDevice ? at.path : ""} />
          )}
          </>
          )}
          {/* Always, whether anything is happening or not.

              It used to appear only while there was something to count, which
              meant it was missing at exactly the moment it was most wanted: the
              window sitting still, apparently doing nothing, with no way to tell
              waiting from finished. A bar that comes and goes is also a bar that
              moves the content under it.

              Inside main, so it spans the panel the work belongs to and stops at
              the sidebar. main is already positioned for the file-diff panel,
              and for the same reason: absolute inside it stays inside it, where
              fixed would escape to the viewport and cover the snapshot list too. */}
          <div className="status-bar" role="status" aria-live="polite">
            {/* What is happening now beats what happened last: a message about a
                finished restore is not what somebody staring at a still window
                wants to read. */}
            <span className="status-bar-label">
              {slowWork?.label || status || t("app.ready")}
            </span>
            {/* The bar and the count only when there is something countable.
                Some of the work has no number — reading a directory is one
                operation, however long it takes — and a bar that cannot fill is
                worse than no bar. */}
            {slowWork && slowWork.total > 0 && (
              <>
                <span className="status-bar-track" aria-hidden="true">
                  <span
                    className="status-bar-fill"
                    style={{ width: `${Math.round((slowWork.done / slowWork.total) * 100)}%` }}
                  />
                </span>
                {/* Centred over the bar, because the number is the thing being
                    read and a count off to one side is read second. */}
                <span className="status-bar-count">
                  {t("app.progressOf", { done: slowWork.done, total: slowWork.total })}
                </span>
              </>
            )}
          </div>
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
  const { t } = useTranslation();
  if (!total) return null;

  const freeRatio = free / total;
  const level = freeRatio < 0.1 ? "bad" : freeRatio < 0.2 ? "warn" : "ok";

  return (
    <span
      className={`disk ${level}`}
      title={t("app.diskFree", {
        free: bytes(free),
        total: bytes(total),
        percent: Math.round(freeRatio * 100),
      })}
    >
      {/* The bar fills with what is USED, because one that empties as things get
          worse reads backwards: a full bar looks like a full disk. */}
      <span className="disk-bar" aria-hidden="true">
        <span className="disk-used" style={{ width: `${Math.min(100, (1 - freeRatio) * 100)}%` }} />
      </span>
      <span className="disk-text">{t("app.freeSpace", { size: bytes(free) })}</span>
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

/**
 * What one row is doing: waiting, just succeeded, just failed, or simply open.
 *
 * The dot alone could only say open or not, so the slow part said nothing and the
 * failed part said nothing either — a mount that was refused looked exactly like
 * one nobody had clicked. Waiting is a spinner, and an outcome is held long
 * enough to be read before the row goes back to reporting what is true.
 */
function RowState({
  state,
  mounted,
  t,
}: {
  state?: "working" | "ok" | "failed";
  mounted: boolean;
  t: TFunction;
}) {
  if (state === "working") {
    return <span className="dot-spinner" role="status" aria-label={t("app.working")} />;
  }
  if (state === "ok") {
    return (
      <span className="mark ok" role="status" aria-label={t("app.succeeded")} title={t("app.succeeded")}>
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 8.5l3 3 6-7" /></svg>
      </span>
    );
  }
  if (state === "failed") {
    return (
      <span className="mark failed" role="status" aria-label={t("app.failed")} title={t("app.failed")}>
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
      </span>
    );
  }
  return <span className="dot" title={mounted ? t("app.isOpen") : t("app.notOpen")} />;
}
