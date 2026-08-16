package config

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/rjeczalik/notify"
)

// Watching the settings file, so a change takes effect where it can rather than
// at the next launch.
//
// Editing a file and being told to restart is a poor answer when nothing about
// the change requires it. Most of what is in here — how often to refresh, how
// big the window is, where things are written — can be applied to a running
// application, and the parts that cannot are the parts worth being explicit
// about rather than lumping in with the rest.

// debounce is how long to wait for the writing to settle.
//
// Save writes a temporary file and renames it over the target, and an editor
// may do the same or may truncate and rewrite. Either way a single logical edit
// arrives as several events, and reloading on each one means reading a file
// mid-write.
const debounce = 150 * time.Millisecond

// Watch reports the settings each time they change on disk, until ctx is done.
//
// The directory is watched rather than the file: Save replaces the file by
// renaming a new one over it, which leaves any watch on the old path pointing at
// an inode nothing will ever write to again. Watching the directory survives
// that, at the cost of having to filter for the one name that matters.
//
// A change that cannot be read is not delivered — the previous settings stay in
// force, which is the same rule Load follows. Someone half way through editing
// YAML should not have the application react to the half.
func Watch(ctx context.Context) (<-chan Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	// The directory may not exist yet: a machine with no settings file is
	// ordinary, and someone creating one later should still be noticed.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	events := make(chan notify.EventInfo, 16)
	if err := notify.Watch(dir, events, notify.All); err != nil {
		return nil, err
	}

	out := make(chan Config)
	go func() {
		defer notify.Stop(events)
		defer close(out)

		var timer <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return

			case ev := <-events:
				// Compared by name rather than by path. FSEvents reports the
				// resolved path, and on macOS the usual temporary and home
				// directories are symlinks — /var/folders is /private/var/folders
				// — so the two spellings never match and every event would be
				// discarded as belonging to some other file. Only one directory is
				// being watched, so the name is enough to identify the file, and it
				// also filters out the temporary file Save renames into place.
				if filepath.Base(ev.Path()) != name {
					continue
				}
				// Restarted on every event, so a burst collapses into one reload
				// once the writing stops.
				timer = time.After(debounce)

			case <-timer:
				timer = nil
				cfg, err := Load()
				if err != nil {
					// Unreadable: keep what is already in force and say nothing.
					// Load has already reported it, and an application that reacts
					// to a half-written file is worse than one that waits.
					continue
				}
				select {
				case out <- cfg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}
