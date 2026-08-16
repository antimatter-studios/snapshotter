// Applying settings to a running application, so that changing one does not mean
// relaunching.
package main

import (
	"context"
	"log"
	"snapshotter/internal/config"
	"snapshotter/internal/mountmgr"
	"snapshotter/services"

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
