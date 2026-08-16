import { useCallback, useEffect, useRef, useState } from "react";
import { Browse, Restore, message, type MergedListing, type Change, type SnapshotView } from "./api";
import { bytes, breadcrumbs, stamp, statusLabel } from "./format";
import { FileIcon } from "./FileIcon";

interface Props {
  snapshot: SnapshotView | null;
  path: string;
  onPathChange: (path: string) => void;
  onMount: () => void;
  /** Opens the line-by-line view of one file. */
  onDiff: (livePath: string) => void;
  onStatus: (text: string) => void;
}

/**
 * Browser shows one folder from the snapshot and from the live disk at once.
 *
 * Every row carries its own verdict, which is the answer to the question that
 * brings someone here: what is in this folder that is no longer on disk.
 */
export function Browser({ snapshot, path, onPathChange, onMount, onDiff, onStatus }: Props) {
  const [listing, setListing] = useState<MergedListing | null>(null);
  const [showUnchanged, setShowUnchanged] = useState(true);
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

  const load = useCallback(async () => {
    if (!snapshot?.mounted) return;
    setBusy(true);
    setError("");
    try {
      const merged = await Browse.Merged(snapshot.name, path, showUnchanged);
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
      const token = ++resolveToken.current;
      let next = 0;
      const worker = async () => {
        while (next < folders.length) {
          const row = folders[next++];
          try {
            const verdict = await Browse.DirectoryStatus(snapshot.name, row.absLive);
            // Dropped if the listing moved on: answers about a folder nobody is
            // looking at any more are worse than useless, because they would
            // overwrite the ones for the folder they are.
            if (token !== resolveToken.current) return;
            setFolderStatus((current) => ({ ...current, [row.absLive]: verdict.status }));
            if (verdict.why) {
              setFolderWhy((current) => ({ ...current, [row.absLive]: verdict.why! }));
            }
          } catch {
            // Left as detecting rather than guessed at.
          }
        }
      };
      await Promise.all(Array.from({ length: Math.min(3, folders.length) }, worker));
    } catch (err) {
      setError(message(err));
      setListing(null);
    } finally {
      setBusy(false);
    }
  }, [snapshot, path, showUnchanged]);

  useEffect(() => {
    void load();
  }, [load]);

  if (!snapshot) {
    return <Empty title="No snapshot selected" detail="Pick a snapshot on the left to look inside it." />;
  }

  if (!snapshot.mounted) {
    return (
      <Empty
        title="This snapshot is not open yet"
        detail="Opening a snapshot attaches it read-only, which macOS requires an administrator password for. Nothing inside it can be changed."
      >
        <button className="primary" onClick={onMount}>
          Open snapshot
        </button>
      </Empty>
    );
  }

  const restore = async (row: Change, replace: boolean) => {
    try {
      const result = await Restore.Restore({
        snapshot: snapshot.name,
        livePath: row.absLive,
        replace,
      });
      onStatus(`Restored to ${result.destination}${result.backedUp ? ` (previous file kept at ${result.backedUp})` : ""}`);
      void load();
    } catch (err) {
      setError(message(err));
    }
  };

  // Unchanged folders are hidden here rather than by the listing, which cannot
  // know: a folder arrives unexamined and only becomes "same" once its own walk
  // answers, long after the rows were built. Files are already filtered on the
  // way out of Merged.
  //
  // Computed once so the table below and the "nothing has changed" message
  // cannot disagree about whether anything is showing.
  const visibleRows = (listing?.rows ?? []).filter(
    (row) => showUnchanged || !row.isDir || (folderStatus[row.absLive] ?? "detecting") !== "same",
  );

  return (
    <div className="browser">
      <div className="toolbar">
        <nav className="crumbs">
          {breadcrumbs(path).map((crumb, i, all) => (
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
            <input type="checkbox" checked={showUnchanged} onChange={(e) => setShowUnchanged(e.target.checked)} />
            Show unchanged
          </label>
          <button onClick={() => Browse.RevealInFinder(snapshot.name, path).catch((e) => setError(message(e)))}>
            Reveal in Finder
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {listing?.note && <p className="note">{listing.note}</p>}

      <table className="rows">
        <thead>
          <tr>
            <th>Name</th>
            <th>Status</th>
            <th className="num">In snapshot</th>
            <th className="num">On disk</th>
            <th>Modified</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {visibleRows.map((row) => {
            // A folder comes back unexamined and is resolved on its own, so it
            // reads as detecting until its answer arrives.
            const status = row.isDir ? folderStatus[row.absLive] ?? "detecting" : row.status;
            return (
            <tr key={row.relPath} className={status}>
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
                  {statusLabel[status] ?? status}
                </span>
              </td>
              <td className="num">{status === "onlyOnDisk" ? "—" : bytes(row.snapSize)}</td>
              <td className="num">{status === "onlyInSnapshot" ? "—" : bytes(row.liveSize)}</td>
              <td>{stamp(row.snapModTime || row.liveModTime)}</td>
              <td className="actions">
                {status !== "onlyOnDisk" && (
                  <>
                    <button onClick={() => restore(row, false)} title="Copy it back alongside whatever is there now">
                      Restore a copy
                    </button>
                    {status === "modified" && (
                      <button
                        onClick={() => restore(row, true)}
                        title="Put it back at the original path; the current file is kept as a .bak copy"
                      >
                        Replace
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
          {showUnchanged ? "This folder is empty on both sides." : "Nothing has changed in this folder."}
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
