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
)

// Cache holds verdicts for one running window.
type Cache struct {
	mu sync.RWMutex
	// Keyed by snapshot name and then by live path. Two keys rather than a joined
	// string because forgetting everything about one snapshot — when it is
	// unmounted — should not mean walking every entry looking for a prefix.
	entries map[string]map[string]Verdict
}

func New() *Cache {
	return &Cache{entries: map[string]map[string]Verdict{}}
}

// Get returns a remembered verdict, if there is one.
func (c *Cache) Get(snapshot, livePath string) (Verdict, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	v, ok := c.entries[snapshot][filepath.Clean(livePath)]
	return v, ok
}

// Put remembers a verdict.
func (c *Cache) Put(snapshot, livePath string, v Verdict) {
	c.mu.Lock()
	defer c.mu.Unlock()

	byPath, ok := c.entries[snapshot]
	if !ok {
		byPath = map[string]Verdict{}
		c.entries[snapshot] = byPath
	}
	byPath[filepath.Clean(livePath)] = v
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
}

// Forget drops everything known about one snapshot, for when it is unmounted and
// its side of every comparison has gone away.
func (c *Cache) Forget(snapshot string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, snapshot)
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
