package services

import (
	"testing"
	"time"

	"snapshotter/internal/events"
)

// The tripwire and the window are separate processes. This is the round trip
// between them: what the agent writes when it fires is what the window shows.
func TestAWarningWrittenByTheAgentIsWhatTheWindowShows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Exactly what agents.go appends when the wire trips.
	if err := events.Append(events.Event{
		Kind:     events.KindBulkDeletion,
		Where:    []string{"~/Documents/Invoices", "~/Desktop"},
		Snapshot: "2026-08-16-105616",
	}); err != nil {
		t.Fatalf("the agent could not record it: %v", err)
	}
	// One the response failed for — the case most worth being able to look back at.
	if err := events.Append(events.Event{
		Kind:  events.KindBulkDeletion,
		Where: []string{"~/Library/Caches/Something"},
		Note:  "no snapshot was taken: tmutil is unavailable",
	}); err != nil {
		t.Fatal(err)
	}

	got := NewStatusService(Deps{}).RecentWarnings(5)
	if len(got) != 2 {
		t.Fatalf("want both warnings, got %d", len(got))
	}

	// Newest first: the most recent deletion is the one being asked about.
	if got[0].Snapshot != "" || got[0].Note == "" {
		t.Errorf("the newest should be the one with no snapshot: %+v", got[0])
	}
	if got[1].Snapshot != "2026-08-16-105616" {
		t.Errorf("the snapshot taken in response was lost: %+v", got[1])
	}
	if len(got[1].Where) != 2 || got[1].Where[0] != "~/Documents/Invoices" {
		t.Errorf("the folders were lost: %v", got[1].Where)
	}
	if got[0].At.IsZero() || time.Since(got[0].At) > time.Minute {
		t.Errorf("the time is wrong: %v", got[0].At)
	}
}

// A machine where nothing has happened shows nothing, without an error: that is
// the ordinary case, and a red banner on a healthy Mac makes things worse.
func TestNoWarningsOnAMachineWhereNothingHasHappened(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if got := NewStatusService(Deps{}).RecentWarnings(5); len(got) != 0 {
		t.Errorf("warnings appeared from nowhere: %v", got)
	}
}
