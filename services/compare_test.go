package services

import (
	"os"
	"path/filepath"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/diffs"
	"snapshotter/internal/mountmgr"
)

// Compare walks a whole folder rather than one file: the snapshot on one side,
// the disk as it is now on the other. FileVersions answers "what changed in this
// file"; this answers "what changed in here", which is the question someone
// arrives with before they know which file to look at.

// compareFixture seeds a folder, mounts a fake snapshot over it, and returns the
// service and the live folder.
//
// The fake clones the seed and then alters the first two files by name, which is
// what produces one modified and one added file. Extra files named after those
// two are left alone on both sides, and so are the ones to use for anything that
// has to start out identical.
func compareFixture(t *testing.T, extra map[string]string) (*DiffService, string) {
	t.Helper()

	seed := t.TempDir()
	files := map[string]string{
		"notes.md":    "one\ntwo\nthree\n",
		"picture.bin": "\xff\xd8\x00\x01\x02",
	}
	for name, body := range extra {
		files[name] = body
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(seed, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	fake := mountmgr.NewFake(t.TempDir(), seed)
	svc := NewDiffService(Deps{
		Runner: browseRunner{}, Mounts: fake, Volume: apfs.DataVolume, Faking: true, FakeSeed: seed,
	})
	if err := fake.Mount(t.Context(), []string{browseSnapshot}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { _ = fake.Unmount(t.Context(), []string{browseSnapshot}) })
	return svc, seed
}

func folderStatuses(t *testing.T, svc *DiffService, live string, deep, includeSame bool) map[string]diffs.Status {
	t.Helper()

	res, err := svc.Compare(t.Context(), CompareRequest{
		Snapshot:    browseSnapshot,
		LivePath:    live,
		Deep:        deep,
		IncludeSame: includeSame,
	})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	got := map[string]diffs.Status{}
	for _, c := range res.Changes {
		got[c.RelPath] = c.Status
	}
	return got
}

func TestComparingAFolderReportsEachKindOfChangeInIt(t *testing.T) {
	svc, seed := compareFixture(t, nil)

	got := folderStatuses(t, svc, seed, false, false)

	// The snapshot is the past, so it is the older side. Putting the sides the
	// wrong way round produces a result that is exactly inverted and entirely
	// plausible, which is why each direction is named here rather than counted.
	for name, want := range map[string]diffs.Status{
		"notes.md":                            diffs.Modified,
		"deleted-since-2026-08-14-003200.txt": diffs.OnlyInSnapshot,
		"picture.bin":                         diffs.OnlyOnDisk,
	} {
		if got[name] != want {
			t.Errorf("%s came back as %q, want %q", name, got[name], want)
		}
	}
}

func TestUnchangedFilesAreLeftOutUnlessAskedFor(t *testing.T) {
	// Sorts after both the files the fake alters, so it is identical on both sides.
	const same = "steady.txt"
	svc, seed := compareFixture(t, map[string]string{same: "this has not been touched\n"})

	// A folder of thousands of untouched files is the ordinary case, and a listing
	// that includes them buries the three that matter.
	if got := folderStatuses(t, svc, seed, false, false); got[same] != "" {
		t.Errorf("an unchanged file was reported as %q", got[same])
	}
	if got := folderStatuses(t, svc, seed, false, true); got[same] != diffs.Same {
		t.Errorf("asked for unchanged files, it came back as %q", got[same])
	}
}

// The shallow comparison trusts size and timestamp, which is right for a first
// look and wrong for a file rewritten to the same length. Deep is the answer to
// that, and the difference between them is the whole reason the option exists.
func TestADeepComparisonNoticesAnEditThatKeptTheSameSizeAndTime(t *testing.T) {
	const name = "rewritten.txt"
	svc, seed := compareFixture(t, map[string]string{name: "one\ntwo\nthree\n"})
	live := filepath.Join(seed, name)

	was, err := os.Stat(live)
	if err != nil {
		t.Fatal(err)
	}
	// Same length, and the modification time put back afterwards: the file a
	// shallow comparison cannot see. It is what a program rewriting in place does,
	// and it is also what a rollback looks like.
	if err := os.WriteFile(live, []byte("one\ntwo\nthreE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(live, was.ModTime(), was.ModTime()); err != nil {
		t.Fatal(err)
	}

	if got := folderStatuses(t, svc, seed, false, false); got[name] != "" {
		t.Errorf("the shallow comparison claimed to see a change it cannot: %q", got[name])
	}
	if got := folderStatuses(t, svc, seed, true, false); got[name] != diffs.Modified {
		t.Errorf("the deep comparison missed it: %q", got[name])
	}
}

func TestComparingAFolderNoSnapshotCoversSaysSo(t *testing.T) {
	svc, _ := compareFixture(t, nil)

	_, err := svc.Compare(t.Context(), CompareRequest{
		Snapshot: browseSnapshot,
		LivePath: "/Volumes/SomeoneElses/work",
	})
	if err == nil {
		t.Fatal("comparing a folder outside the snapshot succeeded")
	}
}

func TestComparingAgainstASnapshotThatIsNotMountedSaysSo(t *testing.T) {
	svc, seed := compareFixture(t, nil)

	_, err := svc.Compare(t.Context(), CompareRequest{
		Snapshot: "com.apple.TimeMachine.2020-01-01-000000.local",
		LivePath: seed,
	})
	if err == nil {
		t.Fatal("comparing against an unmounted snapshot succeeded")
	}
}
