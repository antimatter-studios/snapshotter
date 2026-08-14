package diffs

import (
	"os"
	"path/filepath"
	"testing"
)

func rowFor(t *testing.T, rows []Change, name string) Change {
	t.Helper()
	for _, r := range rows {
		if r.RelPath == name {
			return r
		}
	}
	t.Fatalf("no row for %s (rows: %+v)", name, rows)
	return Change{}
}

func TestLevelMarksEachEntryWithoutDescending(t *testing.T) {
	snap := tree(t, map[string]string{
		"same.txt":        "unchanged",
		"deleted.txt":     "gone",
		"changed.txt":     "before",
		"folder/deep.txt": "buried",
	})
	live := tree(t, map[string]string{
		"same.txt":        "unchanged",
		"changed.txt":     "after, with a different length",
		"added.txt":       "new",
		"folder/deep.txt": "buried",
	})

	rows, err := Level(snap, live, Options{IncludeSame: true})
	if err != nil {
		t.Fatal(err)
	}

	if got := rowFor(t, rows, "same.txt").Status; got != Same {
		t.Errorf("same.txt: want same, got %s", got)
	}
	if got := rowFor(t, rows, "deleted.txt").Status; got != OnlyInSnapshot {
		t.Errorf("deleted.txt: want onlyInSnapshot, got %s", got)
	}
	if got := rowFor(t, rows, "added.txt").Status; got != OnlyOnDisk {
		t.Errorf("added.txt: want onlyOnDisk, got %s", got)
	}
	if got := rowFor(t, rows, "changed.txt").Status; got != Modified {
		t.Errorf("changed.txt: want modified, got %s", got)
	}

	// The folder is one row; nothing inside it was visited.
	folder := rowFor(t, rows, "folder")
	if !folder.IsDir || folder.Status != Same {
		t.Errorf("folder: want an unchanged directory row, got %+v", folder)
	}
	for _, r := range rows {
		if r.RelPath == "deep.txt" || r.RelPath == filepath.Join("folder", "deep.txt") {
			t.Error("descended into a subdirectory")
		}
	}
}

func TestLevelPutsFoldersFirst(t *testing.T) {
	snap := tree(t, map[string]string{"a.txt": "x", "zzz/": ""})
	live := tree(t, map[string]string{"a.txt": "x", "zzz/": ""})

	rows, err := Level(snap, live, Options{IncludeSame: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].RelPath != "zzz" {
		t.Errorf("want the folder first, got %+v", rows)
	}
}

// A folder created since the snapshot has no snapshot side at all; that is an
// answer, not an error.
func TestLevelHandlesAMissingSideAsNewOrDeleted(t *testing.T) {
	live := tree(t, map[string]string{"new.txt": "created since"})
	missing := filepath.Join(t.TempDir(), "never-existed")

	rows, err := Level(missing, live, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowFor(t, rows, "new.txt").Status; got != OnlyOnDisk {
		t.Errorf("want onlyOnDisk, got %s", got)
	}
}

func TestLevelFailsWhenNeitherSideExists(t *testing.T) {
	dir := t.TempDir()
	if _, err := Level(filepath.Join(dir, "a"), filepath.Join(dir, "b"), Options{}); err == nil {
		t.Error("accepted two directories that do not exist")
	}
}

func TestLevelHidesUnchangedEntriesUnlessAsked(t *testing.T) {
	snap := tree(t, map[string]string{"same.txt": "unchanged", "gone.txt": "x"})
	live := tree(t, map[string]string{"same.txt": "unchanged"})

	rows, err := Level(snap, live, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RelPath != "gone.txt" {
		t.Errorf("want only the deleted file, got %+v", rows)
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	if !DirExists(dir) {
		t.Error("reported an existing directory as missing")
	}
	if DirExists(filepath.Join(dir, "nope")) {
		t.Error("reported a missing directory as present")
	}
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if DirExists(file) {
		t.Error("reported a file as a directory")
	}
}
