import { useState } from "react";
import { Search as SearchAPI, Restore, message, type SearchResult, type SnapshotView, type DiffResult } from "./api";
import { bytes, stamp } from "./format";
import { FileIcon } from "./FileIcon";
import { useAction } from "./useAction";
import { useTranslation } from "react-i18next";

/**
 * Find a file by name across every open snapshot.
 *
 * Every other screen here is organised by place, which assumes you know where
 * to look. The moment this application exists for is the one where you do not:
 * you know what the file was called and roughly when it was still there, and
 * not which directory it was in.
 */
export function Search({
  onStatus,
  snapshot,
  path,
}: {
  onStatus: (s: string) => void;
  /** The snapshot the gone-since mode asks about. */
  snapshot: SnapshotView | null;
  /** Where the browser is, which is where someone would ask about first. */
  path: string;
}) {
  const { t } = useTranslation();
  const [mode, setMode] = useState<"name" | "gone">("name");
  const [term, setTerm] = useState("");
  const [under, setUnder] = useState("");
  const [result, setResult] = useState<SearchResult | null>(null);
  // The gone-since mode's own folder, seeded from wherever the browser is. Kept
  // separately so walking around the browser afterwards does not silently change
  // what a result on screen was about.
  const [folder, setFolder] = useState(path);
  const [deep, setDeep] = useState(false);
  const [gone, setGone] = useState<DiffResult | null>(null);
  const { busy, error, setError, run: perform } = useAction(onStatus);

  const run = async () => {
    if (!term.trim()) return;
    await perform(async () => setResult(await SearchAPI.Search(term, under)));
  };

  // What the folder held when the snapshot was taken and holds no longer. The
  // service filters to that one status: everything else in a comparison answers a
  // question this screen is not asking.
  const look = async () => {
    if (!snapshot) return;
    await perform(async () => setGone(await SearchAPI.DeletedSince(snapshot.name, folder || path, deep)));
  };

  const restore = async (snapshot: string, livePath: string) => {
    try {
      const res = await Restore.Restore({ snapshot, livePath, replace: false });
      onStatus(t("search.restoredTo", { path: res.destination }));
    } catch (err) {
      setError(message(err));
    }
  };

  return (
    <>
      <div className="toolbar">
        {/* Two questions, and they are not the same question. By name assumes you
            know what the file was called. Gone since assumes you do not — only
            that something is missing from a folder you remember — which is the
            harder case and the one that had no answer here. */}
        <div className="segmented">
          <button className={mode === "name" ? "on" : ""} onClick={() => setMode("name")}>
            {t("search.byName")}
          </button>
          <button className={mode === "gone" ? "on" : ""} onClick={() => setMode("gone")}>
            {t("search.whatIsGone")}
          </button>
        </div>

        {mode === "name" ? (
          <div className="toolbar-actions">
            <input
              className="search-input"
              placeholder={t("search.namePlaceholder")}
              value={term}
              onChange={(e) => setTerm(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void run()}
              autoFocus
            />
            <input
              className="search-input narrow"
              placeholder={t("search.onlyUnder")}
              value={under}
              onChange={(e) => setUnder(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void run()}
            />
            <button className="primary" onClick={() => void run()} disabled={busy || !term.trim()}>
              {busy ? t("search.searching") : t("search.search")}
            </button>
          </div>
        ) : (
          <div className="toolbar-actions">
            <input
              className="search-input"
              aria-label={t("search.inFolder")}
              placeholder={t("search.inFolder")}
              value={folder}
              onChange={(e) => setFolder(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void look()}
            />
            <label className="check">
              <input type="checkbox" checked={deep} onChange={(e) => setDeep(e.target.checked)} />
              {t("search.deepCompare")}
            </label>
            <button className="primary" onClick={() => void look()} disabled={busy || !snapshot}>
              {busy ? t("search.looking") : t("search.look")}
            </button>
          </div>
        )}
      </div>

      {mode === "gone" && <p className="explain">{t("search.goneExplain")}</p>}

      {error && <p className="error">{error}</p>}

      {/* An unsearched snapshot has to be named. Otherwise finding nothing reads
          as proof that nothing is there, when it may only mean nobody looked. */}
      {mode === "name" && result?.note && <p className="note">{result.note}</p>}

      {mode === "name" && result && result.hits.length === 0 && !result.note && (
        // Through the catalogue with a real plural rule: this sentence was built
        // in the markup with a conditional "s", which is English grammar written
        // into the component.
        <p className="empty">
          {t("search.nothingMatching", { term: result.term, count: result.searched.length })}
        </p>
      )}

      {mode === "gone" && !snapshot && <p className="empty">{t("search.needSnapshot")}</p>}

      {mode === "gone" && gone && gone.changes.length === 0 && (
        <p className="empty">{t("search.nothingGone")}</p>
      )}

      {mode === "gone" && gone && gone.changes.length > 0 && (
        <div className="browser">
          <table className="rows">
            <thead>
              <tr>
                <th>{t("search.colName")}</th>
                <th>{t("search.colWhere")}</th>
                <th>{t("search.colGone")}</th>
                <th className="num">{t("search.colSize")}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {gone.changes.map((c) => (
                <tr key={c.absLive}>
                  <td>
                    <span className="name-cell">
                      <FileIcon name={c.relPath} isDir={c.isDir} />
                      {c.relPath}
                      {c.isDir ? "/" : ""}
                    </span>
                  </td>
                  <td className="path">{c.absLive}</td>
                  {/* When it was last written, not when it went: nothing records
                      the moment of a deletion, and the snapshot only proves it was
                      still there when the snapshot was taken. */}
                  <td>{stamp(c.snapModTime)}</td>
                  <td className="num">{c.isDir ? "—" : bytes(c.snapSize)}</td>
                  <td className="actions">
                    <button onClick={() => void restore(snapshot!.name, c.absLive)}>
                      {t("search.restore")}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {mode === "name" && result && result.hits.length > 0 && (
        <div className="browser">
          <table className="rows">
            <thead>
              <tr>
                <th>{t("search.colName")}</th>
                <th>{t("search.colWhere")}</th>
                <th>{t("search.colSnapshot")}</th>
                <th className="num">{t("search.colSize")}</th>
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
                    <button onClick={() => void restore(h.snapshot, h.livePath)}>{t("search.restore")}</button>
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
