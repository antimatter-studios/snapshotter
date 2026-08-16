import { useEffect, useState } from "react";
import ReactDiffViewer, { DiffMethod } from "react-diff-viewer-continued";
import { Diff, message, type FileVersions, type SnapshotView } from "./api";
import { bytes } from "./format";
import { useTranslation } from "./i18n";
import { ImageDiff } from "./ImageDiff";

/**
 * What changed inside one file, between two chosen versions of it.
 *
 * This is the question a list of changed paths never answered. It said where to
 * look and nothing about what was there, which is why it was worth replacing
 * with a button on the file itself.
 *
 * The left side is fixed: it is the snapshot being browsed, which is the version
 * someone came here to look at. The right side is a choice. It defaults to the
 * live disk, because "what have I done to this since" is the usual question, but
 * any other mounted snapshot is an equally valid target — and picking one turns
 * this into "what happened to this file between these two dates", which the disk
 * alone cannot answer.
 *
 * Two cases are declined rather than attempted, and both are ordinary rather
 * than errors: a file too large to load into a web view, and a binary one, which
 * has no lines to compare. Each still gets its sizes, because "2.1 MB became
 * 2.4 MB" is a real answer about a photograph.
 */
export function FileDiff({
  snapshot,
  livePath,
  snapshots,
  dark,
  onClose,
}: {
  snapshot: string;
  livePath: string;
  /** Every snapshot the window knows about; only the mounted ones can be targets. */
  snapshots: SnapshotView[];
  dark: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [versions, setVersions] = useState<FileVersions | null>(null);
  const [error, setError] = useState("");
  // "" is the live disk. It is the default rather than a special case.
  const [target, setTarget] = useState("");

  // A snapshot has to be mounted to be read from, and comparing the left side
  // with itself has no answer to give.
  const targets = snapshots.filter((s) => s.mounted && s.name !== snapshot);

  // A target that is no longer offered — one unmounted while this was open —
  // falls back to the disk as it is read rather than being corrected in state.
  // Storing only what is valid would mean an effect that runs on every render,
  // since the list above is a new array each time.
  const chosen = targets.some((s) => s.name === target) ? target : "";

  useEffect(() => {
    setVersions(null);
    setError("");
    Diff.FileVersions(snapshot, livePath, chosen)
      .then(setVersions)
      .catch((err) => setError(message(err)));
  }, [snapshot, livePath, chosen]);

  // Escape closes it. A full-width panel with only a mouse target to leave by is
  // a panel people feel stuck in.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const left = snapshots.find((s) => s.name === snapshot);

  return (
    <div className="file-diff">
      <header>
        <div>
          <strong>{livePath.split("/").pop()}</strong>
          <span className="path">{livePath}</span>
        </div>
        <div className="diff-target">
          {/* The two versions read left to right, in the order they appear below,
              with the control sitting on the side it actually changes. The diff
              viewer's own column titles are not used: they are laid out inside a
              row this file does not control, and a snapshot stamp was tall enough
              to be clipped by it. */}
          <span className="version">{left?.stamp ?? t("diff.theSnapshot")}</span>
          <span className="arrow" aria-hidden="true">
            →
          </span>
          <label className="visually-hidden" htmlFor="diff-target">
            {t("diff.compareWith")}
          </label>
          <select id="diff-target" value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value="">{t("diff.theLiveDisk")}</option>
            {targets.map((s) => (
              <option key={s.name} value={s.name}>
                {s.stamp}
              </option>
            ))}
          </select>
          <button onClick={onClose}>{t("diff.close")}</button>
        </div>
      </header>

      {error && <p className="error">{error}</p>}
      {!versions && !error && <p className="empty-note">{t("diff.reading")}</p>}

      {versions?.kind === "image" && (
        <ImageDiff versions={versions} leftLabel={left?.stamp ?? t("diff.theSnapshot")} />
      )}

      {versions && !versions.readable && versions.kind !== "image" && (
        <p className="empty-note">
          {versions.note || t("diff.cannotCompare")}{" "}
          {/* The sizes are what is left to say, and they are worth saying. */}
          {versions.leftExists ? bytes(versions.leftSize) : t("diff.notInThisSnapshot")} →{" "}
          {versions.rightExists ? bytes(versions.rightSize) : t("diff.notIn", { version: versions.rightLabel })}
        </p>
      )}

      {versions?.readable && versions.kind !== "image" && (!versions.leftExists || !versions.rightExists) && (
        <p className="empty-note">
          {!versions.leftExists
            ? t("diff.addedWhole", { version: left?.stamp ?? t("diff.theSnapshot") })
            : t("diff.removedWhole", { version: versions.rightLabel })}
        </p>
      )}

      {versions?.readable && (
        <div className="file-diff-body">
          <ReactDiffViewer
            oldValue={versions.left}
            newValue={versions.right}
            splitView
            useDarkTheme={dark}
            // Word-level within a changed line: a renamed variable should not
            // present as the whole line being different.
            compareMethod={DiffMethod.WORDS}
          />
        </div>
      )}
    </div>
  );
}
