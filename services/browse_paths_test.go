package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/verdict"
)

// Browsing is the screen someone reaches when they have already lost something,
// so the failures worth guarding against are the quiet ones: a listing that omits
// a file, or a snapshot reported as holding a file it does not.

// browseFixture builds a service over a fake mount holding a known tree.
func browseFixture(t *testing.T) (*BrowseService, string) {
	t.Helper()
	return browseFixtureWith(t, nil)
}

// browseFixtureWith is browseFixture with more files in the seed.
//
// They have to go in before the mount, because the fake copies the seed at that
// moment: anything written afterwards exists on the live side alone, which is a
// difference, and a test that wanted two identical trees gets a walk that answers
// "changed" at the first entry.
func browseFixtureWith(t *testing.T, extra map[string]string) (*BrowseService, string) {
	t.Helper()

	// A settings directory of its own. Folder verdicts read the change-detection
	// ignore list, and without this every one of these tests would be answering
	// against whatever the developer running them happens to ignore.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	seed := t.TempDir()
	files := map[string]string{
		"Documents/notes.md":       "notes\n",
		"Documents/deeper/one.txt": "one\n",
		"Pictures/photo.jpg":       "jpeg\n",
	}
	for rel, body := range extra {
		files[rel] = body
	}
	for rel, body := range files {
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

	if got, _ := svc.Home(""); got != seed {
		t.Errorf("faking: want the seed %q, got %q", seed, got)
	}

	real := NewBrowseService(Deps{Volume: apfs.DataVolume})
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, _ := real.Home(""); got != home {
		t.Errorf("not faking: want the home directory %q, got %q", home, got)
	}
}

func TestListingTheLiveDiskNamesWhatIsThere(t *testing.T) {
	svc, seed := browseFixture(t)

	got, err := svc.ListLive("", filepath.Join(seed, "Documents"))
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

	if _, err := svc.ListLive("", filepath.Join(seed, "no-such-directory")); err == nil {
		t.Error("listing a missing directory came back without an error")
	}
}

// The same path inside the snapshot: this is the translation that makes every
// other screen work, and getting it wrong shows one directory's contents under
// another's name.
func TestListingInsideASnapshotShowsThatSnapshotsContents(t *testing.T) {
	svc, seed := browseFixture(t)

	got, err := svc.ListSnapshot("", browseSnapshot, filepath.Join(seed, "Documents"))
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

	_, err := svc.ListSnapshot("", "com.apple.TimeMachine.2020-01-01-000000.local", filepath.Join(seed, "Documents"))
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

// The listing must come back at once, with folders resolved separately: a folder
// whose contents are unchanged costs a full walk to prove it, and a window that
// waits for several of those before drawing anything looks broken.
func TestTheListingDefersFolderVerdictsAndResolvesThemSeparately(t *testing.T) {
	svc, seed := browseFixture(t)

	listing, err := svc.Merged("", browseSnapshot, seed, true)
	if err != nil {
		t.Fatalf("merged: %v", err)
	}

	var sawFolder bool
	for _, row := range listing.Rows {
		if !row.IsDir {
			continue
		}
		sawFolder = true
		if row.Status != "notExamined" {
			t.Errorf("%s came back as %q; the listing should not have walked it",
				row.RelPath, row.Status)
		}
	}
	if !sawFolder {
		t.Skip("the fixture has no folders to defer")
	}

	// And asking directly answers it. The fake mount copies the seed, so nothing
	// has changed between the two sides.
	status, err := svc.DirectoryStatus("", browseSnapshot, seed+"/Documents")
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if status.Status != "same" {
		t.Errorf("an unchanged folder resolved to %q (%s)", status.Status, status.Why)
	}
}

// A folder whose contents differ must resolve to changed, which is the bug that
// started this: it used to say unchanged without looking.
func TestAChangedFolderResolvesToChanged(t *testing.T) {
	svc, seed := browseFixture(t)

	if err := os.WriteFile(seed+"/Documents/notes.md", []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := svc.DirectoryStatus("", browseSnapshot, seed+"/Documents")
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if status.Status != "modified" {
		t.Errorf("a folder with an edited file inside resolved to %q (%s)", status.Status, status.Why)
	}
}

// Browsing asks for the same folder's verdict over and over — open it, look
// inside, come back — and each answer costs a walk. The answer only stops being
// true when the live disk moves, which is what the cache is keyed on.
func TestAFolderVerdictIsRememberedUntilTheDiskMoves(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()
	folder := seed + "/Documents"

	first, err := svc.DirectoryStatus("", browseSnapshot, folder)
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if svc.Verdicts.Len() == 0 {
		t.Fatal("the answer was not remembered")
	}

	// Changed on disk, but nothing has told the cache — so the remembered answer
	// is deliberately still returned. This is what makes it a cache rather than a
	// second implementation of the walk.
	if err := os.WriteFile(folder+"/notes.md", []byte("edited, and quite a bit longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := svc.DirectoryStatus("", browseSnapshot, folder)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != first.Status {
		t.Errorf("the remembered answer was not used: %q then %q", first.Status, again.Status)
	}

	// Once told, it walks again and sees the change.
	svc.Verdicts.Touched(folder + "/notes.md")
	after, err := svc.DirectoryStatus("", browseSnapshot, folder)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "modified" {
		t.Errorf("after being told the disk moved, the folder is %q (%s)", after.Status, after.Why)
	}
}

// A nil cache means compute every time, which is what the command line does.
func TestNoCacheStillAnswers(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = nil

	if _, err := svc.DirectoryStatus("", browseSnapshot, seed+"/Documents"); err != nil {
		t.Errorf("without a cache: %v", err)
	}
}

// Where browsing starts, per volume.
//
// Every volume starts at its own root; the startup disk is the exception, and a
// deliberate one — the home directory is where a person's work is, and starting
// at "/" would put four system directories in front of it. An external disk has
// no home directory, so starting at one would open a path that does not exist
// inside that snapshot and read as an empty snapshot rather than a wrong path.
func TestBrowsingStartsAtTheHomeFolderOnTheStartupDisk(t *testing.T) {
	svc := &BrowseService{Deps: Deps{Runner: &volumeRunner{}, Volume: apfs.DataVolume}}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, _ := svc.Home(""); got != home {
		t.Errorf("the startup disk starts at %q, want the home folder %q", got, home)
	}
}

func TestBrowsingStartsAtTheVolumeRootAnywhereElse(t *testing.T) {
	svc := &BrowseService{Deps: Deps{Runner: &volumeRunner{}, Volume: apfs.DataVolume}}

	if got, _ := svc.Home("disk8s1"); got != "/Volumes/sdcard256gb" {
		t.Errorf("an external volume starts at %q, want its own root", got)
	}
	// And the data volume answers as the home folder even when named by device,
	// since that is the special case rather than a property of being first.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, _ := svc.Home("disk3s1"); got != home {
		t.Errorf("the data volume named by device starts at %q, want the home folder", got)
	}
}

// volumeRunner describes a machine with a startup disk and one external volume,
// each holding a snapshot.
type volumeRunner struct{}

func (volumeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	const snap = "com.apple.TimeMachine.2026-08-27-120000.local"
	block := func(dev string) string {
		return "Snapshots for " + dev + " (1 found)\n|\n+-- 00000000-0000-0000-0000-000000000000\n" +
			"|   Name:        " + snap + "\n|   XID:         1\n|   Purgeable:   Yes\n"
	}
	switch {
	case name == "mount":
		return "/dev/disk3s1 on " + apfs.DataVolume + " (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/sdcard256gb (apfs, local, journaled)\n", nil
	case name == "diskutil" && len(args) == 3 && args[1] == "listSnapshots":
		switch args[2] {
		case apfs.DataVolume:
			return block("disk3s1"), nil
		case "/Volumes/sdcard256gb":
			return block("disk8s1"), nil
		}
	}
	return "", errors.New("unexpected command")
}

// A volume that cannot be identified is not a third place to start.
//
// It used to answer the home directory, which is the startup disk's answer
// wearing a guess's clothes. On an external disk that guess is silently wrong: a
// home path does not exist inside that snapshot, so the listing comes back empty
// and reads as an empty snapshot rather than as a path that was never right.
func TestAVolumeThatCannotBeIdentifiedIsAnError(t *testing.T) {
	svc := &BrowseService{Deps: Deps{Runner: &volumeRunner{}, Volume: apfs.DataVolume}}

	got, err := svc.Home("disk99s9")
	if err == nil {
		t.Fatalf("an unknown volume was answered with %q", got)
	}
	if got != "" {
		t.Errorf("it refused and still named a place to start: %q", got)
	}
	// Named, because the whole use of the message is saying which disk could not
	// be found.
	if !strings.Contains(err.Error(), "disk99s9") {
		t.Errorf("the refusal does not name the volume: %v", err)
	}
}

// And a machine that cannot be interrogated is the same: not knowing is not the
// same as knowing it is the startup disk.
func TestAnUnreadableMachineDoesNotGuessAStartingPlace(t *testing.T) {
	svc := &BrowseService{Deps: Deps{Runner: muteRunner{}, Volume: apfs.DataVolume}}

	if got, err := svc.Home("disk8s1"); err == nil {
		t.Errorf("it guessed %q rather than saying the volume could not be resolved", got)
	}
	// The startup disk still answers, because it needs nothing looked up.
	home, herr := os.UserHomeDir()
	if herr != nil {
		t.Skip("no home directory")
	}
	if got, err := svc.Home(""); err != nil || got != home {
		t.Errorf("the startup disk answered %q (%v), want the home folder", got, err)
	}
}

type muteRunner struct{}

func (muteRunner) Run(context.Context, string, ...string) (string, error) {
	return "", errors.New("nothing here answers")
}
