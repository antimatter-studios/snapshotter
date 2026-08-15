package main

import (
	"context"
	"testing"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/scenario"
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

func TestTheScheduledTaskTakesASnapshot(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()

	before, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}

	// No policy means the flat window built from the retention hours. Ten years
	// of it, so nothing is due for pruning: this test is about the taking, and the
	// pruning has its own.
	t.Setenv(envPolicy, "")
	t.Setenv(envRetention, "87600")

	if err := runScheduledSnapshot(ctx, runner); err != nil {
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

// Pruning is the half that deletes, so the thing worth proving is that it stops:
// the retention window is respected and the run does not empty the volume.
func TestTheScheduledTaskPrunesPastTheRetentionWindowAndNoFurther(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()

	t.Setenv(envPolicy, "")
	t.Setenv(envRetention, "24")

	if err := runScheduledSnapshot(ctx, runner); err != nil {
		t.Fatalf("the scheduled task failed: %v", err)
	}

	after, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) == 0 {
		t.Fatal("pruning removed every snapshot, including the one just taken")
	}
	oldest := time.Now().Add(-25 * time.Hour) // the window, plus an hour of slack
	for _, snap := range after {
		if snap.Taken.Before(oldest) {
			t.Errorf("%s is past the retention window and was kept", snap.Stamp)
		}
	}
}

// An unreadable policy must prune nothing rather than prune on a guess. Keeping
// too much is fixed by the next run; deleting too much cannot be fixed at all.
func TestAnUnreadablePolicyPrunesNothing(t *testing.T) {
	runner := agentRunner(t, "healthy")
	ctx := context.Background()

	before, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(envPolicy, "a-policy-from-a-later-version")
	t.Setenv(envRetention, "24")

	if err := runScheduledSnapshot(ctx, runner); err != nil {
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
