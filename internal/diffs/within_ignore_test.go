package diffs

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// What the ignore list has to be right about, in order of how badly it would go
// wrong: it must not hide a change outside itself, it must actually save the
// reading it exists to save, and it must say that it skipped something so nobody
// mistakes "unchanged" for "unchanged everywhere".

func TestAChangeInsideAnIgnoredFolderIsNotReported(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{
		"src/main.go":                 "package main",
		"node_modules/react/index.js": "old",
	})
	writeTree(t, live, map[string]string{
		"src/main.go":                 "package main",
		"node_modules/react/index.js": "NEW AND DIFFERENT",
	})

	// Without the list this is a changed tree.
	if differs, _ := DiffersWithin(snap, live, Options{}); !differs {
		t.Fatal("the change is not found even without the ignore list, so this test proves nothing")
	}

	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules"})})
	if out.Differs {
		t.Error("a change inside an ignored folder was reported")
	}
	if !out.Answered {
		t.Errorf("not answered: %s", out.Why)
	}
	if out.Ignored != 1 {
		t.Errorf("Ignored=%d, want 1", out.Ignored)
	}
}

// The other half, and the more important one: ignoring node_modules must not
// make the application quiet about the work sitting beside it.
func TestAChangeOutsideAnIgnoredFolderIsStillReported(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{
		"src/main.go":                 "package main",
		"node_modules/react/index.js": "same",
	})
	writeTree(t, live, map[string]string{
		"src/main.go":                 "package main // edited",
		"node_modules/react/index.js": "same",
	})

	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules"})})
	if !out.Differs || !out.Answered {
		t.Errorf("differs=%v answered=%v why=%q", out.Differs, out.Answered, out.Why)
	}
}

// A file only one side has, inside an ignored folder, is the cheapest kind of
// difference to find — cheap enough that it could be found before the ignore
// check if the check were in the wrong place.
func TestSomethingAddedInsideAnIgnoredFolderIsNotReported(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"node_modules/a.js": "x"})
	writeTree(t, live, map[string]string{"node_modules/a.js": "x", "node_modules/b.js": "y"})

	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules"})})
	if out.Differs {
		t.Error("an addition inside an ignored folder was reported")
	}
}

// The saving is the entire point. Counted with the budget, which is the only
// instrument here that measures entries read: an ignored subtree must cost one
// entry — the directory itself — rather than everything under it.
func TestAnIgnoredSubtreeIsNotRead(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{"keep.txt": "x"}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		tree["node_modules/"+name+"/1.js"] = "x"
		tree["node_modules/"+name+"/2.js"] = "x"
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	// A budget too small to walk node_modules, but big enough for what is left.
	// Without the list this cannot answer; with it, it answers easily.
	const budget = 5
	if _, answered := differsWithinBudget(snap, live, Options{}, budget); answered {
		t.Fatalf("a budget of %d walked the whole tree, so this test proves nothing", budget)
	}
	if _, answered := differsWithinBudget(snap, live,
		Options{Ignore: NewIgnore([]string{"node_modules"})}, budget); !answered {
		t.Errorf("the ignored subtree was still read: a budget of %d ran out", budget)
	}
}

// "Nothing changed" and "nothing changed in the parts you asked me to read" are
// different sentences. The count is what lets the caller tell which one it is
// entitled to say.
func TestSkippingIsCountedEvenWhenNothingDiffers(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{
		"src/main.go":       "package main",
		"node_modules/a.js": "x",
		"dist/bundle.js":    "y",
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules", "dist"})})
	if out.Differs || !out.Answered {
		t.Fatalf("differs=%v answered=%v", out.Differs, out.Answered)
	}
	if out.Ignored != 2 {
		t.Errorf("Ignored=%d, want 2", out.Ignored)
	}
}

// Nested, because the count has to come back up through the recursion rather
// than being whatever the top level happened to see.
func TestSkippingDeeperDownIsCountedToo(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{
		"one/node_modules/a.js": "x",
		"two/node_modules/b.js": "x",
		"two/src/main.go":       "package main",
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules"})})
	if out.Ignored != 2 {
		t.Errorf("Ignored=%d, want 2", out.Ignored)
	}
}

// An unreadable subtree and an ignored one are different facts. One is the
// filesystem refusing and the other is an instruction, and reporting the second
// as the first would make a setting look like a fault.
func TestIgnoringIsNotTheSameAsFailingToRead(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{"node_modules/a.js": "x", "src/main.go": "package main"}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules"})})
	if !out.Answered {
		t.Errorf("answered=false: an ignored folder was treated as unreadable (%q)", out.Why)
	}
	if out.Why != "" {
		t.Errorf("Why=%q, want empty: nothing went wrong", out.Why)
	}
}

// The pattern is matched against the live path, so a pattern naming a place
// applies to the disk rather than to whatever a mountpoint happens to be called.
func TestAPathPatternIsMatchedAgainstTheLiveSide(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	// Different lengths, not just different text: the comparison is size and
	// modification time, and two three-letter files written at the same stamp are
	// genuinely identical to it.
	writeTree(t, snap, map[string]string{"work/app/dist/bundle.js": "old"})
	writeTree(t, live, map[string]string{"work/app/dist/bundle.js": "new and longer"})

	// Named by where it is on the running system. The snapshot copy is under a
	// different directory entirely, and a pattern written about it would be a
	// pattern about a mountpoint nobody typed.
	if differs, _ := DiffersWithin(snap, live, Options{}); !differs {
		t.Fatal("the change is not found without the ignore list, so this test proves nothing")
	}

	pattern := filepath.Join(live, "work", "*", "dist")
	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{pattern})})
	if out.Differs {
		t.Errorf("a change under %s was reported despite the pattern naming it", pattern)
	}

	// And the same pattern written about the snapshot side does nothing, which is
	// what says the match is on the live path rather than on either.
	wrongSide := filepath.Join(snap, "work", "*", "dist")
	if out := Explain(snap, live, Options{Ignore: NewIgnore([]string{wrongSide})}); !out.Differs {
		t.Error("a pattern naming the snapshot side suppressed the change")
	}
}

// A symlink named as ignored is not followed either, which matters because a
// link into a large tree is how an ignored name would otherwise still cost a
// walk.
func TestAnIgnoredNameIsNotFollowedThroughALink(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"src/main.go": "package main"})
	writeTree(t, live, map[string]string{"src/main.go": "package main"})

	elsewhere := t.TempDir()
	writeTree(t, elsewhere, map[string]string{"huge.js": "x"})
	if err := os.Symlink(elsewhere, filepath.Join(live, "node_modules")); err != nil {
		t.Fatal(err)
	}

	// Present on one side only, which without the list is a difference.
	if differs, _ := DiffersWithin(snap, live, Options{}); !differs {
		t.Fatal("the link is not noticed without the ignore list, so this test proves nothing")
	}
	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules"})})
	if out.Differs {
		t.Error("an ignored name was reported as a difference because it is a link")
	}
}

// Proving a folder unchanged reads everything under it and cannot stop early on
// its own. So the caller has to be able to say it no longer wants the answer —
// otherwise navigating out of a large folder leaves a walk running to the end for
// a row nobody will see again, and the next folder's answer queues behind it.

func TestAWalkStopsWhenTheAnswerIsNoLongerWanted(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{}
	for _, dir := range []string{"a", "b", "c", "d"} {
		for i := 0; i < 400; i++ {
			tree[dir+"/"+strconv.Itoa(i)+".txt"] = "x"
		}
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	// Wanted throughout, this walks the lot and answers.
	if out := Explain(snap, live, Options{}); !out.Answered || out.Differs {
		t.Fatalf("identical trees: %+v", out)
	}

	// Abandoned from the first time it is asked.
	out := Explain(snap, live, Options{Abandoned: func() bool { return true }})
	if !out.Abandoned {
		t.Error("the walk did not report that it was abandoned")
	}
	// And it must not claim an answer. "Nothing differs" from a walk that stopped
	// early is the one thing this application must never say.
	if out.Answered {
		t.Error("an abandoned walk claimed to have answered")
	}
	if out.Differs {
		t.Error("an abandoned walk claimed a difference")
	}
}

// It has to stop soon rather than eventually. The entries it reads after being
// abandoned are the whole cost being avoided.
func TestAnAbandonedWalkStopsWithoutReadingTheRest(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{}
	for _, dir := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		for i := 0; i < 500; i++ {
			tree[dir+"/"+strconv.Itoa(i)+".txt"] = "x"
		}
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	var asked int
	st := &examination{remaining: examineBudget}
	differsWithin(snap, live, Options{Abandoned: func() bool {
		asked++
		return true
	}}, st)

	// Asked once, said yes, and that was the end of it — rather than being asked
	// again for every remaining directory.
	if asked != 1 {
		t.Errorf("asked %d times after saying the answer was not wanted", asked)
	}
	// Well short of the 4,000-odd entries in the tree.
	read := examineBudget - st.remaining
	if read > 2*askEveryEntries {
		t.Errorf("read %d entries after being abandoned", read)
	}
}

// The check costs a call into the caller, so it is not made on every entry. It
// still has to be made often enough that stopping feels immediate.
func TestTheAnswerIsAskedForOftenEnoughToStopPromptly(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{}
	for i := 0; i < 3000; i++ {
		tree["deep/"+strconv.Itoa(i)+".txt"] = "x"
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	var asked int
	Explain(snap, live, Options{Abandoned: func() bool {
		asked++
		return false
	}})
	if asked < 3 {
		t.Errorf("asked %d times while walking 3,000 entries: too rarely to stop promptly", asked)
	}
}

// Nil means never abandon, which is what the command line needs: it asked for the
// answer and is waiting for it.
func TestNoAbandonFunctionMeansTheWalkRunsToTheEnd(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{"a/1.txt": "x", "a/2.txt": "y"}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	out := Explain(snap, live, Options{Abandoned: nil})
	if out.Abandoned || !out.Answered {
		t.Errorf("%+v", out)
	}
}
