package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Watching exists so that editing the settings file takes effect without a
// relaunch. The failure that matters is silence: a change nobody is told about
// is exactly the relaunch this was meant to remove.

// await waits for one delivery, or fails.
func await(t *testing.T, ch <-chan Config, why string) Config {
	t.Helper()
	select {
	case cfg, ok := <-ch:
		if !ok {
			t.Fatalf("%s: the channel closed instead", why)
		}
		return cfg
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: nothing was delivered", why)
		return Config{}
	}
}

// Save replaces the file by renaming a new one over it, which is why the
// directory is watched rather than the file: a watch on the path alone would be
// left pointing at an inode nothing writes to again.
func TestAChangeMadeThroughSaveIsDelivered(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changes, err := Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	cfg := Defaults()
	cfg.Schedule.IntervalHours = 3
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	got := await(t, changes, "after Save")
	if got.Schedule.IntervalHours != 3 {
		t.Errorf("delivered %v hours, want 3", got.Schedule.IntervalHours)
	}
}

// An editor is as likely to truncate and rewrite in place as to rename, and a
// person is as likely to use an editor as the application.
func TestAFileWrittenInPlaceIsDelivered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := Save(Defaults()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	path := filepath.Join(dir, "snapshotter", "config.yaml")
	body := "appearance:\n    theme: dark\nrefresh:\n    window_seconds: 5\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got := await(t, changes, "after an in-place write")
	if got.Appearance.Theme != "dark" {
		t.Errorf("theme is %q, want dark", got.Appearance.Theme)
	}
	if got.Refresh.WindowSeconds != 5 {
		t.Errorf("window refresh is %v, want 5", got.Refresh.WindowSeconds)
	}
}

// A file half way through being written must not be acted on: the application
// would apply nonsense and then apply the real thing a moment later.
func TestAnUnreadableFileIsNotDelivered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := Save(Defaults()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	path := filepath.Join(dir, "snapshotter", "config.yaml")
	if err := os.WriteFile(path, []byte("schedule: [this is not\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case cfg := <-changes:
		t.Errorf("a file that will not parse was delivered anyway: %+v", cfg)
	case <-time.After(time.Second):
		// Correct: nothing delivered, so what was already in force stays.
	}

	// And the watcher is still alive: the next good write must arrive.
	good := Defaults()
	good.Schedule.RetentionDays = 45
	if err := Save(good); err != nil {
		t.Fatal(err)
	}
	if got := await(t, changes, "after recovering"); got.Schedule.RetentionDays != 45 {
		t.Errorf("retention is %v, want 45", got.Schedule.RetentionDays)
	}
}

// A burst of writes is one edit. Reloading on each event means reading the file
// while it is still being written.
func TestABurstOfWritesCollapsesIntoOneReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	for i := 1; i <= 5; i++ {
		cfg := Defaults()
		cfg.Schedule.IntervalHours = float64(i)
		if err := Save(cfg); err != nil {
			t.Fatal(err)
		}
	}

	// The last value wins, and it arrives once.
	got := await(t, changes, "after a burst")
	if got.Schedule.IntervalHours != 5 {
		t.Errorf("delivered %v, want the last value 5", got.Schedule.IntervalHours)
	}
	select {
	case extra := <-changes:
		t.Errorf("a second delivery for one burst: %+v", extra)
	case <-time.After(500 * time.Millisecond):
	}
}

// The watcher must stop when told, or an application that quits leaves a
// filesystem watch behind it.
func TestCancellingStopsTheWatcher(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())

	changes, err := Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	cancel()

	select {
	case _, ok := <-changes:
		if ok {
			t.Error("a value was delivered after cancelling")
		}
	case <-time.After(5 * time.Second):
		t.Error("the channel was never closed")
	}
}
