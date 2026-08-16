import type { Key } from "./en";

/**
 * Spanish.
 *
 * Machine-translated and not yet checked by a native speaker. "Instantánea" is the
 * correct Spanish word for a snapshot, but Apple's own Spanish documentation uses
 * it inconsistently, so this keeps "snapshot" where it names the APFS feature and
 * translates it where it means the everyday thing.
 */
export const es: Record<Key, string> = {
  "nav.snapshots": "Snapshots",
  "nav.schedule": "Programación",
  "nav.home": "Inicio",
  "nav.browse": "Explorar",
  "nav.search": "Buscar",
  "app.subtitle": "Snapshots locales de APFS en este Mac",
  "app.noSnapshots": "Aún no hay snapshots",

  "browser.showIdentical": "Mostrar idénticos",
  "browser.mount": "Montar este snapshot",
  "browser.emptyBothSides": "Esta carpeta está vacía en ambos lados.",
  "browser.nothingChanged": "No ha cambiado nada en esta carpeta.",
  "browser.colName": "Nombre",
  "browser.colStatus": "Estado",
  "browser.colInSnapshot": "En el snapshot",
  "browser.colOnDisk": "En el disco",
  "browser.colChanged": "Modificado",
  "browser.compare": "Comparar",
  "browser.compareTitle": "Ver qué es diferente dentro de este archivo",
  "browser.restoreCopy": "Restaurar una copia",
  "browser.restoreCopyTitle": "Copiarlo junto a lo que haya ahora",
  "browser.replace": "Reemplazar",
  "browser.replaceTitle": "Devolverlo a su ruta original; el archivo actual se conserva como copia .bak",

  "status.same": "idéntico",
  "status.modified": "modificado",
  "status.onlyInSnapshot": "eliminado desde entonces",
  "status.onlyOnDisk": "nuevo desde entonces",
  "status.typeChanged": "tipo modificado",
  "status.detecting": "comprobando…",
  "status.notExamined": "no se pudo comprobar",

  "diff.compareWith": "Comparar con",
  "diff.theLiveDisk": "El disco actual",
  "diff.theSnapshot": "El snapshot",
  "diff.close": "Cerrar",
  "diff.reading": "Leyendo ambas versiones…",
  "diff.cannotCompare": "Este archivo no se puede comparar línea por línea.",
  "diff.notInThisSnapshot": "no está en este snapshot",
  "diff.notIn": "no está en {version}",
  "diff.addedWhole": "Este archivo no está en {version}: todo lo de abajo se añadió.",
  "diff.removedWhole": "Este archivo ya no está en {version}: todo lo de abajo se eliminó.",

  "language.label": "Idioma",
  "language.en": "Inglés",
  "language.de": "Alemán",
  "language.es": "Español",
  "language.fr": "Francés",
};
