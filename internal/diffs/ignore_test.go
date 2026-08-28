package diffs

import "testing"

// The ignore list is the only thing that helps the expensive direction. A folder
// that differs is answered at the first difference; one that does not has to be
// read in full to prove it. So what is asserted here is mostly about what is NOT
// read — and about the one failure mode that would matter, which is a pattern
// silently matching more than it was meant to.

func TestABareNameMatchesThatComponentAtAnyDepth(t *testing.T) {
	ig := NewIgnore([]string{"node_modules"})
	for _, path := range []string{
		"/Users/me/projects/app/node_modules",
		"/Users/me/node_modules",
		"node_modules",
		// Beneath it as well: a directory nobody is reading has no contents worth
		// asking about.
		"/Users/me/projects/app/node_modules/react/index.js",
	} {
		if !ig.Match(path) {
			t.Errorf("%s is not matched", path)
		}
	}
}

// The failure that would matter: a name matching a longer name that contains it
// would quietly stop comparing somebody's work.
func TestABareNameDoesNotMatchPartOfAName(t *testing.T) {
	ig := NewIgnore([]string{"node_modules"})
	for _, path := range []string{
		"/Users/me/node_modules_backup",
		"/Users/me/old_node_modules",
		"/Users/me/projects/notes",
		"/Users/me/node_modules.txt",
	} {
		if ig.Match(path) {
			t.Errorf("%s is matched and should not be", path)
		}
	}
}

func TestWildcardsWork(t *testing.T) {
	ig := NewIgnore([]string{"*.tmp", "build-*"})
	for _, path := range []string{"/a/b/scratch.tmp", "/a/build-2024", "/a/build-2024/x/y"} {
		if !ig.Match(path) {
			t.Errorf("%s is not matched", path)
		}
	}
	for _, path := range []string{"/a/b/scratch.tmpx", "/a/rebuild-2024", "/a/tmp"} {
		if ig.Match(path) {
			t.Errorf("%s is matched and should not be", path)
		}
	}
}

// A pattern with a separator is about one place rather than every directory of
// that name, which is the whole reason for having the second shape.
func TestAPathPatternPicksOutOnePlace(t *testing.T) {
	ig := NewIgnore([]string{"/Users/me/work/*/dist"})
	if !ig.Match("/Users/me/work/app/dist") {
		t.Error("the place it names is not matched")
	}
	// And beneath it.
	if !ig.Match("/Users/me/work/app/dist/bundle.js") {
		t.Error("inside the place it names is not matched")
	}
	if ig.Match("/Users/me/other/app/dist") {
		t.Error("a dist somewhere else is matched")
	}
	if ig.Match("/Users/me/work/app") {
		t.Error("the parent of the place it names is matched")
	}
}

// filepath.Match reads "[" as the start of a character class, so a directory
// genuinely called "[weird]" would never match itself without the literal
// comparison alongside it.
func TestANameWithBracketsMatchesItself(t *testing.T) {
	ig := NewIgnore([]string{"[weird]"})
	if !ig.Match("/a/[weird]/b") {
		t.Error("a directory named [weird] does not match the pattern [weird]")
	}
}

// A stray blank line in a settings file should not switch change detection off,
// which is what an empty pattern matching everything would do.
func TestBlankPatternsAreDropped(t *testing.T) {
	ig := NewIgnore([]string{"", "   ", "\t"})
	if !ig.Empty() {
		t.Fatalf("kept %q", ig.Patterns())
	}
	if ig.Match("/Users/me/anything") {
		t.Error("a list of blanks matches a path")
	}
}

func TestAnEmptyListMatchesNothing(t *testing.T) {
	var ig Ignore
	if !ig.Empty() {
		t.Error("the zero value is not empty")
	}
	if ig.Match("/Users/me/node_modules") {
		t.Error("the zero value matches something")
	}
}

// An empty path is not a thing anybody configured, and matching it would make
// "ignore everything" reachable by accident.
func TestAnEmptyPathIsNotMatched(t *testing.T) {
	if NewIgnore([]string{"*"}).Match("") {
		t.Error("the empty path is matched")
	}
}
