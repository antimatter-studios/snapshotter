import { useState } from "react";
import { Search as SearchAPI, Restore, message, type SearchResult } from "./api";
import { bytes, stamp } from "./format";

/**
 * Find a file by name across every open snapshot.
 *
 * Every other screen here is organised by place, which assumes you know where
 * to look. The moment this application exists for is the one where you do not:
 * you know what the file was called and roughly when it was still there, and
 * not which directory it was in.
 */
export function Search({ onStatus }: { onStatus: (s: string) => void }) {
  const [term, setTerm] = useState("");
  const [under, setUnder] = useState("");
  const [result, setResult] = useState<SearchResult | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const run = async () => {
    if (!term.trim()) return;
    setBusy(true);
    setError("");
    try {
      setResult(await SearchAPI.Search(term, under));
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  const restore = async (snapshot: string, livePath: string) => {
    try {
      const res = await Restore.Restore({ snapshot, livePath, replace: false });
      onStatus(`Restored to ${res.destination}`);
    } catch (err) {
      setError(message(err));
    }
  };

  return (
    <>
      <div className="toolbar">
        <div className="toolbar-actions">
          <input
            className="search-input"
            placeholder="Part of a file name — id_rsa, vault, .kdbx"
            value={term}
            onChange={(e) => setTerm(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void run()}
            autoFocus
          />
          <input
            className="search-input narrow"
            placeholder="Only under… (optional)"
            value={under}
            onChange={(e) => setUnder(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void run()}
          />
          <button className="primary" onClick={() => void run()} disabled={busy || !term.trim()}>
            {busy ? "Searching…" : "Search"}
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {/* An unsearched snapshot has to be named. Otherwise finding nothing reads
          as proof that nothing is there, when it may only mean nobody looked. */}
      {result?.note && <p className="note">{result.note}</p>}

      {result && result.hits.length === 0 && !result.note && (
        <p className="empty">
          Nothing matching “{result.term}” in {result.searched.length} open snapshot
          {result.searched.length === 1 ? "" : "s"}.
        </p>
      )}

      {result && result.hits.length > 0 && (
        <div className="browser">
          <table className="rows">
            <thead>
              <tr>
                <th>Name</th>
                <th>Where it was</th>
                <th>Snapshot</th>
                <th className="num">Size</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {result.hits.map((h, i) => (
                <tr key={`${h.snapshot}:${h.livePath}:${i}`}>
                  <td>{h.name}</td>
                  <td className="path">{h.livePath}</td>
                  <td>{stamp(h.modTime)}</td>
                  <td className="num">{h.isDir ? "—" : bytes(h.size)}</td>
                  <td className="actions">
                    <button onClick={() => void restore(h.snapshot, h.livePath)}>Restore</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
