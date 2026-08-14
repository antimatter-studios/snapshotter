import { useCallback, useEffect, useState } from "react";
import { Browse, Restore, message, type MergedListing, type Change, type SnapshotView } from "./api";
import { bytes, breadcrumbs, stamp, statusLabel } from "./format";
import { FileIcon } from "./FileIcon";

interface Props {
  snapshot: SnapshotView | null;
  path: string;
  onPathChange: (path: string) => void;
  onMount: () => void;
  onCompare: () => void;
  onStatus: (text: string) => void;
}

/**
 * Browser shows one folder from the snapshot and from the live disk at once.
 *
 * Every row carries its own verdict, which is the answer to the question that
 * brings someone here: what is in this folder that is no longer on disk.
 */
export function Browser({ snapshot, path, onPathChange, onMount, onCompare, onStatus }: Props) {
  const [listing, setListing] = useState<MergedListing | null>(null);
  const [showUnchanged, setShowUnchanged] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!snapshot?.mounted) return;
    setBusy(true);
    setError("");
    try {
      setListing(await Browse.Merged(snapshot.name, path, showUnchanged));
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
          <button onClick={onCompare}>Compare this folder…</button>
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
          {(listing?.rows ?? []).map((row) => (
            <tr key={row.relPath} className={row.status}>
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
                <span className={`badge ${row.status}`}>{statusLabel[row.status] ?? row.status}</span>
              </td>
              <td className="num">{row.status === "onlyOnDisk" ? "—" : bytes(row.snapSize)}</td>
              <td className="num">{row.status === "onlyInSnapshot" ? "—" : bytes(row.liveSize)}</td>
              <td>{stamp(row.snapModTime || row.liveModTime)}</td>
              <td className="actions">
                {row.status !== "onlyOnDisk" && (
                  <>
                    <button onClick={() => restore(row, false)} title="Copy it back alongside whatever is there now">
                      Restore a copy
                    </button>
                    {row.status === "modified" && (
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
          ))}
        </tbody>
      </table>

      {!busy && listing && listing.rows.length === 0 && (
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
