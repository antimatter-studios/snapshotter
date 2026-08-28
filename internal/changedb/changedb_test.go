package changedb

import (
	"path/filepath"
	"testing"
)

// What this table holds is safe to keep only because it is never trusted: every
// row is re-checked before it means anything. So what is asserted here is that it
// survives a restart, that it can be found from any folder above it, and — most
// importantly — that it only ever holds differences.

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "change-detection.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestADifferenceSurvivesARestart(t *testing.T) {
	s, path := open(t)
	if err := s.Record("snap", "/Users/me/projects/app/src/main.go"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A second process, which is what a restart is.
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()

	got, ok := again.Under("snap", "/Users/me/projects")
	if !ok {
		t.Fatal("nothing survived the restart")
	}
	if got != "/Users/me/projects/app/src/main.go" {
		t.Errorf("found %q", got)
	}
}

// One difference answers every folder between the question and the file, which is
// the whole multiplier.
func TestADifferenceIsFoundFromAnyFolderAboveIt(t *testing.T) {
	s, _ := open(t)
	if err := s.Record("snap", "/Users/me/projects/app/src/main.go"); err != nil {
		t.Fatal(err)
	}

	for _, folder := range []string{
		"/Users/me",
		"/Users/me/projects",
		"/Users/me/projects/app",
		"/Users/me/projects/app/src",
		// The path itself is a difference under itself.
		"/Users/me/projects/app/src/main.go",
	} {
		if _, ok := s.Under("snap", folder); !ok {
			t.Errorf("%s found nothing", folder)
		}
	}

	// And nothing for folders it says nothing about, including one whose name is a
	// prefix of the real one — "/Users/me/pro" must not match "/Users/me/projects".
	for _, folder := range []string{"/Users/me/Pictures", "/Users/me/pro", "/etc"} {
		if got, ok := s.Under("snap", folder); ok {
			t.Errorf("%s wrongly found %q", folder, got)
		}
	}
}

func TestTheShallowestIsOffered(t *testing.T) {
	s, _ := open(t)
	for _, p := range []string{"/a/b/c/d/e/deep.txt", "/a/near.txt", "/a/b/middle.txt"} {
		if err := s.Record("snap", p); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := s.Under("snap", "/a")
	if !ok {
		t.Fatal("nothing found")
	}
	if got != "/a/near.txt" {
		t.Errorf("offered %q, want the shallowest /a/near.txt", got)
	}
}

// Recording the same difference twice is one row. Otherwise a folder checked
// every time somebody navigated back to it would grow a row per visit.
func TestRecordingTwiceIsOneRow(t *testing.T) {
	s, _ := open(t)
	for i := 0; i < 3; i++ {
		if err := s.Record("snap", "/a/b.txt"); err != nil {
			t.Fatal(err)
		}
	}
	if n := s.Count("snap"); n != 1 {
		t.Errorf("holding %d rows", n)
	}
}

func TestADifferenceCanBeForgotten(t *testing.T) {
	s, _ := open(t)
	if err := s.Record("snap", "/a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget("snap", "/a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Under("snap", "/a"); ok {
		t.Error("a forgotten difference is still offered")
	}
}

// The paths are on the live disk, but what they record is a difference from one
// snapshot. Unmounting it takes them with it.
func TestForgettingASnapshotLeavesTheOthersAlone(t *testing.T) {
	s, _ := open(t)
	if err := s.Record("snap-1", "/a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Record("snap-2", "/a/b.txt"); err != nil {
		t.Fatal(err)
	}

	if err := s.ForgetSnapshot("snap-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Under("snap-1", "/a"); ok {
		t.Error("a forgotten snapshot kept its differences")
	}
	if _, ok := s.Under("snap-2", "/a"); !ok {
		t.Error("forgetting one snapshot took another's differences")
	}
}

// The table reads as directories and filenames while only the path is ever
// written, so the two cannot drift apart.
func TestTheDirectoryAndFilenameAreDerivedFromThePath(t *testing.T) {
	s, _ := open(t)
	if err := s.Record("snap", "/Users/me/projects/notes.md"); err != nil {
		t.Fatal(err)
	}

	var parent, name string
	err := s.db.QueryRow(`SELECT parent, name FROM change_detection WHERE snapshot = ?`, "snap").
		Scan(&parent, &name)
	if err != nil {
		t.Fatal(err)
	}
	if parent != "/Users/me/projects" {
		t.Errorf("parent %q", parent)
	}
	if name != "notes.md" {
		t.Errorf("name %q", name)
	}

	// They are generated, so they cannot be written to and cannot disagree with
	// the path they came from.
	if _, err := s.db.Exec(`UPDATE change_detection SET name = 'lies'`); err == nil {
		t.Error("the filename column accepted a write")
	}
}

// A path at the top of a volume still has a directory, and it is the root rather
// than an empty string.
func TestATopLevelPathHasTheRootAsItsDirectory(t *testing.T) {
	s, _ := open(t)
	if err := s.Record("snap", "/notes.md"); err != nil {
		t.Fatal(err)
	}
	var parent, name string
	if err := s.db.QueryRow(`SELECT parent, name FROM change_detection`).Scan(&parent, &name); err != nil {
		t.Fatal(err)
	}
	if parent != "/" || name != "notes.md" {
		t.Errorf("parent %q name %q", parent, name)
	}
}

// The cursor is what makes replaying the event log cheap. The uuid beside it is
// what makes the id mean anything: a log wiped and started again gets a new one.
func TestTheEventLogCursorIsKept(t *testing.T) {
	s, path := open(t)
	if err := s.SetCursor("/Volumes/sdcard", "UUID-1", 12345); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()

	uuid, id, ok := again.Cursor("/Volumes/sdcard")
	if !ok {
		t.Fatal("the cursor did not survive")
	}
	if uuid != "UUID-1" || id != 12345 {
		t.Errorf("uuid=%q id=%d", uuid, id)
	}

	// Moved on, not added to.
	if err := again.SetCursor("/Volumes/sdcard", "UUID-1", 99999); err != nil {
		t.Fatal(err)
	}
	if _, id, _ := again.Cursor("/Volumes/sdcard"); id != 99999 {
		t.Errorf("id=%d after moving the cursor on", id)
	}
	if _, _, ok := again.Cursor("/Volumes/somewhere-else"); ok {
		t.Error("a volume nothing was recorded for has a cursor")
	}
}

// Nothing here may be a claim that something did NOT change. There is deliberately
// no way to record one, so this asserts the shape of the table rather than a
// behaviour: a column for it would be the beginning of the mistake.
func TestThereIsNowhereToRecordThatSomethingIsUnchanged(t *testing.T) {
	s, _ := open(t)
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('change_detection')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	for _, c := range columns {
		switch c {
		case "same", "unchanged", "verdict", "status", "identical":
			t.Errorf("the table has a %q column, which is a promise it cannot keep", c)
		}
	}
	if len(columns) == 0 {
		t.Fatal("no columns, so this test is checking nothing")
	}
}

// A store that will not open is an error the caller carries on without. Everything
// here is an optimisation: without it every folder is walked, which is slow rather
// than wrong.
func TestAStoreThatWillNotOpenIsAnError(t *testing.T) {
	// A directory where the file should be.
	dir := t.TempDir()
	if _, err := Open(dir); err == nil {
		t.Error("opening a directory as the table succeeded")
	}
}

// And every method on a nil store is a no-op rather than a panic, so a caller that
// could not open one does not have to guard every call.
func TestANilStoreIsUsableAndDoesNothing(t *testing.T) {
	var s *Store
	if err := s.Record("snap", "/a"); err != nil {
		t.Error(err)
	}
	if err := s.Forget("snap", "/a"); err != nil {
		t.Error(err)
	}
	if _, ok := s.Under("snap", "/a"); ok {
		t.Error("a nil store found something")
	}
	if _, _, ok := s.Cursor("/"); ok {
		t.Error("a nil store had a cursor")
	}
	if err := s.SetCursor("/", "u", 1); err != nil {
		t.Error(err)
	}
	if err := s.Clear(); err != nil {
		t.Error(err)
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
}

// The settings the differences were found under have to be kept beside them.
//
// The bug this stands against: a new run starts with no fingerprint, compares it
// against its own settings, sees a mismatch, and clears the table it has just
// opened. Every restart destroyed exactly the work the table exists to save.
func TestTheSettingsAreKeptBesideTheDifferences(t *testing.T) {
	s, path := open(t)
	if s.Rules() != "" {
		t.Errorf("a fresh table claims settings %q", s.Rules())
	}
	if err := s.SetRules("change_detection.ignore=node_modules"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got := again.Rules(); got != "change_detection.ignore=node_modules" {
		t.Errorf("after a restart the settings read %q", got)
	}
}
