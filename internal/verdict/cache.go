// Package verdict remembers whether a folder differs from a snapshot, and
// forgets when the disk says it might have changed.
//
// Answering costs a walk. Not a slow one — a tree of 192,635 files takes 456ms,
// because size and modification time both arrive with the directory read — but a
// walk all the same, and browsing repeats it constantly: open a folder, look
// inside, come back, and every sibling is walked again to reach the same
// conclusion as a moment ago. Nothing changed in between, so nothing needed
// recomputing.
//
// What makes this safe rather than merely fast is the shape of the two sides. A
// snapshot is read-only and immutable, so it can never invalidate anything. Only
// the live disk can, and the filesystem will say when it does — so an answer is
// kept exactly until the thing it describes is touched.
//
// Nothing is written to disk. A cache that outlives the process would have to be
// right about everything that happened while the process was gone, which is a
// promise this cannot keep and does not need to make: the cost of a cold start is
// one walk, which is what every start costs today.
package verdict

import (
	"path/filepath"
	"strings"
	"sync"
)

// Verdict is what was concluded about one folder.
type Verdict string

const (
	Same     Verdict = "same"
	Modified Verdict = "modified"
	// Unknown is a folder that could not be answered for — something inside it
	// could not be read, or it was large enough to hit the walk's backstop. It is
	// cached like any other answer: the reason will not have changed either.
	Unknown Verdict = "notExamined"
	// Ignored is a folder the change-detection ignore list says not to look
	// inside. Cached like any other answer, and cheap to reach without the cache —
	// but held all the same, so a listing does not have two routes to the same row
	// that could word it differently.
	Ignored Verdict = "ignored"
)

// Answer is a verdict and whatever has to be said alongside it.
//
// The note exists because a folder can be unchanged in everything that was read
// and still have had something skipped, and those are different sentences. It is
// held with the verdict rather than recomputed at the point of display: an
// earlier version cached the word alone, so the sentence appeared the first time
// a folder was looked at and silently vanished the second.
type Answer struct {
	Verdict Verdict
	Note    string
	// ChangedPath is the live path of the one difference this verdict rests on, empty
	// unless the verdict is Modified.
	//
	// A walk stops at the first difference, so a "changed" verdict is really a
	// claim about a single file. Keeping that file's path means the claim can be
	// re-established by looking at it alone — and, because one difference anywhere
	// means the whole tree differs, the same look answers for every folder between
	// that file and wherever the question was asked.
	ChangedPath string
}

// Recorder keeps recorded changes between runs.
//
// Only changes. There is deliberately no way to persist an unchanged verdict:
// that would be a claim about everything that happened while this application was
// not running, which nothing can keep. A recorded change is safe to keep for ever
// because it is re-checked before it is ever believed.
//
// Nil is allowed and means everything is forgotten when the process exits, which
// is what the command line wants: it asks once and leaves.
type Recorder interface {
	Record(snapshot, path string) error
	Forget(snapshot, path string) error
	Under(snapshot, folder string) (string, bool)
	ForgetSnapshot(snapshot string) error
	Clear() error
	// Rules is the settings fingerprint the stored differences were found under,
	// empty when nothing has been stored yet.
	Rules() string
	SetRules(fingerprint string) error
}

// Cache holds verdicts for one running window.
type Cache struct {
	mu sync.RWMutex
	// Keyed by snapshot name and then by live path. Two keys rather than a joined
	// string because forgetting everything about one snapshot — when it is
	// unmounted — should not mean walking every entry looking for a prefix.
	entries map[string]map[string]Answer
	// rules is a fingerprint of the settings the held verdicts were reached
	// under. See UnderRules.
	rules string
	// kept is where changes are written so they outlive the process. Nil means
	// they do not.
	kept Recorder
	// changed holds the known differences, per snapshot, as a set of live paths.
	//
	// Held apart from entries because they are looked up by a different question.
	// entries answers "what was decided about this exact folder"; this answers
	// "is there a known difference anywhere under this folder", which is what lets
	// a folder nobody has asked about before be answered without a walk.
	changed map[string]map[string]bool
}

func New() *Cache {
	return &Cache{
		entries: map[string]map[string]Answer{},
		changed: map[string]map[string]bool{},
	}
}

// UnderRules discards everything if the settings have changed since the held
// verdicts were reached.
//
// The cache is keyed by snapshot and path, which is enough while the question
// stays the same. The change-detection ignore list changes the question: a folder
// answered "unchanged" before node_modules was ignored, and one answered
// "unchanged" after, are the same word about different things — and the second is
// entitled to be reached without walking what the first walked.
//
// So the caller passes a fingerprint of the rules in force and this notices when
// it moves. Checked here rather than by the caller because the callers are the
// browser's workers, several at once: the check and the clear have to be one
// action, and only this side holds the lock that can make them one.
//
// Clearing rather than re-keying: a settings change is rare and a rebuilt cache
// costs one round of walking, whereas keeping both generations would keep the
// stale one alive with no way to tell it had stopped being true.
func (c *Cache) UnderRules(fingerprint string) {
	// Under the READ lock in the common case, which is every call but the one
	// after a setting changes.
	//
	// Browsing calls this once per folder row, and the settings change perhaps
	// once a day. Taking the write lock for that meant every row queued against
	// Touched, which the filesystem watcher calls once per event — a flood while a
	// volume is being written to. The measured cost of getting this wrong was
	// small, so it is not the reason for any particular slowness; it is simply the
	// wrong lock for a question that is almost always answered "no change".
	c.mu.RLock()
	same := c.rules == fingerprint
	c.mu.RUnlock()
	if same {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Asked again, because the lock was released in between and another caller
	// may have done this already. Without the re-check, three browse workers
	// noticing one settings change would clear the cache three times, throwing
	// away the answers the other two had just put there.
	if c.rules == fingerprint {
		return
	}
	c.rules = fingerprint
	c.entries = map[string]map[string]Answer{}
	c.changed = map[string]map[string]bool{}
	if c.kept != nil {
		// What was recorded under the old settings is not an answer to the new
		// question either, and it outlives the process, so it has to go too — and
		// the new settings are recorded beside the empty table, so the next run
		// adopts them rather than clearing it again.
		_ = c.kept.Clear()
		_ = c.kept.SetRules(fingerprint)
	}
}

// Persist gives the cache somewhere to keep recorded changes between runs.
//
// Called once at startup. A cache without one still works exactly as it did:
// everything is an optimisation, and the cost of losing it is walking, which is
// what every start used to cost.
func (c *Cache) Persist(r Recorder) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.kept = r
	// The settings the stored differences were found under are adopted, not
	// compared against. A new cache starts with no fingerprint at all, so without
	// this the first lookup of every run would see a mismatch and throw away the
	// table it had just opened — which is precisely the work it exists to save.
	c.rules = r.Rules()
}

// Get returns a remembered verdict, if there is one.
func (c *Cache) Get(snapshot, livePath string) (Answer, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	a, ok := c.entries[snapshot][filepath.Clean(livePath)]
	return a, ok
}

// Put remembers a verdict.
func (c *Cache) Put(snapshot, livePath string, a Answer) {
	var kept Recorder

	c.mu.Lock()
	byPath, ok := c.entries[snapshot]
	if !ok {
		byPath = map[string]Answer{}
		c.entries[snapshot] = byPath
	}
	byPath[filepath.Clean(livePath)] = a

	if a.ChangedPath != "" {
		known, ok := c.changed[snapshot]
		if !ok {
			known = map[string]bool{}
			c.changed[snapshot] = known
		}
		known[filepath.Clean(a.ChangedPath)] = true
		kept = c.kept
	}
	c.mu.Unlock()

	// Outside the lock, and only for a change. Writing to a file while holding
	// the lock that every browse worker needs would put a disk between them and
	// their next answer — and the unchanged half is deliberately not written at
	// all, because keeping it would be a promise about a period nobody watched.
	if kept != nil {
		_ = kept.Record(snapshot, a.ChangedPath)
	}
}

// ChangedPathUnder returns a known difference somewhere beneath a folder.
//
// The caller is expected to re-check it rather than trust it: this says where a
// difference was last seen, not that one is there now. What makes it worth having
// is the asymmetry — confirming one path is a stat, and proving the same folder
// unchanged is a walk of everything beneath it.
//
// The shallowest is returned when there are several. A change recorded close to
// the folder being asked about is the one most likely to still be there, and its
// re-check reads fewer directories on the way.
func (c *Cache) ChangedPathUnder(snapshot, folder string) (string, bool) {
	dir := filepath.Clean(folder)

	c.mu.RLock()
	var best string
	for path := range c.changed[snapshot] {
		if !isAncestor(dir, path) && path != dir {
			continue
		}
		if best == "" || len(path) < len(best) {
			best = path
		}
	}
	kept := c.kept
	c.mu.RUnlock()

	if best != "" {
		return best, true
	}
	// Nothing in memory. What was recorded on a previous run is just as good:
	// neither is believed without being re-checked, which is the whole reason
	// either can be kept at all.
	if kept == nil {
		return "", false
	}
	return kept.Under(snapshot, dir)
}

// ForgetChangedPath drops a difference that is no longer there.
//
// Called when a re-check finds the file matching again. Keeping it would mean
// paying for a stat that can never succeed before every walk it was meant to
// save.
func (c *Cache) ForgetChangedPath(snapshot, path string) {
	c.mu.Lock()
	delete(c.changed[snapshot], filepath.Clean(path))
	kept := c.kept
	c.mu.Unlock()

	if kept != nil {
		_ = kept.Forget(snapshot, path)
	}
}

// ChangedPaths reports how many differences are remembered for one snapshot, for
// tests and for deciding whether any of this is worth its keep.
func (c *Cache) ChangedPaths(snapshot string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.changed[snapshot])
}

// Touched forgets everything whose answer the path could have changed: the path
// itself, and every folder above it.
//
// Upwards is the whole point. A file edited five levels down changes the verdict
// of all five folders containing it, and the filesystem will not tell you that —
// a directory's modification time moves only when an entry is added, removed or
// renamed directly inside it, which is exactly why the verdict has to be walked
// for rather than read off. Given the path of the thing that changed, though,
// the ancestors are just the path taken apart.
func (c *Cache) Touched(livePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := filepath.Clean(livePath)
	for _, byPath := range c.entries {
		for known := range byPath {
			if known == path || isAncestor(known, path) {
				delete(byPath, known)
			}
		}
	}
	// The recorded changes too, and for the opposite reason: a verdict is
	// forgotten because something beneath it moved, and a recorded change is
	// forgotten because that path itself moved. Keeping a stale one would send
	// every future check to a file that has been put back, which costs a stat and
	// proves nothing.
	for _, known := range c.changed {
		for w := range known {
			if w == path || isAncestor(w, path) {
				delete(known, w)
			}
		}
	}
}

// Forget drops everything known about one snapshot, for when it is unmounted and
// its side of every comparison has gone away.
func (c *Cache) Forget(snapshot string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, snapshot)
	delete(c.changed, snapshot)
	kept := c.kept
	// Deferred out of the lock the same way as the rest: the snapshot is gone and
	// nothing is waiting on the row being removed.
	defer func() {
		if kept != nil {
			_ = kept.ForgetSnapshot(snapshot)
		}
	}()
}

// Len reports how many verdicts are held, for tests and for deciding whether any
// of this is worth its keep.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	n := 0
	for _, byPath := range c.entries {
		n += len(byPath)
	}
	return n
}

// isAncestor reports whether dir contains path.
//
// Compared with a separator appended, so that /a/b is not treated as containing
// /a/bc — a mistake that would forget the wrong folder and, worse, keep the right
// one.
func isAncestor(dir, path string) bool {
	if dir == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
