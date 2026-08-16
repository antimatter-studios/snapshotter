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

// The window shows one form and acts on another: "~" is for reading, and means
// nothing to a comparison against a path the filesystem reported. Sending only
// the short form would produce ignore rules that never match.
func TestAWarningCarriesBothTheRealPathAndAReadableOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	full := home + "/Library/Caches/Something"
	if err := events.Append(events.Event{
		Kind: events.KindBulkDeletion, Where: []string{full}, Snapshot: "x",
	}); err != nil {
		t.Fatal(err)
	}

	got := NewStatusService(Deps{}).RecentWarnings(1)
	if len(got) != 1 {
		t.Fatalf("want one warning, got %d", len(got))
	}
	if got[0].Where[0] != full {
		t.Errorf("the real path was changed: %q", got[0].Where[0])
	}
	if got[0].Labels[0] != "~/Library/Caches/Something" {
		t.Errorf("the readable form is %q, want ~/Library/Caches/Something", got[0].Labels[0])
	}
	// One label per folder, or the window pairs the wrong name with the wrong
	// button.
	if len(got[0].Labels) != len(got[0].Where) {
		t.Errorf("%d labels for %d folders", len(got[0].Labels), len(got[0].Where))
	}
}
