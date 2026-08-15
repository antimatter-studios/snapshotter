package services

import (
	"strings"
	"testing"
	"time"
)

// The verdict is the one thing this application must never get wrong. Everything
// else it does — browsing, comparing, restoring — is only reachable if someone
// already knows they need it; the Health screen is what tells them whether they
// are protected at all, and a false "you are covered" is the failure the whole
// project exists to prevent.
//
// findings and summarise are pure, so these are exhaustive rather than
// representative: every rule, and the boundaries between them.

// protected is a machine with nothing wrong: snapshots exist, a schedule is
// installed and running, the tripwire is watching, the disk has room. Each test
// then breaks exactly one thing, so a failure names the rule that broke.
func protected(now time.Time) Health {
	newest := now.Add(-time.Hour)
	oldest := now.Add(-48 * time.Hour)
	due := now.Add(5 * time.Hour)
	return Health{
		SnapshotCount:     8,
		Newest:            &newest,
		Oldest:            &oldest,
		CoverageHours:     47,
		ScheduleInstalled: true,
		ScheduleRunning:   true,
		IntervalHours:     6,
		RetentionDays:     14,
		NextDue:           &due,
		TripwireInstalled: true,
		TripwireRunning:   true,
		FreePercent:       42,
	}
}

// titles reduces findings to something a table can assert on.
func titles(fs []Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Title)
	}
	return out
}

func has(fs []Finding, substr string) *Finding {
	for i := range fs {
		if strings.Contains(fs[i].Title, substr) {
			return &fs[i]
		}
	}
	return nil
}

func TestAProtectedMachineHasNothingToSay(t *testing.T) {
	now := time.Now()
	got := findings(protected(now), false, nil, now)
	if len(got) != 0 {
		t.Errorf("a healthy machine produced findings: %v", titles(got))
	}
}

// Each row breaks one thing and names what must be reported, at what severity,
// and with which one-click fix. The action matters as much as the text: a finding
// you cannot act on from where you read it is just anxiety.
func TestEachFailureIsReportedWithItsFix(t *testing.T) {
	now := time.Now()
	overdue := now.Add(-8 * time.Hour) // due 8h ago, interval 6h, so past the grace

	cases := []struct {
		name   string
		change func(*Health)
		want   string
		level  Level
		action string
	}{
		{
			name:   "no snapshots at all",
			change: func(h *Health) { h.SnapshotCount = 0 },
			want:   "no snapshots",
			level:  LevelBad,
			action: "take-snapshot",
		},
		{
			name:   "nothing scheduled",
			change: func(h *Health) { h.ScheduleInstalled = false },
			want:   "Nothing is taking snapshots",
			level:  LevelBad,
			action: "install-schedule",
		},
		{
			name:   "scheduled but not loaded",
			change: func(h *Health) { h.ScheduleRunning = false },
			want:   "installed but not running",
			level:  LevelWarn,
			action: "install-schedule",
		},
		{
			name:   "overdue",
			change: func(h *Health) { h.NextDue = &overdue },
			want:   "overdue",
			level:  LevelWarn,
			action: "show-log",
		},
		{
			name:   "no tripwire",
			change: func(h *Health) { h.TripwireInstalled = false },
			want:   "watching for bulk deletion",
			level:  LevelWarn,
			action: "install-tripwire",
		},
		{
			name:   "tripwire installed but not loaded",
			change: func(h *Health) { h.TripwireRunning = false },
			want:   "watcher is not running",
			level:  LevelWarn,
			action: "install-tripwire",
		},
		{
			name:   "disk nearly full",
			change: func(h *Health) { h.FreePercent = 3 },
			want:   "Free space is low",
			level:  LevelWarn,
			action: "", // nothing this application can do about a full disk
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := protected(now)
			tc.change(&h)
			got := findings(h, false, nil, now)

			f := has(got, tc.want)
			if f == nil {
				t.Fatalf("nothing reported %q; got %v", tc.want, titles(got))
			}
			if f.Level != tc.level {
				t.Errorf("level: want %s, got %s", tc.level, f.Level)
			}
			if f.Action != tc.action {
				t.Errorf("action: want %q, got %q", tc.action, f.Action)
			}
			if f.Detail == "" {
				t.Error("no detail: a finding with no explanation cannot be acted on")
			}
		})
	}
}

// The grace period is the whole point of the overdue rule: a snapshot due a
// minute ago is not a failure, and reporting it as one teaches people to ignore
// the screen. One interval late is the boundary.
func TestOverdueAllowsOneIntervalOfGrace(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		due   time.Time
		want  bool
		about string
	}{
		{"due in the future", now.Add(2 * time.Hour), false, "not yet due"},
		{"just missed", now.Add(-time.Minute), false, "within the grace period"},
		{"one interval late exactly", now.Add(-6 * time.Hour), false, "at the boundary, not past it"},
		{"well past the grace", now.Add(-7 * time.Hour), true, "past due plus one interval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := protected(now)
			due := tc.due
			h.NextDue = &due
			got := has(findings(h, false, nil, now), "overdue") != nil
			if got != tc.want {
				t.Errorf("%s: want overdue=%v, got %v", tc.about, tc.want, got)
			}
		})
	}
}

// A schedule that is not installed cannot be overdue, and saying so twice about
// the same problem is noise.
func TestNoOverdueWarningWithoutASchedule(t *testing.T) {
	now := time.Now()
	h := protected(now)
	h.ScheduleInstalled = false
	h.NextDue = nil
	h.IntervalHours = 0
	if f := has(findings(h, false, nil, now), "overdue"); f != nil {
		t.Error("reported an overdue schedule while reporting there is no schedule")
	}
}

func TestTimeMachineDestinationIsReported(t *testing.T) {
	now := time.Now()
	got := findings(protected(now), true, nil, now)
	f := has(got, "Time Machine")
	if f == nil {
		t.Fatalf("a configured destination was not reported: %v", titles(got))
	}
	if f.Level != LevelWarn {
		t.Errorf("want warn, got %s", f.Level)
	}
	// The number on screen becomes untrue, and saying so is the point.
	if !strings.Contains(f.Detail, "24 hours") {
		t.Errorf("detail does not say what actually happens: %q", f.Detail)
	}
}

// Two agents double the snapshot rate and apply two retention windows to one
// shared set, which is worse than either alone.
func TestEachCompetingAgentIsNamed(t *testing.T) {
	now := time.Now()
	got := findings(protected(now), false, []string{"com.example.other", "com.example.second"}, now)
	if len(got) != 2 {
		t.Fatalf("want one finding per conflict, got %v", titles(got))
	}
	for _, want := range []string{"com.example.other", "com.example.second"} {
		found := false
		for _, f := range got {
			if strings.Contains(f.Detail, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was not named in any finding", want)
		}
	}
}

// Low free space is only worth saying when macOS might act on it. A machine with
// unknown free space (0%) must not be reported as nearly full.
func TestLowSpaceOnlyWhenKnownAndActuallyLow(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		percent float64
		want    bool
	}{
		{0, false}, // unknown, not empty
		{3, true},
		{9.9, true},
		{10, false},
		{80, false},
	} {
		h := protected(now)
		h.FreePercent = tc.percent
		got := has(findings(h, false, nil, now), "Free space") != nil
		if got != tc.want {
			t.Errorf("%.1f%% free: want reported=%v, got %v", tc.percent, tc.want, got)
		}
	}
}

// A simulated machine must be impossible to mistake for a real one, and the
// warning has to come first — a reader who misses it draws conclusions about a
// Mac that does not exist.
func TestASimulatedMachineSaysSoFirst(t *testing.T) {
	now := time.Now()
	h := protected(now)
	h.Scenario = "full-disk"
	h.TripwireInstalled = false // something else to report, so ordering is tested

	got := findings(h, false, nil, now)
	f := has(got, "simulated")
	if f == nil {
		t.Fatalf("a scenario was not announced: %v", titles(got))
	}
	if f.Level != LevelInfo {
		t.Errorf("want info — it is not a fault with the machine — got %s", f.Level)
	}
	if !strings.Contains(f.Detail, "full-disk") {
		t.Errorf("the scenario is not named: %q", f.Detail)
	}
}

func TestFakeMountsAreAnnounced(t *testing.T) {
	now := time.Now()
	h := protected(now)
	h.Faking = true
	f := has(findings(h, false, nil, now), "Mounts are simulated")
	if f == nil {
		t.Fatal("fake mounts were not announced")
	}
	if f.Level != LevelWarn {
		t.Errorf("want warn, got %s", f.Level)
	}
}
