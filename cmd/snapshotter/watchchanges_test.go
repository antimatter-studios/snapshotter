package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"snapshotter/internal/verdict"
)

// tempRoot is a temporary folder with its symlinks resolved.
//
// On macOS t.TempDir sits under /var, which is a link to /private/var, and the
// kernel reports events under the resolved path. The cache matches paths
// textually, so a key written one way and an event arriving the other never meet.
// Nothing in production watches a linked path — the root is the home folder — but
// the test would silently pass by never invalidating anything.
func tempRoot(t *testing.T) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// A folder's verdict costs a full walk to reach, so it is remembered. Which means
// something has to notice when it stops being true — and the only thing that can
// is the filesystem, because the change comes from the live side and nothing in
// this process made it.
//
// The failure is quiet and wrong in the worst direction: the browser goes on
// saying a folder is identical to the snapshot after the user has changed it, and
// they conclude there is nothing in there to recover.

// waitFor polls until the condition holds or the deadline passes. Filesystem
// events arrive when the kernel gets round to it, so there is no moment at which
// asking once would be fair.
func waitFor(t *testing.T, why string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not happen within five seconds", why)
}

// started reports whether the watcher managed to attach, so a sandbox that
// forbids filesystem events skips rather than fails. The application treats that
// the same way: without the watcher every answer is computed afresh, which is
// what it did before the cache existed.
func started(t *testing.T, cache *verdict.Cache, root string) bool {
	t.Helper()

	probe := filepath.Join(root, "probe.txt")
	for range 100 {
		if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		cache.Put("snap", probe, verdict.Answer{Verdict: verdict.Same})
		if err := os.Remove(probe); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		if _, ok := cache.Get("snap", probe); !ok {
			return true
		}
	}
	return false
}

func TestAChangedFileForgetsTheVerdictForItsFolder(t *testing.T) {
	root := tempRoot(t)
	nested := filepath.Join(root, "projects", "work")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	cache := verdict.New()
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go watchForChanges(ctx, root, cache)

	if !started(t, cache, root) {
		t.Skip("filesystem events are not delivered here, so there is nothing to test")
	}

	// Every folder above the file, because a file edited five levels down changes
	// the answer for all five: a directory's own modification time moves only when
	// something is added or removed directly inside it.
	cache.Put("snap", root, verdict.Answer{Verdict: verdict.Same})
	cache.Put("snap", filepath.Join(root, "projects"), verdict.Answer{Verdict: verdict.Same})
	cache.Put("snap", nested, verdict.Answer{Verdict: verdict.Same})

	if err := os.WriteFile(filepath.Join(nested, "notes.md"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, folder := range []string{root, filepath.Join(root, "projects"), nested} {
		waitFor(t, "the verdict for "+folder+" being forgotten", func() bool {
			_, remembered := cache.Get("snap", folder)
			return !remembered
		})
	}
}

func TestAnUnrelatedFolderKeepsItsVerdict(t *testing.T) {
	root := tempRoot(t)
	touched := filepath.Join(root, "touched")
	untouched := filepath.Join(root, "untouched")
	for _, dir := range []string{touched, untouched} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	cache := verdict.New()
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go watchForChanges(ctx, root, cache)

	if !started(t, cache, root) {
		t.Skip("filesystem events are not delivered here, so there is nothing to test")
	}

	cache.Put("snap", untouched, verdict.Answer{Verdict: verdict.Same})
	cache.Put("snap", touched, verdict.Answer{Verdict: verdict.Same})

	if err := os.WriteFile(filepath.Join(touched, "notes.md"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the changed folder's verdict being forgotten", func() bool {
		_, remembered := cache.Get("snap", touched)
		return !remembered
	})

	// Forgetting everything on any event would make the cache pointless: the walk
	// it exists to avoid is the expensive part, and a home folder sees writes
	// constantly from things that have nothing to do with what is on screen.
	if _, remembered := cache.Get("snap", untouched); !remembered {
		t.Error("a folder nothing happened in lost its verdict")
	}
}

func TestTheWatcherStopsWithItsContext(t *testing.T) {
	root := tempRoot(t)
	cache := verdict.New()
	ctx, stop := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		watchForChanges(ctx, root, cache)
		close(done)
	}()

	if !started(t, cache, root) {
		stop()
		t.Skip("filesystem events are not delivered here, so there is nothing to test")
	}
	stop()

	// A watcher that outlives its window holds an open handle on the home folder
	// and goes on doing work for a screen nobody is looking at.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher was still running five seconds after being cancelled")
	}
}

func TestAnUnwatchableRootIsNotFatal(t *testing.T) {
	// Without the watcher every answer is computed afresh, which is what the
	// application did before the cache existed. Refusing to open the window over it
	// would trade a slow browser for no browser.
	done := make(chan struct{})
	go func() {
		watchForChanges(context.Background(), filepath.Join(tempRoot(t), "no", "such", "folder"), verdict.New())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watching a folder that does not exist did not return")
	}
}
