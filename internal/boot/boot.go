// Package boot applies the settings that affect a whole process.
//
// It exists because this program has four entry paths — the window, the scheduled
// snapshot agent, the bulk-deletion watcher, and the command line — and each was
// applying its own subset of the settings. That went wrong twice in one day. The
// agents never set the language, so a snapshot failing overnight posted its
// notification in English to someone who had chosen German; and the command line
// returned before the only call to trace.SetEnabled, so `logging.verbose` was
// silently ignored for every command.
//
// Both were the same fault: there was no answer to "what must every process do
// before it prints anything", only four places that each remembered part of it.
// This is that answer, and adding a fifth process-wide setting means editing one
// function rather than finding four.
package boot

import (
	"snapshotter/internal/config"
	"snapshotter/internal/i18n"
	"snapshotter/internal/trace"
)

// Apply pushes the process-wide settings into the packages that hold them.
//
// Safe to call more than once and from any entry path, which matters because the
// settings watcher calls it again every time the file changes.
func Apply(cfg config.Config) {
	// Which words everything speaks: the window, the menu bar, the notifications
	// the agents post, and the command line.
	i18n.SetLanguage(cfg.Language())

	// Whether the verbose channel is open. Toggled without a relaunch, because
	// restarting to look at a problem is how you lose the problem.
	trace.SetEnabled(cfg.Logging.Verbose)
}

// ApplyFromFile reads the settings and applies them, for the entry paths that
// have no configuration in hand yet.
//
// A file that cannot be read leaves the defaults in force rather than failing:
// every one of these callers is on its way to doing something useful, and none of
// them is improved by refusing to start over a settings file.
func ApplyFromFile() config.Config {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Defaults()
	}
	Apply(cfg)
	return cfg
}
