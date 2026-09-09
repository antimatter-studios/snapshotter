package schedule

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"snapshotter/internal/apfs"
)

// Managed and unmanaged snapshots.
//
// The distinction exists because a snapshot somebody took by hand is a
// deliberate act, and the schedule used to eat one the next morning for sharing
// a day with its own. Everything here is about the deleting half being narrow:
// the schedule reaps what it created and nothing else, and every way of failing
// to know what it created has to fall towards keeping.

func TestAMissingRecordManagesNothing(t *testing.T) {
	if got := Managed(t.TempDir()); len(got) != 0 {
		t.Errorf("got %v, want nothing managed", got)
	}
}

func TestARecordedStampIsManaged(t *testing.T) {
	dir := t.TempDir()
	if err := Manage(dir, "2026-09-09-061633"); err != nil {
		t.Fatal(err)
	}
	if err := Manage(dir, "2026-09-10-061700"); err != nil {
		t.Fatal(err)
	}

	managed := Managed(dir)
	if !managed["2026-09-09-061633"] || !managed["2026-09-10-061700"] {
		t.Errorf("got %v, want both recorded", managed)
	}
	if managed["2026-09-09-130736"] {
		t.Error("a stamp nobody recorded came back as managed")
	}
}

// A stamp read back from the record is an argument to a command that deletes, so
// it is checked at the point of use rather than trusted from its source. A
// truncated write or a hand-edited file must not turn a partial line into one.
func TestOnlyRealStampsAreEverManaged(t *testing.T) {
	dir := t.TempDir()
	body := "2026-09-09-061633\n" + // good
		"2026-09-09\n" + // truncated by a crash midway
		"; rm -rf /\n" + // hand-edited into something else entirely
		"2026-13-99-999999\n" + // right shape, impossible date
		"\n" +
		"2026-09-10-061700\n" // good
	if err := os.WriteFile(ManagedPath(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	managed := Managed(dir)
	if len(managed) != 3 {
		t.Errorf("got %d managed, want the three stamp-shaped lines: %v", len(managed), managed)
	}
	for stamp := range managed {
		if !apfs.IsStamp(stamp) {
			t.Errorf("%q reached the managed set", stamp)
		}
	}

	// Manage refuses at the writing end too, so the file does not acquire one.
	if err := Manage(dir, "../../etc/passwd"); err == nil {
		t.Error("recorded something that is not a date stamp")
	}
}

// The record must converge on reality, or it grows forever and eventually claims
// a date some future snapshot reuses.
func TestForgettingDropsStampsThatAreGone(t *testing.T) {
	dir := t.TempDir()
	for _, s := range []string{"2026-09-01-100000", "2026-09-09-061633", "2026-09-10-061700"} {
		if err := Manage(dir, s); err != nil {
			t.Fatal(err)
		}
	}

	if err := Forget(dir, map[string]bool{"2026-09-10-061700": true}); err != nil {
		t.Fatal(err)
	}
	managed := Managed(dir)
	if len(managed) != 1 || !managed["2026-09-10-061700"] {
		t.Errorf("got %v, want only the stamp that still exists", managed)
	}
}

// Adopting from the log is what stops an upgrade silently ending the thinning:
// without it every snapshot already on disk would be unmanaged and permanent.
//
// It is exact rather than a guess. The scheduled task logs "created <stamp>";
// the tripwire writes to a log of its own and the window writes to neither.
func TestAdoptingTakesTheScheduledSnapshotsOutOfTheLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "snapshotter.log")
	body := "2026/09/08 05:32:01 created 2026-09-08-053150\n" +
		"2026/09/08 05:32:12 reaped 2026-09-07-223728\n" +
		"2026/09/08 05:32:13 holding 1 snapshots on /System/Volumes/Data, aiming for one a day\n" +
		"2026/09/09 06:17:45 created 2026-09-09-061633\n"
	if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Adopt(dir, logPath); err != nil {
		t.Fatal(err)
	}
	managed := Managed(dir)
	if !managed["2026-09-08-053150"] || !managed["2026-09-09-061633"] {
		t.Errorf("got %v, want both created stamps", managed)
	}
	// The reaped one is named on a line of its own and is not something this
	// schedule now holds. Adopting it would be harmless — it is gone — but it
	// would also be wrong, and Forget would drop it on the first run anyway.
	if managed["2026-09-07-223728"] {
		t.Error("adopted a stamp the log names as reaped, not created")
	}
	if len(managed) != 2 {
		t.Errorf("got %d managed, want 2: %v", len(managed), managed)
	}
}

// A camera press does not reach that log, which is exactly why the log can be
// trusted to say what the schedule made.
func TestAdoptingLeavesASnapshotTheScheduleDidNotLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "snapshotter.log")
	if err := os.WriteFile(logPath, []byte("2026/09/09 06:17:45 created 2026-09-09-061633\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Adopt(dir, logPath); err != nil {
		t.Fatal(err)
	}
	if Managed(dir)["2026-09-09-130736"] {
		t.Error("a hand-made snapshot was adopted as the schedule's")
	}
}

// Adopting does not overwrite a record that already exists, or an upgrade would
// re-adopt on every run and re-manage snapshots Forget had deliberately dropped.
func TestAdoptingOnlyHappensOnce(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "snapshotter.log")
	if err := os.WriteFile(logPath, []byte("2026/09/01 00:00:00 created 2026-09-01-100000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Manage(dir, "2026-09-09-061633"); err != nil {
		t.Fatal(err)
	}

	if err := Adopt(dir, logPath); err != nil {
		t.Fatal(err)
	}
	managed := Managed(dir)
	if managed["2026-09-01-100000"] {
		t.Error("adopting overwrote an existing record")
	}
	if !managed["2026-09-09-061633"] {
		t.Error("adopting lost what was already recorded")
	}
}

// No log is nothing to adopt, not a failure. An empty record manages nothing,
// which is the safe end.
func TestAdoptingWithoutALogIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	if err := Adopt(dir, filepath.Join(t.TempDir(), "absent.log")); err != nil {
		t.Fatalf("a missing log was treated as a failure: %v", err)
	}
	if len(Managed(dir)) != 0 {
		t.Error("something was adopted from a log that does not exist")
	}
}

// Coverage: the question is whether this period has a restore point, and one
// somebody took themselves answers it as well as one the schedule took.
func TestAnyoneSnapshotCoversThePeriod(t *testing.T) {
	now := time.Date(2026, 9, 9, 6, 16, 0, 0, time.Local)
	daily := Policy{Tiers: []Tier{{Every: 24 * time.Hour, For: 14 * 24 * time.Hour}}}

	mine := apfs.Snapshot{Stamp: "2026-09-09-060000", Taken: now.Add(-16 * time.Minute)}
	if got, ok := Covering([]apfs.Snapshot{mine}, daily, now); !ok || got.Stamp != mine.Stamp {
		t.Errorf("got %v/%v, want the snapshot taken sixteen minutes ago to cover today", got.Stamp, ok)
	}

	// Yesterday's does not cover today, so a snapshot is due.
	old := apfs.Snapshot{Stamp: "2026-09-08-060000", Taken: now.Add(-24 * time.Hour)}
	if _, ok := Covering([]apfs.Snapshot{old}, daily, now); ok {
		t.Error("yesterday's snapshot was treated as covering today")
	}
}

// A policy with no usable tier covers nothing, so a snapshot is taken. An
// unreadable policy must not become a reason to stop protecting the disk.
func TestAnEmptyPolicyCoversNothing(t *testing.T) {
	now := time.Now()
	snap := apfs.Snapshot{Stamp: "2026-09-09-060000", Taken: now}
	if _, ok := Covering([]apfs.Snapshot{snap}, Policy{}, now); ok {
		t.Error("an empty policy reported the period as covered, so nothing would be taken")
	}
	// A keep-everything tier states no period either.
	if _, ok := Covering([]apfs.Snapshot{snap}, FlatPolicy(14*24*time.Hour), now); ok {
		t.Error("a flat policy reported the period as covered")
	}
}

// reapRunner answers the two commands reaping needs and records the deletions.
type reapRunner struct {
	vols    string
	deleted []string
}

func (r *reapRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "tmutil" && len(args) == 2 && args[0] == "deletelocalsnapshots" {
		r.deleted = append(r.deleted, args[1])
		return "Deleted local snapshot " + args[1] + "\n", nil
	}
	return r.vols, nil
}

// The guarantee, at the level below the scheduled task: reaping plans over the
// managed subset and cannot reach anything else, however far outside the policy
// it falls.
func TestReapingTouchesOnlyManagedSnapshots(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.Local)

	stamp := func(s string) apfs.Snapshot {
		taken, err := time.ParseInLocation("2006-01-02-150405", s, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return apfs.Snapshot{Stamp: s, Taken: taken}
	}
	existing := []apfs.Snapshot{
		stamp("2026-09-09-061633"), // managed, inside the window
		stamp("2026-09-09-130736"), // NOT managed, and would be pruned if it were
		stamp("2026-08-01-100000"), // NOT managed, far outside any window
		stamp("2026-08-02-100000"), // managed, far outside the window
	}
	for _, s := range []string{"2026-09-09-061633", "2026-08-02-100000"} {
		if err := Manage(dir, s); err != nil {
			t.Fatal(err)
		}
	}

	r := &reapRunner{}
	deleted, err := ReapManaged(context.Background(), r, dir, existing, FlatPolicy(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.deleted) != 1 || r.deleted[0] != "2026-08-02-100000" {
		t.Errorf("deleted %v, want only the managed snapshot past the window", r.deleted)
	}
	if len(deleted) != 1 {
		t.Errorf("reported %d reaped, want 1", len(deleted))
	}
	// And the record now says only what still exists.
	managed := Managed(dir)
	if managed["2026-08-02-100000"] {
		t.Error("the record still claims a snapshot it deleted")
	}
	if !managed["2026-09-09-061633"] {
		t.Error("the record lost a managed snapshot that is still there")
	}
}
