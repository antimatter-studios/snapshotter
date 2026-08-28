package diffs

import (
	"path/filepath"
	"strings"
)

// Ignore is the set of paths change detection does not look inside.
//
// It exists because proving a folder unchanged is the expensive direction — a
// walk cannot stop early at a negative — and most of what it reads is not
// anybody's work. On the machine this was written for, 17,239 of a project's
// 19,788 entries were node_modules: nine seconds of an SD card's reading, per
// project, to confirm something nobody would restore.
//
// This is deliberately NOT the bulk-deletion watcher's ignore list. That one
// answers "deletions here do not count as a burst"; this one answers "do not read
// this when comparing". They would usually hold the same paths, which is exactly
// why they are separate: sharing them would mean changing what you are warned
// about in order to change what gets walked.
type Ignore struct {
	// patterns are kept as given, so the settings file says back what was typed.
	patterns []string
}

// NewIgnore builds a matcher. Blank entries are dropped rather than matching
// everything, since a stray empty line in a settings file should not silently
// switch change detection off.
func NewIgnore(patterns []string) Ignore {
	kept := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return Ignore{patterns: kept}
}

// Empty reports whether anything is ignored at all, so a caller can skip the
// work of asking.
func (ig Ignore) Empty() bool { return len(ig.patterns) == 0 }

// Patterns returns what was configured, for anything that has to say so.
func (ig Ignore) Patterns() []string { return ig.patterns }

// Match reports whether a path is ignored.
//
// Two shapes, told apart by whether the pattern contains a separator:
//
//   - A bare pattern matches any single path COMPONENT: "node_modules" ignores it
//     wherever it appears, at any depth, which is how people think about it.
//     Wildcards work — "*.tmp", "build-*" — through filepath.Match, so the syntax
//     is the one already used everywhere else in a shell.
//   - A pattern containing "/" is matched against the whole path, so
//     "*/projects/*/dist" can pick out one place without ignoring every dist on
//     the disk.
//
// Matching a component also ignores everything beneath it: a directory nobody is
// reading has no contents worth asking about.
func (ig Ignore) Match(path string) bool {
	if len(ig.patterns) == 0 || path == "" {
		return false
	}
	clean := filepath.Clean(path)
	components := strings.Split(clean, string(filepath.Separator))

	for _, pattern := range ig.patterns {
		if strings.ContainsRune(pattern, filepath.Separator) {
			// A whole-path pattern. Matched against the path and against every
			// ancestor of it, so ignoring a directory ignores its contents too.
			if matchesPathOrAncestor(pattern, clean) {
				return true
			}
			continue
		}
		for _, c := range components {
			if c == "" {
				continue
			}
			// The literal comparison first: filepath.Match treats "[" as the start
			// of a character class, so a directory genuinely named "[weird]" would
			// otherwise never match itself.
			if c == pattern {
				return true
			}
			if ok, err := filepath.Match(pattern, c); err == nil && ok {
				return true
			}
		}
	}
	return false
}

// matchesPathOrAncestor reports whether a whole-path pattern matches the path or
// any directory above it.
func matchesPathOrAncestor(pattern, path string) bool {
	for p := path; ; {
		if ok, err := filepath.Match(pattern, p); err == nil && ok {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}
