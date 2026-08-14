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

	w := New([]string{dir}, func(context.Context) error {
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
