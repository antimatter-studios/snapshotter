package restore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRestoreWritesToAFreshPathWhenNothingIsThere(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "keys.json")
	target := filepath.Join(dir, "live", "keys.json")
	write(t, source, "recovered")

	res, err := Restore(context.Background(), source, target, Options{Tag: "2026-08-13-172036"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Destination != target {
		t.Errorf("want %s, got %s", target, res.Destination)
	}
	if read(t, target) != "recovered" {
		t.Error("contents not restored")
	}
}

// The default must never touch what is already on disk: the live file may be
// the one worth keeping, and the user is usually restoring in order to compare.
func TestSideBySideLeavesTheExistingFileAlone(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "config.json")
	target := filepath.Join(dir, "live", "config.json")
	write(t, source, "from the snapshot")
	write(t, target, "the current one")

	res, err := Restore(context.Background(), source, target, Options{Tag: "2026-08-13-172036"})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, target) != "the current one" {
		t.Fatal("overwrote the live file")
	}
	want := target + ".restored-2026-08-13-172036"
	if res.Destination != want {
		t.Errorf("want %s, got %s", want, res.Destination)
	}
	if read(t, want) != "from the snapshot" {
		t.Error("restored copy has the wrong contents")
	}
}

func TestReplaceBacksUpBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "config.json")
	target := filepath.Join(dir, "live", "config.json")
	write(t, source, "from the snapshot")
	write(t, target, "the current one")

	res, err := Restore(context.Background(), source, target, Options{Mode: Replace, Tag: "2026-08-13-172036"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Destination != target {
		t.Errorf("want the original path, got %s", res.Destination)
	}
	if read(t, target) != "from the snapshot" {
		t.Error("restored copy is not at the original path")
	}
	if res.BackedUp == "" {
		t.Fatal("no backup recorded")
	}
	if read(t, res.BackedUp) != "the current one" {
		t.Error("backup does not hold the previous contents")
	}
}

// Restoring the same snapshot twice must not clobber the first copy.
func TestRepeatedRestoreKeepsEveryCopy(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "config.json")
	target := filepath.Join(dir, "live", "config.json")
	write(t, source, "from the snapshot")
	write(t, target, "the current one")

	first, err := Restore(context.Background(), source, target, Options{Tag: "2026-08-13-172036"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Restore(context.Background(), source, target, Options{Tag: "2026-08-13-172036"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Destination == second.Destination {
		t.Fatalf("second restore reused %s", second.Destination)
	}
	if read(t, first.Destination) != "from the snapshot" {
		t.Error("first copy was damaged")
	}
}

func TestRestoreCopiesADirectoryTree(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "config")
	target := filepath.Join(dir, "live", "config")
	write(t, filepath.Join(source, "a.json"), "one")
	write(t, filepath.Join(source, "nested", "b.json"), "two")
	if err := os.Symlink("../a.json", filepath.Join(source, "nested", "link")); err != nil {
		t.Fatal(err)
	}

	res, err := Restore(context.Background(), source, target, Options{Tag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(target, "a.json")) != "one" {
		t.Error("top-level file missing")
	}
	if read(t, filepath.Join(target, "nested", "b.json")) != "two" {
		t.Error("nested file missing")
	}
	link, err := os.Readlink(filepath.Join(target, "nested", "link"))
	if err != nil || link != "../a.json" {
		t.Errorf("symlink not preserved: %q %v", link, err)
	}
	if res.Files != 3 || res.Dirs != 2 {
		t.Errorf("unexpected counts: %+v", res)
	}
}

func TestRestorePreservesModTimeAndPermissions(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "key.pem")
	target := filepath.Join(dir, "live", "key.pem")
	write(t, source, "secret")
	if err := os.Chmod(source, 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 8, 11, 9, 30, 0, 0, time.UTC)
	if err := os.Chtimes(source, when, when); err != nil {
		t.Fatal(err)
	}

	if _, err := Restore(context.Background(), source, target, Options{Tag: "t"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("want mode 0600, got %v", info.Mode().Perm())
	}
	if !info.ModTime().Equal(when) {
		t.Errorf("want mtime %s, got %s", when, info.ModTime())
	}
}

func TestRestoreReportsAMissingSource(t *testing.T) {
	dir := t.TempDir()
	if _, err := Restore(context.Background(), filepath.Join(dir, "nope"), filepath.Join(dir, "out"), Options{}); err == nil {
		t.Error("accepted a source that does not exist")
	}
}

func TestRestoreStopsOnCancellation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "snap", "big")
	write(t, filepath.Join(source, "a.txt"), "one")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Restore(ctx, source, filepath.Join(dir, "live", "big"), Options{}); err == nil {
		t.Error("ignored a cancelled context")
	}
}

func TestDescribeNamesTheDestination(t *testing.T) {
	got := Result{Destination: "/Users/x/config.restored-t", Files: 2, Dirs: 1}.Describe()
	if got == "" {
		t.Fatal("empty description")
	}
	if want := "/Users/x/config.restored-t"; !strings.Contains(got, want) {
		t.Errorf("description does not name the destination: %s", got)
	}
}
