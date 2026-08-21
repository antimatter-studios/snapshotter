package vfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Whether a snapshot of the data volume can contain a path at all.
//
// This decides whether the browser offers to look inside a folder or says it
// cannot. Getting it wrong in one direction offers a comparison that can only
// fail; in the other it refuses one that would have worked.

func TestCoveredAcceptsWhatTheDataVolumeHolds(t *testing.T) {
	for _, path := range []string{
		"/Users/someone/Documents",
		"/Users",
		"/private/var/log",
		// The symlinked roots the system volume presents, which exist inside a
		// snapshot only as their targets.
		"/var/log",
		"/tmp",
		"/etc/hosts",
		// The data volume's own prefix, as it appears on a running system.
		"/System/Volumes/Data/Users/someone",
	} {
		if !Covered(path) {
			t.Errorf("%s is on the data volume and was reported as not covered", path)
		}
	}
}

func TestCoveredRefusesWhatNoSnapshotHolds(t *testing.T) {
	for _, path := range []string{
		"/Volumes/an-external-disk/photos",
		"/Volumes/sdcard256gb/projects",
		"relative/path",
		"",
	} {
		if Covered(path) {
			t.Errorf("%s cannot be in a snapshot of the data volume and was reported as covered", path)
		}
	}
}

// The message a person reads when the browser declines. It names the path,
// because "not covered" without saying what is the least useful thing it could
// say.
func TestTheNotCoveredMessageNamesThePath(t *testing.T) {
	err := &ErrNotCovered{Path: "/Volumes/backup-drive/thing"}

	got := err.Error()
	if !strings.Contains(got, "/Volumes/backup-drive/thing") {
		t.Errorf("%q does not name the path", got)
	}
	if !strings.Contains(got, "data volume") {
		t.Errorf("%q does not say why", got)
	}
}

// Exists is asked about both sides of a comparison, including paths inside a
// mounted snapshot, so it must answer for anything on disk rather than only for
// regular files.
func TestExistsAnswersForEveryKindOfEntry(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "a-dir")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "a-link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "a-broken-link")
	if err := os.Symlink(filepath.Join(dir, "gone"), broken); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{file, sub, link} {
		if !Exists(path) {
			t.Errorf("%s is there and was reported missing", path)
		}
	}
	// A broken symlink is still an entry in its directory: it is something that
	// exists and can be restored, so Lstat rather than Stat is the right question.
	if !Exists(broken) {
		t.Error("a broken symlink was reported as missing, but it is still an entry")
	}
	if Exists(filepath.Join(dir, "never-made")) {
		t.Error("a path that was never made was reported as existing")
	}
}
