package diffs

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A walk stops at the first difference, so a "changed" verdict always rests on
// one file. Recording which one turns the next question about that folder — and
// about every folder between it and the file — into a single stat.
//
// The direction matters throughout: a recorded change can only ever prove "changed".

func TestTheWalkSaysWhichPathProvedTheDifference(t *testing.T) {
	for _, c := range []struct {
		name       string
		snap, live map[string]string
		want       string
	}{
		{
			name: "a file that differs",
			snap: map[string]string{"a/b/notes.md": "short"},
			live: map[string]string{"a/b/notes.md": "much longer than before"},
			want: "a/b/notes.md",
		},
		{
			name: "a file deleted since the snapshot",
			snap: map[string]string{"a/b/gone.txt": "x", "a/b/kept.txt": "y"},
			live: map[string]string{"a/b/kept.txt": "y"},
			want: "a/b/gone.txt",
		},
		{
			name: "a file created since the snapshot",
			snap: map[string]string{"a/b/kept.txt": "y"},
			live: map[string]string{"a/b/kept.txt": "y", "a/b/new.txt": "z"},
			want: "a/b/new.txt",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			snap, live := t.TempDir(), t.TempDir()
			writeTree(t, snap, c.snap)
			writeTree(t, live, c.live)

			out := Explain(snap, live, Options{})
			if !out.Differs {
				t.Fatalf("no difference found: %+v", out)
			}
			// The live path, because that is the side a re-check can look at
			// without knowing where anything is mounted.
			if want := filepath.Join(live, c.want); out.ChangedPath != want {
				t.Errorf("recorded change %q, want %q", out.ChangedPath, want)
			}
		})
	}
}

// The recorded change has to survive the way back up, because the folder being asked
// about is usually far above the file that settles it.
func TestTheChangedPathIsCarriedUpFromHoweverDeepItWas(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"one/two/three/four/deep.txt": "before"})
	writeTree(t, live, map[string]string{"one/two/three/four/deep.txt": "after, and longer"})

	out := Explain(snap, live, Options{})
	if want := filepath.Join(live, "one/two/three/four/deep.txt"); out.ChangedPath != want {
		t.Errorf("recorded change %q, want %q", out.ChangedPath, want)
	}
}

// Nothing to point at when nothing differs. A recorded change on an unchanged folder
// would be re-checked for ever and never prove anything.
func TestAnUnchangedFolderRecordsNoChangedPath(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{"a/one.txt": "x", "b/two.txt": "y"}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	out := Explain(snap, live, Options{})
	if out.Differs {
		t.Fatal("the trees differ, so this test proves nothing")
	}
	if out.ChangedPath != "" {
		t.Errorf("recorded change %q on an unchanged folder", out.ChangedPath)
	}
}

// The re-check itself: one path, no walking.
func TestStillDiffersAnswersForOnePath(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{
		"changed.txt": "short",
		"same.txt":    "identical",
		"deleted.txt": "gone since",
	})
	writeTree(t, live, map[string]string{
		"changed.txt": "much longer than before",
		"same.txt":    "identical",
		"created.txt": "new since",
	})

	for _, c := range []struct {
		name        string
		differs, ok bool
	}{
		{"changed.txt", true, true},
		{"same.txt", false, true},
		{"deleted.txt", true, true},
		{"created.txt", true, true},
		// Neither side has it: not a difference, and not a failure either.
		{"never-existed.txt", false, true},
	} {
		differs, ok := StillDiffers(filepath.Join(snap, c.name), filepath.Join(live, c.name), Options{})
		if differs != c.differs || ok != c.ok {
			t.Errorf("%s: differs=%v ok=%v, want differs=%v ok=%v", c.name, differs, ok, c.differs, c.ok)
		}
	}
}

// The whole point: a file put back stops proving anything, and says so rather
// than reporting "unchanged" — which would be a claim about a tree it never read.
func TestARestoredFileStopsProvingTheFolderChanged(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"notes.md": "original"})
	writeTree(t, live, map[string]string{"notes.md": "edited, and longer"})

	if differs, ok := StillDiffers(filepath.Join(snap, "notes.md"), filepath.Join(live, "notes.md"), Options{}); !differs || !ok {
		t.Fatalf("differs=%v ok=%v before the file is put back", differs, ok)
	}

	// Put back, exactly as it was — same contents, same stamp.
	writeTree(t, live, map[string]string{"notes.md": "original"})
	differs, ok := StillDiffers(filepath.Join(snap, "notes.md"), filepath.Join(live, "notes.md"), Options{})
	if differs {
		t.Error("a restored file still reports a difference")
	}
	if !ok {
		t.Error("a restored file could not be checked")
	}
}

// A type change is a difference and needs nothing read.
func TestAPathThatChangedTypeStillDiffers(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"thing/inside.txt": "x"})
	if err := os.WriteFile(filepath.Join(live, "thing"), []byte("now a file"), 0o600); err != nil {
		t.Fatal(err)
	}

	if differs, ok := StillDiffers(filepath.Join(snap, "thing"), filepath.Join(live, "thing"), Options{}); !differs || !ok {
		t.Errorf("differs=%v ok=%v", differs, ok)
	}
}

// A directory present on both sides is settled by one read of each — this used to
// give up and hand the whole tree back to the walk. What it must still never do
// is report "unchanged": the subdirectories were deliberately not read, and an
// unexamined subtree is not an unchanged one.
func TestADirectoryOnBothSidesIsSettledShallowlyOrNotAtAll(t *testing.T) {
	// A file at this level differs, which one read finds.
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"folder/a.txt": "x"})
	writeTree(t, live, map[string]string{"folder/a.txt": "different and longer"})
	if differs, ok := StillDiffers(filepath.Join(snap, "folder"), filepath.Join(live, "folder"), Options{}); !differs || !ok {
		t.Errorf("differs=%v ok=%v for a file that differs at this level", differs, ok)
	}

	// Everything at this level matches and the difference is deeper. It has to
	// admit it cannot say, rather than report the folder unchanged.
	deepSnap, deepLive := t.TempDir(), t.TempDir()
	writeTree(t, deepSnap, map[string]string{"folder/sub/deep.txt": "x"})
	writeTree(t, deepLive, map[string]string{"folder/sub/deep.txt": "different and longer"})
	differs, ok := StillDiffers(filepath.Join(deepSnap, "folder"), filepath.Join(deepLive, "folder"), Options{})
	if differs {
		t.Error("claimed a difference it did not read")
	}
	if ok {
		t.Error("claimed to settle a folder whose subdirectories were never read")
	}
}

// An unreadable path is not evidence. Reporting it as a difference would leave a
// folder marked changed for ever on the strength of a permission error.
func TestAnUnreadablePathIsNotEvidence(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"locked/secret.txt": "x"})
	writeTree(t, live, map[string]string{"locked/secret.txt": "x"})

	if err := os.Chmod(filepath.Join(live, "locked"), 0o000); err != nil {
		t.Skip("cannot remove read permission here")
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(live, "locked"), 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read it anyway")
	}

	_, ok := StillDiffers(
		filepath.Join(snap, "locked/secret.txt"),
		filepath.Join(live, "locked/secret.txt"),
		Options{},
	)
	if ok {
		t.Error("an unreadable path was treated as an answer")
	}
}

// A changed FILE is where changed data actually lives; a directory is only ever a
// reason to go looking. So each directory has to be settled by what it holds
// before anything is descended into.
//
// The order used to be alphabetical, which meant a folder called "archive" was
// recursed into — and everything beneath it read — before a changed file called
// "notes.md" in the same directory was ever looked at.
func TestAChangedFileIsFoundWithoutDescendingIntoSiblingFolders(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()

	// "archive" sorts before "notes.md", and holds a large identical tree that a
	// walk would have to read in full to get past.
	tree := map[string]string{}
	for i := 0; i < 2000; i++ {
		tree["archive/"+strconv.Itoa(i)+".txt"] = "x"
	}
	tree["notes.md"] = "before"
	writeTree(t, snap, tree)

	tree["notes.md"] = "after, and longer"
	writeTree(t, live, tree)

	// A budget far too small to read archive, and easily enough for the handful
	// of entries in the top directory.
	const budget = 20
	differs, answered := differsWithinBudget(snap, live, Options{}, budget)
	if !differs || !answered {
		t.Errorf("differs=%v answered=%v: the changed file was not found before the folder beside it was walked", differs, answered)
	}
}

// The same for the cheapest difference of all, which needs nothing opened: a name
// present on one side only is settled by the directory read that already
// happened.
func TestADeletedFileIsFoundWithoutDescendingIntoSiblingFolders(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()

	tree := map[string]string{}
	for i := 0; i < 2000; i++ {
		tree["archive/"+strconv.Itoa(i)+".txt"] = "x"
	}
	writeTree(t, live, tree)
	tree["zz-gone.txt"] = "deleted since"
	writeTree(t, snap, tree)

	const budget = 20
	differs, answered := differsWithinBudget(snap, live, Options{}, budget)
	if !differs || !answered {
		t.Errorf("differs=%v answered=%v", differs, answered)
	}
}

// And the ordering must not change which path is reported. The file in this
// directory is the difference; a file inside the folder beside it is a different
// claim, and the recorded path is what every later cheap check rests on.
func TestTheNearestDifferenceIsTheOneRecorded(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"archive/deep.txt": "before", "notes.md": "before"})
	writeTree(t, live, map[string]string{"archive/deep.txt": "after, longer", "notes.md": "after, longer"})

	out := Explain(snap, live, Options{})
	if want := filepath.Join(live, "notes.md"); out.ChangedPath != want {
		t.Errorf("recorded %q, want the file in this directory %q", out.ChangedPath, want)
	}
}

// A deleted directory needs no special handling and no recorded kind: it is
// caught by the same branch as a deleted file, because "present on one side
// only" does not care what the thing is.
func TestADeletedDirectoryIsCheckedLikeAnythingElse(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"project/src/main.go": "package main"})
	// The live side never had it.

	out := Explain(snap, live, Options{})
	if !out.Differs {
		t.Fatal("a deleted directory was not found")
	}
	if want := filepath.Join(live, "project"); out.ChangedPath != want {
		t.Errorf("recorded %q, want %q", out.ChangedPath, want)
	}

	// And re-checking it needs nothing read, and no stored flag saying it was a
	// directory rather than a file.
	differs, ok := StillDiffers(filepath.Join(snap, "project"), filepath.Join(live, "project"), Options{})
	if !differs || !ok {
		t.Errorf("differs=%v ok=%v re-checking a deleted directory", differs, ok)
	}

	// Recreated under the same name, and empty. A directory carries no data of its
	// own beyond its name, so what settles it is what is inside — and one read of
	// each side finds that "src" is missing, without recursing.
	if err := os.MkdirAll(filepath.Join(live, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	differs, ok = StillDiffers(filepath.Join(snap, "project"), filepath.Join(live, "project"), Options{})
	if !differs || !ok {
		t.Errorf("differs=%v ok=%v: a directory made again with different contents", differs, ok)
	}
}

// The recreated-directory case in full, which is what a recorded difference turns
// into once somebody makes the folder again.
func TestARecreatedDirectoryIsSettledByOneReadOfEachSide(t *testing.T) {
	for _, c := range []struct {
		name        string
		live        map[string]string
		differs, ok bool
	}{
		{
			// A name on one side only, found without opening anything.
			name:    "a file inside it is gone",
			live:    map[string]string{"dogs/rex.txt": "x"},
			differs: true, ok: true,
		},
		{
			name:    "a file inside it changed",
			live:    map[string]string{"dogs/rex.txt": "x", "dogs/bella.txt": "much longer now"},
			differs: true, ok: true,
		},
		{
			// Everything at this level matches. The subdirectory was deliberately
			// not read, so nothing can be concluded — and it says so rather than
			// reporting "unchanged" for a tree it never looked at.
			name:    "everything at this level matches",
			live:    map[string]string{"dogs/rex.txt": "x", "dogs/bella.txt": "y", "dogs/pups/one.txt": "z"},
			differs: false, ok: false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			snap, live := t.TempDir(), t.TempDir()
			writeTree(t, snap, map[string]string{
				"dogs/rex.txt":      "x",
				"dogs/bella.txt":    "y",
				"dogs/pups/one.txt": "z",
			})
			writeTree(t, live, c.live)

			differs, ok := StillDiffers(filepath.Join(snap, "dogs"), filepath.Join(live, "dogs"), Options{})
			if differs != c.differs || ok != c.ok {
				t.Errorf("differs=%v ok=%v, want differs=%v ok=%v", differs, ok, c.differs, c.ok)
			}
		})
	}
}

// A shallow read must not report a difference inside a folder somebody has told
// this application not to look in.
func TestARecreatedDirectoryRespectsTheIgnoreList(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"dogs/node_modules/x.js": "before", "dogs/rex.txt": "x"})
	writeTree(t, live, map[string]string{"dogs/rex.txt": "x"})

	// Without the list, node_modules being gone is the difference.
	if differs, ok := StillDiffers(filepath.Join(snap, "dogs"), filepath.Join(live, "dogs"), Options{}); !differs || !ok {
		t.Fatalf("differs=%v ok=%v without the ignore list", differs, ok)
	}
	differs, ok := StillDiffers(filepath.Join(snap, "dogs"), filepath.Join(live, "dogs"),
		Options{Ignore: NewIgnore([]string{"node_modules"})})
	if differs {
		t.Error("an ignored folder was reported as the difference")
	}
	if ok {
		t.Error("claimed to settle a directory whose subfolders were never read")
	}
}

// A missing folder is as cheap to find as a missing file, and must be found
// before anything is descended into.
//
// The case that motivates it: a folder called "Zebra" is gone, and it sorts last.
// Walking in name order means reading every tree before it — potentially the
// whole disk — to reach a difference the first directory read already contained.
func TestAMissingFolderIsFoundWithoutDescendingIntoAnything(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()

	tree := map[string]string{}
	for _, dir := range []string{"aardvark", "badger", "camel"} {
		for i := 0; i < 700; i++ {
			tree[dir+"/"+strconv.Itoa(i)+".txt"] = "x"
		}
	}
	writeTree(t, live, tree)
	tree["zebra/stripes.txt"] = "only in the snapshot"
	writeTree(t, snap, tree)

	// A budget that could not read one of those folders, let alone three.
	const budget = 30
	differs, answered := differsWithinBudget(snap, live, Options{}, budget)
	if !differs || !answered {
		t.Errorf("differs=%v answered=%v: a missing folder was not found until the trees before it had been read", differs, answered)
	}

	// And it is the missing folder that gets recorded, not something inside the
	// trees that were never the difference.
	out := Explain(snap, live, Options{})
	if want := filepath.Join(live, "zebra"); out.ChangedPath != want {
		t.Errorf("recorded %q, want %q", out.ChangedPath, want)
	}
}

// Why the kind is discovered rather than stored.
//
// A recorded difference says a path differed; it does not say what that path is,
// because that can change. Delete the directory "dogs" and put a FILE called
// "dogs" in its place and a stored "this was a directory" would send the re-check
// down the wrong branch entirely. One lstat of each side reads what is actually
// there now, and costs no more than reading a flag would have.
func TestTheKindIsReadFromTheDiskRatherThanRemembered(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	writeTree(t, snap, map[string]string{"dogs/rex.txt": "x"})

	// Recorded while it was a directory that had been deleted. Nothing else on the
	// live side, so "dogs" is unambiguously the difference — an earlier-sorting
	// name would be found first, and correctly so.
	out := Explain(snap, live, Options{})
	if want := filepath.Join(live, "dogs"); out.ChangedPath != want {
		t.Fatalf("recorded %q, want %q", out.ChangedPath, want)
	}

	// Now a file of that name exists where the directory was. The recorded path
	// has not changed; what it points at has.
	if err := os.WriteFile(filepath.Join(live, "dogs"), []byte("now a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	differs, ok := StillDiffers(filepath.Join(snap, "dogs"), filepath.Join(live, "dogs"), Options{})
	if !differs || !ok {
		t.Errorf("differs=%v ok=%v: a directory replaced by a file of the same name", differs, ok)
	}

	// And the other way round, which is the case a stored flag would get wrong in
	// the opposite direction.
	backSnap, backLive := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(backSnap, "dogs"), []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTree(t, backLive, map[string]string{"dogs/rex.txt": "x"})
	differs, ok = StillDiffers(filepath.Join(backSnap, "dogs"), filepath.Join(backLive, "dogs"), Options{})
	if !differs || !ok {
		t.Errorf("differs=%v ok=%v: a file replaced by a directory of the same name", differs, ok)
	}
}

// The mirror of a recorded difference. One difference proves every folder ABOVE
// it changed; a walk that completes without finding one has read every folder
// BELOW it and proved them all identical — and it has already paid for that, so
// throwing it away means walking each child again the moment somebody opens it.

func TestACompletedCleanWalkVouchesForEveryFolderItRead(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{
		"one/a.txt":        "x",
		"one/deeper/b.txt": "y",
		"two/c.txt":        "z",
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	out := Explain(snap, live, Options{})
	if out.Differs || !out.Answered {
		t.Fatalf("%+v", out)
	}

	got := map[string]bool{}
	for _, d := range out.Clean {
		got[d] = true
	}
	for _, want := range []string{"one", "one/deeper", "two"} {
		if !got[filepath.Join(live, want)] {
			t.Errorf("%s was read and found clean, and was not vouched for", want)
		}
	}
}

// A walk that stops early has read part of a tree, and part of a tree proves
// nothing about the rest of it.
func TestAWalkThatStopsVouchesForNothing(t *testing.T) {
	t.Run("stopped at a difference", func(t *testing.T) {
		snap, live := t.TempDir(), t.TempDir()
		writeTree(t, snap, map[string]string{"one/a.txt": "x", "two/b.txt": "y"})
		writeTree(t, live, map[string]string{"one/a.txt": "x", "two/b.txt": "different and longer"})

		out := Explain(snap, live, Options{})
		if !out.Differs {
			t.Fatal("no difference found, so this test proves nothing")
		}
		if len(out.Clean) != 0 {
			t.Errorf("a walk that found a difference vouched for %v", out.Clean)
		}
	})

	t.Run("abandoned", func(t *testing.T) {
		snap, live := t.TempDir(), t.TempDir()
		tree := map[string]string{}
		for i := 0; i < 2000; i++ {
			tree["one/"+strconv.Itoa(i)+".txt"] = "x"
		}
		writeTree(t, snap, tree)
		writeTree(t, live, tree)

		out := Explain(snap, live, Options{Abandoned: func() bool { return true }})
		if !out.Abandoned {
			t.Fatal("the walk was not abandoned, so this test proves nothing")
		}
		if len(out.Clean) != 0 {
			t.Errorf("an abandoned walk vouched for %v", out.Clean)
		}
	})
}

// A folder whose subtree was partly skipped was not read in full, so it cannot be
// vouched for — whatever the parts that WERE read had to say.
func TestAFolderHoldingSomethingIgnoredIsNotVouchedFor(t *testing.T) {
	snap, live := t.TempDir(), t.TempDir()
	tree := map[string]string{
		"kept/a.txt":                "x",
		"skipped/node_modules/b.js": "y",
		"skipped/c.txt":             "z",
	}
	writeTree(t, snap, tree)
	writeTree(t, live, tree)

	out := Explain(snap, live, Options{Ignore: NewIgnore([]string{"node_modules"})})
	if out.Differs || !out.Answered {
		t.Fatalf("%+v", out)
	}

	for _, d := range out.Clean {
		if d == filepath.Join(live, "skipped") {
			t.Error("a folder holding something the ignore list skipped was vouched for")
		}
	}
	// The one that was read in full still is.
	var sawKept bool
	for _, d := range out.Clean {
		if d == filepath.Join(live, "kept") {
			sawKept = true
		}
	}
	if !sawKept {
		t.Error("a folder read in full was not vouched for")
	}
}
