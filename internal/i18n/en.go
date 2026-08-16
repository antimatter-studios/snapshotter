package i18n

// en is English, and the key list every other catalogue must match.
//
// The test in this package requires the others to carry exactly these keys, so
// a string added here and forgotten elsewhere fails the build rather than
// appearing in English in the middle of a German menu.
var en = map[string]string{
	"status.covered":                     "You are covered",
	"status.noSnapshots.title":           "There are no snapshots",
	"status.noSnapshots.detail":          "Nothing can be rolled back to. Taking one now costs no disk space immediately, because a snapshot only grows as the files it recorded change.",
	"status.noSnapshotsShort":            "No snapshots — nothing to roll back to",
	"status.noSchedule.title":            "Nothing is taking snapshots automatically",
	"status.scheduleNotRunning.title":    "The schedule is installed but not running",
	"status.noTripwire.title":            "Nothing is watching for bulk deletion",
	"status.noTripwire.detail":           "A schedule limits how far back you can go; it does not stop a deletion finishing. The watcher takes a snapshot as soon as something starts removing files in bulk, so the rest of that deletion stays recoverable.",
	"status.tripwireNotRunning.title":    "The bulk-deletion watcher is not running",
	"status.tripwireNotRunning.detail":   "It is installed but launchd has not loaded it, so nothing is watching.",
	"status.timeMachineThins.title":      "Time Machine will thin these snapshots",
	"status.simulatedMounts.title":       "Mounts are simulated",
	"status.simulatedMounts.detail":      "SNAPSHOTTER_FAKE_MOUNTS is set. Everything inside a snapshot is invented for development, and Replace restores are refused. Nothing shown under a snapshot is real.",
	"status.scheduleMissingBinary.title": "The schedule points at a copy of Snapshotter that is gone",
	"status.overdue.title":               "The last snapshot is overdue",
	"status.overdue.detail":              "A snapshot was due at {due} and the newest is still from {newest}. Check the scheduled task's log.",
	"status.conflict.title":              "Another agent is also taking snapshots",
	"status.lowSpace.title":              "Free space is low, so retention is not guaranteed",
	"status.simulated.title":             "These readings are simulated",
	"status.fdaWarning":                  "Granting Full Disk Access to this application may not be enough on its own: the scheduled task runs as a separate program and needs it too.",
	"tray.coverageCaption":               "Last two days (mark represents an hour)",
	"tray.couldNotRead":                  "Could not read snapshot state",
	"tray.newest":                        "Newest: {when}",
	"tray.takeSnapshot":                  "Take a snapshot now",
	"tray.openWindow":                    "Open Snapshotter",
	"tray.quit":                          "Quit",
	"notify.scheduleRestored.title":      "Snapshotter restored your schedule",
	"notify.scheduleRestored.body":       "Something had removed {what}, most likely an upgrade. It is running again.",
	"notify.what.schedule":               "the schedule",
	"notify.what.tripwire":               "the bulk-deletion watcher",
	"notify.what.both":                   "the schedule and the bulk-deletion watcher",
	"status.noSchedule.detail":           "macOS only schedules local snapshots when Time Machine has a backup destination. Without one, and without this schedule, today's snapshot is the last one.",
	"status.scheduleNotRunning.detail":   "launchd has the job on disk but has not loaded it, so no snapshot will be taken.",
}
