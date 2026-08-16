import { useState } from "react";
import { Search as SearchAPI, Restore, message, type SearchResult } from "./api";
import { bytes, stamp } from "./format";
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
export function Search({ onStatus }: { onStatus: (s: string) => void }) {
  const { t } = useTranslation();
  const [term, setTerm] = useState("");
  const [under, setUnder] = useState("");
  const [result, setResult] = useState<SearchResult | null>(null);
  const { busy, error, setError, run: perform } = useAction(onStatus);

  const run = async () => {
    if (!term.trim()) return;
    await perform(async () => setResult(await SearchAPI.Search(term, under)));
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
