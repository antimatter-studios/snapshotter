package watch

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/rjeczalik/notify"
)

// eventBuffer is deliberately large. notify drops events rather than blocking
// when its channel is full, and a burst of deletions is exactly the moment
// events arrive fastest — dropping them would blind the watcher precisely when
// it is needed.
const eventBuffer = 8192

// Watcher trips when deletions arrive in bulk under any of its roots.
type Watcher struct {
	// Roots are watched recursively.
	Roots   []string
	Trigger *Trigger
	// Snapshot is called when a burst trips the wire. It is called from the
	// watch loop, so a slow implementation delays detection of the next burst;
	// tmutil takes a second or so, which is well inside the cooldown.
	Snapshot func(context.Context) error
	// Log reports what happened. Optional.
	Log func(format string, args ...any)
	// Now is swappable for tests.
	Now func() time.Time
}

// New builds a Watcher with the default thresholds.
func New(roots []string, snapshot func(context.Context) error) *Watcher {
	return &Watcher{
		Roots:    roots,
		Trigger:  NewTrigger(0, 0, 0),
		Snapshot: snapshot,
		Now:      time.Now,
	}
}

func (w *Watcher) logf(format string, args ...any) {
	if w.Log != nil {
		w.Log(format, args...)
	}
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Run watches until the context is cancelled.
//
// On macOS notify is backed by FSEvents, so one recursive watch per root costs
// one stream rather than one file descriptor per directory. Watching a whole
// home directory is therefore reasonable, which a kqueue-based watcher could
// not manage.
func (w *Watcher) Run(ctx context.Context) error {
	if len(w.Roots) == 0 {
		return fmt.Errorf("watch: nothing to watch")
	}
	if w.Snapshot == nil {
		return fmt.Errorf("watch: no snapshot function")
	}
	if w.Trigger == nil {
		w.Trigger = NewTrigger(0, 0, 0)
	}

	events := make(chan notify.EventInfo, eventBuffer)
	for _, root := range w.Roots {
		// "/..." is notify's recursive form.
		if err := notify.Watch(filepath.Join(root, "..."), events, notify.Remove, notify.Rename); err != nil {
			notify.Stop(events)
			return fmt.Errorf("watch: watching %s: %w", root, err)
		}
	}
	defer notify.Stop(events)

	w.logf("watching %d location(s) for bulk deletion: %d removals within %s trips a snapshot",
		len(w.Roots), w.Trigger.Threshold, w.Trigger.Window)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-events:
			// A rename is how a move out of a watched tree appears, and is a
			// disappearance from the watcher's point of view.
			if ev.Event() != notify.Remove && ev.Event() != notify.Rename {
				continue
			}
			if !w.Trigger.Deletion(w.now()) {
				continue
			}
			w.logf("bulk deletion detected near %s — taking a snapshot", ev.Path())
			if err := w.Snapshot(ctx); err != nil {
				w.logf("snapshot after bulk deletion failed: %v", err)
				continue
			}
			w.logf("snapshot taken; whatever is still on disk is now recoverable")
		}
	}
}
