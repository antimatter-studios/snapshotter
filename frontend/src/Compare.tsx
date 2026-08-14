import { useEffect, useState } from "react";
import { Events } from "@wailsio/runtime";
import { Diff, Restore, Snapshots, message, type DiffResult, type Change, type SnapshotView } from "./api";
import { bytes, stamp, statusLabel } from "./format";
import "./Compare.css";

interface Props {
  snapshot: SnapshotView | null;
  path: string;
  onStatus: (text: string) => void;
}

interface Progress {
  Scanned?: number;
  scanned?: number;
  Changes?: number;
  changes?: number;
  Current?: string;
  current?: string;
}

/**
 * SnapshotComparison is DiffService.CompareSnapshots' result, written out here
 * because the generated bindings do not carry the method yet — they are
 * regenerated from the Go source at integration, and hand-editing them would be
 * overwritten. The shape matches services.SnapshotComparison; when the bindings
 * arrive, these two declarations and the lookup below are what to delete.
 *
 * The field names of a row still say "snapshot" and "live", because diffs.Change
 * is shared with the comparison against the live disk. Here the snapshot half
 * describes `older` and the live half describes `newer`.
 */
interface SnapshotComparison {
  older: SnapshotView;
  newer: SnapshotView;
  /** True when the request named them newest-first and the roles were corrected. */
  swapped: boolean;
  /** The compared folder in live terms. A row's absolute paths are inside the two
   *  mounts, so a restore has to be rebuilt from this. */
  livePath: string;
  result: DiffResult;
}

interface CompareSnapshotsRequest {
  older: string;
  newer: string;
  livePath: string;
  deep: boolean;
  includeSame: boolean;
}

const compareSnapshots = (
  Diff as unknown as {
    CompareSnapshots?: (req: CompareSnapshotsRequest) => Promise<SnapshotComparison>;
  }
).CompareSnapshots;

/**
 * Outcome keeps the two kinds of answer apart.
 *
 * They are rendered by the same table but they are answers to different
 * questions, and a single nullable result would let a comparison against the live
 * disk be labelled with two snapshot dates.
 */
type Outcome =
  | { kind: "live"; result: DiffResult }
  | { kind: "snapshots"; comparison: SnapshotComparison };

/**
 * Against the live disk, "deleted since" and "new since" are exact: the snapshot
 * is the past and now is now.
 *
 * Between two snapshots "since" has nothing to point at — the change happened
 * between two past instants — so the labels name the change and the direction
 * strip names the two ends it happened between.
 */
const betweenLabel: Record<string, string> = {
  same: "unchanged",
  modified: "changed",
  onlyInSnapshot: "removed",
  onlyOnDisk: "added",
  typeChanged: "type changed",
};

/**
 * Compare walks a whole folder tree and lists everything that differs.
 *
 * This is the slow, thorough counterpart to the browser's per-folder view: it
 * answers "what did I lose anywhere under here", which is the question after a
 * deletion whose extent is unknown. The other side can be the live disk — the
 * usual question — or a second snapshot, which answers "what changed between
 * Tuesday and Wednesday".
 */
export function Compare({ snapshot, path, onStatus }: Props) {
  const [deep, setDeep] = useState(false);
  const [running, setRunning] = useState(false);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [progress, setProgress] = useState("");
  const [error, setError] = useState("");
  const [filter, setFilter] = useState<string>("all");
  // "live" is the default because it is the question people arrive with: what has
  // happened to this folder since the snapshot was taken.
  const [against, setAgainst] = useState<string>("live");
  const [others, setOthers] = useState<SnapshotView[]>([]);

  useEffect(() => {
    // The walk reports progress as an event because a large tree takes long
    // enough that a frozen window would look like a hang.
    const off = Events.On("diff:progress", (event: { data: Progress | Progress[] }) => {
      const raw = Array.isArray(event.data) ? event.data[0] : event.data;
      const scanned = raw?.Scanned ?? raw?.scanned ?? 0;
      const changes = raw?.Changes ?? raw?.changes ?? 0;
      setProgress(`${scanned.toLocaleString()} examined, ${changes.toLocaleString()} differences`);
    });
    return () => {
      if (typeof off === "function") off();
    };
  }, []);

  useEffect(() => {
    // Refetched whenever the selection object changes, which is every time the
    // main screen reloads the overview — including just after a snapshot was
    // opened. A stale list would leave the one the user has only now opened still
    // greyed out as unavailable.
    //
    // A failure here costs the second option, not the screen: comparing against
    // the live disk needs no list at all.
    Snapshots.List()
      .then(setOthers)
      .catch(() => setOthers([]));
  }, [snapshot]);

  const other = others.find((s) => s.name === against) ?? null;
  // The picker has no notion of order — one end comes from the sidebar and the
  // other from a dropdown — so the pair is ordered here by the same rule the
  // service applies. The service still checks, and disagreeing with it is then a
  // bug the strip below will say out loud rather than a routine correction.
  const ordered = other && snapshot ? orderByTaken(snapshot, other) : null;

  useEffect(() => {
    // Selecting the snapshot that was already the other end would ask for a
    // snapshot to be compared with itself. Falling back to the live disk is a
    // better answer than a refusal the user did not ask for.
    if (snapshot && against === snapshot.name) setAgainst("live");
  }, [snapshot, against]);

  useEffect(() => {
    // A result outlives neither end of its pair. Keeping it on screen after the
    // pair changed would label one comparison with another one's dates.
    setOutcome(null);
    setProgress("");
  }, [snapshot?.name, against]);

  const run = async () => {
    if (!snapshot) return;
    setRunning(true);
    setError("");
    setOutcome(null);
    setProgress("starting…");
    try {
      if (ordered) {
        if (!compareSnapshots) {
          throw new Error(
            "the bindings for snapshot-to-snapshot comparison have not been generated yet — run wails3 generate bindings",
          );
        }
        const comparison = await compareSnapshots({
          older: ordered.older.name,
          newer: ordered.newer.name,
          livePath: path,
          deep,
          includeSame: false,
        });
        setOutcome({ kind: "snapshots", comparison });
      } else {
        const result = await Diff.Compare({ snapshot: snapshot.name, livePath: path, deep, includeSame: false });
        setOutcome({ kind: "live", result });
      }
      setProgress("");
    } catch (err) {
      setError(message(err));
      setProgress("");
    } finally {
      setRunning(false);
    }
  };

  /** Restores one row from whichever side of the comparison holds the file. */
  const restore = async (row: Change) => {
    if (!snapshot) return;
    try {
      const from = sourceOf(outcome, snapshot, row);
      const res = await Restore.Restore({ snapshot: from.snapshot, livePath: from.livePath, replace: false });
      onStatus(`Restored to ${res.destination}`);
    } catch (err) {
      setError(message(err));
    }
  };

  if (!snapshot?.mounted) {
    return (
      <div className="empty">
        <h2>Open a snapshot first</h2>
        <p>A comparison reads the snapshot's contents, so it has to be attached before it can run.</p>
      </div>
    );
  }

  const result = outcome?.kind === "snapshots" ? outcome.comparison.result : outcome?.result;
  const rows = (result?.changes ?? []).filter((row) => filter === "all" || row.status === filter);
  const counts = countByStatus(result?.changes ?? []);

  // The pair the result describes, where there is one, and the pair that is about
  // to be compared otherwise. The result is authoritative: it reports the roles Go
  // established from the timestamps, which is not necessarily the order asked for.
  const pair =
    outcome?.kind === "snapshots"
      ? {
          left: stamp(outcome.comparison.older.taken),
          right: stamp(outcome.comparison.newer.taken),
          swapped: outcome.comparison.swapped,
        }
      : ordered
        ? { left: stamp(ordered.older.taken), right: stamp(ordered.newer.taken), swapped: false }
        : { left: stamp(snapshot.taken), right: "the live disk", swapped: false };
  const between = outcome?.kind === "snapshots" || ordered !== null;
  const labels = between ? betweenLabel : statusLabel;
  // Against the live disk the size columns keep their existing headings: one side
  // is a snapshot and the other is the disk, and naming them by date instead would
  // read as though both were snapshots.
  const columns = between
    ? { left: `At ${pair.left}`, right: `At ${pair.right}` }
    : { left: "In snapshot", right: "On disk" };

  return (
    <div className="compare">
      <div className="toolbar">
        <div>
          <div className="label">Comparing</div>
          <div className="path">{path}</div>
        </div>
        <div className="toolbar-actions">
          <label className="compare-against">
            Against
            <select value={against} onChange={(e) => setAgainst(e.target.value)} disabled={running}>
              <option value="live">the live disk</option>
              {others
                .filter((s) => s.name !== snapshot.name)
                .map((s) => (
                  // An unmounted snapshot cannot be read, and listing it greyed out
                  // says so — leaving it out would look as though the history were
                  // shorter than it is.
                  <option key={s.name} value={s.name} disabled={!s.mounted}>
                    {stamp(s.taken)}
                    {s.mounted ? "" : " — not open"}
                  </option>
                ))}
            </select>
          </label>
          <label className="check" title="Compares file contents instead of size and timestamp. Slower, and certain.">
            <input type="checkbox" checked={deep} onChange={(e) => setDeep(e.target.checked)} disabled={running} />
            Compare contents
          </label>
          {running ? (
            <button onClick={() => Diff.Cancel()}>Stop</button>
          ) : (
            <button className="primary" onClick={run}>
              Compare
            </button>
          )}
        </div>
      </div>

      {/* Which two things are being compared, and which way round. A row's
          verdict is meaningless without it, and getting it backwards silently
          inverts every one of them. */}
      <div className="direction">
        <span>From</span>
        <span className="side">{pair.left}</span>
        <span className="arrow" aria-hidden="true">
          →
        </span>
        <span>to</span>
        <span className="side">{pair.right}</span>
        {/* Only reachable if this screen and the service disagreed about which of
            the two is older, which would be a bug. Saying so beats showing the
            corrected direction as though nothing had happened. */}
        {pair.swapped && <span className="corrected">(reordered: the request had these the other way round)</span>}
        {between && (
          <span className="reading">
            <em>Removed</em> means it was there at {pair.left} and gone by {pair.right}; <em>added</em> means it
            appeared between the two.
          </span>
        )}
      </div>

      {progress && <p className="progress">{progress}</p>}
      {error && <p className="error">{error}</p>}

      {result && (
        <>
          <div className="summary">
            <Chip label="all" count={result.changes?.length ?? 0} active={filter === "all"} onClick={() => setFilter("all")} />
            {Object.entries(counts).map(([status, count]) => (
              <Chip
                key={status}
                label={labels[status] ?? status}
                count={count}
                active={filter === status}
                onClick={() => setFilter(status)}
              />
            ))}
          </div>

          {result.errors?.length > 0 && (
            <details className="unreadable">
              <summary>{result.errors.length} path(s) could not be read</summary>
              <ul>
                {result.errors.slice(0, 50).map((e) => (
                  <li key={e}>{e}</li>
                ))}
              </ul>
            </details>
          )}

          <table className="rows">
            <thead>
              <tr>
                <th>Path</th>
                <th>Status</th>
                {/* The size columns are the two ends of the comparison, so between
                    two snapshots they are headed with their dates: "In snapshot"
                    would name whichever of the two the reader assumed. */}
                <th className="num">{columns.left}</th>
                <th className="num">{columns.right}</th>
                <th>Modified</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.relPath} className={row.status}>
                  <td>
                    {row.relPath}
                    {row.isDir ? "/" : ""}
                  </td>
                  <td>
                    <span className={`badge ${row.status}`}>{labels[row.status] ?? row.status}</span>
                  </td>
                  <td className="num">{row.status === "onlyOnDisk" ? "—" : bytes(row.snapSize)}</td>
                  <td className="num">{row.status === "onlyInSnapshot" ? "—" : bytes(row.liveSize)}</td>
                  <td>{stamp(modTimeOf(row))}</td>
                  <td className="actions">
                    {/* Against the live disk there is nothing to restore for a file
                        that only exists there. Between two snapshots both sides are
                        the past, so either end is worth a copy. */}
                    {(outcome?.kind === "snapshots" || row.status !== "onlyOnDisk") && (
                      <button onClick={() => restore(row)}>
                        {outcome?.kind === "snapshots"
                          ? `Restore from ${stamp(sideHolding(outcome.comparison, row).taken)}`
                          : "Restore a copy"}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>

          {rows.length === 0 && <p className="empty-note">Nothing differs under this folder.</p>}
        </>
      )}
    </div>
  );
}

/** The two ends in the order the rows will read, decided by the snapshots' own
 *  timestamps rather than by which control they came from. */
function orderByTaken(a: SnapshotView, b: SnapshotView): { older: SnapshotView; newer: SnapshotView } {
  return new Date(b.taken).getTime() < new Date(a.taken).getTime() ? { older: b, newer: a } : { older: a, newer: b };
}

/** The modification time worth showing: the one belonging to the side that holds
 *  the file.
 *
 *  A row for something present on only one side carries Go's zero time on the
 *  other, which serialises as year 1 and is a non-empty string — so preferring the
 *  snapshot side unconditionally dates every newly created file to January 1st
 *  0001. */
function modTimeOf(row: Change): string {
  return row.status === "onlyOnDisk" ? row.liveModTime : row.snapModTime;
}

/** Which end of a snapshot-to-snapshot comparison holds the file in a row. */
function sideHolding(comparison: SnapshotComparison, row: Change): SnapshotView {
  return row.status === "onlyOnDisk" ? comparison.newer : comparison.older;
}

/** Where a restore of one row should read from, and where the file belongs.
 *
 *  Between two snapshots neither of a row's absolute paths is a live path — both
 *  are inside a mount — so the destination is rebuilt from the folder that was
 *  compared and the row's position under it. */
function sourceOf(
  outcome: Outcome | null,
  snapshot: SnapshotView,
  row: Change,
): { snapshot: string; livePath: string } {
  if (outcome?.kind !== "snapshots") return { snapshot: snapshot.name, livePath: row.absLive };
  const { comparison } = outcome;
  return {
    snapshot: sideHolding(comparison, row).name,
    livePath: `${comparison.livePath.replace(/\/$/, "")}/${row.relPath}`,
  };
}

function countByStatus(changes: Change[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const change of changes) out[change.status] = (out[change.status] ?? 0) + 1;
  return out;
}

function Chip({ label, count, active, onClick }: { label: string; count: number; active: boolean; onClick: () => void }) {
  return (
    <button className={`chip ${active ? "active" : ""}`} onClick={onClick}>
      {label} <span className="count">{count}</span>
    </button>
  );
}
