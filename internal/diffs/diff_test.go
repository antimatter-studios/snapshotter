package diffs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// baseTime is stamped on every file these fixtures create. A real snapshot
// shares its blocks with the live volume, so an untouched file has an
// identical modification time on both sides; building the two trees seconds
// apart would make every file look modified for a reason that never occurs in
// production.
var baseTime = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// tree builds a directory from a map of relative path to contents. A path
// ending in "/" becomes an empty directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if rel[len(rel)-1] == '/' {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(full, baseTime, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func statusOf(t *testing.T, res Result, rel string) Status {
	t.Helper()
	for _, c := range res.Changes {
		if c.RelPath == rel {
			return c.Status
		}
	}
	t.Fatalf("no change reported for %s (changes: %+v)", rel, res.Changes)
	return ""
}

func TestCompareClassifiesEachKindOfChange(t *testing.T) {
	snap := tree(t, map[string]string{
		"keep.txt":          "same",
		"deleted.txt":       "gone",
		"changed.txt":       "before",
		"nested/inner.txt":  "same",
		"nested/vanished":   "gone",
		"deleted-dir/a.txt": "gone",
	})
	live := tree(t, map[string]string{
		"keep.txt":         "same",
		"changed.txt":      "after, and a different length",
		"added.txt":        "new",
		"nested/inner.txt": "same",
	})

	res, err := Compare(context.Background(), snap, live, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := statusOf(t, res, "deleted.txt"); got != OnlyInSnapshot {
		t.Errorf("deleted.txt: want onlyInSnapshot, got %s", got)
	}
	if got := statusOf(t, res, "added.txt"); got != OnlyOnDisk {
		t.Errorf("added.txt: want onlyOnDisk, got %s", got)
	}
	if got := statusOf(t, res, "changed.txt"); got != Modified {
		t.Errorf("changed.txt: want modified, got %s", got)
	}
	if got := statusOf(t, res, "nested/vanished"); got != OnlyInSnapshot {
		t.Errorf("nested/vanished: want onlyInSnapshot, got %s", got)
	}
	for _, c := range res.Changes {
		if c.RelPath == "keep.txt" {
			t.Error("unchanged file reported as a change")
		}
	}
}

// A deleted folder should be one row naming the folder, not a row per file
// inside it.
func TestCompareReportsADeletedDirectoryAsASingleChange(t *testing.T) {
	snap := tree(t, map[string]string{
		"config/a.json": "1",
		"config/b.json": "2",
		"config/c.json": "3",
	})
	live := tree(t, map[string]string{"other.txt": "x"})

	res, err := Compare(context.Background(), snap, live, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var configRows int
	for _, c := range res.Changes {
		if c.RelPath == "config" {
			configRows++
			if !c.IsDir {
				t.Error("deleted directory not marked as a directory")
			}
		}
		if filepath.Dir(c.RelPath) == "config" {
			t.Errorf("expanded the contents of a deleted directory: %s", c.RelPath)
		}
	}
	if configRows != 1 {
		t.Errorf("want 1 row for the deleted directory, got %d", configRows)
	}
}

func TestCompareIncludesSameWhenAsked(t *testing.T) {
	snap := tree(t, map[string]string{"keep.txt": "same"})
	live := tree(t, map[string]string{"keep.txt": "same"})

	// Equal size and content but a different timestamp: the shallow comparison
	// calls that modified, so the timestamps are aligned first.
	when := time.Now().Add(-time.Hour)
	for _, root := range []string{snap, live} {
		if err := os.Chtimes(filepath.Join(root, "keep.txt"), when, when); err != nil {
			t.Fatal(err)
		}
	}

	res, err := Compare(context.Background(), snap, live, Options{IncludeSame: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, res, "keep.txt"); got != Same {
		t.Errorf("want same, got %s", got)
	}
}

// Same length, same content, different mtime: shallow says modified, deep says
// same. This is the whole reason the deep option exists.
func TestDeepComparisonIgnoresTimestampOnlyDifferences(t *testing.T) {
	snap := tree(t, map[string]string{"file.txt": "identical"})
	live := tree(t, map[string]string{"file.txt": "identical"})
	older := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(snap, "file.txt"), older, older); err != nil {
		t.Fatal(err)
	}

	shallow, err := Compare(context.Background(), snap, live, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, shallow, "file.txt"); got != Modified {
		t.Errorf("shallow: want modified, got %s", got)
	}

	deep, err := Compare(context.Background(), snap, live, Options{Deep: true, IncludeSame: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, deep, "file.txt"); got != Same {
		t.Errorf("deep: want same, got %s", got)
	}
}

// Same length but different content, with matching timestamps: only the deep
// comparison can see this.
func TestDeepComparisonCatchesSameSizeRewrites(t *testing.T) {
	snap := tree(t, map[string]string{"file.txt": "aaaa"})
	live := tree(t, map[string]string{"file.txt": "bbbb"})
	when := time.Now().Add(-time.Hour)
	for _, root := range []string{snap, live} {
		if err := os.Chtimes(filepath.Join(root, "file.txt"), when, when); err != nil {
			t.Fatal(err)
		}
	}

	shallow, err := Compare(context.Background(), snap, live, Options{IncludeSame: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, shallow, "file.txt"); got != Same {
		t.Errorf("shallow comparison is expected to miss this, got %s", got)
	}

	deep, err := Compare(context.Background(), snap, live, Options{Deep: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, deep, "file.txt"); got != Modified {
		t.Errorf("deep: want modified, got %s", got)
	}
}

func TestCompareDetectsTypeChanges(t *testing.T) {
	snap := tree(t, map[string]string{"thing/inner.txt": "dir on this side"})
	live := tree(t, map[string]string{})
	if err := os.Symlink("/elsewhere", filepath.Join(live, "thing")); err != nil {
		t.Fatal(err)
	}

	res, err := Compare(context.Background(), snap, live, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, res, "thing"); got != TypeChanged {
		t.Errorf("want typeChanged, got %s", got)
	}
}

func TestCompareComparesSymlinksByTarget(t *testing.T) {
	snap := tree(t, map[string]string{})
	live := tree(t, map[string]string{})
	if err := os.Symlink("/one", filepath.Join(snap, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/two", filepath.Join(live, "link")); err != nil {
		t.Fatal(err)
	}

	res, err := Compare(context.Background(), snap, live, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, res, "link"); got != Modified {
		t.Errorf("want modified, got %s", got)
	}
}

func TestCompareRespectsMaxDepth(t *testing.T) {
	snap := tree(t, map[string]string{"a/b/c/deep.txt": "gone"})
	live := tree(t, map[string]string{"a/b/c/": ""})

	res, err := Compare(context.Background(), snap, live, Options{MaxDepth: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Changes {
		if c.RelPath == filepath.Join("a", "b", "c", "deep.txt") {
			t.Error("descended past MaxDepth")
		}
	}
}

func TestCompareStopsOnCancellation(t *testing.T) {
	snap := tree(t, map[string]string{"a.txt": "1", "b.txt": "2", "c.txt": "3"})
	live := tree(t, map[string]string{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Compare(ctx, snap, live, Options{}, nil); err == nil {
		t.Error("ignored a cancelled context")
	}
}

func TestCompareReportsProgress(t *testing.T) {
	snap := tree(t, map[string]string{"a.txt": "1", "b.txt": "2"})
	live := tree(t, map[string]string{})

	var calls int
	if _, err := Compare(context.Background(), snap, live, Options{}, func(Progress) { calls++ }); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Error("no progress reported")
	}
}

func TestCountsSummarisesByStatus(t *testing.T) {
	res := Result{Changes: []Change{
		{Status: OnlyInSnapshot}, {Status: OnlyInSnapshot}, {Status: Modified},
	}}
	counts := res.Counts()
	if counts[OnlyInSnapshot] != 2 || counts[Modified] != 1 {
		t.Errorf("unexpected counts: %+v", counts)
	}
}

func TestCompareRejectsAFileAsARoot(t *testing.T) {
	root := tree(t, map[string]string{"file.txt": "x"})
	if _, err := Compare(context.Background(), filepath.Join(root, "file.txt"), root, Options{}, nil); err == nil {
		t.Error("accepted a file as the snapshot side")
	}
}
