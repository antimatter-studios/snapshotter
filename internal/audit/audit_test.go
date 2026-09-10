package audit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The record exists because a question could not be answered: three snapshots
// disappeared and nothing here could say whether this application had deleted
// them. So what is tested is the properties that make an answer possible — that
// a deletion always leaves a line, that a REFUSED deletion is distinguishable
// from one that never happened, and that writing the line can never interfere
// with the act it describes.

// at points Path at a temporary directory by moving HOME, which is what Path
// reads. Restored by t.Setenv when the test ends.
func at(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, "Library", "Logs", "snapshotter-audit.log")
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nothing was recorded: %v", err)
	}
	return string(body)
}

func TestASuccessfulDeletionIsRecorded(t *testing.T) {
	path := at(t)
	now = func() time.Time { return time.Date(2026, 9, 10, 6, 16, 33, 0, time.UTC) }
	defer func() { now = time.Now }()

	Deleted("2026-09-09-061633", "disk3s1", "diskutil apfs deleteSnapshot", nil)

	line := read(t, path)
	for _, want := range []string{"2026-09-10 06:16:33", "2026-09-09-061633", "disk3s1", "deleteSnapshot", "ok"} {
		if !strings.Contains(line, want) {
			t.Errorf("the record does not mention %q: %s", want, line)
		}
	}
}

// The distinction the whole file exists to support: "attempted and refused" and
// "never attempted" mean different things about whether the snapshot should
// still be there.
func TestARefusedDeletionIsRecordedAsRefused(t *testing.T) {
	path := at(t)

	Deleted("2026-09-09-061633", "disk8s1", "diskutil apfs deleteSnapshot", errors.New("Ownership of the affected disks is required"))

	line := read(t, path)
	if !strings.Contains(line, "FAILED") {
		t.Errorf("a refused deletion does not read as refused: %s", line)
	}
	if !strings.Contains(line, "Ownership") {
		t.Errorf("the reason for the refusal was dropped: %s", line)
	}
}

// A multi-line error must not become several lines in the record, or one entry
// reads as several deletions.
func TestAMultiLineErrorStaysOneLine(t *testing.T) {
	path := at(t)

	Deleted("2026-09-09-061633", "disk8s1", "why", errors.New("first line\nsecond line\nthird"))

	body := read(t, path)
	if n := strings.Count(strings.TrimRight(body, "\n"), "\n"); n != 0 {
		t.Errorf("one deletion produced %d extra lines: %q", n, body)
	}
}

// Appended, never rewritten. A record that replaced itself would answer only the
// most recent question.
func TestRecordsAccumulate(t *testing.T) {
	path := at(t)

	Deleted("2026-09-09-061633", "disk3s1", "one", nil)
	Deleted("2026-09-09-130736", "disk8s1", "two", nil)
	Note("replaced /Users/someone/notes.txt")

	body := read(t, path)
	if n := strings.Count(body, "\n"); n != 3 {
		t.Errorf("want three lines, got %d: %s", n, body)
	}
	if !strings.Contains(body, "061633") || !strings.Contains(body, "130736") || !strings.Contains(body, "notes.txt") {
		t.Errorf("an earlier record was lost: %s", body)
	}
}

// Deletions run concurrently — a batch unmount, a sweep across volumes — and
// interleaved partial writes would corrupt the one record meant to be trusted.
func TestConcurrentRecordsDoNotInterleave(t *testing.T) {
	path := at(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Deleted("2026-09-09-061633", "disk3s1", "concurrent", nil)
		}()
	}
	wg.Wait()

	body := read(t, path)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) != 50 {
		t.Fatalf("want 50 lines, got %d", len(lines))
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, "ok") || !strings.Contains(l, "061633") {
			t.Errorf("a line was torn: %q", l)
		}
	}
}

// Writing the record must never prevent or fail the act it describes. A disk
// that will not take the line is a worse reason to leave a snapshot undeleted
// than it is to lose the line.
func TestAnUnwritableRecordIsSurvivable(t *testing.T) {
	t.Setenv("HOME", "/dev/null/nowhere")
	Deleted("2026-09-09-061633", "disk3s1", "unwritable", nil)
	Note("also unwritable")
	// Reaching here without a panic is the assertion.
}
