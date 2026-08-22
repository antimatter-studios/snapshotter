package mountmgr

import (
	"os"
	"path/filepath"
	"testing"
)

// isMountPoint decides whether a directory already holds a mounted snapshot, and
// it is the reason mounts left behind by a previous run are recognised rather
// than mounted over. It reads the live filesystem instead of tracking state in
// memory, which is what makes it worth testing directly: the thing it consults
// is not something this process controls.

func TestAnOrdinaryDirectoryIsNotAMountPoint(t *testing.T) {
	dir := t.TempDir()

	got, err := isMountPoint(dir)
	if err != nil {
		t.Fatalf("checking %s: %v", dir, err)
	}
	if got {
		t.Error("a freshly made directory was reported as a mount point")
	}
}

// A path that is not there is not mounted, and is not an error either. The
// directory a snapshot mounts into does not exist until the first mount, and
// asking about it then must not fail.
func TestAMissingPathIsNotAMountPointAndNotAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never", "made")

	got, err := isMountPoint(missing)
	if err != nil {
		t.Fatalf("a missing path reported an error: %v", err)
	}
	if got {
		t.Error("a missing path was reported as a mount point")
	}
}

// The filesystem root is the one place this answers wrongly, and it is worth
// pinning rather than leaving for somebody to discover.
//
// The check compares a path's device number with its parent's, and the parent of
// "/" is "/" — same device, so it reports false for the one directory that is
// unarguably a mount point. It does not matter here, because this is only ever
// asked about the directories snapshots are mounted into, which always have a
// real parent. It would matter to anyone who reused it.
func TestTheFilesystemRootIsTheCaseThisGetsWrong(t *testing.T) {
	got, err := isMountPoint("/")
	if err != nil {
		t.Fatalf("checking /: %v", err)
	}
	if got {
		t.Error("/ now reports as a mount point; the comparison has changed and the " +
			"comment on isMountPoint needs revisiting")
	}
}

// A file is not a mount point. Worth stating because the check compares device
// numbers with the parent, and a regular file has the same device as its
// directory — so it answers correctly by accident rather than by design, and a
// change to the comparison should have to notice.
func TestAFileIsNotAMountPoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := isMountPoint(path)
	if err != nil {
		t.Fatalf("checking a file: %v", err)
	}
	if got {
		t.Error("a regular file was reported as a mount point")
	}
}
