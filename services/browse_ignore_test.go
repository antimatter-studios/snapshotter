package services

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"snapshotter/internal/changedb"
	"snapshotter/internal/config"
	"snapshotter/internal/diffs"
	"snapshotter/internal/verdict"
)

// ignoring writes a change-detection ignore list into the settings the fixture
// reads, so a test can say what is skipped in the terms somebody would type.
func ignoring(t *testing.T, patterns ...string) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChangeDetection.Ignore = patterns
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// The whole point of the list, and the only part of it that is free: a folder
// that is itself ignored is answered without reading anything.
func TestAnIgnoredFolderIsAnsweredWithoutBeingRead(t *testing.T) {
	svc, seed := browseFixture(t)
	ignoring(t, "Documents")

	got, err := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "ignored" {
		t.Errorf("status %q, want ignored", got.Status)
	}
	// It has to say why, or the row is a state with no explanation and no way
	// back to the setting that caused it.
	if got.Why == "" {
		t.Error("nothing said why the folder was not looked inside")
	}
}

// "Could not check" is a failure to read and reads as a fault. This is not one:
// somebody said they did not want to be told.
func TestIgnoringIsNotReportedAsAFailureToCheck(t *testing.T) {
	svc, seed := browseFixture(t)
	ignoring(t, "Documents")

	got, _ := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if got.Status == "notExamined" {
		t.Error("an ignored folder was reported as one that could not be checked")
	}
}

// A change inside an ignored folder does not reach the folder above it either,
// which is the behaviour somebody is asking for when they ignore node_modules.
func TestAChangeInsideAnIgnoredFolderDoesNotChangeItsParent(t *testing.T) {
	svc, seed := browseFixture(t)
	ignoring(t, "deeper")

	edited := filepath.Join(seed, "Documents", "deeper", "one.txt")
	if err := os.WriteFile(edited, []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "same" {
		t.Errorf("status %q, want same: the change is inside an ignored folder", got.Status)
	}
	// But it must not claim a clean bill of health silently. The word on the badge
	// is what was asked for; the row still has to say what it skipped.
	if got.Why == "" {
		t.Error("reported unchanged without saying that something was skipped")
	}
}

// And the other half, which matters more: ignoring one folder must not make the
// application quiet about the one beside it.
func TestAChangeOutsideAnIgnoredFolderIsStillFound(t *testing.T) {
	svc, seed := browseFixture(t)
	ignoring(t, "deeper")

	edited := filepath.Join(seed, "Documents", "notes.md")
	if err := os.WriteFile(edited, []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "modified" {
		t.Errorf("status %q, want modified", got.Status)
	}
}

// With nothing ignored, a folder that matches nothing says nothing extra. A
// tooltip about skipping on every unchanged folder would be noise that teaches
// people to stop reading it.
func TestAnUnchangedFolderSaysNothingExtraWhenNothingIsIgnored(t *testing.T) {
	svc, seed := browseFixture(t)

	got, err := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "same" {
		t.Fatalf("status %q, want same", got.Status)
	}
	if got.Why != "" {
		t.Errorf("Why=%q, want empty", got.Why)
	}
}

// Editing the list changes what "unchanged" means, so verdicts reached under the
// old list are not answers to the new question. Without this, ignoring
// node_modules would appear to do nothing until the application was restarted.
func TestEditingTheListDiscardsVerdictsReachedUnderTheOldOne(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()
	folder := filepath.Join(seed, "Documents")

	// Answered and remembered while nothing is ignored.
	if _, err := svc.DirectoryStatus("", browseSnapshot, folder); err != nil {
		t.Fatal(err)
	}
	if svc.Verdicts.Len() == 0 {
		t.Fatal("the first answer was not remembered, so this test proves nothing")
	}

	// Now something inside it is ignored. Nothing on disk moved, so the cache has
	// not been told anything — the settings changing is what has to be noticed.
	ignoring(t, "deeper")
	if _, err := svc.DirectoryStatus("", browseSnapshot, folder); err != nil {
		t.Fatal(err)
	}
	got, err := svc.DirectoryStatus("", browseSnapshot, folder)
	if err != nil {
		t.Fatal(err)
	}
	if got.Why == "" {
		t.Error("the stale answer was reused: it still reports unchanged with nothing skipped")
	}
}

// A settings file that will not parse leaves change detection reading
// everything. That is the safe direction: the cost of ignoring nothing is time,
// and the cost of ignoring everything is telling somebody their work is safe
// without having looked at it.
func TestABrokenSettingsFileIgnoresNothing(t *testing.T) {
	svc, seed := browseFixture(t)

	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("change_detection: [this is not\n  valid: yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	edited := filepath.Join(seed, "Documents", "deeper", "one.txt")
	if err := os.WriteFile(edited, []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "modified" {
		t.Errorf("status %q, want modified: a broken settings file must not hide a change", got.Status)
	}
}

// Navigating away has to stop the walks, not just discard their answers. Proving
// a folder unchanged reads everything under it, so three walks for a folder
// nobody is looking at keep a slow disk busy while the folder somebody IS looking
// at waits behind them — measured at five to ten seconds on an SD card, which
// reads as the application having locked up.

// bigIdenticalTree is a seed large enough that a walk of it cannot finish before
// the abandon check is reached. Identical on both sides by construction: the fake
// mount copies the seed, so a walk of it has to go the whole distance to answer,
// which is the only case where being abandoned matters.
func bigIdenticalTree() map[string]string {
	files := map[string]string{}
	for _, dir := range []string{"a", "b", "c", "d", "e", "f"} {
		for i := 0; i < 400; i++ {
			files["big/"+dir+"/"+strconv.Itoa(i)+".txt"] = "x"
		}
	}
	return files
}

func TestAbandoningStopsAWalkInFlight(t *testing.T) {
	svc, seed := browseFixtureWith(t, bigIdenticalTree())
	big := filepath.Join(seed, "big")

	// Left alone, this answers: the two sides are identical.
	if got, err := svc.DirectoryStatus("", browseSnapshot, big); err != nil || got.Status != "same" {
		t.Fatalf("unabandoned: %q %v — this test proves nothing unless the walk answers", got.Status, err)
	}

	// Now abandoned throughout, which is what the window does when somebody clicks
	// into another folder. Every check sees a number it does not recognise.
	svc.Verdicts = verdict.New()
	stop := make(chan struct{})
	moving := make(chan struct{})
	go func() {
		defer close(moving)
		for {
			select {
			case <-stop:
				return
			default:
			}
			svc.AbandonFolderChecks()
			runtime.Gosched()
		}
	}()

	got, err := svc.DirectoryStatus("", browseSnapshot, big)
	close(stop)
	<-moving
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}

	// It must not be a claim about the folder. An abandoned walk reporting "same"
	// would be this application saying somebody's work was safe without having
	// looked at all of it.
	if got.Status != "notExamined" {
		t.Errorf("an abandoned walk reported %q", got.Status)
	}

	// And it must not be remembered. A cached answer would outlive the navigation
	// that caused it, and make a folder look permanently unreadable.
	if svc.Verdicts.Len() != 0 {
		t.Errorf("an abandoned walk left %d verdicts cached", svc.Verdicts.Len())
	}
}

// The counter only ever rises, so a walk can tell "still wanted" from "the window
// has moved on" by comparing it with the number it started under.
func TestAbandoningMovesTheGenerationOn(t *testing.T) {
	svc, _ := browseFixture(t)

	before := svc.checks.Load()
	svc.AbandonFolderChecks()
	if after := svc.checks.Load(); after <= before {
		t.Errorf("generation went from %d to %d", before, after)
	}
}

// The recorded change: a "changed" verdict rests on one file, so re-checking that file is
// enough to reach the same verdict again without walking anything — and the same
// check answers for every folder between it and the file.

func TestAKnownDifferenceAnswersWithoutWalking(t *testing.T) {
	svc, seed := browseFixtureWith(t, bigIdenticalTree())
	svc.Verdicts = verdict.New()
	big := filepath.Join(seed, "big")

	// One difference, buried where only a full walk would find it.
	buried := filepath.Join(big, "f", "399.txt")
	if err := os.WriteFile(buried, []byte("edited, and rather longer"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The expensive answer, once. It records where it stopped.
	first, err := svc.DirectoryStatus("", browseSnapshot, big)
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if first.Status != "modified" {
		t.Fatalf("status %q, want modified", first.Status)
	}
	if svc.Verdicts.ChangedPaths(browseSnapshot) != 1 {
		t.Fatalf("recorded %d recorded changes", svc.Verdicts.ChangedPaths(browseSnapshot))
	}

	// A folder between the question and the file, which nothing has been asked
	// about before. It is answered by the recorded change alone.
	between := filepath.Join(big, "f")
	got, err := svc.DirectoryStatus("", browseSnapshot, between)
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "modified" {
		t.Errorf("status %q for the folder holding the difference, want modified", got.Status)
	}
}

// The one that proves the walk did not happen.
//
// Every other test here would pass with the recorded change lookup deleted, because the
// walk finds the same difference by reading the tree — which is exactly the cost
// being avoided, and invisible from outside. So the snapshot's directory is made
// traversable but not listable: opening it to read its entries fails, while
// looking at one known path inside it still works. A verdict of "changed" can
// then only have come from the recorded change.
func TestARecordedChangeAnswersAFolderThatCannotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read the directory anyway")
	}
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	documents := filepath.Join(seed, "Documents")
	deeper := filepath.Join(documents, "deeper")
	if err := os.WriteFile(filepath.Join(deeper, "one.txt"), []byte("edited, and longer"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The deep folder is answered the expensive way, and records where it stopped.
	if got, err := svc.DirectoryStatus("", browseSnapshot, deeper); err != nil || got.Status != "modified" {
		t.Fatalf("status %q %v", got.Status, err)
	}

	// Now make the snapshot's copy of Documents impossible to list. 0111 is
	// execute without read: a path inside it can still be reached by name, and
	// asking for its contents cannot.
	mountPoint, err := svc.mountPointOf("", browseSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotDocuments, err := svc.volumeFor(context.Background(), "").ToSnapshot(mountPoint, documents)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(snapshotDocuments, 0o111); err != nil {
		t.Skip("cannot remove read permission here")
	}
	t.Cleanup(func() { _ = os.Chmod(snapshotDocuments, 0o700) })

	// Confirm the walk really cannot answer, or this proves nothing.
	if _, ok := diffs.DiffersWithin(snapshotDocuments, documents, diffs.Options{}); ok {
		t.Skip("the directory is still readable here")
	}

	// Documents has never been asked about, so there is no verdict to reuse. The
	// only thing that can answer is the recorded change recorded below it.
	got, err := svc.DirectoryStatus("", browseSnapshot, documents)
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "modified" {
		t.Errorf("status %q — the folder was not answered by its recorded change", got.Status)
	}
}

// A recorded change answers upward as far as it goes, which is the whole multiplier: one
// stat settles a chain of folders rather than one.
func TestOneRecordedChangeAnswersEveryFolderAboveIt(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	edited := filepath.Join(seed, "Documents", "deeper", "one.txt")
	if err := os.WriteFile(edited, []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Asked about the deepest folder first, so only that one is in the cache.
	if _, err := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents", "deeper")); err != nil {
		t.Fatal(err)
	}

	// Its parent has never been asked about. The recorded change recorded below answers
	// it, because one difference anywhere means the tree differs.
	got, err := svc.DirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "modified" {
		t.Errorf("status %q for the folder above the difference, want modified", got.Status)
	}
}

// And the other half: a file put back proves nothing, so the walk has to happen
// again — once — and settle on whatever differs now.
func TestARestoredFileSendsItBackToTheWalk(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	documents := filepath.Join(seed, "Documents")
	notes := filepath.Join(documents, "notes.md")
	original, err := os.ReadFile(notes)
	if err != nil {
		t.Fatal(err)
	}
	// The stamp as well as the contents. The comparison is size and modification
	// time, so writing the same bytes back leaves the file still differing — which
	// is correct, and not what "put back" is supposed to mean here. A real restore
	// preserves the time, so this one does too.
	before, err := os.Stat(notes)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(notes, []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.DirectoryStatus("", browseSnapshot, documents); err != nil || got.Status != "modified" {
		t.Fatalf("status %q %v", got.Status, err)
	}
	if svc.Verdicts.ChangedPaths(browseSnapshot) != 1 {
		t.Fatalf("recorded %d recorded changes", svc.Verdicts.ChangedPaths(browseSnapshot))
	}

	// Put back exactly as it was, and the cached verdict cleared the way the
	// filesystem watcher would clear it.
	if err := os.WriteFile(notes, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(notes, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	svc.Verdicts.Touched(notes)

	// The recorded change is gone with it, and the folder is answered by walking again.
	got, err := svc.DirectoryStatus("", browseSnapshot, documents)
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "same" {
		t.Errorf("status %q after the file was put back, want same", got.Status)
	}
	if svc.Verdicts.ChangedPaths(browseSnapshot) != 0 {
		t.Errorf("a recorded change survived the difference being undone")
	}
}

// A recorded change inside a folder somebody has told this application not to read is not
// usable, whatever it would have said. This path skips the walk entirely, so the
// ignore list has to be honoured here as well as inside it.
func TestARecordedChangeInsideAnIgnoredFolderIsNotUsed(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	documents := filepath.Join(seed, "Documents")
	edited := filepath.Join(documents, "deeper", "one.txt")
	if err := os.WriteFile(edited, []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.DirectoryStatus("", browseSnapshot, documents); got.Status != "modified" {
		t.Fatalf("status %q, want modified before anything is ignored", got.Status)
	}

	// Now the folder holding the difference is ignored. The verdict cache is
	// cleared by the rules changing; the recorded change must not survive it either.
	ignoring(t, "deeper")
	got, err := svc.DirectoryStatus("", browseSnapshot, documents)
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "same" {
		t.Errorf("status %q — a recorded change inside an ignored folder was still used", got.Status)
	}
}

// The three sources of an answer cost wildly different amounts: a lookup, then
// the event log, then reading the tree. KnownDirectoryStatus is the first, and
// what makes it worth having is that it stops before the third.

func TestTheKnownAnswerNeverReadsATree(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read the directory anyway")
	}
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	documents := filepath.Join(seed, "Documents")
	if err := os.WriteFile(filepath.Join(documents, "notes.md"), []byte("edited, and longer"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Nothing recorded yet, so there is nothing to look up — and it must say so
	// rather than going and finding out, which is what the full check is for.
	got, err := svc.KnownDirectoryStatus("", browseSnapshot, documents)
	if err != nil {
		t.Fatalf("known status: %v", err)
	}
	if got.Status != "notExamined" {
		t.Errorf("status %q with nothing recorded, want notExamined", got.Status)
	}
	if svc.Verdicts.Len() != 0 {
		t.Errorf("the lookup recorded %d verdicts, so it did more than look", svc.Verdicts.Len())
	}
}

// Once something IS recorded, the lookup answers from it — including for a folder
// nobody has ever asked about, because one difference proves every folder above
// it changed.
func TestTheKnownAnswerUsesWhatWasRecorded(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	deeper := filepath.Join(seed, "Documents", "deeper")
	if err := os.WriteFile(filepath.Join(deeper, "one.txt"), []byte("edited, and longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The expensive answer, once, which records where it stopped.
	if got, err := svc.DirectoryStatus("", browseSnapshot, deeper); err != nil || got.Status != "modified" {
		t.Fatalf("status %q %v", got.Status, err)
	}

	// The folder above has never been asked about, and is answered by lookup.
	got, err := svc.KnownDirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("known status: %v", err)
	}
	if got.Status != "modified" {
		t.Errorf("status %q, want modified", got.Status)
	}
}

// An ignored folder is settled by the lookup too. It costs a string comparison,
// and sending it to the event log or the disk would be reading for an answer
// already given.
func TestTheKnownAnswerSettlesAnIgnoredFolder(t *testing.T) {
	svc, seed := browseFixture(t)
	ignoring(t, "Documents")

	got, err := svc.KnownDirectoryStatus("", browseSnapshot, filepath.Join(seed, "Documents"))
	if err != nil {
		t.Fatalf("known status: %v", err)
	}
	if got.Status != "ignored" {
		t.Errorf("status %q, want ignored", got.Status)
	}
}

// The point of keeping the table: a difference found in one run answers a folder
// in the next, without anything being read.
func TestARecordedDifferenceSurvivesTheProcessEnding(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read the directory anyway")
	}
	svc, seed := browseFixture(t)

	store, err := changedb.Open(filepath.Join(t.TempDir(), "change-detection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc.Changes = store
	svc.Verdicts = verdict.New()
	svc.Verdicts.Persist(store)

	documents := filepath.Join(seed, "Documents")
	deeper := filepath.Join(documents, "deeper")
	if err := os.WriteFile(filepath.Join(deeper, "one.txt"), []byte("edited, and longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.DirectoryStatus("", browseSnapshot, deeper); err != nil || got.Status != "modified" {
		t.Fatalf("status %q %v", got.Status, err)
	}
	if store.Count(browseSnapshot) != 1 {
		t.Fatalf("the table holds %d rows", store.Count(browseSnapshot))
	}

	// A second run: everything in memory is gone, and only the table is left.
	fresh := verdict.New()
	fresh.Persist(store)
	svc.Verdicts = fresh

	// And make the folder impossible to list, so a verdict can only have come
	// from the table.
	mountPoint, err := svc.mountPointOf("", browseSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotDocuments, err := svc.volumeFor(context.Background(), "").ToSnapshot(mountPoint, documents)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(snapshotDocuments, 0o111); err != nil {
		t.Skip("cannot remove read permission here")
	}
	t.Cleanup(func() { _ = os.Chmod(snapshotDocuments, 0o700) })
	if _, ok := diffs.DiffersWithin(snapshotDocuments, documents, diffs.Options{}); ok {
		t.Skip("the directory is still readable here")
	}

	got, err := svc.DirectoryStatus("", browseSnapshot, documents)
	if err != nil {
		t.Fatalf("directory status: %v", err)
	}
	if got.Status != "modified" {
		t.Errorf("status %q — the recorded difference did not survive", got.Status)
	}
}

// And the other half: a difference that has been undone is dropped from the table
// as well as from memory, or every later run would pay for a stat that can never
// again succeed.
func TestUndoingADifferenceRemovesItFromTheTable(t *testing.T) {
	svc, seed := browseFixture(t)
	store, err := changedb.Open(filepath.Join(t.TempDir(), "change-detection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc.Changes = store
	svc.Verdicts = verdict.New()
	svc.Verdicts.Persist(store)

	documents := filepath.Join(seed, "Documents")
	notes := filepath.Join(documents, "notes.md")
	original, err := os.ReadFile(notes)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(notes)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(notes, []byte("edited, and longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.DirectoryStatus("", browseSnapshot, documents); got.Status != "modified" {
		t.Fatalf("status %q", got.Status)
	}
	if store.Count(browseSnapshot) != 1 {
		t.Fatalf("the table holds %d rows", store.Count(browseSnapshot))
	}

	// Put back, stamp and all, and the cache told the disk moved.
	if err := os.WriteFile(notes, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(notes, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	svc.Verdicts.Touched(notes)

	if got, err := svc.DirectoryStatus("", browseSnapshot, documents); err != nil || got.Status != "same" {
		t.Fatalf("status %q %v", got.Status, err)
	}
	if n := store.Count(browseSnapshot); n != 0 {
		t.Errorf("the table still holds %d rows for a difference that was undone", n)
	}
}

// The mirror of a recorded difference. A walk that completes without finding one
// has read every folder beneath it and proved them all identical — and it has
// already paid for that, so each child was being walked again the moment somebody
// opened the folder above it.
func TestACleanWalkAnswersItsChildrenWithoutWalkingThemAgain(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read the directory anyway")
	}
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	documents := filepath.Join(seed, "Documents")
	deeper := filepath.Join(documents, "deeper")

	// Nothing differs, so this reads Documents and everything under it.
	if got, err := svc.DirectoryStatus("", browseSnapshot, documents); err != nil || got.Status != "same" {
		t.Fatalf("status %q %v", got.Status, err)
	}

	// Now make the child impossible to list, so an answer about it can only have
	// come from the walk that already read it.
	mountPoint, err := svc.mountPointOf("", browseSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotDeeper, err := svc.volumeFor(context.Background(), "").ToSnapshot(mountPoint, deeper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(snapshotDeeper, 0o111); err != nil {
		t.Skip("cannot remove read permission here")
	}
	t.Cleanup(func() { _ = os.Chmod(snapshotDeeper, 0o700) })
	if _, ok := diffs.DiffersWithin(snapshotDeeper, deeper, diffs.Options{}); ok {
		t.Skip("the directory is still readable here")
	}

	got, err := svc.KnownDirectoryStatus("", browseSnapshot, deeper)
	if err != nil {
		t.Fatalf("known status: %v", err)
	}
	if got.Status != "same" {
		t.Errorf("status %q — the child was not answered by the walk that had already read it", got.Status)
	}
}

// And a walk that found a difference vouches for nothing: it stopped, so most of
// the tree was never read.
func TestAWalkThatFoundADifferenceVouchesForNoChildren(t *testing.T) {
	svc, seed := browseFixture(t)
	svc.Verdicts = verdict.New()

	documents := filepath.Join(seed, "Documents")
	if err := os.WriteFile(filepath.Join(documents, "notes.md"), []byte("edited, and longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.DirectoryStatus("", browseSnapshot, documents); got.Status != "modified" {
		t.Fatalf("status %q", got.Status)
	}

	// The subfolder was never read, so nothing is known about it. Anything else
	// would be a claim about a tree this walk stopped short of.
	got, err := svc.KnownDirectoryStatus("", browseSnapshot, filepath.Join(documents, "deeper"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == "same" {
		t.Error("a walk that stopped at a difference vouched for a folder it never read")
	}
}
