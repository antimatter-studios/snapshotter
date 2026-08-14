// Package find searches inside mounted snapshots by name.
//
// Browsing assumes you know where to look. After a deletion you do not: you know
// what you lost, not which directory it was in, and often not which day it was
// still there. That is the question this answers — every version of a name,
// across every snapshot that is open, with where each one would be restored to.
package find

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"snapshotter/internal/vfs"
)

// Hit is one match inside one snapshot.
type Hit struct {
	// Snapshot and Stamp identify which snapshot held it.
	Snapshot string `json:"snapshot"`
	Stamp    string `json:"stamp"`
	// LivePath is where this would be restored to, in the running system's
	// terms, which is what the user recognises.
	LivePath string    `json:"livePath"`
	Name     string    `json:"name"`
	IsDir    bool      `json:"isDir"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"modTime"`
}

// Options bound a search. A snapshot is a whole volume, so an unbounded walk of
// several of them is not something to start by accident.
type Options struct {
	// Limit stops the walk once this many hits are found. Zero means DefaultLimit.
	Limit int
	// MaxEntries bounds the walk itself. Zero means DefaultMaxEntries.
	//
	// Limit alone is not enough, and a real mount is what showed it: it bounds
	// how many matches are collected, not how much tree is read, so a term with
	// few matches walks the entire snapshot. A snapshot is a whole volume — every
	// application, every framework, every other user's home — and over two of them
	// an unscoped search did not answer inside two minutes, which in the interface
	// is a spinner that never stops.
	MaxEntries int
	// Under restricts the search to one live directory and below. Empty searches
	// the whole snapshot.
	Under string
	// IncludeDirs reports matching directories as well as files.
	IncludeDirs bool
}

// DefaultLimit is enough to answer "where did it go" without walking a terabyte
// to prove there is nothing else.
const DefaultLimit = 500

// DefaultMaxEntries is the walk's budget: large enough to cover a home directory
// comfortably, small enough that an accidental whole-volume search returns in
// seconds with an honest answer rather than appearing to hang.
const DefaultMaxEntries = 200_000

// ErrTruncated reports that the limit was reached, so the answer is partial.
// It is deliberately an error the caller shows rather than swallows: a search
// that silently stopped early would read as "that is all there is".
type ErrTruncated struct{ Limit int }

func (e *ErrTruncated) Error() string {
	return fmt.Sprintf("stopped after %d matches; narrow the search to see the rest", e.Limit)
}

// ErrBudget reports that the walk ran out of budget before it ran out of tree,
// which means the term may exist somewhere unread. It is a different message from
// ErrTruncated because it wants different advice: there is nothing to narrow by
// name, only somewhere to point the search.
type ErrBudget struct{ Scanned int }

func (e *ErrBudget) Error() string {
	return fmt.Sprintf("looked at %d entries and stopped before the end; "+
		"search inside a folder to cover it properly", e.Scanned)
}

// Search walks one mounted snapshot for entries whose name contains term,
// case-insensitively.
//
// Substring rather than glob: the caller is usually typing a remembered fragment
// of a filename, not composing a pattern, and "rsa" finding id_rsa.pub is the
// helpful reading.
func Search(ctx context.Context, mountPoint, snapshotName, stamp, term string, opts Options) ([]Hit, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, fmt.Errorf("find: nothing to search for")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	needle := strings.ToLower(term)

	root := mountPoint
	if opts.Under != "" {
		var err error
		if root, err = vfs.ToSnapshot(mountPoint, opts.Under); err != nil {
			return nil, err
		}
	}

	budget := opts.MaxEntries
	if budget <= 0 {
		budget = DefaultMaxEntries
	}

	var hits []Hit
	var truncated, exhausted bool
	scanned := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A snapshot contains other users' directories, which are readable to
			// nobody here. An unreadable subtree is normal and not the answer to
			// the question, so it is skipped rather than failing the search.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Counted before the name is even looked at, because the cost being bounded
		// here is reading the tree rather than matching against it.
		scanned++
		if scanned > budget {
			exhausted = true
			return filepath.SkipAll
		}
		if len(hits) >= limit {
			truncated = true
			return filepath.SkipAll
		}
		if d.IsDir() && !opts.IncludeDirs {
			if !strings.Contains(strings.ToLower(d.Name()), needle) {
				return nil
			}
			return nil
		}
		if !strings.Contains(strings.ToLower(d.Name()), needle) {
			return nil
		}

		live, err := vfs.ToLive(mountPoint, path)
		if err != nil {
			return nil
		}
		hit := Hit{
			Snapshot: snapshotName, Stamp: stamp,
			LivePath: live, Name: d.Name(), IsDir: d.IsDir(),
		}
		if info, err := d.Info(); err == nil {
			hit.Size, hit.ModTime = info.Size(), info.ModTime()
		}
		hits = append(hits, hit)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return hits, err
	}

	// Shallowest first, then alphabetical: the thing you lost is usually nearer
	// the top of a tree than the bottom of it.
	sort.Slice(hits, func(i, j int) bool {
		di, dj := strings.Count(hits[i].LivePath, "/"), strings.Count(hits[j].LivePath, "/")
		if di != dj {
			return di < dj
		}
		return hits[i].LivePath < hits[j].LivePath
	})

	if truncated {
		return hits, &ErrTruncated{Limit: limit}
	}
	if exhausted {
		return hits, &ErrBudget{Scanned: scanned - 1}
	}
	return hits, nil
}
