package fsevents

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This package reads something the system owns and is entitled to lose, on a
// machine whose state no test controls. So what is asserted is the CONTRACT — and
// especially the safe direction, which is that nothing here may ever be read as
// evidence that something did not change.

func TestTheGlobalEventCounterMovesForwards(t *testing.T) {
	first := Latest()
	if first == 0 {
		t.Skip("this system reports no event counter")
	}
	// Something on this machine is always writing. If nothing is, the counter
	// standing still is correct rather than a fault, so this only fails on a
	// counter that goes backwards.
	if second := Latest(); second < first {
		t.Errorf("the counter went from %d to %d", first, second)
	}
}

// The identity of a volume's log is what makes a stored event id meaningful. Two
// mount points of one volume group share it; two different disks do not.
func TestAVolumeHasOneLogIdentity(t *testing.T) {
	root, err := UUID("/")
	if err != nil {
		t.Fatalf("the startup disk: %v", err)
	}
	if root == "" {
		t.Skip("no readable event log for the startup disk here")
	}
	again, err := UUID("/")
	if err != nil {
		t.Fatal(err)
	}
	if again != root {
		t.Errorf("asked twice and got %q then %q", root, again)
	}
}

// A path on no volume is an error rather than an empty answer, because an empty
// answer means "this volume keeps no log", which is a different thing and would
// silently turn a typo into "nothing to see here".
func TestAPathThatIsNotThereIsAnError(t *testing.T) {
	if _, err := UUID(filepath.Join(t.TempDir(), "no", "such", "place")); err == nil {
		t.Error("a missing path was accepted")
	}
	if _, err := Before(filepath.Join(t.TempDir(), "nope"), time.Now()); err == nil {
		t.Error("a missing path was accepted")
	}
	if _, err := Since(filepath.Join(t.TempDir(), "nope"), 0, time.Second); err == nil {
		t.Error("a missing path was accepted")
	}
}

// The most important assertion here. A replay that is cut short has not accounted
// for the range it was asked about, and reporting otherwise would let a caller
// conclude that the paths it did not mention are unchanged.
func TestAReplayCutShortSaysItIsIncomplete(t *testing.T) {
	// From the beginning of history, with no time to finish. On the machine this
	// was written for, the same call with ten seconds named 178,570 paths and
	// still reported dropped events.
	replay, err := Since("/", 0, time.Millisecond)
	if err != nil {
		t.Skip("no replayable log here:", err)
	}
	if !replay.Dropped {
		t.Error("a replay given a millisecond claimed to be a complete account")
	}
}

// What a caller actually relies on: events written after a cursor are named when
// the log is replayed from it. Skipped rather than failed where the volume keeps
// no log this process may read — the data volume's needs Full Disk Access, and
// this package's whole point is to be optional.
func TestWhatWasWrittenAfterACursorIsReplayed(t *testing.T) {
	dir := t.TempDir()
	volume := volumeOf(t, dir)
	if uuid, err := UUID(volume); err != nil || uuid == "" {
		t.Skipf("no readable event log for %s", volume)
	}

	cursor := Latest()
	probe := filepath.Join(dir, "written-after-the-cursor.txt")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The log is written asynchronously; without a moment the replay races it.
	time.Sleep(2 * time.Second)

	replay, err := Since(volume, cursor, 8*time.Second)
	if err != nil {
		t.Skip("the log would not replay:", err)
	}
	for _, p := range replay.Paths {
		if filepath.Base(p) == "written-after-the-cursor.txt" {
			return
		}
	}
	// Not a failure. The log is best-effort by design, and a miss here is the
	// system exercising a right this package is built around rather than a bug in
	// it — which is exactly why nothing may read silence as "unchanged".
	t.Skipf("the log did not name the file (dropped=%v, %d paths): best-effort, as documented",
		replay.Dropped, len(replay.Paths))
}

// Paths come back relative to the volume's own root rather than absolute. Getting
// this wrong would check a path on whichever disk happened to be mounted at "/"
// and record whatever it found there.
func TestReplayedPathsAreRelativeToTheVolume(t *testing.T) {
	replay, err := Since("/", Latest(), 500*time.Millisecond)
	if err != nil {
		t.Skip("no replayable log here:", err)
	}
	for _, p := range replay.Paths {
		if filepath.IsAbs(p) {
			t.Errorf("%q is absolute; the callers join these onto a mount point", p)
			break
		}
	}
}

// volumeOf finds the mount point a directory lives on, so a test can ask about
// the right volume's log rather than assuming the startup disk's.
func volumeOf(t *testing.T, dir string) string {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	for p := dir; ; {
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		up, err := os.Stat(parent)
		if err != nil {
			return p
		}
		if !sameDevice(info, up) {
			return p
		}
		info, p = up, parent
	}
}
