package verdict

import "testing"

// What makes a cache of these answers safe is that only one side can change. A
// snapshot is immutable; the live disk is not, and the filesystem says when it
// moves. What it does not say is which folders that affects — so the important
// behaviour here is forgetting upwards.

func TestAnAnswerIsRememberedUntilSomethingTouchesIt(t *testing.T) {
	c := New()
	c.Put("snap-1", "/Users/me/projects", Modified)

	if v, ok := c.Get("snap-1", "/Users/me/projects"); !ok || v != Modified {
		t.Fatalf("not remembered: %v %v", v, ok)
	}

	c.Touched("/Users/me/projects/a/b/file.go")
	if _, ok := c.Get("snap-1", "/Users/me/projects"); ok {
		t.Error("a change inside was not enough to forget the folder containing it")
	}
}

// The reason this exists. A file five levels down changes the verdict of all
// five folders above it, and nothing in the filesystem will tell you so.
func TestEveryFolderAboveTheChangeIsForgotten(t *testing.T) {
	c := New()
	for _, p := range []string{"/Users/me", "/Users/me/projects", "/Users/me/projects/a", "/Users/me/projects/a/b"} {
		c.Put("snap-1", p, Same)
	}
	// Somewhere else entirely, which must survive.
	c.Put("snap-1", "/Users/me/Pictures", Same)

	c.Touched("/Users/me/projects/a/b/deep/file.txt")

	for _, gone := range []string{"/Users/me", "/Users/me/projects", "/Users/me/projects/a", "/Users/me/projects/a/b"} {
		if _, ok := c.Get("snap-1", gone); ok {
			t.Errorf("%s was kept, though the change was inside it", gone)
		}
	}
	if _, ok := c.Get("snap-1", "/Users/me/Pictures"); !ok {
		t.Error("an unrelated folder was forgotten")
	}
}

// /a/b must not be treated as containing /a/bc. Getting this wrong forgets the
// wrong folder and keeps the right one, which is the worst of both.
func TestASiblingWithASharedPrefixIsNotConfusedForAChild(t *testing.T) {
	c := New()
	c.Put("snap-1", "/a/b", Same)
	c.Put("snap-1", "/a/bc", Same)

	c.Touched("/a/bc/file.txt")

	if _, ok := c.Get("snap-1", "/a/b"); !ok {
		t.Error("/a/b was forgotten because of a change in /a/bc")
	}
	if _, ok := c.Get("snap-1", "/a/bc"); ok {
		t.Error("/a/bc kept its answer despite the change inside it")
	}
}

// Two snapshots compared against the same live disk are two different questions
// with two different answers, and a change to the disk invalidates both.
func TestEverySnapshotIsInvalidatedByTheSameChange(t *testing.T) {
	c := New()
	c.Put("snap-1", "/Users/me/projects", Same)
	c.Put("snap-2", "/Users/me/projects", Modified)

	c.Touched("/Users/me/projects/x")

	if _, ok := c.Get("snap-1", "/Users/me/projects"); ok {
		t.Error("snap-1 kept a stale answer")
	}
	if _, ok := c.Get("snap-2", "/Users/me/projects"); ok {
		t.Error("snap-2 kept a stale answer")
	}
}

// Unmounting a snapshot takes one side of every comparison away with it.
func TestForgettingASnapshotLeavesTheOthersAlone(t *testing.T) {
	c := New()
	c.Put("snap-1", "/a", Same)
	c.Put("snap-2", "/a", Same)

	c.Forget("snap-1")

	if _, ok := c.Get("snap-1", "/a"); ok {
		t.Error("the unmounted snapshot kept its answers")
	}
	if _, ok := c.Get("snap-2", "/a"); !ok {
		t.Error("the other snapshot lost its answers too")
	}
}

// Unknown is cached like any other answer: whatever stopped the walk — an
// unreadable folder, a tree past the backstop — will not have changed either.
func TestNotKnowingIsAlsoRemembered(t *testing.T) {
	c := New()
	c.Put("snap-1", "/a", Unknown)

	if v, ok := c.Get("snap-1", "/a"); !ok || v != Unknown {
		t.Errorf("got %v %v", v, ok)
	}
	if c.Len() != 1 {
		t.Errorf("holding %d entries", c.Len())
	}
}
