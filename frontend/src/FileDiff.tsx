import { useEffect, useState } from "react";
import ReactDiffViewer, { DiffMethod } from "react-diff-viewer-continued";
import { Diff, message, type FileVersions } from "./api";
import { bytes } from "./format";

/**
 * What changed inside one file, between a snapshot and now.
 *
 * This is the question a list of changed paths never answered. It said where to
 * look and nothing about what was there, which is why it was worth replacing
 * with a button on the file itself.
 *
 * Two cases are declined rather than attempted, and both are ordinary rather
 * than errors: a file too large to load into a web view, and a binary one, which
 * has no lines to compare. Each still gets its sizes, because "2.1 MB became
 * 2.4 MB" is a real answer about a photograph.
 */
export function FileDiff({
  snapshot,
  livePath,
  dark,
  onClose,
}: {
  snapshot: string;
  livePath: string;
  dark: boolean;
  onClose: () => void;
}) {
  const [versions, setVersions] = useState<FileVersions | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    setVersions(null);
    setError("");
    Diff.FileVersions(snapshot, livePath)
      .then(setVersions)
      .catch((err) => setError(message(err)));
  }, [snapshot, livePath]);

  // Escape closes it. A full-width panel with only a mouse target to leave by is
  // a panel people feel stuck in.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="file-diff">
      <header>
        <div>
          <strong>{livePath.split("/").pop()}</strong>
          <span className="path">{livePath}</span>
        </div>
        <button onClick={onClose}>Close</button>
      </header>

      {error && <p className="error">{error}</p>}
      {!versions && !error && <p className="empty-note">Reading both versions…</p>}

      {versions && !versions.readable && (
        <p className="empty-note">
          {versions.note || "This file cannot be compared line by line."}
          {" "}
          {/* The sizes are what is left to say, and they are worth saying. */}
          {versions.inSnapshot ? bytes(versions.snapshotSize) : "not in the snapshot"} →{" "}
          {versions.onDisk ? bytes(versions.liveSize) : "no longer on disk"}
        </p>
      )}

      {versions?.readable && (
        <div className="file-diff-body">
          <ReactDiffViewer
            oldValue={versions.snapshot}
            newValue={versions.live}
            splitView
            useDarkTheme={dark}
            // Word-level within a changed line: a renamed variable should not
            // present as the whole line being different.
            compareMethod={DiffMethod.WORDS}
            leftTitle={versions.inSnapshot ? "In the snapshot" : "Not in the snapshot"}
            rightTitle={versions.onDisk ? "On disk now" : "Deleted from disk"}
          />
        </div>
      )}
    </div>
  );
}
