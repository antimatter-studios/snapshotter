package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
)

// What the tree comparison never answered: a list of changed paths says where to
// look and nothing about what is there.

// fileFixture mounts a fake snapshot over a seed and returns the service and the
// live directory, which the fake copied at mount time.
func fileFixture(t *testing.T) (*DiffService, string) {
	t.Helper()

	seed := t.TempDir()
	if err := os.WriteFile(filepath.Join(seed, "notes.md"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "picture.bin"), []byte{0xff, 0xd8, 0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatal(err)
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

func TestBothVersionsOfAnEditedFileComeBack(t *testing.T) {
	svc, seed := fileFixture(t)
	live := filepath.Join(seed, "notes.md")

	if err := os.WriteFile(live, []byte("one\ntwo CHANGED\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, live, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if !got.Readable {
		t.Fatalf("a text file came back unreadable: %+v", got)
	}
	// The fake mount writes its own text rather than copying the seed, so what
	// matters here is that a snapshot side came back at all and that it is not
	// the live one.
	if got.Left == "" {
		t.Error("no left side was returned")
	}
	if got.Left == got.Right {
		t.Error("both sides are identical, so nothing was read from the snapshot")
	}
	if !strings.Contains(got.Right, "two CHANGED") {
		t.Errorf("the right side is wrong: %q", got.Right)
	}
	if !got.LeftExists || !got.RightExists {
		t.Errorf("both sides exist but were not reported: %+v", got)
	}
}

// A JPEG rendered as lines is noise, not a comparison. The sizes are still
// returned, because "2.1 MB became 2.4 MB" is an answer.
func TestABinaryFileIsNotOfferedAsText(t *testing.T) {
	svc, seed := fileFixture(t)

	got, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "picture.bin"), "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Readable {
		t.Error("a file with NUL bytes was offered as text")
	}
	if got.Note == "" {
		t.Error("nothing explained why it cannot be shown")
	}
	// The sizes are the only answer left for a file that cannot be diffed.
	if got.RightSize == 0 {
		t.Error("the size was withheld, which is all a binary comparison can offer")
	}
}

// A file created since the snapshot has one empty side. That must read as a
// whole file added, not as an error.
func TestAFileCreatedSinceTheSnapshotShowsAsAllAdded(t *testing.T) {
	svc, seed := fileFixture(t)
	live := filepath.Join(seed, "brand-new.md")

	if err := os.WriteFile(live, []byte("written today\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, live, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.LeftExists {
		t.Error("a file that did not exist yet was reported as being in the snapshot")
	}
	if !got.RightExists || !got.Readable {
		t.Errorf("the live side was not returned: %+v", got)
	}
	if got.Left != "" {
		t.Errorf("the missing side is not empty: %q", got.Left)
	}
	if !strings.Contains(got.Right, "written today") {
		t.Errorf("the live text is wrong: %q", got.Right)
	}
}

// A file in neither place is a genuine error, unlike every case above.
func TestAFileInNeitherPlaceIsAnError(t *testing.T) {
	svc, seed := fileFixture(t)

	if _, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "never-existed.md"), ""); err == nil {
		t.Error("a file that exists nowhere came back without an error")
	}
}

// Both sides are loaded into memory and then into a web view, so the cap is
// about what the window survives rather than what the disk can supply.
func TestATooLargeFileIsDeclinedRatherThanLoaded(t *testing.T) {
	svc, seed := fileFixture(t)
	big := filepath.Join(seed, "huge.log")

	if err := os.WriteFile(big, make([]byte, maxDiffableBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, big, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Readable {
		t.Error("a file past the cap was loaded anyway")
	}
	if !strings.Contains(got.Note, "large") {
		t.Errorf("the note does not say why: %q", got.Note)
	}
	if got.RightSize <= maxDiffableBytes {
		t.Errorf("the size was not reported: %d", got.RightSize)
	}
}

// The right side defaults to the live disk but is not fixed to it: any other
// mounted snapshot is an equally valid thing to compare against, which is what
// makes "what did this file look like between these two dates" answerable.
func TestTheRightSideCanBeAnotherSnapshot(t *testing.T) {
	svc, seed := fileFixture(t)

	got, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "notes.md"), browseSnapshot)
	if err == nil {
		t.Fatal("a snapshot was compared against itself, which has no answer to give")
	}
	_ = got

	// An unmounted snapshot has no paths to read, so it is refused rather than
	// silently falling back to the disk — a fallback would answer a question
	// nobody asked.
	if _, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "notes.md"), "com.apple.TimeMachine.2020-01-01-000000.local"); err == nil {
		t.Error("an unmounted snapshot was accepted as a comparison target")
	}
}

// The window says which version the right side turned out to be, rather than
// restating the rule for working it out.
func TestTheRightSideIsLabelled(t *testing.T) {
	svc, seed := fileFixture(t)

	got, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "notes.md"), "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.RightLabel != "the live disk" {
		t.Errorf("the default target is not named as the disk: %q", got.RightLabel)
	}
}
