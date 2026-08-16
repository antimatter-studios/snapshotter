package events

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file is how three processes that never run at the same time tell each
// other what happened. What matters is that a writer cannot lose a reader's
// history, that a reader survives whatever a writer left behind, and that the
// file cannot grow without bound.

// isolate points Path at a temporary home for one test.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return filepath.Join(dir, "Library", "Application Support", "Snapshotter", "events.jsonl")
}

func TestAnEventSurvivesBeingWrittenAndReadBack(t *testing.T) {
	isolate(t)

	want := Event{
		Kind:     KindBulkDeletion,
		Where:    []string{"~/Documents/Invoices", "~/Desktop"},
		Snapshot: "2026-08-16-105616",
	}
	if err := Append(want); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := Recent(5)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want one event, got %d", len(got))
	}
	if got[0].Kind != want.Kind || got[0].Snapshot != want.Snapshot {
		t.Errorf("came back changed: %+v", got[0])
	}
	if len(got[0].Where) != 2 || got[0].Where[0] != "~/Documents/Invoices" {
		t.Errorf("the locations were lost: %v", got[0].Where)
	}
	// Stamped even though the caller did not: an event with no time cannot be
	// ordered or shown.
	if got[0].At.IsZero() {
		t.Error("no time was recorded")
	}
}

// Newest first, because every caller wants the last few.
func TestRecentIsNewestFirstAndCapped(t *testing.T) {
	isolate(t)

	for i := 0; i < 10; i++ {
		if err := Append(Event{Kind: KindBulkDeletion, Note: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Recent(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("asked for 3, got %d", len(got))
	}
	if got[0].Note != "9" || got[2].Note != "7" {
		t.Errorf("wrong order: %s, %s, %s", got[0].Note, got[1].Note, got[2].Note)
	}
}

// Trimmed to the newest rather than emptied. Emptying at the limit throws away
// exactly what a screen is displaying, so the list would go blank at the moment
// it had most to say.
func TestTheFileIsTrimmedToTheNewestRatherThanEmptied(t *testing.T) {
	path := isolate(t)

	for i := 0; i < maxRows+25; i++ {
		if err := Append(Event{Kind: KindBulkDeletion, Note: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > maxRows {
		t.Errorf("the file grew to %d lines, past the %d limit", len(lines), maxRows)
	}
	if len(lines) == 0 {
		t.Fatal("the file was emptied, losing everything a screen would show")
	}

	got, err := Recent(1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Note != fmt.Sprint(maxRows+24) {
		t.Errorf("the newest event was lost: %+v", got[0])
	}
}

// A writer that died mid-append leaves a partial line, and a later build may
// write fields this one has never seen. Neither is a reason to lose the rest.
func TestABadLineDoesNotCostTheGoodOnes(t *testing.T) {
	path := isolate(t)

	if err := Append(Event{Kind: KindBulkDeletion, Note: "first"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	// A truncated line, then a line from a version that knows more than this one.
	f.WriteString("{\"kind\":\"bulk-del\n")
	f.WriteString("{\"at\":\"2026-08-16T10:00:00Z\",\"kind\":\"something-new\",\"unknown\":42}\n")
	f.Close()

	if err := Append(Event{Kind: KindBulkDeletion, Note: "second"}); err != nil {
		t.Fatal(err)
	}

	got, err := Recent(10)
	if err != nil {
		t.Fatalf("a bad line broke reading entirely: %v", err)
	}
	var notes []string
	for _, e := range got {
		if e.Note != "" {
			notes = append(notes, e.Note)
		}
	}
	if len(notes) != 2 {
		t.Errorf("lost a good event to a bad neighbour: %v", notes)
	}
	// The unknown kind is kept rather than discarded: this build does not know
	// what it is, but it is not this build's to throw away.
	var sawUnknown bool
	for _, e := range got {
		if e.Kind == "something-new" {
			sawUnknown = true
		}
	}
	if !sawUnknown {
		t.Error("an event from a later version was dropped")
	}
}

// Nothing has happened yet is not an error, and neither is a first run.
func TestNoFileMeansNoEventsAndNoError(t *testing.T) {
	isolate(t)

	got, err := Recent(5)
	if err != nil {
		t.Errorf("a machine where nothing has happened reported an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("events appeared from nowhere: %v", got)
	}
}

// Three processes write this file. Concurrent appends must not interleave into
// unparseable lines or lose each other.
func TestConcurrentAppendsAllSurviveIntact(t *testing.T) {
	isolate(t)

	const writers = 8
	const each = 12
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := Append(Event{
					Kind: KindBulkDeletion,
					Note: fmt.Sprintf("w%d-%d", w, i),
					At:   time.Now(),
				}); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	got, err := Recent(0)
	if err != nil {
		t.Fatal(err)
	}
	// Every write survives while the total is under the cap, and each one parsed:
	// a torn or interleaved line would have produced neither.
	want := writers * each
	if want > maxRows {
		want = maxRows
	}
	if len(got) != want {
		t.Errorf("want %d surviving events, got %d", want, len(got))
	}
	for _, e := range got {
		if e.Note == "" || !strings.HasPrefix(e.Note, "w") {
			t.Errorf("a line came back malformed: %+v", e)
		}
	}
}
