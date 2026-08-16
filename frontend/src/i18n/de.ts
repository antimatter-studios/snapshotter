import type { Key } from "./en";

/**
 * German.
 *
 * Typed against the English key list, so a key added there and forgotten here is
 * a build failure rather than an English word appearing mid-sentence.
 *
 * Machine-translated and not yet checked by a native speaker. The technical terms
 * are the ones most likely to be wrong: "Momentaufnahme" is the literal word for a
 * snapshot but APFS documentation in German generally keeps "Snapshot", which is
 * what this uses.
 */
export const de: Record<Key, string> = {
  "nav.snapshots": "Snapshots",
  "nav.schedule": "Zeitplan",
  "nav.home": "Übersicht",
  "nav.browse": "Durchsuchen",
  "nav.search": "Suchen",
  "app.subtitle": "Lokale APFS-Snapshots auf diesem Mac",
  "app.noSnapshots": "Noch keine Snapshots",

  "browser.showIdentical": "Identische anzeigen",
  "browser.mount": "Diesen Snapshot einbinden",
  "browser.emptyBothSides": "Dieser Ordner ist auf beiden Seiten leer.",
  "browser.nothingChanged": "In diesem Ordner hat sich nichts geändert.",
  "browser.colName": "Name",
  "browser.colStatus": "Status",
  "browser.colInSnapshot": "Im Snapshot",
  "browser.colOnDisk": "Auf der Festplatte",
  "browser.colChanged": "Geändert",
  "browser.compare": "Vergleichen",
  "browser.compareTitle": "Sehen, was sich in dieser Datei unterscheidet",
  "browser.restoreCopy": "Kopie wiederherstellen",
  "browser.restoreCopyTitle": "Neben der vorhandenen Datei zurückkopieren",
  "browser.replace": "Ersetzen",
  "browser.replaceTitle": "Am ursprünglichen Ort zurücklegen; die aktuelle Datei bleibt als .bak erhalten",

  "status.same": "identisch",
  "status.modified": "geändert",
  "status.onlyInSnapshot": "seitdem gelöscht",
  "status.onlyOnDisk": "seitdem neu",
  "status.typeChanged": "Typ geändert",
  "status.detecting": "wird geprüft…",
  "status.notExamined": "nicht prüfbar",

  "diff.compareWith": "Vergleichen mit",
  "diff.theLiveDisk": "Die aktuelle Festplatte",
  "diff.theSnapshot": "Der Snapshot",
  "diff.close": "Schließen",
  "diff.reading": "Beide Versionen werden gelesen…",
  "diff.cannotCompare": "Diese Datei lässt sich nicht zeilenweise vergleichen.",
  "diff.notInThisSnapshot": "nicht in diesem Snapshot",
  "diff.notIn": "nicht in {version}",
  "diff.addedWhole": "Diese Datei ist nicht in {version} — alles unten wurde hinzugefügt.",
  "diff.removedWhole": "Diese Datei ist nicht mehr in {version} — alles unten wurde entfernt.",

  "language.label": "Sprache",
  "language.en": "Englisch",
  "language.de": "Deutsch",
  "language.es": "Spanisch",
  "language.fr": "Französisch",
};
