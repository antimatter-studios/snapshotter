package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"snapshotter/internal/elevate"
	"snapshotter/internal/mountmgr"
)

// Mounting is the only thing here that asks for a password, so it is the only
// thing whose "no" needs to read as a decision rather than as a fault.

func TestMountingAndUnmountingRoundTrips(t *testing.T) {
	s := newStack(t, "healthy")
	ctx := context.Background()

	snaps, err := s.Snapshots.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) == 0 {
		t.Fatal("nothing to mount")
	}
	name := snaps[0].Name

	if err := s.Snapshots.Mount(ctx, "", []string{name}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	mounted, err := s.Deps.Mounts.IsMounted(name)
	if err != nil {
		t.Fatal(err)
	}
	if !mounted {
		t.Fatal("mount reported success but nothing is mounted")
	}

	if err := s.Snapshots.Unmount(ctx, "", []string{name}); err != nil {
		t.Fatalf("unmount: %v", err)
	}
	if mounted, _ := s.Deps.Mounts.IsMounted(name); mounted {
		t.Error("unmount reported success but it is still mounted")
	}
}

// The window closes by unmounting everything it opened. Leaving a mount behind
// leaves a read-only copy of the disk attached until the next reboot.
func TestUnmountAllLeavesNothingAttached(t *testing.T) {
	s := newStack(t, "healthy")
	ctx := context.Background()

	snaps, err := s.Snapshots.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		names = append(names, snap.Name)
	}
	if err := s.Snapshots.Mount(ctx, "", names); err != nil {
		t.Fatalf("mount: %v", err)
	}

	if err := s.Snapshots.UnmountAll(ctx); err != nil {
		t.Fatalf("unmount all: %v", err)
	}
	for _, name := range names {
		if mounted, _ := s.Deps.Mounts.IsMounted(name); mounted {
			t.Errorf("%s survived unmount all", name)
		}
	}
}

// UnmountAll runs from the window's close handler, where there is nobody left to
// read an error. It must be safe to call when nothing is mounted at all.
func TestUnmountAllWithNothingMountedIsNotAnError(t *testing.T) {
	s := newStack(t, "healthy")
	if err := s.Snapshots.UnmountAll(context.Background()); err != nil {
		t.Errorf("unmounting nothing failed: %v", err)
	}
}

// A dismissed password dialog is a choice. Reported as a raw error it becomes a
// red banner blaming the application for something the user just did on purpose.
func TestADismissedPasswordPromptReadsAsAChoice(t *testing.T) {
	err := describeAuth(elevate.ErrCancelled)
	if err == nil {
		t.Fatal("a cancellation was swallowed entirely, so the screen would show no reason")
	}
	if errors.Is(err, elevate.ErrCancelled) {
		t.Error("the raw sentinel reached the interface")
	}

	real := errors.New("mount_apfs: Operation not permitted")
	if got := describeAuth(real); got != real {
		t.Errorf("a genuine failure was rewritten: %v", got)
	}
	if describeAuth(nil) != nil {
		t.Error("success was turned into an error")
	}
}

// Deleted files are what someone is looking for when they open this at all. The
// fake mount copies the tree at mount time, so removing a file afterwards is a
// deletion in exactly the way the real thing is.
func TestDeletedSinceFindsWhatIsGoneFromTheLiveDisk(t *testing.T) {
	s := newStack(t, "healthy")
	ctx := context.Background()

	live := t.TempDir()
	for _, rel := range []string{"kept.txt", "deleted.txt"} {
		if err := os.WriteFile(filepath.Join(live, rel), []byte(rel), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	snaps, err := s.Snapshots.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := snaps[0].Name

	search := NewSearchService(Deps{
		Runner: s.Deps.Runner, Volume: s.Deps.Volume, Faking: true,
		Mounts: mountmgr.NewFake(t.TempDir(), live),
	})
	if err := search.Mounts.Mount(ctx, []string{name}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	// The fake seals its tree read-only to imitate a mount, so it has to be
	// unmounted before the temporary directory can be removed.
	t.Cleanup(func() { _ = search.Mounts.Unmount(ctx, []string{name}) })

	// The snapshot now holds both files; the live disk loses one.
	if err := os.Remove(filepath.Join(live, "deleted.txt")); err != nil {
		t.Fatal(err)
	}

	res, err := search.DeletedSince(ctx, name, live, false)
	if err != nil {
		t.Fatalf("deleted since: %v", err)
	}

	names := map[string]bool{}
	for _, c := range res.Changes {
		names[filepath.Base(c.RelPath)] = true
	}
	if !names["deleted.txt"] {
		t.Errorf("the deleted file was not reported: %+v", names)
	}
	if names["kept.txt"] {
		t.Errorf("a file that is still there was reported as deleted: %+v", names)
	}
}

// A snapshot nobody opened cannot be compared against, and saying so beats
// reporting an empty result — which would read as "nothing was deleted".
func TestDeletedSinceRefusesAnUnmountedSnapshot(t *testing.T) {
	s := newStack(t, "healthy")
	search := NewSearchService(s.Deps)

	snaps, err := s.Snapshots.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := search.DeletedSince(context.Background(), snaps[0].Name, t.TempDir(), false); err == nil {
		t.Error("comparing against an unmounted snapshot came back without an error")
	}
}
