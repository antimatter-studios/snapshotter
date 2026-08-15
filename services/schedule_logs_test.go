package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snapshotter/internal/schedule"
)

// A schedule that fails does so unattended, at four in the morning, with nobody
// watching. The log tail is the only evidence, so its failure modes matter: a
// log that has not been written yet must not look like an error, and a log too
// large to show must not be truncated at the wrong end.

func TestTheLogTailSaysSoWhenNothingHasBeenWrittenYet(t *testing.T) {
	dir := t.TempDir()
	s := NewScheduleService(Deps{
		Agent:    &schedule.Agent{LogPath: filepath.Join(dir, "never-written.log")},
		Tripwire: &schedule.Tripwire{LogPath: filepath.Join(dir, "also-never.log")},
	})

	got, err := s.Log(0)
	if err != nil {
		t.Fatalf("a missing log was reported as an error: %v", err)
	}
	if got == "" {
		t.Error("a missing log came back blank, which reads as a broken screen")
	}

	tw, err := s.TripwireLog(0)
	if err != nil {
		t.Fatalf("a missing tripwire log was reported as an error: %v", err)
	}
	if tw == got {
		t.Error("both logs give the same message, so neither says which is quiet")
	}
}

func TestTheLogTailReturnsWhatWasWritten(t *testing.T) {
	dir := t.TempDir()
	agentLog := filepath.Join(dir, "agent.log")
	twLog := filepath.Join(dir, "tripwire.log")
	if err := os.WriteFile(agentLog, []byte("took a snapshot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(twLog, []byte("watched a deletion\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewScheduleService(Deps{
		Agent:    &schedule.Agent{LogPath: agentLog},
		Tripwire: &schedule.Tripwire{LogPath: twLog},
	})

	got, err := s.Log(0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "took a snapshot") {
		t.Errorf("the agent log did not come back: %q", got)
	}

	tw, err := s.TripwireLog(0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tw, "watched a deletion") {
		t.Errorf("the tripwire log did not come back: %q", tw)
	}
}

// The tail must be the end of the file. Showing the first 200 bytes of a log
// that has been running for a month is showing a month-old success while the
// thing is failing now.
func TestTheLogTailKeepsTheEndAndNotTheStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.log")
	body := strings.Repeat("old and irrelevant\n", 500) + "THE MOST RECENT LINE\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewScheduleService(Deps{Agent: &schedule.Agent{LogPath: path}})
	got, err := s.Log(200)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "THE MOST RECENT LINE") {
		t.Error("the tail dropped the newest line, which is the only one worth reading")
	}
	if int64(len(got)) > 200 {
		t.Errorf("asked for 200 bytes, got %d", len(got))
	}
}

// Describe is the sentence on the settings screen. It has to distinguish three
// states someone can act on: nothing scheduled, scheduled but not running (which
// needs fixing), and running.
func TestDescribeTellsTheThreeStatesApart(t *testing.T) {
	none := ScheduleView{}.Describe()
	if !strings.Contains(strings.ToLower(none), "no schedule") {
		t.Errorf("an absent schedule does not say so: %q", none)
	}

	stopped := ScheduleView{
		Installed: true, Loaded: false, IntervalHours: 4,
		MaxSnapshots: 24, ReachDays: 4, PolicySummary: "One an hour.",
	}.Describe()
	running := ScheduleView{
		Installed: true, Loaded: true, IntervalHours: 4,
		MaxSnapshots: 24, ReachDays: 4, PolicySummary: "One an hour.",
	}.Describe()

	if stopped == running {
		t.Error("an installed-but-stopped schedule reads the same as a running one")
	}
	if !strings.Contains(running, "running") {
		t.Errorf("a running schedule does not say so: %q", running)
	}
	for _, want := range []string{"4 hours", "24", "One an hour."} {
		if !strings.Contains(running, want) {
			t.Errorf("%q missing from the description: %q", want, running)
		}
	}
}
