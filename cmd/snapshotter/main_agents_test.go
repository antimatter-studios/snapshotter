package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/config"
	"snapshotter/internal/scenario"
	"snapshotter/internal/schedule"
)

// These are the names launchd sets in the plist, which is why they are spelled
// out here rather than imported: they are an external contract with an installed
// agent, and a rename that this test did not notice would silently stop an
// already-installed schedule from pruning.
const (
	envPolicy    = "SNAPSHOTTER_RETENTION_POLICY"
	envRetention = "SNAPSHOTTER_RETENTION_HOURS"
)

// The scheduled task and the tripwire are the two things that run when nobody is
// watching — launchd starts them, their output goes to a log file, and the first
// anyone hears of a failure is when a snapshot they expected is not there. So
// they get tested against a scenario rather than left to the machine.

// agentRunner builds a runner over a scenario, which answers tmutil and diskutil
// out of a sandbox instead of touching this machine's snapshots.
func agentRunner(t *testing.T, presetName string) apfs.Runner {
	t.Helper()

	spec, err := scenario.Load(presetName)
	if err != nil {
		t.Fatalf("loading %s: %v", presetName, err)
	}
	sim, err := scenario.New(spec, scenario.Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}
	return sim.Runner
}

// scheduledRun gives the task its own configuration directory and log path, and
// hands back the directory so a test can say which snapshots are the schedule's.
//
// A directory per test, because the managed record is a file in it: two tests
// sharing one would inherit each other's idea of what this schedule created,
// which is exactly the state that decides whether a snapshot is deleted.
func scheduledRun(t *testing.T) (paths, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	// A log path inside the temp directory and deliberately absent, so Adopt
	// finds nothing and the test says for itself what is managed. Pointing at the
	// real log would make the outcome depend on the developer's own machine.
	return paths{logPath: filepath.Join(t.TempDir(), "snapshotter.log")}, dir
}

func TestTheScheduledTaskTakesASnapshot(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()
	p, _ := scheduledRun(t)

	before, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}

	// No policy means the flat window built from the retention hours. Ten years
	// of it, so nothing is due for reaping: this test is about the taking.
	t.Setenv(envPolicy, "")
	t.Setenv(envRetention, "87600")

	if err := runScheduledSnapshot(ctx, runner, p); err != nil {
		t.Fatalf("the scheduled task failed: %v", err)
	}

	after, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Errorf("want one more snapshot, went from %d to %d", len(before), len(after))
	}
}

// The change of principle: a period that already has a snapshot needs no second
// one.
//
// Somebody taking a snapshot by hand at 06:00 has covered the day. The run at
// 06:16 used to take another regardless, and then delete one of the two for
// sharing a day — so the deliberate act was undone by the automatic one.
func TestTheScheduledTaskTakesNothingWhenThePeriodIsAlreadyCovered(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()
	p, _ := scheduledRun(t)

	// One a day for 14 days, in the every/for hours the plist carries. The
	// healthy scenario's newest snapshot is hours old, so today is already
	// covered and nothing should be taken.
	t.Setenv(envPolicy, "24/336")
	t.Setenv(envRetention, "336")

	before, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("the scenario has no snapshots, so there is nothing to be covered by")
	}

	if err := runScheduledSnapshot(ctx, runner, p); err != nil {
		t.Fatalf("the scheduled task failed: %v", err)
	}

	after, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("took a snapshot for a period that already had one: %d -> %d", len(before), len(after))
	}
}

// Reaping is the half that deletes, so the thing worth proving is that it stops:
// the window is respected and the run does not empty the volume.
func TestTheScheduledTaskReapsPastTheWindowAndNoFurther(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()
	p, dir := scheduledRun(t)

	before, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	// Everything the scenario has is this schedule's, which is what makes the
	// window the thing under test rather than the ownership.
	for _, snap := range before {
		if err := schedule.Manage(dir, snap.Stamp); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv(envPolicy, "")
	t.Setenv(envRetention, "24")

	if err := runScheduledSnapshot(ctx, runner, p); err != nil {
		t.Fatalf("the scheduled task failed: %v", err)
	}

	after, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) == 0 {
		t.Fatal("reaping removed every snapshot, including the one just taken")
	}
	oldest := time.Now().Add(-25 * time.Hour) // the window, plus an hour of slack
	for _, snap := range after {
		if snap.Taken.Before(oldest) {
			t.Errorf("%s is past the window and was kept", snap.Stamp)
		}
	}
}

// The guarantee this whole change exists for: a snapshot the schedule did not
// create is never deleted by it, however far outside the policy it falls.
//
// The old behaviour planned over every snapshot on the machine, so a hand-made
// one was reaped the next morning for sharing a period with a scheduled one. It
// had no way of knowing that would happen, and nothing said so afterwards.
func TestTheScheduledTaskNeverReapsWhatItDidNotCreate(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()
	p, _ := scheduledRun(t)

	before, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) < 2 {
		t.Fatalf("the scenario has %d snapshots; this needs several outside the window", len(before))
	}

	// A one-hour window, which puts nearly every snapshot the scenario has well
	// outside it. None of them is managed, so none may be touched.
	t.Setenv(envPolicy, "")
	t.Setenv(envRetention, "1")

	if err := runScheduledSnapshot(ctx, runner, p); err != nil {
		t.Fatalf("the scheduled task failed: %v", err)
	}

	after, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]bool{}
	for _, snap := range after {
		kept[snap.Stamp] = true
	}
	for _, snap := range before {
		if !kept[snap.Stamp] {
			t.Errorf("%s was not this schedule's and was deleted anyway", snap.Stamp)
		}
	}
}

// An unreadable policy must reap nothing rather than reap on a guess, and must
// still take a snapshot. Keeping too much is fixed by the next run; deleting too
// much cannot be fixed at all, and neither can failing to protect the disk.
func TestAnUnreadablePolicyReapsNothing(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()
	p, dir := scheduledRun(t)

	before, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	// Managed, so that "nothing was reaped" is the policy being refused rather
	// than the ownership rule quietly covering for it.
	for _, snap := range before {
		if err := schedule.Manage(dir, snap.Stamp); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv(envPolicy, "a-policy-from-a-later-version")
	t.Setenv(envRetention, "24")

	if err := runScheduledSnapshot(ctx, runner, p); err != nil {
		t.Fatalf("an unreadable policy stopped the task entirely: %v", err)
	}

	after, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	// One added, none removed.
	if len(after) != len(before)+1 {
		t.Errorf("an unreadable policy deleted snapshots: %d -> %d", len(before), len(after))
	}
}

// The tripwire runs until it is stopped. What is checked here is that stopping
// works — an agent that ignores its context is an agent launchd has to kill.
func TestTheTripwireStopsWhenItIsToldTo(t *testing.T) {
	runner := agentRunner(t, "healthy")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- runWatch(ctx, runner) }()

	select {
	case <-done: // returned, which is all that is being asked
	case <-time.After(10 * time.Second):
		t.Fatal("the tripwire ignored a cancelled context")
	}
}
