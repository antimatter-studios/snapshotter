package vfs

import (
	"errors"
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
		got, err := Canonical(in)
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
		_, err := Canonical(in)
		var notCovered *ErrNotCovered
		if !errors.As(err, &notCovered) {
			t.Errorf("%s: want ErrNotCovered, got %v", in, err)
		}
	}
}

func TestCanonicalRequiresAbsolutePaths(t *testing.T) {
	if _, err := Canonical("projects/snapshotter"); err == nil {
		t.Error("accepted a relative path")
	}
}

func TestToSnapshotJoinsUnderTheMountPoint(t *testing.T) {
	got, err := ToSnapshot("/mnt/2026-08-13-172036", "/Users/someone/tmp/config")
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

	snap, err := ToSnapshot(mp, live)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ToLive(mp, snap)
	if err != nil {
		t.Fatal(err)
	}
	if back != live {
		t.Errorf("round trip changed the path: %s -> %s -> %s", live, snap, back)
	}
}

func TestToLiveRejectsPathsOutsideTheMount(t *testing.T) {
	if _, err := ToLive("/mnt/2026-08-13-172036", "/Users/someone"); err == nil {
		t.Error("accepted a path outside the snapshot mount")
	}
}
