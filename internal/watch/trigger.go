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
	"path/filepath"
	"sort"
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

	mu sync.Mutex
	// events holds the burst still inside the window. Each carries the directory
	// it happened in, not the file: the file is gone and its name is of no use,
	// whereas "two hundred things vanished from Documents/Invoices" is the whole
	// of what someone needs to decide whether they did it on purpose.
	events []deletion
	last   time.Time
}

// deletion is one disappearance: when, and which directory it was in.
type deletion struct {
	at  time.Time
	dir string
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

// Deletion records one observed deletion, in the directory the file was in, and
// reports whether it completes a burst that should be snapshotted.
//
// Returning true also starts the cooldown, so a caller that ignores the result
// still gets correct rate limiting on the next call. When it returns true it also
// returns where the burst happened, commonest place first — the burst is cleared
// on the way out, so this is the only chance to ask.
func (t *Trigger) Deletion(now time.Time, path string) (bool, []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := now.Add(-t.Window)
	kept := t.events[:0]
	for _, e := range t.events {
		if e.at.After(cutoff) {
			kept = append(kept, e)
		}
	}
	t.events = append(kept, deletion{at: now, dir: filepath.Dir(path)})

	if len(t.events) < t.Threshold {
		return false, nil
	}
	if !t.last.IsZero() && now.Sub(t.last) < t.Cooldown {
		return false, nil
	}
	t.last = now
	where := placesLocked(t.events)
	// The burst is spent: a snapshot now covers everything still on disk, and
	// the next one should need a fresh burst rather than the tail of this one.
	t.events = t.events[:0]
	return true, where
}

// maxPlacesReported is how many directories are worth naming.
//
// A burst usually happens in one place, sometimes two. Past a handful the list
// stops being a description and becomes a wall of paths in a notification, which
// is read as noise and dismissed.
const maxPlacesReported = 3

// placesLocked returns the directories in a burst, commonest first. The caller
// holds the lock.
func placesLocked(events []deletion) []string {
	counts := map[string]int{}
	for _, e := range events {
		counts[e.dir]++
	}
	dirs := make([]string, 0, len(counts))
	for dir := range counts {
		dirs = append(dirs, dir)
	}
	// By count, then by name: a stable order matters because this ends up in a
	// notification and a log, and two runs over the same burst should read the
	// same way.
	sort.Slice(dirs, func(i, j int) bool {
		if counts[dirs[i]] != counts[dirs[j]] {
			return counts[dirs[i]] > counts[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	if len(dirs) > maxPlacesReported {
		dirs = dirs[:maxPlacesReported]
	}
	return dirs
}

// Pending reports how many deletions are currently inside the window, which is
// what an interface would show while a burst is building.
func (t *Trigger) Pending(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := now.Add(-t.Window)
	n := 0
	for _, e := range t.events {
		if e.at.After(cutoff) {
			n++
		}
	}
	return n
}

// Sensitivity is how readily a burst of deletions is treated as one worth
// snapshotting, expressed as a name rather than a count.
//
// A name, because the number on its own is unanswerable: nobody knows whether two
// hundred files in five seconds is a lot without knowing what their machine does
// all day. The names say what each setting is for, and the count is shown beside
// them for whoever wants it.
//
// Only the count varies. The window stays at five seconds across all of them,
// which keeps this one dimension: widening the window makes slower deletions count
// too, and a setting that moves two things at once cannot be reasoned about from
// its name.
type Sensitivity string

const (
	// Cautious only notices an unmistakable sweep. For a machine that deletes in
	// bulk as a matter of course — build trees, container layers, video caches —
	// where a lower setting means warnings nobody reads.
	Cautious Sensitivity = "cautious"
	// Balanced is the default, and what every build before this setting existed
	// used. Ordinary work does not reach it: builds, package installs and browser
	// caches all delete steadily, and a burst of two hundred inside five seconds is
	// not that.
	Balanced Sensitivity = "balanced"
	// Sensitive catches a folder of documents going, which Balanced can miss:
	// seventy-five invoices is a bad afternoon and well under two hundred.
	Sensitive Sensitivity = "sensitive"
	// VerySensitive is for a machine holding work that could not be reproduced,
	// where a snapshot too many costs disk and a snapshot too few costs the work.
	VerySensitive Sensitivity = "very-sensitive"
)

// thresholds is the count each name stands for.
//
// Balanced is DefaultThreshold rather than a repetition of it, so the setting and
// the default cannot drift apart.
var thresholds = map[Sensitivity]int{
	Cautious:      500,
	Balanced:      DefaultThreshold,
	Sensitive:     75,
	VerySensitive: 25,
}

// Sensitivities are the settings on offer, coarsest first. Ordered, because a
// dropdown reading cautious to very sensitive is a scale and a map is not.
var Sensitivities = []Sensitivity{Cautious, Balanced, Sensitive, VerySensitive}

// ThresholdFor is how many deletions a sensitivity counts as a burst.
//
// An unrecognised name gets the default rather than an error: this arrives from a
// settings file that someone may have typed into, and a watcher that refuses to
// start over a misspelling would trade the protection for the typo.
func ThresholdFor(s Sensitivity) int {
	if n, ok := thresholds[s]; ok {
		return n
	}
	return DefaultThreshold
}

// Known reports whether a name is one of the settings on offer, so an interface
// can say so rather than silently showing something else as selected.
func Known(s Sensitivity) bool {
	_, ok := thresholds[s]
	return ok
}
