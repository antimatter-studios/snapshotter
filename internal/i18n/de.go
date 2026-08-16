package i18n

// German.
//
// Machine-translated and not read by a native speaker. Keys are checked
// against English by the test in this package.
var de = map[string]string{
	"status.covered":                     "Sie sind abgesichert",
	"status.noSnapshots.title":           "Es gibt keine Snapshots",
	"status.noSnapshots.detail":          "Es gibt nichts, wohin zurückgekehrt werden könnte. Einen jetzt zu erstellen kostet zunächst keinen Speicherplatz, denn ein Snapshot wächst erst, wenn sich die erfassten Dateien ändern.",
	"status.noSnapshotsShort":            "Keine Snapshots — nichts, wohin zurückgekehrt werden kann",
	"status.noSchedule.title":            "Es werden keine Snapshots automatisch erstellt",
	"status.scheduleNotRunning.title":    "Der Zeitplan ist eingerichtet, läuft aber nicht",
	"status.noTripwire.title":            "Nichts überwacht Massenlöschungen",
	"status.noTripwire.detail":           "Ein Zeitplan begrenzt, wie weit Sie zurückgehen können; er hält eine laufende Löschung nicht auf. Die Überwachung erstellt einen Snapshot, sobald etwas beginnt, Dateien in großer Zahl zu entfernen, sodass der Rest dieser Löschung wiederherstellbar bleibt.",
	"status.tripwireNotRunning.title":    "Die Überwachung von Massenlöschungen läuft nicht",
	"status.tripwireNotRunning.detail":   "Sie ist eingerichtet, aber launchd hat sie nicht geladen — es wird also nichts überwacht.",
	"status.timeMachineThins.title":      "Time Machine wird diese Snapshots ausdünnen",
	"status.simulatedMounts.title":       "Einbindungen sind simuliert",
	"status.simulatedMounts.detail":      "SNAPSHOTTER_FAKE_MOUNTS ist gesetzt. Alles in einem Snapshot ist für die Entwicklung erfunden, und Ersetzen-Wiederherstellungen werden abgelehnt. Nichts, was unter einem Snapshot gezeigt wird, ist echt.",
	"status.scheduleMissingBinary.title": "Der Zeitplan verweist auf eine nicht mehr vorhandene Kopie von Snapshotter",
	"status.overdue.title":               "Der letzte Snapshot ist überfällig",
	"status.overdue.detail":              "Ein Snapshot war für {due} fällig, der neueste stammt weiterhin von {newest}. Prüfen Sie das Protokoll der geplanten Aufgabe.",
	"status.conflict.title":              "Ein weiterer Agent erstellt ebenfalls Snapshots",
	"status.lowSpace.title":              "Wenig freier Speicher — die Aufbewahrung ist nicht garantiert",
	"status.simulated.title":             "Diese Werte sind simuliert",
	"status.fdaWarning":                  "Vollen Festplattenzugriff nur für diese Anwendung zu erteilen genügt möglicherweise nicht: Die geplante Aufgabe läuft als eigenes Programm und benötigt ihn ebenfalls.",
	"tray.coverageCaption":               "Letzte zwei Tage (eine Marke entspricht einer Stunde)",
	"tray.couldNotRead":                  "Snapshot-Status konnte nicht gelesen werden",
	"tray.newest":                        "Neuester: {when}",
	"tray.takeSnapshot":                  "Jetzt einen Snapshot erstellen",
	"tray.openWindow":                    "Snapshotter öffnen",
	"tray.quit":                          "Beenden",
	"notify.scheduleRestored.title":      "Snapshotter hat Ihren Zeitplan wiederhergestellt",
	"notify.scheduleRestored.body":       "Etwas hatte {what} entfernt, höchstwahrscheinlich eine Aktualisierung. Es läuft wieder.",
	"notify.what.schedule":               "den Zeitplan",
	"notify.what.tripwire":               "die Überwachung von Massenlöschungen",
	"notify.what.both":                   "den Zeitplan und die Überwachung von Massenlöschungen",
	"status.noSchedule.detail":           "macOS plant lokale Snapshots nur, wenn für Time Machine ein Backup-Ziel eingerichtet ist. Ohne eines und ohne diesen Zeitplan ist der heutige Snapshot der letzte.",
	"status.scheduleNotRunning.detail":   "launchd hat den Auftrag auf der Festplatte, ihn aber nicht geladen — es wird also kein Snapshot erstellt.",
}
