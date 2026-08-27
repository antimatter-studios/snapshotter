package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestTheTripwireFiresOnRealDeletions exercises the FSEvents plumbing rather
// than the decision logic: real files, really deleted, really observed.
//
// No snapshot is taken — the snapshot function is injected, so this never
// touches the machine's restore points.
func TestTheTripwireFiresOnRealDeletions(t *testing.T) {
	dir := t.TempDir()

	const count = 40
	for i := 0; i < count; i++ {
		p := filepath.Join(dir, fmt.Sprintf("file-%03d.txt", i))
		if err := os.WriteFile(p, []byte("delete me\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var taken atomic.Int32
	fired := make(chan struct{}, 1)

	w := New([]string{dir}, func(context.Context, []string) error {
		taken.Add(1)
		select {
		case fired <- struct{}{}:
		default:
		}
		return nil
	})
	// A threshold this low would be absurd in production; here it keeps the
	// test quick without weakening what it proves.
	w.Trigger = NewTrigger(10, 5*time.Second, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errs := make(chan error, 1)
	go func() { errs <- w.Run(ctx) }()

	// FSEvents needs a moment to establish the stream, and deletions made
	// before it is up would go unseen.
	time.Sleep(600 * time.Millisecond)

	for i := 0; i < count; i++ {
		if err := os.Remove(filepath.Join(dir, fmt.Sprintf("file-%03d.txt", i))); err != nil {
			t.Fatal(err)
		}
	}

	select {
	case <-fired:
	case <-time.After(15 * time.Second):
		t.Fatal("deleted 40 files under a watched directory and the tripwire never fired")
	}

	// The cooldown is an hour, so the rest of the burst must not fire again.
	time.Sleep(500 * time.Millisecond)
	if n := taken.Load(); n != 1 {
		t.Errorf("snapshot taken %d times for one burst; the cooldown did not hold", n)
	}

	cancel()
	if err := <-errs; err != nil && err != context.Canceled {
		t.Errorf("Run returned %v", err)
	}
}

func TestRunRefusesAnIncompleteWatcher(t *testing.T) {
	if err := (&Watcher{}).Run(context.Background()); err == nil {
		t.Error("Run accepted a watcher with no roots")
	}
	if err := (&Watcher{Roots: []string{t.TempDir()}}).Run(context.Background()); err == nil {
		t.Error("Run accepted a watcher with no snapshot function")
	}
}

// A browser clearing its cache deletes hundreds of files in seconds — the exact
// shape of the thing being watched for, and none of its meaning. This machine's
// tripwire fired on Microsoft Edge doing precisely that, took a snapshot nobody
// needed, and would have gone on doing so.

func TestCacheChurnIsNotABulkDeletion(t *testing.T) {
	w := &Watcher{Ignore: []string{"/Library/Caches/", "/private/var/folders/"}}

	for _, path := range []string{
		"/Users/someone/Library/Caches/Microsoft Edge/Default/Code Cache/js/a.bin",
		"/Users/someone/Library/Caches/com.apple.Safari/x",
		"/private/var/folders/0k/T/build-artifact",
	} {
		if !w.ignored(path) {
			t.Errorf("%s should not count towards a burst", path)
		}
	}
}

// The list must stay narrow. Anything someone would ask to recover has to reach
// the trigger, or the tripwire is quiet about the one deletion that mattered.
func TestWorkIsNeverIgnored(t *testing.T) {
	w := &Watcher{Ignore: []string{"/Library/Caches/", "/private/var/folders/", "/.Trash/"}}

	for _, path := range []string{
		"/Users/someone/Documents/Invoices/january.pdf",
		"/Users/someone/Desktop/thesis.docx",
		"/Users/someone/Pictures/wedding/001.jpg",
		"/Users/someone/src/project/main.go",
		// A folder merely NAMED cache, in someone's own work, is still theirs.
		"/Users/someone/Documents/cache-study/notes.md",
	} {
		if w.ignored(path) {
			t.Errorf("%s was ignored, so its deletion would go unwatched", path)
		}
	}
}

// No list configured means watch everything: a settings file that could not be
// read must not quietly turn the tripwire into a no-op.
func TestNoIgnoreListWatchesEverything(t *testing.T) {
	w := &Watcher{}
	if w.ignored("/Users/someone/Library/Caches/anything") {
		t.Error("something was ignored with no list configured")
	}

	// An empty string would match every path — a single stray entry must not
	// silence the whole watcher.
	w = &Watcher{Ignore: []string{""}}
	if w.ignored("/Users/someone/Documents/x") {
		t.Error("an empty fragment silenced the watcher")
	}
}

// Which watched directory a deletion belongs to.
//
// This is where the per-directory count comes from, and it is a string
// comparison against a path FSEvents chose the spelling of — so every way the
// two can differ is a way for the tripwire to silently count nothing.

func TestADeletionIsAttributedToTheDirectoryItIsIn(t *testing.T) {
	scopes := []scope{newScope("/Users/someone/projects"), newScope("/Users/someone/Documents")}

	for _, c := range []struct{ path, want string }{
		{"/Users/someone/projects/snapshotter/main.go", "/Users/someone/projects"},
		{"/Users/someone/Documents/Invoices/jan.pdf", "/Users/someone/Documents"},
	} {
		got, ok := within(scopes, c.path)
		if !ok || got != c.want {
			t.Errorf("%s was attributed to %q (found=%v), want %q", c.path, got, ok, c.want)
		}
	}
}

// Nothing outside the watched directories counts. That is the entire change:
// the tripwire used to watch a whole home directory, and ~/Library alone
// produced bursts all day, each one costing a whole-volume snapshot.
func TestADeletionOutsideEveryWatchedDirectoryIsNotCounted(t *testing.T) {
	scopes := []scope{newScope("/Users/someone/projects")}

	for _, path := range []string{
		"/Users/someone/Library/Caches/Microsoft Edge/x",
		"/Users/someone/Downloads/installer.dmg",
		"/Volumes/scratch/build/out.o",
		"/Users/someone",
	} {
		if root, ok := within(scopes, path); ok {
			t.Errorf("%s was counted against %q, and nothing outside a watched directory should be counted at all", path, root)
		}
	}
}

// A sibling whose name merely starts the same way is a different directory, and
// a prefix test without a separator cannot tell them apart. Someone watching
// ~/projects would find ~/projects-archive counted against it.
func TestASiblingWithASharedPrefixIsNotInside(t *testing.T) {
	scopes := []scope{newScope("/Users/someone/projects")}

	if _, ok := within(scopes, "/Users/someone/projects-archive/old/file"); ok {
		t.Error("projects-archive was treated as part of projects")
	}
}

// A watched directory inside another keeps its own count, or adding the inner
// one would silently make the outer one easier to trip.
func TestTheInnermostWatchedDirectoryWins(t *testing.T) {
	scopes := []scope{newScope("/Users/someone"), newScope("/Users/someone/projects")}

	got, ok := within(scopes, "/Users/someone/projects/snapshotter/main.go")
	if !ok || got != "/Users/someone/projects" {
		t.Errorf("attributed to %q, want the innermost watched directory", got)
	}
	got, ok = within(scopes, "/Users/someone/Music/track.aiff")
	if !ok || got != "/Users/someone" {
		t.Errorf("attributed to %q, want the outer watched directory", got)
	}
}

// FSEvents reports resolved paths — /private/tmp, never /tmp — so a watched
// directory named through a symlink has to match both spellings or it counts
// nothing and says nothing about why.
func TestASymlinkedWatchedDirectoryStillMatches(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}

	scopes := []scope{newScope(link)}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := within(scopes, filepath.Join(resolved, "gone.txt"))
	if !ok {
		t.Fatal("a deletion under the resolved path was not attributed to the directory that was configured through a symlink")
	}
	// Reported in the spelling that was configured, because that is the one the
	// person recognises.
	if got != link {
		t.Errorf("attributed to %q, want the configured spelling %q", got, link)
	}
}

// A directory that is not there yet keeps its configured spelling rather than
// being dropped, so it starts counting the moment it appears.
func TestAWatchedDirectoryThatDoesNotExistIsStillAScope(t *testing.T) {
	s := newScope("/Users/someone/not-created-yet")
	if s.resolved != "/Users/someone/not-created-yet" {
		t.Errorf("resolved to %q, want the configured path back", s.resolved)
	}
	if _, ok := within([]scope{s}, "/Users/someone/not-created-yet/file"); !ok {
		t.Error("a deletion under it was not attributed to it")
	}
}

// The same separation, through the real FSEvents plumbing rather than the
// trigger alone: real files under two real watched directories, really deleted.
//
// The unit tests prove the counter is keyed per directory. This proves the
// events actually arrive attributed to the right one — which is a string
// comparison against a path FSEvents chose the spelling of, and therefore the
// part that can be silently wrong on a real machine and right in a unit test.
func TestDeletionsInTwoWatchedDirectoriesDoNotTripTheRealWatcher(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()

	const each = 15
	for _, dir := range []string{first, second} {
		for i := 0; i < each; i++ {
			p := filepath.Join(dir, fmt.Sprintf("file-%03d.txt", i))
			if err := os.WriteFile(p, []byte("delete me\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	var taken atomic.Int32
	w := New([]string{first, second}, func(context.Context, []string) error {
		taken.Add(1)
		return nil
	})
	// Twenty is more than either directory holds and less than both together, so
	// this fires only if the two are being added up.
	w.Trigger = NewTrigger(20, 5*time.Second, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errs := make(chan error, 1)
	go func() { errs <- w.Run(ctx) }()

	// FSEvents needs a moment to establish the streams, and deletions made before
	// they are up would go unseen — which would make this pass for the wrong reason.
	time.Sleep(600 * time.Millisecond)

	for _, dir := range []string{first, second} {
		for i := 0; i < each; i++ {
			if err := os.Remove(filepath.Join(dir, fmt.Sprintf("file-%03d.txt", i))); err != nil {
				t.Fatal(err)
			}
		}
	}
	time.Sleep(2 * time.Second)

	if n := taken.Load(); n != 0 {
		t.Errorf("thirty deletions split across two watched directories took %d snapshots; "+
			"neither directory reached the threshold of 20 on its own", n)
	}

	// And the deletions were actually seen, each against its own directory.
	// Without this the test passes just as happily when no event arrives at all,
	// which is the failure it is supposed to be able to detect.
	now := time.Now()
	for _, dir := range []string{first, second} {
		if n := w.Trigger.Pending(now, dir); n == 0 {
			t.Errorf("no deletions were counted against %s, so this test proved nothing", dir)
		}
	}

	cancel()
	if err := <-errs; err != nil && err != context.Canceled {
		t.Errorf("Run returned %v", err)
	}
}
