// Package watch takes a snapshot when something starts deleting in bulk.
//
// # What this can and cannot do
//
// FSEvents reports what has already happened. By the time a deletion is
// observed, that file is gone, and no snapshot taken afterwards will bring it
// back. This is a tripwire, not an interlock: it cannot prevent a deletion,
// only stop one from running to completion.
//
// That is still worth having, because the losses that hurt are rarely
// instantaneous. A cleanup script working through ten thousand files takes
// seconds to minutes; tripping at the two-hundredth saves the rest. An `rm -rf`
// of one small directory is over before anything can react, and this will not
// help.
//
// Preventing a deletion would mean interposing on the filesystem — Endpoint
// Security AUTH events — which needs an Apple-granted entitlement and root.
// Out of scope, and worth saying plainly rather than implying otherwise.
package watch

import (
	"sync"
	"time"
)

// Defaults chosen so that ordinary work never trips the wire.
//
// Builds, package installs and browser caches all delete steadily; a burst of
// two hundred inside five seconds is not that. The cooldown matters more than
// the threshold: without it, one long deletion trips repeatedly and fills the
// disk with snapshots of a disk that is being emptied.
const (
	DefaultThreshold = 200
	DefaultWindow    = 5 * time.Second
	DefaultCooldown  = 10 * time.Minute
)

// Trigger decides when a burst of deletions is worth a snapshot.
//
// It is deliberately separate from the event source: the decision is the part
// worth testing, and testing it should not require deleting real files.
type Trigger struct {
	// Threshold is how many deletions inside Window constitute a burst.
	Threshold int
	// Window is how far back a deletion still counts toward the burst.
	Window time.Duration
	// Cooldown is the shortest gap between two triggered snapshots.
	Cooldown time.Duration

	mu     sync.Mutex
	events []time.Time
	last   time.Time
}

// NewTrigger builds a Trigger, filling in any zero field with its default.
func NewTrigger(threshold int, window, cooldown time.Duration) *Trigger {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	if window <= 0 {
		window = DefaultWindow
	}
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	return &Trigger{Threshold: threshold, Window: window, Cooldown: cooldown}
}

// Deletion records one observed deletion and reports whether it completes a
// burst that should be snapshotted.
//
// Returning true also starts the cooldown, so a caller that ignores the result
// still gets correct rate limiting on the next call.
func (t *Trigger) Deletion(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := now.Add(-t.Window)
	kept := t.events[:0]
	for _, at := range t.events {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	t.events = append(kept, now)

	if len(t.events) < t.Threshold {
		return false
	}
	if !t.last.IsZero() && now.Sub(t.last) < t.Cooldown {
		return false
	}
	t.last = now
	// The burst is spent: a snapshot now covers everything still on disk, and
	// the next one should need a fresh burst rather than the tail of this one.
	t.events = t.events[:0]
	return true
}

// Pending reports how many deletions are currently inside the window, which is
// what an interface would show while a burst is building.
func (t *Trigger) Pending(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := now.Add(-t.Window)
	n := 0
	for _, at := range t.events {
		if at.After(cutoff) {
			n++
		}
	}
	return n
}
