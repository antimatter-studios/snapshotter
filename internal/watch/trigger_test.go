package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var start = time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)

// feed reports n deletions spaced apart, all under one watched directory,
// returning how many fired.
func feed(t *Trigger, from time.Time, n int, gap time.Duration) (fired int, at time.Time) {
	return feedUnder(t, "/tmp/x", from, n, gap)
}

// feedUnder is feed with the watched directory named, for the tests that care
// which counter the deletions land in.
func feedUnder(t *Trigger, root string, from time.Time, n int, gap time.Duration) (fired int, at time.Time) {
	at = from
	for i := 0; i < n; i++ {
		if firstOf(t.Deletion(at, root, root+"/f")) {
			fired++
		}
		at = at.Add(gap)
	}
	return fired, at
}

func TestBelowTheThresholdNothingFires(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)
	if fired, _ := feed(tr, start, 9, time.Millisecond); fired != 0 {
		t.Fatalf("fired %d times below the threshold", fired)
	}
}

func TestABurstFires(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)
	fired, _ := feed(tr, start, 10, time.Millisecond)
	if fired != 1 {
		t.Fatalf("fired %d times, want exactly 1", fired)
	}
}

// Ordinary work deletes steadily. Spread widely enough, it must never trip the
// wire however long it runs, or the feature becomes a snapshot generator.
func TestASlowTrickleNeverFires(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)
	// One deletion every 200ms: five per window, half the threshold, forever.
	if fired, _ := feed(tr, start, 500, 200*time.Millisecond); fired != 0 {
		t.Fatalf("a steady trickle fired %d times", fired)
	}
}

// The cooldown is what stops one long deletion filling the disk with snapshots
// of a disk that is being emptied.
func TestTheCooldownHoldsOffASecondSnapshot(t *testing.T) {
	tr := NewTrigger(10, time.Second, 10*time.Minute)

	fired, at := feed(tr, start, 10, time.Millisecond)
	if fired != 1 {
		t.Fatalf("first burst fired %d times, want 1", fired)
	}
	// A second burst immediately afterwards is inside the cooldown.
	if fired, at = feed(tr, at, 10, time.Millisecond); fired != 0 {
		t.Fatalf("a burst inside the cooldown fired %d times", fired)
	}
	// Past the cooldown it should fire again.
	if fired, _ = feed(tr, at.Add(11*time.Minute), 10, time.Millisecond); fired != 1 {
		t.Fatalf("a burst after the cooldown fired %d times, want 1", fired)
	}
}

// Old deletions must leave the window, or a long enough run of anything
// eventually reaches the threshold by accumulation.
func TestTheWindowForgets(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)

	feed(tr, start, 9, time.Millisecond)
	if n := tr.Pending(start.Add(9*time.Millisecond), "/tmp/x"); n != 9 {
		t.Fatalf("pending = %d, want 9", n)
	}
	if n := tr.Pending(start.Add(2*time.Second), "/tmp/x"); n != 0 {
		t.Fatalf("pending after the window = %d, want 0", n)
	}
	// Those nine are stale, so nine more must not add up to eighteen.
	if fired, _ := feed(tr, start.Add(2*time.Second), 9, time.Millisecond); fired != 0 {
		t.Fatal("stale deletions counted toward a new burst")
	}
}

func TestZeroFieldsFallBackToTheDefaults(t *testing.T) {
	tr := NewTrigger(0, 0, 0)
	if tr.Threshold != DefaultThreshold || tr.Window != DefaultWindow || tr.Cooldown != DefaultCooldown {
		t.Fatalf("got %d/%v/%v, want the defaults", tr.Threshold, tr.Window, tr.Cooldown)
	}
}

// firstOf keeps the tests that only care whether the wire tripped readable now
// that Deletion also reports where the burst was. The tests that DO care about
// the location call Deletion directly.
func firstOf(tripped bool, _ []string) bool { return tripped }

// Where a burst is happening is the difference between "something is deleting
// files" — which tells someone only to worry — and "files are being deleted from
// ~/Documents/Invoices", which tells them whether they did it on purpose.

func TestATrippedBurstNamesWhereItHappened(t *testing.T) {
	tr := NewTrigger(3, time.Minute, 0)
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)

	const root = "/Users/someone/Documents"
	tr.Deletion(at, root, root+"/Invoices/jan.pdf")
	tr.Deletion(at.Add(time.Second), root, root+"/Invoices/feb.pdf")
	tripped, where := tr.Deletion(at.Add(2*time.Second), root, root+"/Invoices/mar.pdf")

	if !tripped {
		t.Fatal("three deletions inside the window did not trip a threshold of three")
	}
	if len(where) != 1 || where[0] != "/Users/someone/Documents/Invoices" {
		t.Errorf("want the one directory, got %v", where)
	}
}

// The directory, not the file: the file is gone and its name helps nobody.
func TestItReportsTheDirectoryRatherThanTheFile(t *testing.T) {
	tr := NewTrigger(1, time.Minute, 0)
	_, where := tr.Deletion(time.Now(), "/Users/someone", "/Users/someone/Pictures/holiday.jpg")

	if len(where) != 1 || where[0] != "/Users/someone/Pictures" {
		t.Errorf("want the directory, got %v", where)
	}
}

// A burst spread over several folders should name the busiest first, because
// that is the one most likely to be the answer.
func TestSeveralPlacesAreOrderedByHowMuchWentFromEach(t *testing.T) {
	tr := NewTrigger(6, time.Minute, 0)
	at := time.Now()

	// File paths, not directories: Deletion is given what vanished and keeps the
	// folder it was in.
	for i := 0; i < 3; i++ {
		tr.Deletion(at, "/a", fmt.Sprintf("/a/busiest/file%d", i))
	}
	tr.Deletion(at, "/a", "/a/quietest/file")
	tr.Deletion(at, "/a", "/a/middle/one")
	tripped, where := tr.Deletion(at, "/a", "/a/middle/two")

	if !tripped {
		t.Fatal("did not trip")
	}
	if len(where) < 2 || where[0] != "/a/busiest" || where[1] != "/a/middle" {
		t.Errorf("want busiest then middle, got %v", where)
	}
}

// A notification is not a place for a wall of paths: past a handful the list
// stops describing anything and gets dismissed unread.
func TestOnlyAHandfulOfPlacesAreNamed(t *testing.T) {
	tr := NewTrigger(8, time.Minute, 0)
	at := time.Now()

	var where []string
	for i := 0; i < 8; i++ {
		_, where = tr.Deletion(at, "/a", fmt.Sprintf("/a/dir%d/file", i))
	}
	if len(where) > maxPlacesReported {
		t.Errorf("named %d places, want at most %d: %v", len(where), maxPlacesReported, where)
	}
}

// Nothing is reported until the wire actually trips — the burst is still
// building, and a location for a burst that never happened is a false alarm.
func TestNoPlacesAreReportedBeforeItTrips(t *testing.T) {
	tr := NewTrigger(5, time.Minute, 0)
	tripped, where := tr.Deletion(time.Now(), "/a", "/a/b/c")

	if tripped {
		t.Fatal("one deletion tripped a threshold of five")
	}
	if where != nil {
		t.Errorf("a location was reported for a burst that has not tripped: %v", where)
	}
}

// Only what is inside the window counts, for the location as much as the count:
// a folder emptied an hour ago is not where this burst is happening.
func TestPlacesIgnoreDeletionsThatHaveAgedOut(t *testing.T) {
	tr := NewTrigger(2, time.Minute, 0)
	at := time.Now()

	tr.Deletion(at.Add(-time.Hour), "/watched", "/watched/old/place/file")
	tr.Deletion(at, "/watched", "/watched/new/place/one")
	tripped, where := tr.Deletion(at, "/watched", "/watched/new/place/two")

	if !tripped {
		t.Fatal("did not trip")
	}
	for _, dir := range where {
		if dir == "/watched/old/place" {
			t.Errorf("named a folder whose deletions aged out of the window: %v", where)
		}
	}
}

// Places is what a person reads, so home is written the way people write it.
func TestPlacesWordsHomeTheWayPeopleDo(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	got := Places([]string{filepath.Join(home, "Documents", "Invoices")})
	if got != "~/Documents/Invoices" {
		t.Errorf("got %q, want ~/Documents/Invoices", got)
	}

	// Outside home there is nothing to shorten, and shortening the wrong thing
	// would point somewhere that does not exist.
	if got := Places([]string{"/Volumes/work/build"}); got != "/Volumes/work/build" {
		t.Errorf("got %q", got)
	}

	// Never empty: this goes into a notification title, and "Files are being
	// deleted from " with nothing after it is worse than saying it is unknown.
	if Places(nil) == "" {
		t.Error("no wording for an unknown location")
	}
}

// Counting per watched directory is the whole reason directories are named.
//
// A single global count meant the threshold got EASIER to reach the more
// directories were watched, so adding one made the tripwire noisier rather than
// more useful — and the noise was two unrelated folders' ordinary afternoons
// being added together.

func TestDeletionsInTwoWatchedDirectoriesDoNotAddUpToABurst(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)

	// Nine in one and nine in the other: eighteen deletions, no burst.
	if fired, _ := feedUnder(tr, "/Users/someone/projects", start, 9, time.Millisecond); fired != 0 {
		t.Fatalf("nine deletions in one directory fired %d times", fired)
	}
	if fired, _ := feedUnder(tr, "/Users/someone/Documents", start, 9, time.Millisecond); fired != 0 {
		t.Fatalf("nine deletions in a second directory fired %d times — the counts were added together", fired)
	}
}

func TestABurstInOneWatchedDirectoryStillFires(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)

	feedUnder(tr, "/Users/someone/Documents", start, 9, time.Millisecond)
	if fired, _ := feedUnder(tr, "/Users/someone/projects", start, 10, time.Millisecond); fired != 1 {
		t.Fatalf("ten deletions in one watched directory fired %d times, want 1", fired)
	}
}

// The cooldown is shared, because an APFS snapshot is of the whole volume: the
// one taken for ~/projects already holds every file in ~/Documents too, and a
// second one a moment later costs disk and captures nothing new. Disk cost is
// what this whole rework is about.
func TestTheCooldownIsSharedAcrossWatchedDirectories(t *testing.T) {
	tr := NewTrigger(10, time.Second, 10*time.Minute)

	fired, at := feedUnder(tr, "/Users/someone/projects", start, 10, time.Millisecond)
	if fired != 1 {
		t.Fatalf("the first burst fired %d times, want 1", fired)
	}
	if fired, _ = feedUnder(tr, "/Users/someone/Documents", at, 10, time.Millisecond); fired != 0 {
		t.Errorf("a burst elsewhere inside the cooldown fired %d times, so one deletion could take two snapshots of the same volume", fired)
	}
}

// A trip clears every directory's part-built burst, not only the one that
// tripped. What the snapshot captured includes theirs, so letting a half-built
// count survive would let the next deletion trip on work already saved.
func TestATripClearsEveryDirectorysCount(t *testing.T) {
	tr := NewTrigger(10, time.Second, 0)

	feedUnder(tr, "/Users/someone/Documents", start, 9, time.Millisecond)
	if fired, _ := feedUnder(tr, "/Users/someone/projects", start, 10, time.Millisecond); fired != 1 {
		t.Fatal("the burst did not fire")
	}
	// Documents had nine banked. With them cleared, one more must not make ten.
	if fired, _ := feedUnder(tr, "/Users/someone/Documents", start.Add(20*time.Millisecond), 1, time.Millisecond); fired != 0 {
		t.Error("a directory's count survived a snapshot that already covered it")
	}
}

// Pending is per directory too, or an interface showing a burst building would
// show one directory's activity against another's name.
func TestPendingIsCountedPerWatchedDirectory(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)

	feedUnder(tr, "/a", start, 4, time.Millisecond)
	feedUnder(tr, "/b", start, 2, time.Millisecond)

	if n := tr.Pending(start.Add(5*time.Millisecond), "/a"); n != 4 {
		t.Errorf("pending under /a = %d, want 4", n)
	}
	if n := tr.Pending(start.Add(5*time.Millisecond), "/b"); n != 2 {
		t.Errorf("pending under /b = %d, want 2", n)
	}
	if n := tr.Pending(start.Add(5*time.Millisecond), "/never-watched"); n != 0 {
		t.Errorf("pending under a directory with no deletions = %d, want 0", n)
	}
}
