package restore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What a restore does with the things a folder holds besides ordinary files, and
// what it leaves behind when it cannot finish.
//
// A restore writes to the user's own disk, which makes a half-finished one worse
// than none: a truncated file in the right place looks recovered.

func TestASymlinkIsRecreatedRatherThanFollowed(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "config")
	write(t, filepath.Join(source, "real.conf"), "settings")
	if err := os.Symlink("real.conf", filepath.Join(source, "current.conf")); err != nil {
		t.Fatal(err)
	}

	res, err := Restore(context.Background(), source, filepath.Join(dir, "live", "config"), Options{Tag: "t"})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Following it would turn one link into a second full copy, and a tree of
	// links into however many copies the links happen to point at — which is how a
	// restore of a small folder fills a disk.
	link := filepath.Join(res.Destination, "current.conf")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("the link came back as something else: %v", err)
	}
	if target != "real.conf" {
		t.Errorf("the link points at %q", target)
	}
}

func TestABrokenSymlinkIsStillRestored(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "config")
	write(t, filepath.Join(source, "keep.txt"), "kept")
	// Points at nothing. Common in a snapshot: the target was deleted after the
	// snapshot was taken, or it was always outside the folder.
	if err := os.Symlink("../gone/elsewhere.conf", filepath.Join(source, "dangling.conf")); err != nil {
		t.Fatal(err)
	}

	res, err := Restore(context.Background(), source, filepath.Join(dir, "live", "config"), Options{Tag: "t"})
	if err != nil {
		t.Fatalf("a broken link stopped the whole restore: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(res.Destination, "dangling.conf")); err != nil {
		t.Errorf("the broken link was dropped: %v", err)
	}
	// And the file beside it still arrived, which is the point: one unrecoverable
	// thing must not cost the rest of the folder.
	if read(t, filepath.Join(res.Destination, "keep.txt")) != "kept" {
		t.Error("the file next to the broken link was not restored")
	}
}

func TestSomethingWithNoContentToRecoverIsSkippedQuietly(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "run")
	write(t, filepath.Join(source, "notes.txt"), "text")
	fifo := filepath.Join(source, "socket")
	if err := makeFifo(fifo); err != nil {
		t.Skipf("this system will not make a fifo to test with: %v", err)
	}

	res, err := Restore(context.Background(), source, filepath.Join(dir, "live", "run"), Options{Tag: "t"})
	if err != nil {
		t.Fatalf("a fifo stopped the restore: %v", err)
	}

	// Sockets, devices and fifos carry nothing recoverable. Recreating one would
	// need privileges a restore does not have, and failing on one would make a
	// folder that contains one impossible to restore at all.
	if _, err := os.Lstat(filepath.Join(res.Destination, "socket")); !os.IsNotExist(err) {
		t.Errorf("the fifo was recreated rather than skipped: %v", err)
	}
	if read(t, filepath.Join(res.Destination, "notes.txt")) != "text" {
		t.Error("the file beside it was not restored")
	}
	// And it is not counted, because nothing was recovered from it.
	if res.Files != 1 {
		t.Errorf("the count says %d files", res.Files)
	}
}

func TestNestedFoldersAreCountedSeparatelyFromFiles(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "projects")
	write(t, filepath.Join(source, "a", "one.txt"), "1")
	write(t, filepath.Join(source, "a", "b", "two.txt"), "22")
	write(t, filepath.Join(source, "three.txt"), "333")

	res, err := Restore(context.Background(), source, filepath.Join(dir, "live", "projects"), Options{Tag: "t"})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// The figures are what the window reports back, and "3 files in 3 folders" is
	// the sentence someone checks against what they expected to get.
	if res.Files != 3 {
		t.Errorf("files: %d, want 3", res.Files)
	}
	if res.Dirs != 3 {
		t.Errorf("folders: %d, want 3 (projects, a, a/b)", res.Dirs)
	}
	if res.Bytes != 6 {
		t.Errorf("bytes: %d, want 6", res.Bytes)
	}
}

// The partial-write rule. A copy that fails halfway must not leave what it got,
// because a truncated file in the right place looks recovered.
func TestACancelledCopyLeavesNoHalfWrittenFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "big.bin")
	write(t, source, strings.Repeat("x", 1<<20))
	target := filepath.Join(dir, "live", "big.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := Restore(ctx, source, target, Options{Tag: "t"})
	if err == nil {
		t.Fatal("a cancelled restore reported success")
	}

	// Whatever path it had chosen, nothing readable is left at it.
	if res.Destination != "" {
		if _, err := os.Stat(res.Destination); err == nil {
			t.Errorf("a partial file was left at %s", res.Destination)
		}
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("a partial file was left at %s", target)
	}
}

func TestAFolderThatCannotBeReadSaysWhichOne(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a folder with no permission bits, so there is nothing to test")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "projects")
	write(t, filepath.Join(source, "sealed", "inside.txt"), "hidden")
	sealed := filepath.Join(source, "sealed")
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o700) })

	_, err := Restore(context.Background(), source, filepath.Join(dir, "live", "projects"), Options{Tag: "t"})
	if err == nil {
		t.Fatal("a folder that could not be read restored successfully")
	}
	// Naming it is the whole value of the error: "permission denied" on its own
	// leaves the reader to guess which of a thousand folders stopped it.
	if !strings.Contains(err.Error(), "sealed") {
		t.Errorf("the error does not name the folder: %v", err)
	}
}
