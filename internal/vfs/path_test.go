package vfs

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalRewritesDataVolumeAndSymlinkedRoots(t *testing.T) {
	cases := map[string]string{
		"/Users/someone/projects":                     "/Users/someone/projects",
		"/System/Volumes/Data/Users/someone/projects": "/Users/someone/projects",
		"/System/Volumes/Data":                        "/",
		"/var/log/system.log":                         "/private/var/log/system.log",
		"/tmp":                                        "/private/tmp",
		"/etc/hosts":                                  "/private/etc/hosts",
		"/Users/someone/../someone/x":                 "/Users/someone/x",
		"/usr/local/bin/task":                         "/usr/local/bin/task",
		"/Applications/Safari.app":                    "/Applications/Safari.app",
	}
	for in, want := range cases {
		got, err := dataVolume.Canonical(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%s: want %s, got %s", in, want, got)
		}
	}
}

// The project itself lives on an SD card, which no snapshot of the data volume
// contains. Saying so plainly is more useful than showing an empty directory.
func TestCanonicalRejectsPathsOnOtherVolumes(t *testing.T) {
	for _, in := range []string{
		"/Volumes/sdcard256gb/projects/snapshotter",
		"/System/Library/CoreServices",
		"/usr/bin/tmutil",
		"/bin/sh",
	} {
		_, err := dataVolume.Canonical(in)
		var notCovered *ErrNotCovered
		if !errors.As(err, &notCovered) {
			t.Errorf("%s: want ErrNotCovered, got %v", in, err)
		}
	}
}

func TestCanonicalRequiresAbsolutePaths(t *testing.T) {
	if _, err := dataVolume.Canonical("projects/snapshotter"); err == nil {
		t.Error("accepted a relative path")
	}
}

func TestToSnapshotJoinsUnderTheMountPoint(t *testing.T) {
	got, err := dataVolume.ToSnapshot("/mnt/2026-08-13-172036", "/Users/someone/tmp/config")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/mnt/2026-08-13-172036/Users/someone/tmp/config"; got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestToLiveIsTheInverse(t *testing.T) {
	mp := "/mnt/2026-08-13-172036"
	live := "/Users/someone/tmp/config"

	snap, err := dataVolume.ToSnapshot(mp, live)
	if err != nil {
		t.Fatal(err)
	}
	back, err := dataVolume.ToLive(mp, snap)
	if err != nil {
		t.Fatal(err)
	}
	if back != live {
		t.Errorf("round trip changed the path: %s -> %s -> %s", live, snap, back)
	}
}

func TestToLiveRejectsPathsOutsideTheMount(t *testing.T) {
	if _, err := dataVolume.ToLive("/mnt/2026-08-13-172036", "/Users/someone"); err == nil {
		t.Error("accepted a path outside the snapshot mount")
	}
}

// dataVolume is the startup disk, which is what every test above is about. Named
// rather than implied: this package used to translate every path as though there
// were only one volume, and the tests read as though that were a fact about
// paths rather than a choice of volume.
var dataVolume = Volume{}

// Translating against a volume that is not the startup disk.
//
// This package assumed there was only one. `tmutil localsnapshot` takes no
// arguments and writes to every mounted APFS volume at once, so a snapshot's
// contents are rooted wherever its volume is — and browsing an external disk's
// snapshot asked whether its files were under /Users, was told no, and reported
// "/Volumes/sdcard256gb is not on the data volume, so snapshots do not cover it"
// about the very disk on screen.

var sdcard = At("/Volumes/sdcard256gb")

func TestAnExternalVolumesOwnFilesAreCovered(t *testing.T) {
	for _, path := range []string{
		"/Volumes/sdcard256gb",
		"/Volumes/sdcard256gb/projects",
		"/Volumes/sdcard256gb/projects/snapshotter/README.md",
	} {
		if !sdcard.Covered(path) {
			t.Errorf("%s is on the volume being browsed and was reported as not covered", path)
		}
	}
}

func TestTheVolumeRootIsTheSnapshotRoot(t *testing.T) {
	for _, c := range []struct{ live, want string }{
		{"/Volumes/sdcard256gb", "/"},
		{"/Volumes/sdcard256gb/projects", "/projects"},
		{"/Volumes/sdcard256gb/projects/snapshotter", "/projects/snapshotter"},
	} {
		got, err := sdcard.Canonical(c.live)
		if err != nil {
			t.Errorf("%s: %v", c.live, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s canonicalised to %q, want %q", c.live, got, c.want)
		}
	}
}

// A path on one volume is not on another. Translating it anyway would read one
// disk's files out of another disk's snapshot.
func TestAPathOnAnotherVolumeIsNotCovered(t *testing.T) {
	for _, path := range []string{"/Users/someone/Documents", "/Volumes/other/thing", "/"} {
		if sdcard.Covered(path) {
			t.Errorf("%s is not on the SD card and was reported as covered", path)
		}
	}
	// And the reverse: the data volume does not cover what is on the SD card.
	if dataVolume.Covered("/Volumes/sdcard256gb/projects") {
		t.Error("the data volume claimed to cover a path on an external disk")
	}
}

// The message has to name the disk someone is looking at. "Not on the data
// volume" is true and unhelpful when the volume on screen is an SD card.
func TestTheRefusalNamesTheVolumeItIsAbout(t *testing.T) {
	_, err := sdcard.Canonical("/Users/someone/Documents")
	if err == nil {
		t.Fatal("a path on another volume was accepted")
	}
	if !strings.Contains(err.Error(), "/Volumes/sdcard256gb") {
		t.Errorf("the refusal does not name the volume: %v", err)
	}
	if strings.Contains(err.Error(), "data volume") {
		t.Errorf("the refusal talks about the data volume, which is not the one being browsed: %v", err)
	}
}

// Coming back out again has to land on the volume the file is actually on. It
// returned the snapshot-relative path, so a file at
// /Volumes/sdcard256gb/projects came back as /projects — a path on the startup
// disk, and a restore aimed at the wrong volume.
func TestComingBackOutLandsOnTheRightVolume(t *testing.T) {
	const mp = "/tmp/mounts/disk8s1/2026-08-27-182002"

	got, err := sdcard.ToLive(mp, mp+"/projects/snapshotter")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/Volumes/sdcard256gb/projects/snapshotter" {
		t.Errorf("came back as %q, which is on the startup disk", got)
	}

	root, err := sdcard.ToLive(mp, mp)
	if err != nil {
		t.Fatal(err)
	}
	if root != "/Volumes/sdcard256gb" {
		t.Errorf("the snapshot root came back as %q", root)
	}
}

// A round trip is the property that matters: what goes in comes back.
func TestATranslationRoundTripsOnEitherVolume(t *testing.T) {
	const mp = "/tmp/mounts/x"
	for _, c := range []struct {
		volume Volume
		live   string
	}{
		{dataVolume, "/Users/someone/Documents/notes.md"},
		{sdcard, "/Volumes/sdcard256gb/projects/notes.md"},
	} {
		inside, err := c.volume.ToSnapshot(mp, c.live)
		if err != nil {
			t.Errorf("%s: %v", c.live, err)
			continue
		}
		back, err := c.volume.ToLive(mp, inside)
		if err != nil {
			t.Errorf("%s: %v", c.live, err)
			continue
		}
		if back != c.live {
			t.Errorf("%s went in and %s came back", c.live, back)
		}
	}
}

// At is how a volume is named, and the data volume is a layout rather than a
// prefix — so naming it by its mount point must give the special case, not a
// prefix rule that would refuse every path on it.
func TestNamingTheDataVolumeGivesItsOwnLayout(t *testing.T) {
	for _, name := range []string{"", "/System/Volumes/Data"} {
		v := At(name)
		if v.Root != "" {
			t.Errorf("At(%q) produced a prefix volume rooted at %q", name, v.Root)
		}
		if !v.Covered("/Users/someone") {
			t.Errorf("At(%q) does not cover a home directory", name)
		}
	}
}
