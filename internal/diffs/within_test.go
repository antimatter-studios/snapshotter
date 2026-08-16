package diffs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The bug this replaces: Level reported every directory present on both sides as
// unchanged, without looking inside it once. A home folder with thirteen
// thousand modified files under ~/projects showed "no changes" — which is the
// one thing this application must never say wrongly.

// fixedTime is stamped on every file these tests write.
//
// Without it a file written to one tree and an identical file written to the
// other differ by however long the two writes were apart, and a comparison that
// checks size and modification time — which is what this does, and what makes it
// cheap — calls that modified. It is not wrong to do so: a real snapshot
// preserves modification times exactly, so two unchanged files genuinely carry
// the same one. Only a test writing both sides by hand has to say so.
var fixedTime = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

// writeTree writes files under dir, all with the same modification time.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, fixedTime, fixedTime); err != nil {
			t.Fatal(err)
		}
	}
}

func TestADeeplyBuriedChangeIsFound(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	same := map[string]string{"a/b/c/d/e/deep.txt": "original", "top.txt": "same"}
	writeTree(t, snap, same)
	writeTree(t, live, same)

	if differs, answered := DiffersWithin(snap, live, Options{}); differs || !answered {
		t.Fatalf("identical trees: differs=%v answered=%v", differs, answered)
	}

	// The change the old code could not see: five levels down, with every
	// directory's own metadata untouched. Different length, so size alone catches
	// it and the test does not depend on clock resolution.
	writeTree(t, live, map[string]string{"a/b/c/d/e/deep.txt": "edited and longer"})

	differs, answered := DiffersWithin(snap, live, Options{})
	if !answered {
		t.Fatal("the question was not answered")
	}
	if !differs {
		t.Error("a change five levels down was reported as no change")
	}
}

func TestSomethingAddedOrRemovedCounts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		snap, live map[string]string
	}{
		{"added on disk", map[string]string{"a/x": "1"}, map[string]string{"a/x": "1", "a/y": "2"}},
		{"deleted from disk", map[string]string{"a/x": "1", "a/y": "2"}, map[string]string{"a/x": "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap, live := t.TempDir(), t.TempDir()
			writeTree(t, snap, tc.snap)
			writeTree(t, live, tc.live)

			differs, answered := DiffersWithin(snap, live, Options{})
			if !answered || !differs {
				t.Errorf("differs=%v answered=%v", differs, answered)
			}
		})
	}
}

// Level is what the browser calls, and it is where the wrong answer was given.
func TestLevelGivesADirectoryARealVerdict(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"changed/deep/file.txt": "before", "untouched/deep/file.txt": "same"})
	writeTree(t, live, map[string]string{"changed/deep/file.txt": "after and longer", "untouched/deep/file.txt": "same"})

	rows, err := Level(snap, live, Options{IncludeSame: true})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]Status{}
	for _, r := range rows {
		got[r.RelPath] = r.Status
	}
	if got["changed"] != Modified {
		t.Errorf("a folder with a changed file inside is %q, want modified", got["changed"])
	}
	if got["untouched"] != Same {
		t.Errorf("a folder with nothing changed inside is %q, want same", got["untouched"])
	}
}

// With unchanged rows hidden — the browser's default — a changed folder must
// still appear. This is exactly how the folder went missing.
func TestAChangedFolderIsNotHiddenAsUnchanged(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"projects/deep/thing.go": "before"})
	writeTree(t, live, map[string]string{"projects/deep/thing.go": "after and longer"})

	rows, err := Level(snap, live, Options{IncludeSame: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.RelPath == "projects" {
			return
		}
	}
	t.Error("a folder with changes inside was filtered out as unchanged")
}

// Not knowing is not the same as nothing having changed. The budget exists so a
// vast untouched tree cannot block the window, and the row must say so rather
// than claim it is unchanged.
func TestRunningOutOfBudgetIsNotAClaimOfSameness(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()

	// A tiny budget rather than a vast tree: the real one is half a million
	// entries, and building that to prove a boundary would make this test take
	// eight minutes, which it once did.
	const tiny = 3
	files := map[string]string{}
	for i := 0; i < tiny+20; i++ {
		files[filepath.Join("many", "f"+itoa(i))] = "x"
	}
	writeTree(t, snap, files)
	writeTree(t, live, files)

	differs, answered := differsWithinBudget(snap, live, Options{}, tiny)
	if answered {
		t.Fatal("a budget of three examined a folder of twenty without giving up")
	}
	if differs {
		t.Error("an unanswered question reported a difference")
	}
}

// The backstop must be far above anything ordinary, or it becomes a refusal to
// answer dressed up as a result — which is what it was, with every large folder
// in a home directory reporting "not examined" and nothing else.
func TestTheBudgetIsABackstopRatherThanALimit(t *testing.T) {
	if examineBudget < 100000 {
		t.Errorf("the budget is %d, low enough that ordinary folders will hit it", examineBudget)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// A folder that cannot be read must not decide the answer for everything around
// it. One protected subfolder used to abort the entire walk, and the interface
// then reported that as "too large to check" — which was not what had happened.
func TestAnUnreadableSubfolderDoesNotAbortTheWholeAnswer(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{
		"locked/secret.txt": "hidden",
		"visible/thing.txt": "before",
	})
	writeTree(t, live, map[string]string{
		"locked/secret.txt": "hidden",
		"visible/thing.txt": "after and longer",
	})

	// Unreadable on one side only, which is what a privacy-protected folder
	// looks like from here.
	locked := filepath.Join(live, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skipf("cannot make a directory unreadable here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	differs, answered := DiffersWithin(snap, live, Options{})
	if !answered {
		t.Fatal("one unreadable folder made the whole tree unanswerable")
	}
	if !differs {
		t.Error("the change in the readable folder was missed")
	}
}

// But when the only thing that could have differed was unreadable, saying
// "unchanged" would be a guess dressed as a fact.
func TestAnUnreadableFolderWithNothingElseIsUnanswered(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"locked/secret.txt": "hidden"})
	writeTree(t, live, map[string]string{"locked/secret.txt": "hidden"})

	locked := filepath.Join(live, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skipf("cannot make a directory unreadable here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	differs, answered := DiffersWithin(snap, live, Options{})
	if answered {
		t.Error("a tree whose only folder could not be read was answered for")
	}
	if differs {
		t.Error("an unanswered question reported a difference")
	}
}
