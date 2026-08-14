package vfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDirPutsDirectoriesFirstThenSortsCaseInsensitively(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "zebra.txt"), "z")
	mustWrite(t, filepath.Join(dir, "Apple.txt"), "a")
	if err := os.Mkdir(filepath.Join(dir, "middle"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{entries[0].Name, entries[1].Name, entries[2].Name}
	want := []string{"middle", "Apple.txt", "zebra.txt"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

func TestListDirReportsSymlinksWithoutFollowingThem(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "real.txt"), "hello")
	if err := os.Symlink("/Volumes/elsewhere", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var link *Entry
	for i := range entries {
		if entries[i].Name == "link" {
			link = &entries[i]
		}
	}
	if link == nil {
		t.Fatal("symlink missing from listing")
	}
	if link.Symlink != "/Volumes/elsewhere" {
		t.Errorf("want target /Volumes/elsewhere, got %q", link.Symlink)
	}
	if link.IsDir {
		t.Error("symlink reported as a directory")
	}
}

func TestStatReadsSizeAndName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	mustWrite(t, path, "12345")

	e, err := Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "file.txt" || e.Size != 5 {
		t.Errorf("unexpected entry: %+v", e)
	}
}

func TestListDirReportsMissingDirectory(t *testing.T) {
	if _, err := ListDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("accepted a directory that does not exist")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
