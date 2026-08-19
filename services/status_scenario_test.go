package services

import (
	"context"
	"os"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/scenario"
	"snapshotter/internal/schedule"
)

// The unit tests beside this one exercise the verdict rules directly. These go
// through the whole service instead — real tmutil parsing, real diskutil parsing,
// real launchctl reading, real plist writing — against machines described by the
// scenario presets.
//
// That is the difference worth having: findings() can be right about a Health it
// was handed while Check() fills that Health in wrongly, and nothing that only
// tests the rules would notice. Everything below asks the question the interface
// asks, and reads the answer someone would actually see.
//
// Nothing here touches the real machine. The scenario's runner answers every
// command, and its sandbox holds any plists, so a test cannot install a schedule
// on the computer running it.

// underScenario builds the service stack the window builds, for a named preset.
func underScenario(t *testing.T, name string) *StatusService {
	t.Helper()

	spec, err := scenario.Load(name)
	if err != nil {
		t.Fatalf("loading the preset: %v", err)
	}
	sim, err := scenario.New(spec, scenario.Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("building the scenario: %v", err)
	}
	box, err := sim.Sandbox(context.Background())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(box.Dir) })

	deps := Deps{
		Runner:   sim.Runner,
		Volume:   apfs.DataVolume,
		Scenario: spec.Name,
		Mounts:   mountmgr.NewFake(box.MountRoot, t.TempDir()),
		// Without this the service asks the kernel, and the full-disk scenario would
		// be reporting the real machine's free space instead of its own.
		Space: spec.Space(),
		Agent: &schedule.Agent{
			Runner: sim.Runner, AgentDir: box.AgentDir,
			Program: "/usr/bin/true", LogPath: box.LogPath, UID: os.Getuid(),
		},
		Tripwire: &schedule.Tripwire{
			Runner: sim.Runner, AgentDir: box.AgentDir,
			Program: "/usr/bin/true", LogPath: box.TripwireLogPath, UID: os.Getuid(),
		},
	}
	return NewStatusService(deps)
}

func checkUnder(t *testing.T, name string) Health {
	t.Helper()
	h, err := underScenario(t, name).Check(context.Background())
	if err != nil {
		t.Fatalf("%s: Check failed: %v", name, err)
	}
	return h
}

// A machine with nothing protecting it must say so in the strongest terms it has,
// because this is the state the whole application exists to get someone out of.
func TestAnUnprotectedMachineReadsAsBad(t *testing.T) {
	h := checkUnder(t, "empty")

	if h.Level != LevelBad {
		t.Errorf("want bad, got %s (%q)", h.Level, h.Headline)
	}
	if h.SnapshotCount != 0 {
		t.Errorf("want no snapshots, got %d", h.SnapshotCount)
	}
	if has(h.Findings, "no snapshots") == nil {
		t.Errorf("nothing said there are no snapshots: %v", titles(h.Findings))
	}
	if has(h.Findings, "Nothing is taking snapshots") == nil {
		t.Errorf("nothing said nothing is scheduled: %v", titles(h.Findings))
	}
}

// The opposite end: everything configured and running. Anything short of a clean
// verdict here means the screen cries wolf, and a screen that cries wolf is worse
// than no screen.
func TestAProtectedMachineReadsAsWell(t *testing.T) {
	h := checkUnder(t, "healthy")

	if h.SnapshotCount == 0 {
		t.Fatal("the healthy preset produced no snapshots")
	}
	if !h.ScheduleInstalled || !h.ScheduleRunning {
		t.Errorf("the schedule was not read back as installed and running: %+v", h)
	}
	for _, f := range h.Findings {
		if f.Level == LevelBad {
			t.Errorf("a healthy machine reported a failure: %q", f.Title)
		}
	}
	if h.CoverageHours <= 0 {
		t.Error("cover was not computed from the snapshot span")
	}
}

// Time Machine silently invalidates the retention the settings screen promises,
// which is exactly the kind of quiet untruth this application should refuse to
// participate in.
func TestATimeMachineDestinationIsNoticedThroughTheWholeStack(t *testing.T) {
	h := checkUnder(t, "time-machine")

	f := has(h.Findings, "Time Machine")
	if f == nil {
		t.Fatalf("a configured destination was not noticed: %v", titles(h.Findings))
	}
	if !strings.Contains(f.Detail, "24 hours") {
		t.Errorf("the consequence was not stated: %q", f.Detail)
	}
}

// Two agents taking snapshots of one volume double the rate and apply two
// retention windows to a shared set. Reading another agent's plist back off disk
// is the only way to know, so this is worth an end-to-end test.
func TestACompetingAgentIsFoundOnDisk(t *testing.T) {
	h := checkUnder(t, "conflict")

	if has(h.Findings, "Another agent") == nil {
		t.Errorf("a competing agent was not detected: %v", titles(h.Findings))
	}
}

// A nearly full disk makes retention a hope rather than a setting, because macOS
// reclaims purgeable snapshots under pressure whatever the policy says.
func TestAFullDiskIsReportedAsUnreliableRetention(t *testing.T) {
	h := checkUnder(t, "full-disk")

	if h.FreePercent <= 0 || h.FreePercent >= 10 {
		t.Fatalf("the preset did not produce a tight disk: %.1f%% free", h.FreePercent)
	}
	if hasKind(h.Findings, KindSpace) == nil {
		t.Errorf("low space was not reported: %v", titles(h.Findings))
	}
}

// A schedule that looks configured and is not working is the failure mode this
// screen exists for: the belief that you are covered survives everything except
// being told otherwise.
func TestAnOverdueScheduleIsReported(t *testing.T) {
	h := checkUnder(t, "overdue")

	if !h.ScheduleInstalled {
		t.Fatal("the overdue preset should have a schedule installed")
	}
	if h.Level == LevelOK {
		t.Errorf("an overdue schedule read as healthy: %q", h.Headline)
	}
	if has(h.Findings, "overdue") == nil {
		t.Errorf("nothing reported the overdue schedule: %v", titles(h.Findings))
	}
}

// Every scenario must announce itself, in every scenario, or a reader draws
// conclusions about a machine that does not exist.
func TestEveryScenarioAnnouncesItself(t *testing.T) {
	for _, name := range scenario.Names() {
		t.Run(name, func(t *testing.T) {
			h := checkUnder(t, name)
			f := has(h.Findings, "simulated")
			if f == nil {
				t.Fatalf("scenario %s did not announce itself: %v", name, titles(h.Findings))
			}
			if !strings.Contains(f.Detail, name) {
				t.Errorf("the scenario is not named in its own warning: %q", f.Detail)
			}
			if h.Scenario != name {
				t.Errorf("Health.Scenario is %q, want %q", h.Scenario, name)
			}
		})
	}
}

// The headline is the menu bar's entire vocabulary, so it must never come back
// empty whatever the machine looks like.
func TestEveryScenarioProducesAHeadlineAndALevel(t *testing.T) {
	for _, name := range scenario.Names() {
		t.Run(name, func(t *testing.T) {
			h := checkUnder(t, name)
			if strings.TrimSpace(h.Headline) == "" {
				t.Error("no headline")
			}
			switch h.Level {
			case LevelOK, LevelWarn, LevelBad:
			default:
				t.Errorf("unusable level %q", h.Level)
			}
			if h.Version == "" {
				t.Error("no version reported, so the window cannot say which build it is")
			}
		})
	}
}
