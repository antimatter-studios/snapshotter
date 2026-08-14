import { FileIcon as TypeIcon, defaultStyles, type DefaultExtensionType } from "react-file-icon";

/** Icons for the file listings.
 *
 *  Files come from react-file-icon (MIT), which carries a per-extension style map
 *  covering the hundred or so extensions anyone actually meets — a glyph for the
 *  kind and a colour for the family. Hand-drawing that was the alternative and it
 *  would have been a worse version of the same thing.
 *
 *  Folders are still drawn here, because the package has no folder: it models a
 *  document, and a directory is the one row in these listings that is not one.
 */

/** The extensions the package has a style for. Anything else falls back to its
 *  plain document, which is the honest answer for a file with no known kind —
 *  and these listings are full of them, since a dotfile like `.bashrc` or an SSH
 *  key has no extension at all. */
const known = new Set<string>(Object.keys(defaultStyles));

function extensionOf(name: string): string | undefined {
  // lastIndexOf at 0 is a leading dot, which names the file rather than its kind:
  // ".bundle" is not a "bundle" file.
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return undefined;
  return name.slice(dot + 1).toLowerCase();
}

export function FileIcon({ name, isDir }: { name: string; isDir: boolean }) {
  if (isDir) {
    return (
      <svg
        className="file-icon folder"
        width="15"
        height="15"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        focusable="false"
      >
        <path d="M1.8 4.2a1 1 0 0 1 1-1h2.9l1.3 1.5h5.4a1 1 0 0 1 1 1v6.1a1 1 0 0 1-1 1H2.8a1 1 0 0 1-1-1z" />
      </svg>
    );
  }

  const ext = extensionOf(name);
  const style = ext && known.has(ext) ? defaultStyles[ext as DefaultExtensionType] : {};
  return (
    // The extension is passed, and it matters: the label is most of how this set
    // distinguishes one kind from another. Suppressing it left every document type
    // looking identical, because the glyph alone is shared across a whole family.
    // The rows are tall enough to read three uppercase characters, so the label
    // earns its place rather than being a smudge.
    <span className="file-icon doc" aria-hidden="true">
      <TypeIcon extension={ext} labelUppercase {...style} />
    </span>
  );
}
