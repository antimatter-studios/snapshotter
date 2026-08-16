package i18n

// French.
//
// Machine-translated and not read by a native speaker. Keys are checked
// against English by the test in this package.
var fr = map[string]string{
	"status.covered":                     "Vous êtes couvert",
	"status.noSnapshots.title":           "Aucun snapshot",
	"status.noSnapshots.detail":          "Il n'y a rien vers quoi revenir. En créer un maintenant ne coûte aucun espace disque immédiatement, car un snapshot ne grandit qu'à mesure que les fichiers qu'il a enregistrés changent.",
	"status.noSnapshotsShort":            "Aucun snapshot — rien vers quoi revenir",
	"status.noSchedule.title":            "Rien ne crée de snapshots automatiquement",
	"status.scheduleNotRunning.title":    "La planification est installée mais ne s'exécute pas",
	"status.noTripwire.title":            "Rien ne surveille les suppressions massives",
	"status.noTripwire.detail":           "Une planification limite jusqu'où vous pouvez remonter ; elle n'empêche pas une suppression d'aboutir. La surveillance crée un snapshot dès que quelque chose commence à supprimer des fichiers en masse, si bien que le reste de cette suppression reste récupérable.",
	"status.tripwireNotRunning.title":    "La surveillance des suppressions massives ne s'exécute pas",
	"status.tripwireNotRunning.detail":   "Elle est installée mais launchd ne l'a pas chargée, donc rien n'est surveillé.",
	"status.timeMachineThins.title":      "Time Machine va éclaircir ces snapshots",
	"status.simulatedMounts.title":       "Les montages sont simulés",
	"status.simulatedMounts.detail":      "SNAPSHOTTER_FAKE_MOUNTS est défini. Tout ce qui se trouve dans un snapshot est inventé pour le développement, et les restaurations par remplacement sont refusées. Rien de ce qui apparaît sous un snapshot n'est réel.",
	"status.scheduleMissingBinary.title": "La planification pointe vers une copie de Snapshotter qui n'existe plus",
	"status.overdue.title":               "Le dernier snapshot est en retard",
	"status.overdue.detail":              "Un snapshot était attendu à {due} et le plus récent date toujours de {newest}. Consultez le journal de la tâche planifiée.",
	"status.conflict.title":              "Un autre agent crée aussi des snapshots",
	"status.lowSpace.title":              "L'espace libre est faible ; la rétention n'est donc pas garantie",
	"status.simulated.title":             "Ces relevés sont simulés",
	"status.fdaWarning":                  "Accorder l'accès complet au disque à cette application peut ne pas suffire : la tâche planifiée s'exécute comme un programme distinct et en a besoin aussi.",
	"tray.coverageCaption":               "Deux derniers jours (une marque représente une heure)",
	"tray.couldNotRead":                  "Impossible de lire l'état des snapshots",
	"tray.newest":                        "Le plus récent : {when}",
	"tray.takeSnapshot":                  "Créer un snapshot maintenant",
	"tray.openWindow":                    "Ouvrir Snapshotter",
	"tray.quit":                          "Quitter",
	"notify.scheduleRestored.title":      "Snapshotter a rétabli votre planification",
	"notify.scheduleRestored.body":       "Quelque chose avait supprimé {what}, très probablement une mise à jour. C'est de nouveau actif.",
	"notify.what.schedule":               "la planification",
	"notify.what.tripwire":               "la surveillance des suppressions massives",
	"notify.what.both":                   "la planification et la surveillance des suppressions massives",
	"status.noSchedule.detail":           "macOS ne planifie des snapshots locaux que si Time Machine dispose d'une destination de sauvegarde. Sans elle, et sans cette planification, le snapshot d'aujourd'hui est le dernier.",
	"status.scheduleNotRunning.detail":   "launchd a la tâche sur le disque mais ne l'a pas chargée ; aucun snapshot ne sera donc créé.",
}
