/**
 * English, and the shape every other language must match.
 *
 * This object is the source of truth for the key list: the other catalogues are
 * typed as `Record<Key, string>`, so a language missing a key fails the build
 * rather than falling back silently at runtime. Silent fallback is how a
 * half-translated interface ships without anyone noticing.
 *
 * Keys are `area.thing`, named for what the text says rather than where it
 * appears, so moving a control does not strand its key.
 *
 * Placeholders are `{name}` and are substituted positionally by name, never by
 * order — German and French routinely need a different word order than English,
 * and a translator must be free to move them.
 */
export const en = {
  // Shell and navigation
  "nav.snapshots": "Snapshots",
  "nav.schedule": "Schedule",
  "nav.home": "Home",
  "nav.browse": "Browse",
  "nav.search": "Search",
  "app.subtitle": "APFS local snapshots on this Mac",
  "app.noSnapshots": "No snapshots yet",

  // The browser
  "browser.showIdentical": "Show identical",
  "browser.mount": "Mount this snapshot",
  "browser.emptyBothSides": "This folder is empty on both sides.",
  "browser.nothingChanged": "Nothing has changed in this folder.",
  "browser.colName": "Name",
  "browser.colStatus": "Status",
  "browser.colInSnapshot": "In snapshot",
  "browser.colOnDisk": "On disk",
  "browser.colChanged": "Changed",
  "browser.compare": "Compare",
  "browser.compareTitle": "See what is different inside this file",
  "browser.restoreCopy": "Restore a copy",
  "browser.restoreCopyTitle": "Copy it back alongside whatever is there now",
  "browser.replace": "Replace",
  "browser.replaceTitle": "Put it back at the original path; the current file is kept as a .bak copy",

  // Verdicts
  "status.same": "identical",
  "status.modified": "changed",
  "status.onlyInSnapshot": "deleted since",
  "status.onlyOnDisk": "new since",
  "status.typeChanged": "type changed",
  "status.detecting": "detecting…",
  "status.notExamined": "could not check",

  // Comparing one file
  "diff.compareWith": "Compare with",
  "diff.theLiveDisk": "The live disk",
  "diff.theSnapshot": "The snapshot",
  "diff.close": "Close",
  "diff.reading": "Reading both versions…",
  "diff.cannotCompare": "This file cannot be compared line by line.",
  "diff.notInThisSnapshot": "not in this snapshot",
  "diff.notIn": "not in {version}",
  "diff.addedWhole": "This file is not in {version} — everything below was added.",
  "diff.removedWhole": "This file is no longer in {version} — everything below was removed.",

  // Language picker
  "language.label": "Language",
  "language.en": "English",
  "language.de": "German",
  "language.es": "Spanish",
  "language.fr": "French",
} as const;

/** Every key the application can ask for. */
export type Key = keyof typeof en;
