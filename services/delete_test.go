package services

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Deleting a snapshot, which is the one action here that cannot be undone.
//
// A snapshot records a state of the disk that has passed: nothing recreates it,
// and the two guards in front of it are the whole safety of the operation. Both
// were reached by one branch out of three.

// stubMounts answers the two questions Delete asks and records nothing else.
type stubMounts struct {
	mounted map[string]bool
	err     error
}

func (s *stubMounts) MountPoint(string) (string, error) { return "", nil }
func (s *stubMounts) IsMounted(name string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.mounted[name], nil
}
func (s *stubMounts) Mount(context.Context, []string) error   { return nil }
func (s *stubMounts) Unmount(context.Context, []string) error { return nil }
func (s *stubMounts) MountedNames(names []string) []string    { return nil }

// deleteRunner records what it was asked to run so the test can say whether the
// deletion actually reached tmutil.
type deleteRunner struct{ ran []string }

func (d *deleteRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	d.ran = append(d.ran, strings.Join(append([]string{name}, args...), " "))
	return "", nil
}

func TestDeletingAnUnmountedSnapshotReachesTheTool(t *testing.T) {
	r := &deleteRunner{}
	s := NewSnapshotService(Deps{Runner: r, Mounts: &stubMounts{mounted: map[string]bool{}}})

	if err := s.Delete(context.Background(), "2026-08-20-120000"); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if len(r.ran) == 0 {
		t.Fatal("nothing was run")
	}
	if !strings.Contains(r.ran[0], "2026-08-20-120000") {
		t.Errorf("ran %q, which does not name the snapshot", r.ran[0])
	}
}

// A mounted snapshot is refused, and the message says what to do about it. macOS
// would fail the deletion anyway; saying so first turns a confusing failure into
// an instruction.
func TestAMountedSnapshotIsRefusedWithAnInstruction(t *testing.T) {
	r := &deleteRunner{}
	name := "com.apple.TimeMachine.2026-08-20-120000.local"
	s := NewSnapshotService(Deps{Runner: r, Mounts: &stubMounts{mounted: map[string]bool{name: true}}})

	err := s.Delete(context.Background(), "2026-08-20-120000")
	if err == nil {
		t.Fatal("a mounted snapshot was deleted")
	}
	if !strings.Contains(err.Error(), "unmount") {
		t.Errorf("the refusal does not say what to do: %v", err)
	}
	if len(r.ran) != 0 {
		t.Errorf("it ran %v anyway", r.ran)
	}
}

// A stamp that is not a date is refused before anything is asked about it, so a
// name built from it can never reach a delete.
func TestAStampThatIsNotADateNeverReachesTheTool(t *testing.T) {
	r := &deleteRunner{}
	s := NewSnapshotService(Deps{Runner: r, Mounts: &stubMounts{mounted: map[string]bool{}}})

	for _, bad := range []string{"", "yesterday", "2026-08-20", "2026-08-20-120000 ; rm -rf /"} {
		if err := s.Delete(context.Background(), bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if len(r.ran) != 0 {
		t.Errorf("something was run for a stamp that is not a date: %v", r.ran)
	}
}

// Not knowing whether it is mounted is not the same as knowing it is not. The
// deletion is refused rather than attempted, because attempting it on a mounted
// snapshot is the case the guard exists for.
func TestAnUnansweredMountCheckStopsTheDeletion(t *testing.T) {
	r := &deleteRunner{}
	s := NewSnapshotService(Deps{
		Runner: r,
		Mounts: &stubMounts{err: errors.New("cannot tell")},
	})

	if err := s.Delete(context.Background(), "2026-08-20-120000"); err == nil {
		t.Fatal("a snapshot was deleted without knowing whether it was mounted")
	}
	if len(r.ran) != 0 {
		t.Errorf("it ran %v anyway", r.ran)
	}
}
