package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rjeczalik/notify"
)

// eventBuffer is deliberately large. notify drops events rather than blocking
// when its channel is full, and a burst of deletions is exactly the moment
// events arrive fastest — dropping them would blind the watcher precisely when
// it is needed.
const eventBuffer = 8192

// Watcher trips when deletions arrive in bulk under any one of its roots.
//
// "Any one" rather than "any": each root is counted separately, so deletions
// spread across two watched directories are two quiet afternoons rather than one
// burst. See Trigger.
type Watcher struct {
	// Roots are the directories to watch, each recursively, and they are the whole
	// of what is watched. A deletion outside all of them is not seen and does not
	// count.
	//
	// This used to be the home directory and nothing else. Naming directories
	// instead is what makes the tripwire affordable: ~/Library alone produces
	// bursts all day, every one of which cost a whole-volume snapshot.
	Roots   []string
	Trigger *Trigger
	// Snapshot is called when a burst trips the wire. It is called from the
	// watch loop, so a slow implementation delays detection of the next burst;
	// tmutil takes a second or so, which is well inside the cooldown.
	// where names the directories the burst happened in, commonest first, so the
	// caller can say more than "something is deleting files".
	Snapshot func(ctx context.Context, where []string) error

	// Ignore lists path fragments whose deletions do not count towards a burst.
	//
	// A browser clearing its cache deletes hundreds of files in seconds — the
	// exact shape of the thing being watched for, and none of its meaning. Left
	// in, it trips the wire, takes a snapshot nobody needs, and teaches the user
	// that these warnings are noise.
	Ignore []string
	// Log reports what happened. Optional.
	Log func(format string, args ...any)
	// Now is swappable for tests.
	Now func() time.Time
}

// scope is one watched root, in both the form it was configured in and the form
// FSEvents will report.
//
// Both are needed because they differ in practice and the difference is silent.
// FSEvents reports the resolved path — /private/tmp, never /tmp — so matching an
// event against the configured spelling would attribute nothing and the wire
// would never trip. The configured spelling is kept for messages, because it is
// the one the person recognises.
type scope struct {
	configured string
	resolved   string
}

// New builds a Watcher with the default thresholds.
func New(roots []string, snapshot func(ctx context.Context, where []string) error) *Watcher {
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
	scopes := make([]scope, 0, len(w.Roots))
	for _, root := range w.Roots {
		// "/..." is notify's recursive form.
		if err := notify.Watch(filepath.Join(root, "..."), events, notify.Remove, notify.Rename); err != nil {
			notify.Stop(events)
			return fmt.Errorf("watch: watching %s: %w", root, err)
		}
		scopes = append(scopes, newScope(root))
	}
	defer notify.Stop(events)

	// Named, not counted. "watching 2 locations" is unfalsifiable from a log; the
	// names are what someone checks when the wire did not trip on a directory they
	// were sure they had added.
	w.logf("watching %s for bulk deletion: %d removals from any one of them within %s trips a snapshot",
		Places(w.Roots), w.Trigger.Threshold, w.Trigger.Window)

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
			if w.ignored(ev.Path()) {
				continue
			}
			// Which watched directory this belongs to, which is the counter it
			// counts against. Nothing is watched that is under none of them, so
			// there should always be one; a miss is not worth guessing about, and
			// counting it against a directory it is not in would be worse than
			// dropping it.
			root, ok := within(scopes, ev.Path())
			if !ok {
				continue
			}
			tripped, where := w.Trigger.Deletion(w.now(), root, ev.Path())
			if !tripped {
				continue
			}
			w.logf("bulk deletion in %s — taking a snapshot", Places(where))
			if err := w.Snapshot(ctx, where); err != nil {
				w.logf("snapshot after bulk deletion failed: %v", err)
				continue
			}
			w.logf("snapshot taken; whatever is still on disk is now recoverable")
		}
	}
}

// Places words a list of directories for a person: home-relative where it can be,
// because "~/Documents/Invoices" is read at a glance and the absolute path is
// not, and joined with commas because a burst is usually in one place and
// occasionally in two.
func Places(dirs []string) string {
	if len(dirs) == 0 {
		return "an unknown location"
	}
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, Shorten(dir))
	}
	return strings.Join(out, ", ")
}

// Shorten writes a path the way a person writes it, with the home directory as
// "~". Twenty characters of "/Users/somebody" carry no information at all on the
// machine they refer to.
//
// Only for display. What gets stored, matched and ignored is always the full
// path: "~" means nothing to a string comparison against an FSEvents path.
func Shorten(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(dir, home+string(os.PathSeparator)) {
		return dir
	}
	return "~" + strings.TrimPrefix(dir, home)
}

// newScope resolves a configured root to the form FSEvents will report it in.
//
// A root that cannot be resolved keeps its configured spelling rather than being
// dropped: the common reason is that it does not exist yet, and a watcher that
// silently stops covering a directory because it was missing for a moment is the
// failure this whole feature is meant to avoid.
func newScope(root string) scope {
	s := scope{configured: root, resolved: root}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		s.resolved = real
	}
	return s
}

// within reports which watched root a path lies under, longest match first, and
// returns that root in the spelling it was configured with.
//
// Longest match, so that a directory watched inside another watched directory
// keeps its own count rather than feeding its parent's. Configured spelling, so
// that everything downstream — the log, the counter, anything a person reads —
// says the directory back to them the way they wrote it.
func within(scopes []scope, path string) (string, bool) {
	best, found := scope{}, false
	for _, s := range scopes {
		if !under(s.resolved, path) && !under(s.configured, path) {
			continue
		}
		if !found || len(s.resolved) > len(best.resolved) {
			best, found = s, true
		}
	}
	return best.configured, found
}

// under reports whether path is dir or lies inside it.
//
// The separator is required rather than assumed, because a prefix test without it
// puts everything in "~/projects-archive" inside "~/projects" — two directories a
// person would never confuse and a string comparison cannot tell apart.
func under(dir, path string) bool {
	if dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(os.PathSeparator))+string(os.PathSeparator))
}

// ignored reports whether a path is machine churn rather than anybody's work.
func (w *Watcher) ignored(path string) bool {
	for _, fragment := range w.Ignore {
		if fragment != "" && strings.Contains(path, fragment) {
			return true
		}
	}
	return false
}
