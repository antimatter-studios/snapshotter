package mountmgr

import (
	"context"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/elevate"
)

type fakeElevator struct {
	scripts []string
	reasons []string
	err     error
}

func (f *fakeElevator) RunPrivileged(_ context.Context, script, reason string) (string, error) {
	f.scripts = append(f.scripts, script)
	f.reasons = append(f.reasons, reason)
	return "", f.err
}

const (
	snapA = "com.apple.TimeMachine.2026-08-13-172036.local"
	snapB = "com.apple.TimeMachine.2026-08-12-040000.local"
)

// newTestManager returns a Manager whose view of the mount table is a set the
// test controls.
func newTestManager(elev elevate.Elevator, mounted map[string]bool) (*Manager, map[string]bool) {
	m := New(elev, apfs.DataVolume, "/tmp/snapshotter-test/mounts", "/tmp/snapshotter-test/snapshotter")
	m.isMounted = func(path string) (bool, error) { return mounted[path], nil }
	return m, mounted
}

func TestMountPointUsesTheDateStamp(t *testing.T) {
	m, _ := newTestManager(&fakeElevator{}, nil)
	mp, err := m.MountPoint(snapA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/tmp/snapshotter-test/mounts/2026-08-13-172036"; mp != want {
		t.Errorf("want %s, got %s", want, mp)
	}
}

func TestMountPointRejectsJunk(t *testing.T) {
	m, _ := newTestManager(&fakeElevator{}, nil)
	for _, in := range []string{"Snapshots for disk /System/Volumes/Data:", "/", "com.apple.os.update-8A9F"} {
		if _, err := m.MountPoint(in); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
}

// A single prompt for the whole batch is the point of Mount taking a slice.
func TestMountBatchesIntoOneAuthorization(t *testing.T) {
	elev := &fakeElevator{}
	m, mounted := newTestManager(elev, map[string]bool{})
	m.isMounted = func(path string) (bool, error) {
		// Report success only after the privileged script has run.
		return len(elev.scripts) > 0 || mounted[path], nil
	}

	if err := m.Mount(context.Background(), []string{snapA, snapB}); err != nil {
		t.Fatal(err)
	}
	if len(elev.scripts) != 1 {
		t.Fatalf("want 1 authorization, got %d", len(elev.scripts))
	}
	script := elev.scripts[0]
	// What is elevated is this binary, not mount_apfs — see Manager.Program. The
	// mount_apfs flags are asserted against helperPlan instead, on the far side of
	// the prompt, which is now the only place that builds them.
	for _, want := range []string{"2026-08-13-172036", "2026-08-12-040000", "-" + HelperFlag + "=" + helperMount} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q: %s", want, script)
		}
	}
}

// Re-mounting something already mounted should cost no prompt at all.
func TestMountSkipsAlreadyMountedWithoutPrompting(t *testing.T) {
	elev := &fakeElevator{}
	m, _ := newTestManager(elev, map[string]bool{
		"/tmp/snapshotter-test/mounts/2026-08-13-172036": true,
	})

	if err := m.Mount(context.Background(), []string{snapA}); err != nil {
		t.Fatal(err)
	}
	if len(elev.scripts) != 0 {
		t.Errorf("prompted for an already-mounted snapshot: %v", elev.scripts)
	}
}

func TestUnmountSkipsWhatIsNotMounted(t *testing.T) {
	elev := &fakeElevator{}
	m, _ := newTestManager(elev, map[string]bool{})

	if err := m.Unmount(context.Background(), []string{snapA}); err != nil {
		t.Fatal(err)
	}
	if len(elev.scripts) != 0 {
		t.Errorf("prompted to unmount nothing: %v", elev.scripts)
	}
}

// The privileged script reports only its last command's status, so a snapshot
// that silently failed to mount has to be caught by re-reading the mount table.
func TestMountReportsSnapshotsThatDidNotAttach(t *testing.T) {
	elev := &fakeElevator{}
	m, _ := newTestManager(elev, map[string]bool{})

	err := m.Mount(context.Background(), []string{snapA})
	if err == nil {
		t.Fatal("reported success for a mount that did not happen")
	}
	if !strings.Contains(err.Error(), "2026-08-13-172036") {
		t.Errorf("error does not name the snapshot: %v", err)
	}
}

func TestMountPropagatesCancellation(t *testing.T) {
	elev := &fakeElevator{err: elevate.ErrCancelled}
	m, _ := newTestManager(elev, map[string]bool{})

	err := m.Mount(context.Background(), []string{snapA})
	if err != elevate.ErrCancelled {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}

func TestQuoteAllRefusesEmbeddedQuotes(t *testing.T) {
	if _, err := quoteAll("/tmp/it's"); err == nil {
		t.Error("accepted a path containing a single quote")
	}
}
