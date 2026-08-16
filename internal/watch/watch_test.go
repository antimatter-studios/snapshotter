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
