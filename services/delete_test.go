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
//
// It deletes ONE VOLUME'S COPY. `tmutil localsnapshot` writes to every mounted
// APFS volume at once, so a date exists in several places and deleting by date
// removes all of them — which is right for retention and wrong for a button
// beside one row.

// aDevice and aUUID identify one copy, the way the window does.
const (
	aDevice = "disk3s1"
	aUUID   = "8DE94CCB-5B60-4C09-B249-D7E0067AE4B4"
)

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

	if err := s.Delete(context.Background(), aDevice, aUUID, "2026-08-20-120000"); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	// diskutil, naming the volume and the copy — not tmutil naming the date.
	// tmutil would take every volume's copy of that date, including snapshots the
	// person pressing the button was never shown.
	var deletion string
	for _, ran := range r.ran {
		if strings.Contains(ran, "deleteSnapshot") {
			deletion = ran
		}
	}
	if deletion == "" {
		t.Fatalf("nothing deleted anything: %v", r.ran)
	}
	if !strings.Contains(deletion, aDevice) || !strings.Contains(deletion, aUUID) {
		t.Errorf("ran %q, which does not name the volume and the copy", deletion)
	}
	for _, ran := range r.ran {
		if strings.Contains(ran, "deletelocalsnapshots") {
			t.Errorf("ran %q, which deletes that date from every volume holding it", ran)
		}
	}
}

// Without a volume and an identifier there is no call that deletes one copy, and
// the only one available deletes them all. So it refuses.
func TestACopyThatCannotBeIdentifiedIsNotDeleted(t *testing.T) {
	for _, c := range []struct{ device, uuid string }{
		{"", aUUID}, {aDevice, ""}, {"", ""},
		{"; rm -rf /", aUUID}, {aDevice, "not-a-uuid"},
	} {
		r := &deleteRunner{}
		s := NewSnapshotService(Deps{Runner: r, Mounts: &stubMounts{mounted: map[string]bool{}}})

		if err := s.Delete(context.Background(), c.device, c.uuid, "2026-08-20-120000"); err == nil {
			t.Errorf("device=%q uuid=%q was accepted", c.device, c.uuid)
		}
		for _, ran := range r.ran {
			if strings.Contains(ran, "deleteSnapshot") || strings.Contains(ran, "deletelocalsnapshots") {
				t.Errorf("device=%q uuid=%q still ran %q", c.device, c.uuid, ran)
			}
		}
	}
}

// A mounted snapshot is refused, and the message says what to do about it. macOS
// would fail the deletion anyway; saying so first turns a confusing failure into
// an instruction.
func TestAMountedSnapshotIsRefusedWithAnInstruction(t *testing.T) {
	r := &deleteRunner{}
	name := "com.apple.TimeMachine.2026-08-20-120000.local"
	s := NewSnapshotService(Deps{Runner: r, Mounts: &stubMounts{mounted: map[string]bool{name: true}}})

	err := s.Delete(context.Background(), aDevice, aUUID, "2026-08-20-120000")
	if err == nil {
		t.Fatal("a mounted snapshot was deleted")
	}
	if !strings.Contains(err.Error(), "unmount") {
		t.Errorf("the refusal does not say what to do: %v", err)
	}
	for _, ran := range r.ran {
		if strings.Contains(ran, "deleteSnapshot") {
			t.Errorf("it ran %q anyway", ran)
		}
	}
}

// A stamp that is not a date is refused before anything is asked about it, so a
// name built from it can never reach a delete.
func TestAStampThatIsNotADateNeverReachesTheTool(t *testing.T) {
	r := &deleteRunner{}
	s := NewSnapshotService(Deps{Runner: r, Mounts: &stubMounts{mounted: map[string]bool{}}})

	for _, bad := range []string{"", "yesterday", "2026-08-20", "2026-08-20-120000 ; rm -rf /"} {
		if err := s.Delete(context.Background(), aDevice, aUUID, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, ran := range r.ran {
		if strings.Contains(ran, "deleteSnapshot") {
			t.Errorf("something was run for a stamp that is not a date: %q", ran)
		}
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

	if err := s.Delete(context.Background(), aDevice, aUUID, "2026-08-20-120000"); err == nil {
		t.Fatal("a snapshot was deleted without knowing whether it was mounted")
	}
	for _, ran := range r.ran {
		if strings.Contains(ran, "deleteSnapshot") {
			t.Errorf("it ran %q anyway", ran)
		}
	}
}
