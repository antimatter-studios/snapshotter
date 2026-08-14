package scenario

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"snapshotter/internal/apfs"
)

// startedAt is a fixed clock. Every time in a scenario is relative, so the
// instant they are measured from only has to be stable.
func startedAt() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.Local) }

func fixedClock() func() time.Time { return startedAt }

func mustNew(t *testing.T, spec Spec) *Scenario {
	t.Helper()
	sc, err := New(spec, Options{Now: fixedClock()})
	if err != nil {
		t.Fatalf("New(%s): %v", spec.Name, err)
	}
	return sc
}

func mustLoad(t *testing.T, name string) Spec {
	t.Helper()
	spec, err := Load(name)
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	return spec
}

// The fake's entire justification is that the real parsers do the parsing. If its
// output only parsed through a parser written to match it, a mistake about
// tmutil's format would be invisible here and fatal in production — which is
// exactly what happened with the listing header and the container NOTE.
func TestBuiltInListingsParseThroughTheRealParser(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			spec := mustLoad(t, name)
			sc := mustNew(t, spec)

			snaps, err := apfs.List(context.Background(), sc.Runner, apfs.DataVolume)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(snaps) != len(spec.Snapshots) {
				t.Fatalf("the scenario asks for %d snapshots and %d parsed out", len(spec.Snapshots), len(snaps))
			}

			// List promises newest first, and the fake emits oldest first as
			// tmutil does, so this also proves the sort is doing something.
			for i := 1; i < len(snaps); i++ {
				if !snaps[i-1].Taken.After(snaps[i].Taken) {
					t.Fatalf("snapshot %d is not newer than %d: %s then %s", i-1, i, snaps[i-1].Stamp, snaps[i].Stamp)
				}
			}

			// Each snapshot has to land at the age the scenario asked for, or a
			// screen driven into "the newest snapshot is three days old" would be
			// showing something else. Newest first means youngest first, whatever
			// order the spec happens to list them in.
			ages := make([]time.Duration, 0, len(spec.Snapshots))
			for _, snap := range spec.Snapshots {
				ages = append(ages, snap.Age.Duration())
			}
			sort.Slice(ages, func(i, j int) bool { return ages[i] < ages[j] })
			for i, snap := range snaps {
				if got := startedAt().Sub(snap.Taken); got != ages[i] {
					t.Errorf("snapshot %d is %s old, want %s", i, got, ages[i])
				}
			}
		})
	}
}

// The header is the trap this listing has: anything that takes it for a snapshot
// name produces an "is not a valid disk" error much further downstream. So the
// fake emits it, and the real filter has to keep removing it.
func TestTheListingHeaderIsEmittedAndFiltered(t *testing.T) {
	sc := mustNew(t, mustLoad(t, "healthy"))

	out, err := sc.Runner.Run(context.Background(), "tmutil", "listlocalsnapshots", apfs.DataVolume)
	if err != nil {
		t.Fatalf("listlocalsnapshots: %v", err)
	}
	if want := "Snapshots for disk " + apfs.DataVolume + ":"; !strings.HasPrefix(out, want) {
		t.Fatalf("the listing does not start with %q:\n%s", want, out)
	}
	if strings.Count(out, "\n") != len(sc.Spec.Snapshots)+1 {
		t.Errorf("the listing has %d lines for %d snapshots plus a header", strings.Count(out, "\n"), len(sc.Spec.Snapshots))
	}
}

func TestDetailsCarryTheFlagsDiskutilWouldReport(t *testing.T) {
	no := false
	spec := Spec{
		Name: "flags",
		Snapshots: []SnapshotSpec{
			{Age: Span(time.Hour), Purgeable: &no},
			{Age: Span(6 * time.Hour)},
			{Age: Span(12 * time.Hour), LimitsContainer: true},
		},
	}
	sc := mustNew(t, spec)

	details, err := apfs.Details(context.Background(), sc.Runner, apfs.DataVolume)
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	if len(details) != 3 {
		t.Fatalf("got %d details, want 3: %v", len(details), details)
	}

	byAge := func(age time.Duration) apfs.Detail {
		t.Helper()
		stamp := startedAt().Add(-age).Format(stampLayout)
		name, err := apfs.NameForStamp(stamp)
		if err != nil {
			t.Fatal(err)
		}
		return details[name]
	}

	if byAge(time.Hour).Purgeable {
		t.Error("a snapshot marked not purgeable parsed as purgeable")
	}
	// Omitting the flag has to mean purgeable, because every Time Machine local
	// snapshot on a real machine is, and a scenario that had to say so every time
	// would mostly be saying so.
	if !byAge(6 * time.Hour).Purgeable {
		t.Error("a snapshot with no purgeable field parsed as not purgeable")
	}
	if !byAge(12 * time.Hour).LimitsContainer {
		t.Error("the container NOTE did not reach the snapshot it was written for")
	}
	// The NOTE belongs to one block. Leaking it forward would name the wrong
	// snapshot as the one worth deleting first.
	if byAge(time.Hour).LimitsContainer || byAge(6*time.Hour).LimitsContainer {
		t.Error("the container NOTE leaked into another block")
	}
}

// diskutil indents the last block with spaces where the others use a pipe. A fake
// that emitted the tidy form would let a parser handling only piped lines pass
// here and lose the newest snapshot's flags for real.
func TestDetailsReproduceDiskutilsLastBlockIndentation(t *testing.T) {
	sc := mustNew(t, mustLoad(t, "healthy"))

	out, err := sc.Runner.Run(context.Background(), "diskutil", "apfs", "listSnapshots", apfs.DataVolume)
	if err != nil {
		t.Fatalf("listSnapshots: %v", err)
	}
	if !strings.Contains(out, "|   Name:") {
		t.Error("no block is drawn with the pipe diskutil uses")
	}
	if !strings.Contains(out, "\n    Name:") {
		t.Errorf("the last block is not indented with spaces:\n%s", out)
	}
}

// A fixture would answer the same thing forever. The application takes snapshots
// from the menu bar and prunes them on a schedule, so a scenario that could not
// be driven onwards would only cover the first screen.
func TestTakingASnapshotAddsOneToTheListing(t *testing.T) {
	ctx := context.Background()
	sc := mustNew(t, mustLoad(t, "empty"))

	before, err := apfs.List(ctx, sc.Runner, apfs.DataVolume)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("the empty scenario has %d snapshots", len(before))
	}

	snap, err := apfs.Create(ctx, sc.Runner)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if snap.Stamp != startedAt().Format(stampLayout) {
		t.Errorf("the new snapshot is stamped %s, want the current time %s", snap.Stamp, startedAt().Format(stampLayout))
	}

	after, err := apfs.List(ctx, sc.Runner, apfs.DataVolume)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(after) != 1 || after[0].Stamp != snap.Stamp {
		t.Fatalf("after creating %s the listing is %+v", snap.Stamp, after)
	}
}

// Prune is the destructive half of the scheduled task, and the fake has to let it
// be wrong here rather than on a real machine.
func TestPruneDeletesOnlyWhatIsPastTheWindow(t *testing.T) {
	ctx := context.Background()
	sc := mustNew(t, Spec{
		Name: "pruning",
		Snapshots: []SnapshotSpec{
			{Age: Span(time.Hour)},
			{Age: Span(13 * 24 * time.Hour)},
			{Age: Span(20 * 24 * time.Hour)},
		},
	})

	pruned, err := apfs.Prune(ctx, sc.Runner, apfs.DataVolume, 14*24*time.Hour, startedAt())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(pruned) != 1 {
		t.Fatalf("pruned %d snapshots, want the one past the window: %+v", len(pruned), pruned)
	}
	if want := startedAt().Add(-20 * 24 * time.Hour).Format(stampLayout); pruned[0].Stamp != want {
		t.Errorf("pruned %s, want %s", pruned[0].Stamp, want)
	}

	left, err := apfs.List(ctx, sc.Runner, apfs.DataVolume)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(left) != 2 {
		t.Errorf("%d snapshots left, want 2: %+v", len(left), left)
	}
}

func TestDeletingSomethingThatIsNotThereFails(t *testing.T) {
	sc := mustNew(t, mustLoad(t, "empty"))
	if err := apfs.Delete(context.Background(), sc.Runner, "2020-01-01-000000"); err == nil {
		t.Fatal("deleting a snapshot that does not exist succeeded")
	}
}

func TestDestinationInfoAnswersEitherWay(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"healthy", false},
		{"time-machine", true},
	} {
		sc := mustNew(t, mustLoad(t, tc.name))
		if got := apfs.DestinationInfo(context.Background(), sc.Runner); got.HasDestination != tc.want {
			t.Errorf("%s: HasDestination = %v, want %v (detail %q)", tc.name, got.HasDestination, tc.want, got.Detail)
		}
	}
}

// An unmodelled command must fail rather than return empty success. Empty output
// parses as "no snapshots" or "nothing configured", so a silent answer would look
// like a finding about the machine instead of a gap in the fake.
func TestAnUnmodelledCommandIsRefused(t *testing.T) {
	sc := mustNew(t, mustLoad(t, "healthy"))
	for _, cmd := range [][]string{
		{"tmutil", "thinlocalsnapshots", "/"},
		{"diskutil", "eraseVolume"},
		{"rm", "-rf", "/"},
	} {
		out, err := sc.Runner.Run(context.Background(), cmd[0], cmd[1:]...)
		if err == nil {
			t.Errorf("%v was answered with %q instead of refused", cmd, out)
		}
	}

	// Whatever was asked has to be recoverable afterwards, because "the scenario
	// answered the whole machine" is only checkable against the list of what was
	// asked of it.
	if got := len(sc.Runner.commandsRun()); got != 3 {
		t.Errorf("%d commands recorded, want 3", got)
	}
}
