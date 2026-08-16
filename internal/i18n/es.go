package i18n

// Spanish.
//
// Machine-translated and not read by a native speaker. Keys are checked
// against English by the test in this package.
var es = map[string]string{
	"status.covered":                     "Está protegido",
	"status.noSnapshots.title":           "No hay snapshots",
	"status.noSnapshots.detail":          "No hay nada a lo que volver. Crear uno ahora no ocupa espacio de inmediato, porque un snapshot solo crece a medida que cambian los archivos que registró.",
	"status.noSnapshotsShort":            "Sin snapshots: no hay nada a lo que volver",
	"status.noSchedule.title":            "Nada crea snapshots automáticamente",
	"status.scheduleNotRunning.title":    "La programación está instalada pero no se ejecuta",
	"status.noTripwire.title":            "Nada vigila los borrados masivos",
	"status.noTripwire.detail":           "Una programación limita hasta dónde puede retroceder; no impide que un borrado termine. El vigilante crea un snapshot en cuanto algo empieza a eliminar archivos en masa, de modo que el resto de ese borrado sigue siendo recuperable.",
	"status.tripwireNotRunning.title":    "El vigilante de borrados masivos no se está ejecutando",
	"status.tripwireNotRunning.detail":   "Está instalado pero launchd no lo ha cargado, así que no se vigila nada.",
	"status.timeMachineThins.title":      "Time Machine reducirá estos snapshots",
	"status.simulatedMounts.title":       "Los montajes son simulados",
	"status.simulatedMounts.detail":      "SNAPSHOTTER_FAKE_MOUNTS está definido. Todo lo que hay dentro de un snapshot es inventado para el desarrollo y las restauraciones de tipo Reemplazar se rechazan. Nada de lo que se muestra bajo un snapshot es real.",
	"status.scheduleMissingBinary.title": "La programación apunta a una copia de Snapshotter que ya no existe",
	"status.overdue.title":               "El último snapshot está atrasado",
	"status.overdue.detail":              "Se esperaba un snapshot a las {due} y el más reciente sigue siendo de {newest}. Revise el registro de la tarea programada.",
	"status.conflict.title":              "Otro agente también crea snapshots",
	"status.lowSpace.title":              "Queda poco espacio libre, así que la retención no está garantizada",
	"status.simulated.title":             "Estas lecturas son simuladas",
	"status.fdaWarning":                  "Conceder Acceso total al disco a esta aplicación puede no bastar: la tarea programada se ejecuta como un programa aparte y también lo necesita.",
	"tray.coverageCaption":               "Últimos dos días (cada marca es una hora)",
	"tray.couldNotRead":                  "No se pudo leer el estado de los snapshots",
	"tray.newest":                        "Más reciente: {when}",
	"tray.takeSnapshot":                  "Crear un snapshot ahora",
	"tray.openWindow":                    "Abrir Snapshotter",
	"tray.quit":                          "Salir",
	"notify.scheduleRestored.title":      "Snapshotter restauró su programación",
	"notify.scheduleRestored.body":       "Algo había eliminado {what}, probablemente una actualización. Vuelve a estar activo.",
	"notify.what.schedule":               "la programación",
	"notify.what.tripwire":               "el vigilante de borrados masivos",
	"notify.what.both":                   "la programación y el vigilante de borrados masivos",
	"status.noSchedule.detail":           "macOS solo programa snapshots locales cuando Time Machine tiene un destino de copia. Sin él, y sin esta programación, el snapshot de hoy es el último.",
	"status.scheduleNotRunning.detail":   "launchd tiene el trabajo en disco pero no lo ha cargado, así que no se creará ningún snapshot.",
}
