package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
)

// Browsing is the screen someone reaches when they have already lost something,
// so the failures worth guarding against are the quiet ones: a listing that omits
// a file, or a snapshot reported as holding a file it does not.

// browseFixture builds a service over a fake mount holding a known tree.
func browseFixture(t *testing.T) (*BrowseService, string) {
	t.Helper()

	seed := t.TempDir()
	for rel, body := range map[string]string{
		"Documents/notes.md":       "notes\n",
		"Documents/deeper/one.txt": "one\n",
		"Pictures/photo.jpg":       "jpeg\n",
	} {
		p := filepath.Join(seed, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	fake := mountmgr.NewFake(t.TempDir(), seed)
	if err := fake.Mount(context.Background(), []string{browseSnapshot}); err != nil {
		t.Fatalf("mounting the fake: %v", err)
	}
	t.Cleanup(func() { _ = fake.Unmount(context.Background(), []string{browseSnapshot}) })

	return NewBrowseService(Deps{
		Runner:   browseRunner{},
		Mounts:   fake,
		Volume:   apfs.DataVolume,
		Faking:   true,
		FakeSeed: seed,
	}), seed
}

const browseSnapshot = "com.apple.TimeMachine.2026-08-14-003200.local"

type browseRunner struct{}

func (browseRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "tmutil" && len(args) > 0 && args[0] == "listlocalsnapshots" {
		return "Snapshots for disk /:\n" + browseSnapshot + "\n", nil
	}
	return "", nil
}

// Browsing starts in the home folder, because that is where the things worth
// recovering live. Under a fake mount it starts in the seed instead, or every
// path would point outside the tree that was mounted.
func TestBrowsingStartsSomewhereUseful(t *testing.T) {
	svc, seed := browseFixture(t)

	if got := svc.Home(); got != seed {
		t.Errorf("faking: want the seed %q, got %q", seed, got)
	}

	real := NewBrowseService(Deps{Volume: apfs.DataVolume})
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := real.Home(); got != home {
		t.Errorf("not faking: want the home directory %q, got %q", home, got)
	}
}

func TestListingTheLiveDiskNamesWhatIsThere(t *testing.T) {
	svc, seed := browseFixture(t)

	got, err := svc.ListLive(filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	names := map[string]bool{}
	for _, e := range got.Entries {
		names[e.Name] = true
	}
	for _, want := range []string{"notes.md", "deeper"} {
		if !names[want] {
			t.Errorf("%s missing from the listing: %+v", want, names)
		}
	}
	if got.Parent == "" {
		t.Error("no parent, so there is no way back up the tree")
	}
}

// A listing has to fail rather than come back empty. An empty listing for a
// directory that does not exist reads as "nothing was lost from here", which is
// the opposite of the truth.
func TestListingSomethingThatIsNotThereIsAnError(t *testing.T) {
	svc, seed := browseFixture(t)

	if _, err := svc.ListLive(filepath.Join(seed, "no-such-directory")); err == nil {
		t.Error("listing a missing directory came back without an error")
	}
}

// The same path inside the snapshot: this is the translation that makes every
// other screen work, and getting it wrong shows one directory's contents under
// another's name.
func TestListingInsideASnapshotShowsThatSnapshotsContents(t *testing.T) {
	svc, seed := browseFixture(t)

	got, err := svc.ListSnapshot(browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("listing the snapshot: %v", err)
	}
	names := map[string]bool{}
	for _, e := range got.Entries {
		names[e.Name] = true
	}
	if !names["notes.md"] {
		t.Errorf("the snapshot listing is missing a file that is in it: %+v", names)
	}
	// The live path is what the interface shows and what a restore uses; the
	// mounted location must not leak into it.
	if strings.Contains(got.LivePath, "mounts") {
		t.Errorf("the mount point leaked into the displayed path: %q", got.LivePath)
	}
}

func TestListingAnUnmountedSnapshotSaysSoRatherThanShowingNothing(t *testing.T) {
	svc, seed := browseFixture(t)

	_, err := svc.ListSnapshot("com.apple.TimeMachine.2020-01-01-000000.local", filepath.Join(seed, "Documents"))
	if err == nil {
		t.Error("listing a snapshot that is not open came back without an error")
	}
}

// Locate answers "which snapshots still have this file", which is the question
// someone asks after deleting one. A false yes sends them to a snapshot that
// cannot help; a false no loses a file that was recoverable.
func TestLocateReportsPresencePerSnapshot(t *testing.T) {
	svc, seed := browseFixture(t)

	present, err := svc.Locate(context.Background(), filepath.Join(seed, "Documents", "notes.md"))
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if len(present) == 0 {
		t.Fatal("locate reported no snapshots at all")
	}
	found := false
	for _, p := range present {
		if p.Snapshot == browseSnapshot {
			found = true
			if !p.Present {
				t.Error("a file that is in the mounted snapshot was reported absent")
			}
		}
	}
	if !found {
		t.Errorf("the mounted snapshot was not among those checked: %+v", present)
	}

	absent, err := svc.Locate(context.Background(), filepath.Join(seed, "Documents", "never-existed.md"))
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	for _, p := range absent {
		if p.Present {
			t.Errorf("%s claims to hold a file that never existed", p.Snapshot)
		}
	}
}
