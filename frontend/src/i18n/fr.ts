import type { Key } from "./en";

/**
 * French.
 *
 * Machine-translated and not yet checked by a native speaker. French keeps a
 * space before the colon and inside guillemets, which is preserved here; the
 * em dash is used where English uses one, rather than being replaced by a comma.
 */
export const fr: Record<Key, string> = {
  "nav.snapshots": "Snapshots",
  "nav.schedule": "Planification",
  "nav.home": "Accueil",
  "nav.browse": "Parcourir",
  "nav.search": "Rechercher",
  "app.subtitle": "Snapshots locaux APFS sur ce Mac",
  "app.noSnapshots": "Aucun snapshot pour l'instant",

  "browser.showIdentical": "Afficher les identiques",
  "browser.mount": "Monter ce snapshot",
  "browser.emptyBothSides": "Ce dossier est vide des deux côtés.",
  "browser.nothingChanged": "Rien n'a changé dans ce dossier.",
  "browser.colName": "Nom",
  "browser.colStatus": "État",
  "browser.colInSnapshot": "Dans le snapshot",
  "browser.colOnDisk": "Sur le disque",
  "browser.colChanged": "Modifié",
  "browser.compare": "Comparer",
  "browser.compareTitle": "Voir ce qui diffère dans ce fichier",
  "browser.restoreCopy": "Restaurer une copie",
  "browser.restoreCopyTitle": "Le recopier à côté de ce qui s'y trouve actuellement",
  "browser.replace": "Remplacer",
  "browser.replaceTitle": "Le remettre à son emplacement d'origine ; le fichier actuel est conservé en .bak",

  "status.same": "identique",
  "status.modified": "modifié",
  "status.onlyInSnapshot": "supprimé depuis",
  "status.onlyOnDisk": "nouveau depuis",
  "status.typeChanged": "type modifié",
  "status.detecting": "vérification…",
  "status.notExamined": "vérification impossible",

  "diff.compareWith": "Comparer avec",
  "diff.theLiveDisk": "Le disque actuel",
  "diff.theSnapshot": "Le snapshot",
  "diff.close": "Fermer",
  "diff.reading": "Lecture des deux versions…",
  "diff.cannotCompare": "Ce fichier ne peut pas être comparé ligne par ligne.",
  "diff.notInThisSnapshot": "absent de ce snapshot",
  "diff.notIn": "absent de {version}",
  "diff.addedWhole": "Ce fichier est absent de {version} — tout ce qui suit a été ajouté.",
  "diff.removedWhole": "Ce fichier ne figure plus dans {version} — tout ce qui suit a été supprimé.",

  "language.label": "Langue",
  "language.en": "Anglais",
  "language.de": "Allemand",
  "language.es": "Espagnol",
  "language.fr": "Français",
};
