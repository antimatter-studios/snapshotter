package services

import (
	"context"
	"os"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/config"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/scenario"
	"snapshotter/internal/schedule"
)

// Installing a schedule writes a plist and asks launchd to load it — a real side
// effect on a real machine. These tests do it for real, in a scenario's sandbox,
// so the plist is written and read back by the same code that would write it to
// ~/Library/LaunchAgents without ever going near the real one.
//
// XDG_CONFIG_HOME is redirected too, because installing now records the choice in
// the settings file, and a test must not rewrite the settings of whoever ran it.

// stack builds every service over one sandboxed scenario, so a test can install
// through one and observe through another exactly as the window does.
type stack struct {
	Status    *StatusService
	Schedule  *ScheduleService
	Snapshots *SnapshotService
	Browse    *BrowseService
	Deps      Deps
}

func newStack(t *testing.T, presetName string) stack {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	spec, err := scenario.Load(presetName)
	if err != nil {
		t.Fatalf("loading %s: %v", presetName, err)
	}
	sim, err := scenario.New(spec, scenario.Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}
	box, err := sim.Sandbox(context.Background())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(box.Dir) })

	d := Deps{
		Runner:   sim.Runner,
		Volume:   apfs.DataVolume,
		Scenario: spec.Name,
		Space:    spec.Space(),
		Mounts:   mountmgr.NewFake(box.MountRoot, t.TempDir()),
		Agent: &schedule.Agent{
			Runner: sim.Runner, AgentDir: box.AgentDir,
			Program: "/usr/bin/true", LogPath: box.LogPath, UID: os.Getuid(),
		},
		Tripwire: &schedule.Tripwire{
			Runner: sim.Runner, AgentDir: box.AgentDir,
			Program: "/usr/bin/true", LogPath: box.TripwireLogPath, UID: os.Getuid(),
		},
	}
	return stack{
		Status:    NewStatusService(d),
		Schedule:  NewScheduleService(d),
		Snapshots: NewSnapshotService(d),
		Browse:    NewBrowseService(d),
		Deps:      d,
	}
}

// The round trip that matters: a machine with nothing scheduled, told to schedule
// something, reports it as scheduled — through a written plist and launchctl,
// not through a variable someone set.
func TestInstallingASchedulePersistsAndIsReadBack(t *testing.T) {
	s := newStack(t, "empty")
	ctx := context.Background()

	before, err := s.Schedule.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if before.Installed {
		t.Fatal("the empty scenario already had a schedule")
	}

	view, err := s.Schedule.Install(ctx, 3, 30)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !view.Installed {
		t.Error("installing did not report an installed schedule")
	}
	if view.IntervalHours != 3 || view.RetentionDays != 30 {
		t.Errorf("asked for 3h/30d, got %vh/%vd", view.IntervalHours, view.RetentionDays)
	}
	if view.PlistPath == "" {
		t.Error("no plist path, so nobody can find what was installed")
	}
	if _, err := os.Stat(view.PlistPath); err != nil {
		t.Errorf("the plist it claims to have written is not there: %v", err)
	}

	// Read back through a separate call, which is what the window does on refresh.
	after, err := s.Schedule.Status(ctx)
	if err != nil {
		t.Fatalf("status after install: %v", err)
	}
	if !after.Installed || after.IntervalHours != 3 || after.RetentionDays != 30 {
		t.Errorf("the schedule did not survive being read back: %+v", after)
	}
}

// The choice has to outlive the installation, so a second copy of the application
// inherits it rather than asking again.
func TestInstallingRecordsTheChoiceInTheSettingsFile(t *testing.T) {
	s := newStack(t, "empty")

	if _, err := s.Schedule.InstallPolicy(context.Background(), 2, 60, "flat"); err != nil {
		t.Fatalf("install: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reading the settings back: %v", err)
	}
	if cfg.Schedule.IntervalHours != 2 || cfg.Schedule.RetentionDays != 60 {
		t.Errorf("the settings file did not record the choice: %+v", cfg.Schedule)
	}
	if cfg.Schedule.Policy != "flat" {
		t.Errorf("policy not recorded: %q", cfg.Schedule.Policy)
	}
}

// Uninstalling stops the schedule and leaves the snapshots alone — deleting
// someone's restore points because they turned off the timer would be the worst
// thing this application could do.
func TestUninstallingStopsTheScheduleAndKeepsTheSnapshots(t *testing.T) {
	s := newStack(t, "healthy")
	ctx := context.Background()

	before, err := s.Snapshots.List(ctx)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("the healthy scenario had no snapshots to protect")
	}

	view, err := s.Schedule.Uninstall(ctx)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if view.Installed {
		t.Error("still reports an installed schedule")
	}

	after, err := s.Snapshots.List(ctx)
	if err != nil {
		t.Fatalf("listing after uninstall: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("uninstalling the schedule changed the snapshots: %d -> %d", len(before), len(after))
	}
}

// The tripwire is a second agent with the same lifecycle, and the same rule about
// not touching what has already been recorded.
func TestTheTripwireInstallsAndUninstalls(t *testing.T) {
	s := newStack(t, "empty")
	ctx := context.Background()

	before, err := s.Schedule.TripwireStatus(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if before.Installed {
		t.Fatal("the empty scenario already had a tripwire")
	}

	on, err := s.Schedule.InstallTripwire(ctx)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !on.Installed {
		t.Error("installing did not report an installed tripwire")
	}

	off, err := s.Schedule.UninstallTripwire(ctx)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if off.Installed {
		t.Error("uninstalling left it installed")
	}
}

// Installing over an existing schedule is how someone changes the interval, so it
// must replace rather than duplicate — two agents would double the rate.
func TestReinstallingReplacesRatherThanDuplicates(t *testing.T) {
	s := newStack(t, "healthy")
	ctx := context.Background()

	first, err := s.Schedule.Install(ctx, 6, 14)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	second, err := s.Schedule.Install(ctx, 12, 7)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}

	if second.PlistPath != first.PlistPath {
		t.Errorf("a second install wrote a different plist: %q then %q", first.PlistPath, second.PlistPath)
	}
	if second.IntervalHours != 12 || second.RetentionDays != 7 {
		t.Errorf("the new settings did not take: %+v", second)
	}
	for _, c := range second.Conflicts {
		if c == schedule.Label {
			t.Error("the schedule was reported as conflicting with itself")
		}
	}
}

// Taking a snapshot is the one action every screen offers, and the count has to
// move — a button that appears to work and changes nothing is the worst outcome
// for an application about not losing things.
func TestTakingASnapshotAddsOne(t *testing.T) {
	s := newStack(t, "healthy")
	ctx := context.Background()

	before, err := s.Snapshots.Overview(ctx)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}

	taken, err := s.Snapshots.TakeNow(ctx)
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if taken.Name == "" {
		t.Error("the new snapshot has no name")
	}

	after, err := s.Snapshots.Overview(ctx)
	if err != nil {
		t.Fatalf("overview after: %v", err)
	}
	if len(after.Snapshots) != len(before.Snapshots)+1 {
		t.Errorf("want one more snapshot, went from %d to %d",
			len(before.Snapshots), len(after.Snapshots))
	}
	if after.Snapshots[0].Name != taken.Name {
		t.Errorf("the newest snapshot is not the one just taken: %q vs %q",
			after.Snapshots[0].Name, taken.Name)
	}
}

// Deleting is the one destructive thing here, and apfs.Delete already refuses
// anything but a bare date stamp. This is the service-level half of that guard:
// a mount point or a path must never reach tmutil, which given one deletes every
// snapshot on the volume.
func TestDeleteRefusesAnythingThatIsNotAStamp(t *testing.T) {
	s := newStack(t, "healthy")
	ctx := context.Background()

	before, err := s.Snapshots.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"/", apfs.DataVolume, "com.apple.TimeMachine.2026-08-13-172036.local", "", "2026-08-13-172036 ; rm -rf /"} {
		if err := s.Snapshots.Delete(ctx, bad); err == nil {
			t.Errorf("%q was accepted as a deletion target", bad)
		}
	}

	after, err := s.Snapshots.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("a refused deletion still removed snapshots: %d -> %d", len(before), len(after))
	}
}

// The overview is what the sidebar renders, so its figures have to match the
// snapshots it lists rather than being gathered separately and drifting.
func TestOverviewAgreesWithTheSnapshotsItLists(t *testing.T) {
	s := newStack(t, "healthy")

	o, err := s.Snapshots.Overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(o.Snapshots) == 0 {
		t.Fatal("no snapshots in a healthy scenario")
	}
	if o.VolumeTotalBytes == 0 {
		t.Error("no volume size, so the sidebar cannot show free space")
	}
	if o.VolumeFreeBytes > o.VolumeTotalBytes {
		t.Errorf("more free than total: %d of %d", o.VolumeFreeBytes, o.VolumeTotalBytes)
	}
	// Newest first is what every screen assumes.
	for i := 1; i < len(o.Snapshots); i++ {
		if o.Snapshots[i-1].Taken.Before(o.Snapshots[i].Taken) {
			t.Errorf("snapshots are not newest-first at %d", i)
		}
	}
}

// Restoring what was configured.
//
// The case that prompted it: `brew upgrade` unloads both agents before staging
// the new version, so an upgrade silently removes the schedule while the
// settings file still records the interval that was chosen.

func TestRestorePutsBackAScheduleThatWasRemovedBehindOurBack(t *testing.T) {
	s := newStack(t, "empty")
	ctx := context.Background()

	if _, err := s.Schedule.Install(ctx, 3, 21); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Removed the way something else would remove it — the plist deleted without
	// the settings file being told.
	view, err := s.Schedule.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(view.PlistPath); err != nil {
		t.Fatalf("removing the plist: %v", err)
	}
	if before, _ := s.Schedule.Status(ctx); before.Installed {
		t.Fatal("the plist is still there, so this proves nothing")
	}

	restored, err := s.Schedule.Restore(ctx)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restored.Schedule {
		t.Error("restore did not report putting the schedule back")
	}

	after, err := s.Schedule.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Installed {
		t.Fatal("the schedule was not put back")
	}
	// The settings it was installed with, not the defaults.
	if after.IntervalHours != 3 || after.RetentionDays != 21 {
		t.Errorf("restored with the wrong settings: %vh/%vd", after.IntervalHours, after.RetentionDays)
	}
}

// The rule that keeps this from being obnoxious: a schedule somebody removed on
// purpose must stay removed, or the application argues with its user once per
// launch.
func TestRestoreLeavesADeliberatelyRemovedScheduleAlone(t *testing.T) {
	s := newStack(t, "empty")
	ctx := context.Background()

	if _, err := s.Schedule.Install(ctx, 6, 14); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Schedule.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}

	restored, err := s.Schedule.Restore(ctx)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	// Only the schedule is asserted on: the tripwire is on by default, so a
	// scenario with no tripwire installed will legitimately gain one here.
	if restored.Schedule {
		t.Error("restore reinstated a schedule that was deliberately removed")
	}
	if after, _ := s.Schedule.Status(ctx); after.Installed {
		t.Error("the schedule came back after being uninstalled")
	}
}

// A machine that never asked for a schedule must not acquire one by being
// launched. A fresh settings file carries defaults, and defaults are not a
// request.
func TestRestoreInstallsNothingOnAMachineThatNeverAskedForIt(t *testing.T) {
	s := newStack(t, "empty")
	ctx := context.Background()

	if err := config.Save(config.Defaults()); err != nil {
		t.Fatal(err)
	}

	restored, err := s.Schedule.Restore(ctx)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Schedule {
		t.Error("a schedule was installed from the defaults alone")
	}
	if after, _ := s.Schedule.Status(ctx); after.Installed {
		t.Error("a schedule appeared on a machine that never asked for one")
	}

	// The tripwire is the deliberate exception, and this is where that decision
	// is written down. It costs nothing until something starts deleting in bulk,
	// and it is the half of the protection that catches what people actually lose
	// files to — so a fresh installation gets it without having to know it exists.
	// The schedule is not treated the same way: it takes snapshots on a timer,
	// which is a thing to opt into.
	if !restored.Tripwire {
		t.Error("the tripwire was not installed from the defaults")
	}
	if after, _ := s.Schedule.TripwireStatus(ctx); !after.Installed {
		t.Error("the tripwire is on by default but was not installed")
	}
}

// Nothing to do is not an error, and must not be reported as one: this runs on
// every launch.
func TestRestoreIsQuietWhenThereIsNothingToDo(t *testing.T) {
	s := newStack(t, "empty")
	ctx := context.Background()

	if _, err := s.Schedule.Install(ctx, 4, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Schedule.InstallTripwire(ctx); err != nil {
		t.Fatal(err)
	}

	restored, err := s.Schedule.Restore(ctx)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Any() {
		t.Error("restore claimed to have done something to a healthy machine")
	}
}

// The tripwire has the same lifecycle and the same failure.
func TestRestorePutsBackTheTripwire(t *testing.T) {
	s := newStack(t, "empty")
	ctx := context.Background()

	if _, err := s.Schedule.InstallTripwire(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := s.Schedule.TripwireStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(view.PlistPath); err != nil {
		t.Fatal(err)
	}

	restored, err := s.Schedule.Restore(ctx)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restored.Tripwire {
		t.Error("the tripwire was not reported as restored")
	}
	if after, _ := s.Schedule.TripwireStatus(ctx); !after.Installed {
		t.Error("the tripwire was not put back")
	}
}
