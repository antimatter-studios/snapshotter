// Applying settings to a running application, so that changing one does not mean
// relaunching.
package main

import (
	"context"
	"log"
	"path/filepath"
	"snapshotter/internal/config"
	"snapshotter/internal/i18n"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/trace"
	"snapshotter/internal/verdict"
	"snapshotter/services"

	"github.com/rjeczalik/notify"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// watchSettings applies changes to the settings file as they are made.
func watchSettings(ctx context.Context, p paths, deps services.Deps, win application.Window, applyToTray func(config.Config)) {
	changes, err := config.Watch(ctx)
	if err != nil {
		// Not fatal. Everything already read at startup stays in force; the only
		// loss is that a later edit needs a relaunch, which is where this began.
		log.Printf("settings are not being watched, so changes need a relaunch: %v", err)
		return
	}
	for cfg := range changes {
		applySettings(cfg, p, deps, win, applyToTray)
	}
}

// applySettings pushes one set of settings into the running application.
//
// What is applied here, and what is not:
//
//   - the window's size, the refresh intervals and the theme take effect at once;
//   - paths take effect for work that has not started yet. A mount already
//     attached stays where it is, because a mounted filesystem cannot be moved by
//     editing a file, and an installed agent keeps writing to the log its plist
//     names until the agent is installed again;
//   - nothing here installs or removes an agent. Restore does that at startup
//     from what was asked for, and a file being saved is not the same as a
//     request to start taking snapshots.
func applySettings(cfg config.Config, p paths, deps services.Deps, win application.Window, applyToTray func(config.Config)) {
	// Turned on and off without a relaunch, which is the point: restarting to
	// look at a problem is how you lose the problem.
	trace.SetEnabled(cfg.Logging.Verbose)

	// Before the tray is redrawn, so the menu it builds is in the new language
	// rather than one change behind. This is what makes choosing a language in
	// the window reach the menu bar without a relaunch: the window writes the
	// setting, the watcher above notices the file, and this applies it.
	i18n.SetLanguage(cfg.Language())

	applyToTray(cfg)

	width, height := cfg.WindowSize()
	win.SetSize(width, height)

	// The manager holds the root it was built with; changing it here means the
	// next mount lands in the new place. Only the concrete type has a root to
	// change — a fake mounter has its own and is not configurable.
	if m, ok := deps.Mounts.(*mountmgr.Manager); ok {
		m.Root = config.ResolvePath(cfg.Paths.MountRoot, p.mountRoot)
	}

	// Where a future install will point launchd. The plist already on disk keeps
	// naming the old log until it is written again.
	if deps.Agent != nil {
		deps.Agent.LogPath = config.ResolvePath(cfg.Paths.Log, p.logPath)
	}
	if deps.Tripwire != nil {
		deps.Tripwire.LogPath = config.ResolvePath(cfg.Paths.TripwireLog, p.tripwireLogPath)
	}

	log.Printf("settings reloaded: window %dx%d, menu bar every %s, window every %s",
		width, height, cfg.MenuBarRefresh(), cfg.WindowRefresh())
}

// watchForChanges forgets cached folder verdicts as the disk moves under them.
//
// A verdict is only ever invalidated by the live side — a snapshot is read-only
// — and the filesystem is what knows when that happens. What it does not say is
// which folders an event affects: a file edited five levels down changes the
// answer for all five above it, because a directory's modification time moves
// only when something is added, removed or renamed directly inside it. Given the
// path that changed, though, the ancestors are just that path taken apart.
func watchForChanges(ctx context.Context, root string, cache *verdict.Cache) {
	events := make(chan notify.EventInfo, 1024)
	// Recursive, and deliberately every kind of event: a rename is a deletion
	// from one folder and a creation in another, and either can change a verdict.
	if err := notify.Watch(filepath.Join(root, "..."), events, notify.All); err != nil {
		// Not fatal. Without it every answer is computed afresh, which is what
		// the application did before the cache existed.
		log.Printf("cached folder verdicts will not be refreshed automatically: %v", err)
		return
	}
	defer notify.Stop(events)

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			cache.Touched(ev.Path())
		}
	}
}
