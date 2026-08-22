package services

import (
	"context"
	"strings"
	"testing"

	"snapshotter/internal/config"
)

// Restoring what an upgrade removed. Both agents, independently.
//
// A launchd job is not durable in the way people assume: Homebrew's cask unloads
// both agents before staging a new version, so the settings file is the intent and
// launchd is the current state, reconciled at startup.
//
// The two used to share a single early return, which quietly made them one
// protection instead of two. A policy identifier the settings file named and the
// build no longer knew — exactly what renaming a preset produces — failed the
// schedule and then skipped the watcher entirely, though the watcher has no policy
// and nothing was wrong with it. One rename took both protections off a machine,
// and the only trace was a line in a log file.

// settingsWritten puts a settings file where the stack will read it.
//
// Called after newStack, which points XDG_CONFIG_HOME at a directory of its own so
// a test never reads or writes the settings of whoever is running it. Writing
// first would be silently undone by that.
//
// Written through config.Save rather than as literal YAML: the file's shape is the
// serializer's business, and a hand-written one that quietly failed to parse would
// leave every assertion below testing the defaults instead.
func settingsWritten(t *testing.T, edit func(*config.Config)) {
	t.Helper()

	cfg := config.Defaults()
	edit(&cfg)
	if err := config.Save(cfg); err != nil {
		t.Fatalf("saving settings: %v", err)
	}

	// Proof the file is the one being read, so a test that stopped reaching it
	// fails here rather than passing against the defaults.
	back, err := config.Load()
	if err != nil {
		t.Fatalf("reading the settings back: %v", err)
	}
	if back.Schedule.Policy != cfg.Schedule.Policy || back.Tripwire.Enabled != cfg.Tripwire.Enabled {
		t.Fatalf("the settings written are not the settings read back: %+v", back)
	}
}

func TestAFailedScheduleRestoreStillPutsTheWatcherBack(t *testing.T) {
	// A policy name no build knows, which is what a settings file written by a
	// version with different preset names looks like from here.
	s := newStack(t, "empty")
	settingsWritten(t, func(c *config.Config) {
		c.Schedule.Enabled = true
		c.Schedule.IntervalHours = 3
		c.Schedule.RetentionDays = 14
		c.Schedule.Policy = "tiered-every-third-tuesday"
		c.Tripwire.Enabled = true
	})
	ctx := context.Background()

	restored, err := s.Schedule.Restore(ctx)

	// It has to say the schedule could not be put back.
	if err == nil {
		t.Fatal("an unresolvable policy restored successfully")
	}
	if restored.Schedule {
		t.Error("it claims to have restored a schedule it could not build")
	}
	// And it has to have put the watcher back anyway. This is the whole point: the
	// watcher is what notices a folder being emptied, and it has nothing to do with
	// retention policies.
	if !restored.Tripwire {
		t.Error("the watcher was skipped because the schedule failed")
	}
	if st, statusErr := s.Schedule.TripwireStatus(ctx); statusErr != nil || !st.Installed {
		t.Errorf("the watcher is not installed: %v", statusErr)
	}
}

func TestBothFailuresAreReportedRatherThanTheFirst(t *testing.T) {
	s := newStack(t, "empty")
	settingsWritten(t, func(c *config.Config) {
		c.Schedule.Enabled = true
		c.Schedule.IntervalHours = 3
		c.Schedule.RetentionDays = 14
		c.Schedule.Policy = "tiered-nonsense"
		c.Tripwire.Enabled = false
	})

	_, err := s.Schedule.Restore(context.Background())
	if err == nil {
		t.Fatal("no failure was reported")
	}
	// Named, so the notification can say which protection is missing rather than
	// "something went wrong".
	if !strings.Contains(err.Error(), "schedule") {
		t.Errorf("the failure does not say what could not be restored: %v", err)
	}
}

// The identifier this whole cascade was triggered by. A settings file written
// before the presets were renamed has to restore, or every upgrade from an older
// version leaves the machine unprotected.
func TestASettingsFileFromBeforeTheRenameRestores(t *testing.T) {
	s := newStack(t, "empty")
	settingsWritten(t, func(c *config.Config) {
		c.Schedule.Enabled = true
		c.Schedule.IntervalHours = 3
		c.Schedule.RetentionDays = 14
		c.Schedule.Policy = "tiered-52-weeks"
		c.Tripwire.Enabled = true
	})
	ctx := context.Background()

	restored, err := s.Schedule.Restore(ctx)
	if err != nil {
		t.Fatalf("a settings file from before the rename did not restore: %v", err)
	}
	if !restored.Schedule || !restored.Tripwire {
		t.Errorf("restored schedule=%v watcher=%v, wanted both", restored.Schedule, restored.Tripwire)
	}

	// And it restored the policy that name used to mean, not a default.
	view, err := s.Schedule.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.PolicyID != "tiered-weekly-monthly" {
		t.Errorf("the restored policy is %q, want tiered-weekly-monthly", view.PolicyID)
	}
	if view.IntervalHours != 3 {
		t.Errorf("the restored interval is %v, want 3", view.IntervalHours)
	}
}

func TestNothingIsRestoredThatWasDeliberatelyTurnedOff(t *testing.T) {
	// Restore only ever adds. Somebody who removed their schedule on purpose set
	// enabled to false, and putting it back would be the application overruling
	// them once per launch.
	s := newStack(t, "empty")
	settingsWritten(t, func(c *config.Config) {
		c.Schedule.Enabled = false
		c.Tripwire.Enabled = false
	})

	restored, err := s.Schedule.Restore(context.Background())
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Any() {
		t.Errorf("it installed something nobody asked for: %+v", restored)
	}
}
