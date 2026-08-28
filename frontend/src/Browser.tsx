import { useCallback, useEffect, useRef, useState } from "react";
import { Browse, Restore, message, type MergedListing, type Change, type SnapshotView } from "./api";
import { bytes, breadcrumbs, stamp } from "./format";
import { FileIcon } from "./FileIcon";
import { StatusIcon } from "./StatusIcon";
import { useTranslation } from "react-i18next";


interface Props {
  snapshot: SnapshotView | null;
  path: string;
  onPathChange: (path: string) => void;
  onMount: () => void;
  /** Opens the line-by-line view of one file. */
  onDiff: (livePath: string) => void;
  onStatus: (text: string) => void;
  /** Reports how far the folder checks have got, or null when there is nothing
   *  in flight. The window shows it; this only counts. */
  onProgress?: (p: { label: string; done: number; total: number } | null) => void;
}

/**
 * Browser shows one folder from the snapshot and from the live disk at once.
 *
 * Every row carries its own verdict, which is the answer to the question that
 * brings someone here: what is in this folder that is no longer on disk.
 */
export function Browser({ snapshot, path, onPathChange, onMount, onDiff, onStatus, onProgress }: Props) {
  const { t } = useTranslation();
  const [listing, setListing] = useState<MergedListing | null>(null);
  const [showIdentical, setShowIdentical] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  // Folder verdicts arrive one per call, keyed by absolute path. Held separately
  // from the listing so a resolved verdict survives the listing being refreshed,
  // and so a slow folder never delays the rows around it.
  const [folderStatus, setFolderStatus] = useState<Record<string, string>>({});
  // Why a folder could not be answered for, keyed the same way. Shown in the
  // row's tooltip so "could not check" says what stopped it without anyone
  // having to find a log.
  const [folderWhy, setFolderWhy] = useState<Record<string, string>>({});
  // Rises on every listing, so answers from an abandoned one are discarded
  // rather than landing on the folder that replaced it.
  const resolveToken = useRef(0);
  // Which folder rows are actually on screen, kept by an IntersectionObserver.
  //
  // A ref rather than state: it is read inside the checking loop, and as state it
  // would be whatever it was when the listing started — and re-rendering the whole
  // table every time a row scrolled past would cost more than the ordering saves.
  const onScreen = useRef<Set<string>>(new Set());
  // The verdicts as the checking loop needs to see them, for the same reason.
  const priorStatus = useRef<Record<string, string>>({});
  // One observer for the whole table, kept across renders. Rows register as they
  // mount and drop out as they unmount.
  const watcher = useRef<IntersectionObserver | null>(null);

  useEffect(() => {
    // jsdom has no IntersectionObserver. Guarded rather than polyfilled: without
    // it every folder simply ranks as off-screen, which is the ordering this had
    // before, so the tests exercise the fallback rather than failing on it.
    if (typeof IntersectionObserver === "undefined") return;
    watcher.current = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const path = (entry.target as HTMLElement).dataset.path;
          if (!path) continue;
          if (entry.isIntersecting) onScreen.current.add(path);
          else onScreen.current.delete(path);
        }
      },
      // A margin, so a folder just below the fold is already answered by the time
      // it is scrolled to rather than starting its walk at that moment.
      { rootMargin: "200px" },
    );
    return () => {
      watcher.current?.disconnect();
      watcher.current = null;
    };
  }, []);

  // Attached to every folder row. The path rides on the element, because the
  // observer hands back nodes rather than the data they were built from.
  const watchRow = useCallback((node: HTMLTableRowElement | null, path: string) => {
    if (!node || !watcher.current) return;
    node.dataset.path = path;
    watcher.current.observe(node);
  }, []);

  const load = useCallback(async () => {
    if (!snapshot?.mounted) return;
    // An empty path means the volume's starting folder has not been resolved
    // yet. Listing against it would pair a snapshot with a path belonging to
    // whichever volume was selected before, which is a question about neither.
    if (!path) return;

    // Everything down to the first await happens in the same tick as the click,
    // so the window reacts before anything is read.
    //
    // It used to react when the new listing arrived, which on a slow disk was
    // eight to ten seconds after the click — and for all of that time the folder
    // somebody had just left was still on screen. Nothing said the click had
    // landed, so the application read as hung rather than as busy. Showing an
    // empty folder immediately and filling it in late is the same wait and a
    // completely different experience: people will forgive a slow answer, but not
    // a control that appears not to work.
    const token = ++resolveToken.current;
    setListing(null);
    setBusy(true);
    setError("");
    // Said before anything is read, because this is the stretch that used to look
    // like nothing happening: the rows are gone, the new ones have not arrived,
    // and on a slow disk that is seconds. No number, because reading a directory
    // is one operation however long it takes.
    onProgress?.({ label: t("app.readingFolder"), done: 0, total: 0 });

    // Before the listing is asked for, not after.
    //
    // This was the other half of those eight seconds, and it was not perception:
    // the folder being left still had a dozen walks running, and the two directory
    // reads for the new one queued behind them at the disk. Giving up first is
    // what makes the new folder's listing the next thing the disk does.
    // Caught, because this sits before the try below and a rejection here would
    // escape load() altogether — and load() is called from an effect, where
    // nothing is waiting to catch it. Giving up on the old walks is also not
    // something worth stopping for: the worst case is that they run on a little
    // longer, which is what happened before this call existed.
    await Browse.AbandonFolderChecks().catch(() => {});
    if (token !== resolveToken.current) return;

    try {
      const merged = await Browse.Merged(snapshot?.device ?? "", snapshot.name, path, showIdentical);
      if (token !== resolveToken.current) return;
      setListing(merged);

      // Each folder is asked about on its own and fills in when it answers, a few
      // at a time.
      //
      // Not all at once: a folder whose contents are unchanged costs a full walk
      // to prove it, and firing every row together turned a listing of the home
      // directory — which contains Library and whole source trees — into six
      // cores of simultaneous walking for the best part of a minute. A small
      // queue turns the same work into a trickle that finishes just as soon and
      // leaves the machine usable while it does.
      const folders = (merged.rows ?? []).filter((row) => row.isDir);
      // Settled by the lookup below, and skipped by the passes after it.
      const answered = new Set<string>();

      // What is already recorded, before anything else is asked.
      //
      // Three sources, in order of what they cost: a lookup, then the event log,
      // then reading the tree. This is the lookup — a verdict reached a moment
      // ago, or a recorded difference under the folder that one stat confirms.
      //
      // No progress meter: it is a lookup per row and finishes before a bar could
      // draw. The meter exists to explain waiting, and there is none here.
      for (const folder of folders) {
        if (token !== resolveToken.current) return;
        try {
          const known = await Browse.KnownDirectoryStatus(snapshot?.device ?? "", snapshot.name, folder.absLive);
          if (known.status && known.status !== "notExamined") {
            priorStatus.current = { ...priorStatus.current, [folder.absLive]: known.status };
            setFolderStatus((current) => ({ ...current, [folder.absLive]: known.status }));
            if (known.why) setFolderWhy((current) => ({ ...current, [folder.absLive]: known.why! }));
            answered.add(folder.absLive);
          }
        } catch {
          // Nothing known is the ordinary case, not a failure.
        }
      }

      // Nothing left to find out. The event log and the walks below both exist to
      // answer folders nothing is known about, so with none of those there is no
      // reason to read anything at all.
      if (folders.every((f) => answered.has(f.absLive))) {
        onProgress?.(null);
        return;
      }


      // Second: what macOS already remembers about this volume.
      //
      // It reads no trees at all — it asks the system where things were written
      // since we last looked and verifies only those. Anything it confirms is
      // recorded, and the folder checks below then answer from it in a stat
      // rather than a walk. It can only ever find changes; a log that says
      // nothing leaves every folder exactly as unknown as it was.
      onProgress?.({ label: t("browser.checkingFromLog"), done: 0, total: 0 });
      await Browse.ScanEventLog(snapshot?.device ?? "", snapshot.name).catch(() => null);
      if (token !== resolveToken.current) return;

      // How many at once, asked of the disk rather than assumed. This was three
      // for every volume, which is more than an SD card's one slow channel can
      // use and a small fraction of what internal storage wants.
      const lanes = await Browse.Lanes(snapshot?.device ?? "").catch(() => 4);

      // Taken by priority rather than in listing order, because listing order is
      // alphabetical and the reader is looking at the top of their own screen.
      //
      //  1. What is on screen. In a home directory of 125 folders, about eight are
      //     visible; the other 117 were being proved unchanged before the reader
      //     got an answer about anything they could see.
      //  2. Folders nothing is known about yet.
      //  3. Ones that were identical last time, which are the expensive ones —
      //     there is no early exit from proving a negative.
      //  4. Ones that differed last time, last. A folder that differed almost
      //     certainly still differs, so re-proving it is the least urgent work
      //     here and it is also the cheapest to answer when its turn comes.
      // Folders the lookup already settled are not asked about again.
      const waiting = new Set(folders.filter((f) => !answered.has(f.absLive)).map((f) => f.absLive));
      const byPath = new Map(folders.map((f) => [f.absLive, f]));
      const rank = (p: string) => {
        const prior = priorStatus.current[p];
        if (!prior || prior === "detecting") return 0;
        if (prior === "same" || prior === "ignored") return 1;
        return 2;
      };
      const take = () => {
        let best: string | null = null;
        let bestKey = Infinity;
        for (const p of waiting) {
          // On screen beats everything: a visible folder that differed last time
          // is still worth answering before an off-screen one nothing is known
          // about.
          const key = (onScreen.current.has(p) ? 0 : 10) + rank(p);
          if (key < bestKey) {
            bestKey = key;
            best = p;
            if (key === 0) break;
          }
        }
        if (best !== null) waiting.delete(best);
        return best;
      };

      // Counted so the window can say how far along this is. A folder's verdict
      // can be a full walk of everything beneath it, so a listing of source trees
      // takes a while — and until it said so, the only evidence it was working
      // was a column of "detecting…" that never changed, which reads as frozen.
      let done = 0;
      const total = waiting.size;
      onProgress?.({ label: t("browser.checkingByDisk"), done, total });
      const worker = async () => {
        for (;;) {
          // Before taking a row as well as after answering one. Discarding the
          // answers was never enough on its own: the calls kept being made, so a
          // listing of 125 folders went on asking about all of them long after
          // nobody was looking at any.
          if (token !== resolveToken.current) return;
          const path = take();
          if (path === null) return;
          const row = byPath.get(path)!;
          try {
            const verdict = await Browse.DirectoryStatus(snapshot?.device ?? "", snapshot.name, row.absLive);
            // Dropped if the listing moved on: answers about a folder nobody is
            // looking at any more are worse than useless, because they would
            // overwrite the ones for the folder they are.
            if (token !== resolveToken.current) return;
            // Mirrored into the ref as well as the state, because the ordering
            // above runs inside this closure and would otherwise be reading the
            // statuses as they were when the listing started.
            priorStatus.current = { ...priorStatus.current, [row.absLive]: verdict.status };
            setFolderStatus((current) => ({ ...current, [row.absLive]: verdict.status }));
            if (verdict.why) {
              setFolderWhy((current) => ({ ...current, [row.absLive]: verdict.why! }));
            }
          } catch {
            // Left as detecting rather than guessed at.
          } finally {
            done++;
            if (token === resolveToken.current) {
              onProgress?.({ label: t("browser.checkingByDisk"), done, total });
            }
          }
        }
      };
      await Promise.all(
        Array.from({ length: Math.max(1, Math.min(lanes, folders.length)) }, worker),
      );
      // Cleared only by the listing that owns it, so a slow one finishing after a
      // newer one started does not wipe the newer one's meter.
      if (token === resolveToken.current) onProgress?.(null);
    } catch (err) {
      setError(message(err));
      setListing(null);
      // The bar is always on screen, so a failed listing must not leave it
      // saying "Reading folder" for ever. The error itself is shown above it.
      if (token === resolveToken.current) onProgress?.(null);
    } finally {
      setBusy(false);
    }
  }, [snapshot, path, showIdentical, onProgress, t]);

  useEffect(() => {
    // Nothing awaits an effect, so anything load() lets escape becomes an
    // unhandled rejection. It reports its own failures on screen; this is the
    // backstop for the ones it cannot.
    void load().catch(() => {});
    // Leaving the browser entirely is the same as navigating within it: whatever
    // is still being walked is for a listing nobody is looking at.
    return () => {
      resolveToken.current++;
      // void does not catch. An unmount is the one moment nothing is left to
      // handle a rejection, so this says so explicitly.
      void Browse.AbandonFolderChecks().catch(() => {});
    };
  }, [load]);

  if (!snapshot) {
    return <Empty title={t("browser.noSnapshotSelected")} detail={t("browser.pickOne")} />;
  }

  if (!snapshot.mounted) {
    return (
      <Empty
        title={t("browser.notOpenYet")}
        detail={t("browser.openExplain")}
      >
        <button className="primary" onClick={onMount}>
          {t("browser.openSnapshot")}
        </button>
      </Empty>
    );
  }

  const restore = async (row: Change, replace: boolean) => {
    try {
      const result = await Restore.Restore({
        snapshot: snapshot.name,
        // Restoring from the copy on the volume being browsed. The same date on
        // another disk is a different file, and writing it here would put one
        // disk's contents over another's.
        device: snapshot.device,
        livePath: row.absLive,
        replace,
      });
      onStatus(`Restored to ${result.destination}${result.backedUp ? ` (previous file kept at ${result.backedUp})` : ""}`);
      // Fire and forget, so it needs its own catch: the try around this only
      // covers what is awaited.
      void load().catch(() => {});
    } catch (err) {
      setError(message(err));
    }
  };

  // Identical folders are hidden here rather than by the listing, which cannot
  // know: a folder arrives unexamined and only becomes "same" once its own walk
  // answers, long after the rows were built. Files are already filtered on the
  // way out of Merged.
  //
  // Computed once so the table below and the "nothing has changed" message
  // cannot disagree about whether anything is showing.
  const visibleRows = (listing?.rows ?? []).filter(
    (row) => showIdentical || !row.isDir || (folderStatus[row.absLive] ?? "detecting") !== "same",
  );

  return (
    <div className="browser">
      <div className="toolbar">
        <nav className="crumbs">
          {breadcrumbs(path, listing?.root ?? "/").map((crumb, i, all) => (
            <span key={crumb.path}>
              <button className="crumb" onClick={() => onPathChange(crumb.path)}>
                {crumb.label}
              </button>
              {i < all.length - 1 && <span className="sep">›</span>}
            </span>
          ))}
        </nav>
        <div className="toolbar-actions">
          <label className="check">
            <input type="checkbox" checked={showIdentical} onChange={(e) => setShowIdentical(e.target.checked)} />
            {t("browser.showIdentical")}
          </label>
          <button onClick={() => Browse.RevealInFinder(snapshot?.device ?? "", snapshot.name, path).catch((e) => setError(message(e)))}>
            {t("browser.revealInFinder")}
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {listing?.note && <p className="note">{listing.note}</p>}

      <table className="rows">
        <thead>
          <tr>
            <th>{t("browser.colName")}</th>
            <th>{t("browser.colStatus")}</th>
            <th className="num">{t("browser.colInSnapshot")}</th>
            <th className="num">{t("browser.colOnDisk")}</th>
            <th>{t("browser.colModified")}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {visibleRows.map((row) => {
            // A folder comes back unexamined and is resolved on its own, so it
            // reads as detecting until its answer arrives.
            const status = row.isDir ? folderStatus[row.absLive] ?? "detecting" : row.status;
            return (
            // Prefixed, because a bare status word is a class in the same global
            // namespace as every component's own classes — and "ignored" was
            // already the tripwire's panel of silenced folders, whose rules put a
            // border on this row and a box around its name. Nothing styles the row
            // by status today; the prefix is so that nothing can collide with one
            // by accident tomorrow.
            <tr
              key={row.relPath}
              className={`row-${status}`}
              ref={row.isDir ? (node) => watchRow(node, row.absLive) : undefined}
            >
              <td>
                <span className="name-cell">
                  <FileIcon name={row.relPath} isDir={row.isDir} />
                  {row.isDir ? (
                    <button className="link" onClick={() => onPathChange(row.absLive)}>
                      {row.relPath}/
                    </button>
                  ) : (
                    <span>{row.relPath}</span>
                  )}
                </span>
              </td>
              <td>
                <span className={`badge ${status}`} title={row.isDir ? folderWhy[row.absLive] : undefined}>
                  {/* Only while detecting. notExamined shares this badge's quiet
                      colour but is a finished answer, and a spinner on it would
                      promise a result that is never coming. */}
                  {status === "detecting" ? (
                    <span className="spinner" aria-hidden="true" />
                  ) : (
                    <StatusIcon status={status} />
                  )}
                  {/* The catalogue is keyed by the same names the service uses, so
                      a status added in Go needs a key rather than a branch here.
                      statusLabel remains the English registry the icon test reads. */}
                  {t(`status.${status}` as never) ?? status}
                </span>
              </td>
              <td className="num">{status === "onlyOnDisk" ? "—" : bytes(row.snapSize)}</td>
              <td className="num">{status === "onlyInSnapshot" ? "—" : bytes(row.liveSize)}</td>
              <td>{stamp(row.snapModTime || row.liveModTime)}</td>
              <td className="actions">
                {/* Files only: a folder has no lines to compare. Offered whatever
                    the verdict, because a file that exists on one side alone is
                    still worth seeing as a whole side added or removed. */}
                {!row.isDir && (
                  <button onClick={() => onDiff(row.absLive)} title={t("browser.compareTitle")}>
                    {t("browser.compare")}
                  </button>
                )}
                {status !== "onlyOnDisk" && (
                  <>
                    <button onClick={() => restore(row, false)} title={t("browser.restoreCopyTitle")}>
                      {t("browser.restoreCopy")}
                    </button>
                    {status === "modified" && (
                      <button
                        onClick={() => restore(row, true)}
                        title={t("browser.replaceTitle")}
                      >
                        {t("browser.replace")}
                      </button>
                    )}
                  </>
                )}
              </td>
            </tr>
            );
          })}
        </tbody>
      </table>

      {!busy && listing && visibleRows.length === 0 && (
        <p className="empty-note">
          {showIdentical ? t("browser.emptyBothSides") : t("browser.nothingChanged")}
        </p>
      )}
    </div>
  );
}

function Empty({ title, detail, children }: { title: string; detail: string; children?: React.ReactNode }) {
  return (
    <div className="empty">
      <h2>{title}</h2>
      <p>{detail}</p>
      {children}
    </div>
  );
}
