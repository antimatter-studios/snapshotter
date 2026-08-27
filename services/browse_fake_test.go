package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"snapshotter/internal/diffs"
	"snapshotter/internal/mountmgr"
)

const testSnapshot = "com.apple.TimeMachine.2026-08-14-003200.local"

// TestBrowseAgainstAFakeMount is the proof that the browse and compare surface
// works without mounting anything.
//
// Mounting needs root and Full Disk Access and is refused on a machine without
// both, which would otherwise leave this half of the application untestable.
// Nothing below knows that its snapshot is a directory rather than a mount.
func TestBrowseAgainstAFakeMount(t *testing.T) {
	// The seed has to be somewhere vfs treats as the data volume, which
	// t.TempDir is not: it lives under /private/var and translates differently.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	seed, err := os.MkdirTemp(home, ".snapshotter-browse-test-")
	if err != nil {
		t.Skipf("cannot create a seed under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(seed) })

	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(seed, name), []byte("live contents\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	fake := mountmgr.NewFake(t.TempDir(), seed)
	if err := fake.Mount(context.Background(), []string{testSnapshot}); err != nil {
		t.Fatalf("faking a mount: %v", err)
	}
	// A fake mount is sealed read-only to imitate a real one, and t.TempDir
	// cannot remove a read-only tree. Unmount is what takes it apart.
	t.Cleanup(func() { _ = fake.Unmount(context.Background(), []string{testSnapshot}) })

	browse := NewBrowseService(Deps{Mounts: fake, Volume: "/System/Volumes/Data", Faking: true})

	merged, err := browse.Merged("", testSnapshot, seed, true)
	if err != nil {
		t.Fatalf("Merged: %v", err)
	}
	if !merged.SnapshotExists || !merged.LiveExists {
		t.Fatalf("both sides should exist: snapshot=%v live=%v", merged.SnapshotExists, merged.LiveExists)
	}

	// All three states the interface renders have to be reachable, or the fake
	// is not exercising the thing it exists to exercise.
	seen := map[diffs.Status]int{}
	for _, row := range merged.Rows {
		seen[row.Status]++
	}
	for _, want := range []diffs.Status{diffs.OnlyInSnapshot, diffs.Modified, diffs.OnlyOnDisk} {
		if seen[want] == 0 {
			t.Errorf("no %q rows; the fake did not produce that state. rows: %+v", want, merged.Rows)
		}
	}
}

// A faked snapshot holds invented files, so putting one back over a real one
// would destroy real work to demonstrate a feature.
func TestReplaceRestoreIsRefusedWhileFaking(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	seed, err := os.MkdirTemp(home, ".snapshotter-restore-test-")
	if err != nil {
		t.Skipf("cannot create a seed under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(seed) })

	target := filepath.Join(seed, "alpha.txt")
	if err := os.WriteFile(target, []byte("the real file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := mountmgr.NewFake(t.TempDir(), seed)
	if err := fake.Mount(context.Background(), []string{testSnapshot}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fake.Unmount(context.Background(), []string{testSnapshot}) })

	svc := NewRestoreService(Deps{Mounts: fake, Volume: "/System/Volumes/Data", Faking: true})
	_, err = svc.Restore(context.Background(), RestoreRequest{
		Snapshot: testSnapshot, LivePath: target, Replace: true,
	})
	if err == nil {
		t.Fatal("a Replace restore was allowed while mounts are simulated")
	}

	if body, readErr := os.ReadFile(target); readErr == nil && string(body) != "the real file\n" {
		t.Fatalf("the real file was overwritten: %q", body)
	}
}

// Search has to work against the fake for the same reason browse does: it is a
// directory walk, and nothing in it can tell a mount from a directory.
func TestSearchAcrossAFakeMount(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	seed, err := os.MkdirTemp(home, ".snapshotter-search-test-")
	if err != nil {
		t.Skipf("cannot create a seed under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(seed) })

	if err := os.WriteFile(filepath.Join(seed, "id_rsa"), []byte("private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "unrelated.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := mountmgr.NewFake(t.TempDir(), seed)
	if err := fake.Mount(context.Background(), []string{testSnapshot}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fake.Unmount(context.Background(), []string{testSnapshot}) })

	// One mounted snapshot, and one that is not, so the result has to distinguish
	// "not there" from "nobody looked".
	runner := &searchRunner{stamps: []string{"2026-08-14-003200", "2026-08-13-172036"}}
	svc := NewSearchService(Deps{Runner: runner, Mounts: fake, Volume: "/System/Volumes/Data", Faking: true})

	res, err := svc.Search(context.Background(), "id_rsa", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(res.Hits), res.Hits)
	}
	if res.Hits[0].LivePath != filepath.Join(seed, "id_rsa") {
		t.Errorf("hit is not a live path: %q", res.Hits[0].LivePath)
	}

	// The unsearched snapshot must be named, or an absence reads as proof.
	if len(res.Skipped) != 1 || res.Skipped[0] != "2026-08-13-172036" {
		t.Errorf("skipped = %v, want the unmounted snapshot", res.Skipped)
	}
	if res.Note == "" {
		t.Error("no note explaining that part of the history was not searched")
	}
}

// searchRunner answers tmutil listlocalsnapshots with a fixed set.
type searchRunner struct{ stamps []string }

func (r *searchRunner) Run(_ context.Context, _ string, _ ...string) (string, error) {
	out := "Snapshots for disk /:\n"
	for _, s := range r.stamps {
		out += "com.apple.TimeMachine." + s + ".local\n"
	}
	return out, nil
}
