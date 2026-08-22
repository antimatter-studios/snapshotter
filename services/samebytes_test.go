package services

import (
	"os"
	"path/filepath"
	"testing"
)

// Whether two versions of a picture are the same file.
//
// Worth answering, and worth testing, because two images can look identical on
// screen and there is no line-by-line comparison to settle it — so this is the
// only thing that can say "nothing changed" with authority. It went in with one
// branch of five exercised.

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIdenticalContentsAreTheSame(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a", []byte("the same bytes"))
	b := write(t, dir, "b", []byte("the same bytes"))

	if !sameBytes(a, b, true, true, 14, 14) {
		t.Error("two identical files were reported as different")
	}
}

func TestDifferentContentsOfTheSameLengthAreNotTheSame(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a", []byte("aaaaaaaaaaaaaa"))
	b := write(t, dir, "b", []byte("bbbbbbbbbbbbbb"))

	// The same length, so the cheap check passes and the contents decide. This is
	// the case a size comparison alone would get wrong.
	if sameBytes(a, b, true, true, 14, 14) {
		t.Error("two different files of equal length were called the same")
	}
}

// Sizes are compared first, which settles most pairs without reading anything.
// A different size cannot be the same file, and saying so must not depend on
// either being readable.
func TestDifferentSizesAreSettledWithoutReading(t *testing.T) {
	if sameBytes("/nonexistent/a", "/nonexistent/b", true, true, 10, 20) {
		t.Error("files of different sizes were called the same")
	}
}

// A side that is not there cannot match one that is. This is how a picture added
// or deleted since the snapshot arrives here.
func TestAMissingSideIsNeverTheSame(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a", []byte("something"))

	if sameBytes(a, filepath.Join(dir, "gone"), true, false, 9, 9) {
		t.Error("a missing right side was called the same")
	}
	if sameBytes(filepath.Join(dir, "gone"), a, false, true, 9, 9) {
		t.Error("a missing left side was called the same")
	}
	if sameBytes("x", "y", false, false, 0, 0) {
		t.Error("two absent files were called the same")
	}
}

// A file that exists but cannot be read is not the same as anything, rather than
// being reported as a match by default.
func TestAnUnreadableFileIsNotTreatedAsAMatch(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a", []byte("readable"))
	locked := write(t, dir, "locked", []byte("readable"))
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skip("cannot remove read permission here")
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })

	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read it anyway")
	}
	if sameBytes(a, locked, true, true, 8, 8) {
		t.Error("an unreadable file was reported as matching")
	}
}
