package verdict

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// What makes a cache of these answers safe is that only one side can change. A
// snapshot is immutable; the live disk is not, and the filesystem says when it
// moves. What it does not say is which folders that affects — so the important
// behaviour here is forgetting upwards.

func TestAnAnswerIsRememberedUntilSomethingTouchesIt(t *testing.T) {
	c := New()
	c.Put("snap-1", "/Users/me/projects", Answer{Verdict: Modified})

	if a, ok := c.Get("snap-1", "/Users/me/projects"); !ok || a.Verdict != Modified {
		t.Fatalf("not remembered: %v %v", a, ok)
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
		c.Put("snap-1", p, Answer{Verdict: Same})
	}
	// Somewhere else entirely, which must survive.
	c.Put("snap-1", "/Users/me/Pictures", Answer{Verdict: Same})

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
	c.Put("snap-1", "/a/b", Answer{Verdict: Same})
	c.Put("snap-1", "/a/bc", Answer{Verdict: Same})

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
	c.Put("snap-1", "/Users/me/projects", Answer{Verdict: Same})
	c.Put("snap-2", "/Users/me/projects", Answer{Verdict: Modified})

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
	c.Put("snap-1", "/a", Answer{Verdict: Same})
	c.Put("snap-2", "/a", Answer{Verdict: Same})

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
	c.Put("snap-1", "/a", Answer{Verdict: Unknown})

	if a, ok := c.Get("snap-1", "/a"); !ok || a.Verdict != Unknown {
		t.Errorf("got %v %v", a, ok)
	}
	if c.Len() != 1 {
		t.Errorf("holding %d entries", c.Len())
	}
}

// The note travels with the verdict. An earlier version cached the word alone,
// so a folder saying "unchanged, apart from what you told me to skip" said it the
// first time it was looked at and not the second.
func TestTheNoteIsRememberedWithTheVerdict(t *testing.T) {
	c := New()
	c.Put("snap-1", "/a", Answer{Verdict: Same, Note: "apart from 3 paths"})

	a, ok := c.Get("snap-1", "/a")
	if !ok {
		t.Fatal("not remembered")
	}
	if a.Note != "apart from 3 paths" {
		t.Errorf("note %q was not kept", a.Note)
	}
}

// The settings deciding what "unchanged" means can change while the window is
// open, and nothing on the filesystem moves when they do. Held answers were
// reached under the old ones and are not answers to the new question.
func TestChangingTheRulesDiscardsEverything(t *testing.T) {
	c := New()
	c.UnderRules("first")
	c.Put("snap-1", "/a", Answer{Verdict: Same})
	c.Put("snap-2", "/b", Answer{Verdict: Modified})

	// The same rules leave everything alone, which is what makes it safe to call
	// on every lookup.
	c.UnderRules("first")
	if c.Len() != 2 {
		t.Errorf("unchanged rules dropped entries: holding %d", c.Len())
	}

	c.UnderRules("second")
	if c.Len() != 0 {
		t.Errorf("changed rules kept %d entries", c.Len())
	}
}

// Browsing asks this once per folder row while the filesystem watcher calls
// Touched once per event, so the two meet constantly on one lock.
//
// Honest about what it does and does not prove: written to catch a per-row write
// lock starving the readers, it does NOT — Go's RWMutex alternates rather than
// starving, and this passes either way. What it does hold down is that the two
// sides make progress together at all, which is worth a test on a lock this hot.
func TestReadersAreNotStarvedByAFloodOfWrites(t *testing.T) {
	c := New()
	c.UnderRules("settings")
	for i := 0; i < 500; i++ {
		c.Put("snap", filepath.Join("/Users/me/projects", strconv.Itoa(i)), Answer{Verdict: Same})
	}

	stop := make(chan struct{})
	var flooding sync.WaitGroup
	// Two writers doing what the filesystem watcher does, as fast as they can.
	for i := 0; i < 2; i++ {
		flooding.Add(1)
		go func() {
			defer flooding.Done()
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				c.Touched(filepath.Join("/Users/me/projects", strconv.Itoa(n%500), "file.go"))
			}
		}()
	}

	// And the browse workers, asking the way DirectoryStatus asks: the rules, then
	// the answer.
	done := make(chan struct{})
	go func() {
		defer close(done)
		var reading sync.WaitGroup
		for w := 0; w < 3; w++ {
			reading.Add(1)
			go func() {
				defer reading.Done()
				for i := 0; i < 2000; i++ {
					c.UnderRules("settings")
					c.Get("snap", "/Users/me/projects/1")
				}
			}()
		}
		reading.Wait()
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		close(stop)
		flooding.Wait()
		t.Fatal("readers made no progress against a flood of writes: UnderRules is taking the write lock")
	}
	close(stop)
	flooding.Wait()
}

// Three browse workers noticing one settings change must clear the cache once.
// Clearing it per worker would throw away the answers the other two had just put
// there, and the listing would walk everything twice over.
func TestOneSettingsChangeClearsTheCacheOnce(t *testing.T) {
	c := New()
	c.UnderRules("first")

	var start, done sync.WaitGroup
	start.Add(1)
	for i := 0; i < 8; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			c.UnderRules("second")
			c.Put("snap", "/a", Answer{Verdict: Same})
		}()
	}
	start.Done()
	done.Wait()

	// Every one of them put an answer in after noticing the change. If the clear
	// had run more than once, some of those would have been swept away.
	if c.Len() != 1 {
		t.Errorf("holding %d entries, want 1", c.Len())
	}
}

// A "changed" verdict rests on one file. Remembering which one lets the next
// question about any folder above it be answered by a stat instead of a walk.

func TestARecordedChangeIsFoundFromAnyFolderAboveIt(t *testing.T) {
	c := New()
	c.Put("snap", "/Users/me/projects", Answer{
		Verdict:     Modified,
		ChangedPath: "/Users/me/projects/app/src/main.go",
	})

	// Every folder between the question and the file it rests on.
	for _, folder := range []string{
		"/Users/me",
		"/Users/me/projects",
		"/Users/me/projects/app",
		"/Users/me/projects/app/src",
	} {
		got, ok := c.ChangedPathUnder("snap", folder)
		if !ok {
			t.Errorf("%s found no recorded change", folder)
			continue
		}
		if got != "/Users/me/projects/app/src/main.go" {
			t.Errorf("%s found %q", folder, got)
		}
	}

	// And nothing for a folder it says nothing about. A recorded change under Pictures
	// would be a claim about a tree it is not in.
	if got, ok := c.ChangedPathUnder("snap", "/Users/me/Pictures"); ok {
		t.Errorf("a recorded change elsewhere answered for Pictures: %q", got)
	}
	// Nor for the file's own folder's sibling.
	if got, ok := c.ChangedPathUnder("snap", "/Users/me/projects/app/docs"); ok {
		t.Errorf("a recorded change in src answered for docs: %q", got)
	}
}

// The shallowest, when there are several: it is the one most likely to still be
// there, and re-checking it reads fewer directories on the way.
func TestTheShallowestRecordedChangeIsOffered(t *testing.T) {
	c := New()
	c.Put("snap", "/a/deep", Answer{Verdict: Modified, ChangedPath: "/a/b/c/d/e/deep.txt"})
	c.Put("snap", "/a/shallow", Answer{Verdict: Modified, ChangedPath: "/a/near.txt"})

	got, ok := c.ChangedPathUnder("snap", "/a")
	if !ok {
		t.Fatal("no recorded change")
	}
	if got != "/a/near.txt" {
		t.Errorf("offered %q, want the shallower /a/near.txt", got)
	}
}

// An unchanged verdict has nothing to point at, and a recorded change on one would be
// re-checked for ever while proving nothing.
func TestAnUnchangedVerdictRecordsNothing(t *testing.T) {
	c := New()
	c.Put("snap", "/a", Answer{Verdict: Same})

	if c.ChangedPaths("snap") != 0 {
		t.Errorf("holding %d recorded changes for an unchanged folder", c.ChangedPaths("snap"))
	}
}

// Dropped once the file matches again. Keeping it would cost a stat before every
// walk it was meant to save, and that stat can never succeed.
func TestARecordedChangeCanBeForgotten(t *testing.T) {
	c := New()
	c.Put("snap", "/a", Answer{Verdict: Modified, ChangedPath: "/a/b/notes.md"})
	if c.ChangedPaths("snap") != 1 {
		t.Fatalf("holding %d", c.ChangedPaths("snap"))
	}

	c.ForgetChangedPath("snap", "/a/b/notes.md")
	if _, ok := c.ChangedPathUnder("snap", "/a"); ok {
		t.Error("a forgotten recorded change is still offered")
	}
}

// The filesystem moving under a recorded change is the same news as it moving under a
// verdict: what was true about that path may not be any more.
func TestTouchingARecordedChangeForgetsIt(t *testing.T) {
	c := New()
	c.Put("snap", "/a", Answer{Verdict: Modified, ChangedPath: "/a/b/notes.md"})

	c.Touched("/a/b/notes.md")
	if _, ok := c.ChangedPathUnder("snap", "/a"); ok {
		t.Error("a recorded change survived the file under it being touched")
	}
}

// Unmounting a snapshot takes its recorded changes with it. They are paths on the live
// disk, but what they prove is a difference from that snapshot in particular.
func TestForgettingASnapshotTakesItsRecordedChanges(t *testing.T) {
	c := New()
	c.Put("snap-1", "/a", Answer{Verdict: Modified, ChangedPath: "/a/b/notes.md"})
	c.Put("snap-2", "/a", Answer{Verdict: Modified, ChangedPath: "/a/b/notes.md"})

	c.Forget("snap-1")
	if _, ok := c.ChangedPathUnder("snap-1", "/a"); ok {
		t.Error("a forgotten snapshot kept its recorded change")
	}
	if _, ok := c.ChangedPathUnder("snap-2", "/a"); !ok {
		t.Error("forgetting one snapshot took another's recorded change")
	}
}

// Changing the ignore list changes what counts as a difference, so the paths a
// previous list settled on are not answers to the new question either.
func TestChangingTheRulesDiscardsRecordedChangesToo(t *testing.T) {
	c := New()
	c.UnderRules("first")
	c.Put("snap", "/a", Answer{Verdict: Modified, ChangedPath: "/a/node_modules/x.js"})

	c.UnderRules("second")
	if _, ok := c.ChangedPathUnder("snap", "/a"); ok {
		t.Error("a recorded change survived the rules changing")
	}
}

// A cache handed a store adopts the settings the store was written under, rather
// than comparing them with its own empty ones.
//
// Without this the first lookup of every run cleared the table: a new cache has no
// fingerprint, UnderRules saw a mismatch, and everything kept from the last run
// was thrown away before it could answer anything.
func TestOpeningAStoreAdoptsItsSettingsRatherThanClearingIt(t *testing.T) {
	kept := &fakeRecorder{rules: "settings-as-they-were"}
	c := New()
	c.Persist(kept)

	// The same settings the store was written under. Nothing is cleared.
	c.UnderRules("settings-as-they-were")
	if kept.cleared {
		t.Error("opening a store under unchanged settings cleared it")
	}

	// Genuinely different settings do clear it, and record the new ones so the
	// next run adopts them instead of clearing again.
	c.UnderRules("settings-as-they-now-are")
	if !kept.cleared {
		t.Error("changed settings did not clear the store")
	}
	if kept.rules != "settings-as-they-now-are" {
		t.Errorf("the store still says %q", kept.rules)
	}
}

type fakeRecorder struct {
	rules   string
	cleared bool
	paths   map[string]string
}

func (f *fakeRecorder) Record(snapshot, path string) error {
	if f.paths == nil {
		f.paths = map[string]string{}
	}
	f.paths[snapshot] = path
	return nil
}
func (f *fakeRecorder) Forget(snapshot, path string) error { delete(f.paths, snapshot); return nil }
func (f *fakeRecorder) Under(snapshot, folder string) (string, bool) {
	p, ok := f.paths[snapshot]
	return p, ok
}
func (f *fakeRecorder) ForgetSnapshot(snapshot string) error { delete(f.paths, snapshot); return nil }
func (f *fakeRecorder) Clear() error                         { f.cleared = true; f.paths = nil; return nil }
func (f *fakeRecorder) Rules() string                        { return f.rules }
func (f *fakeRecorder) SetRules(fingerprint string) error    { f.rules = fingerprint; return nil }

// A change found in one run is offered in the next, when memory holds nothing.
func TestAChangeFromAPreviousRunIsOffered(t *testing.T) {
	kept := &fakeRecorder{rules: "settings", paths: map[string]string{"snap": "/a/b/notes.md"}}
	c := New()
	c.Persist(kept)
	c.UnderRules("settings")

	got, ok := c.ChangedPathUnder("snap", "/a")
	if !ok {
		t.Fatal("nothing from the previous run was offered")
	}
	if got != "/a/b/notes.md" {
		t.Errorf("offered %q", got)
	}
}

// An unchanged verdict is never written. It would be a claim about everything
// that happened while this application was not running.
func TestAnUnchangedVerdictIsNeverWrittenDown(t *testing.T) {
	kept := &fakeRecorder{rules: "settings"}
	c := New()
	c.Persist(kept)

	c.Put("snap", "/a", Answer{Verdict: Same})
	if len(kept.paths) != 0 {
		t.Errorf("an unchanged verdict was written down: %v", kept.paths)
	}

	c.Put("snap", "/b", Answer{Verdict: Modified, ChangedPath: "/b/c.txt"})
	if len(kept.paths) != 1 {
		t.Errorf("a difference was not written down: %v", kept.paths)
	}
}
