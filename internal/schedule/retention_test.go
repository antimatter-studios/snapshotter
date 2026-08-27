package schedule

import (
	"context"
	"fmt"
	"math/rand"
	"snapshotter/internal/i18n"
	"strings"
	"testing"
	"time"

	"snapshotter/internal/apfs"
)

// planNow is fixed so every expectation below is arithmetic rather than a
// guess, and local because tmutil writes its stamps in the machine's own zone —
// a snapshot built here and read back through apfs.ParseName has to survive the
// round trip.
var planNow = time.Date(2026, 8, 14, 12, 0, 0, 0, time.Local)

// snapshotAt builds the snapshot tmutil would have written at that instant. The
// stamp layout is tmutil's, and it is spelled out here rather than borrowed
// because the point of the test is to feed Plan what a real listing produces.
func snapshotAt(t time.Time) apfs.Snapshot {
	stamp := t.Format("2006-01-02-150405")
	return apfs.Snapshot{Name: "com.apple.TimeMachine." + stamp + ".local", Stamp: stamp, Taken: t}
}

// history is count snapshots taken every interval up to now, newest first —
// the order apfs.List hands over.
func history(now time.Time, interval time.Duration, count int) []apfs.Snapshot {
	snaps := make([]apfs.Snapshot, 0, count)
	for i := 0; i < count; i++ {
		snaps = append(snaps, snapshotAt(now.Add(-time.Duration(i)*interval)))
	}
	return snaps
}

func stamps(snaps []apfs.Snapshot) []string {
	out := make([]string, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, s.Stamp)
	}
	return out
}

// tierIndex is which band governs a snapshot of this age, or -1 for one the
// policy no longer reaches. The tests use it to ask whether a snapshot's
// situation changed between two plans.
func tierIndex(tiers []Tier, age time.Duration) int {
	for i, t := range tiers {
		if age <= t.For {
			return i
		}
	}
	return -1
}

// The machine must never be left without a restore point. A retention policy is
// a setting, and no setting a user can choose is worth the state this
// application exists to prevent.
func TestPlanKeepsTheNewestEvenWhenThePolicyWouldPruneEverything(t *testing.T) {
	// Every snapshot is months past a one-hour window.
	snaps := history(planNow.Add(-90*day), 6*time.Hour, 20)

	keep, prune := Plan(snaps, FlatPolicy(time.Hour), planNow)
	if len(keep) != 1 {
		t.Fatalf("kept %d snapshots, want exactly the newest: %v", len(keep), stamps(keep))
	}
	if keep[0].Stamp != snaps[0].Stamp {
		t.Errorf("kept %s, want the newest %s", keep[0].Stamp, snaps[0].Stamp)
	}
	if len(prune) != len(snaps)-1 {
		t.Errorf("pruned %d of %d", len(prune), len(snaps))
	}
}

func TestPlanOnNothingPlansNothing(t *testing.T) {
	for _, snaps := range [][]apfs.Snapshot{nil, {}} {
		keep, prune := Plan(snaps, Presets(6*time.Hour, 14*day)[0].Policy, planNow)
		if len(keep) != 0 || len(prune) != 0 {
			t.Errorf("kept %d and pruned %d from an empty set", len(keep), len(prune))
		}
	}
}

func TestPlanWithASingleSnapshotKeepsIt(t *testing.T) {
	// Older than the policy reaches, so only the newest-snapshot rule saves it.
	snaps := []apfs.Snapshot{snapshotAt(planNow.Add(-5 * 365 * day))}

	keep, prune := Plan(snaps, Presets(6*time.Hour, 14*day)[0].Policy, planNow)
	if len(keep) != 1 || len(prune) != 0 {
		t.Fatalf("kept %v, pruned %v", stamps(keep), stamps(prune))
	}
}

// The flat window has to stay expressible to the boundary, or every schedule
// installed before tiering quietly changes meaning on the next run. apfs.Prune
// keeps a snapshot unless Taken is before now-retain, and so must this.
func TestFlatPolicyMatchesTheWindowItReplaces(t *testing.T) {
	const retain = 24 * time.Hour
	snaps := history(planNow, time.Hour, 40)

	keep, prune := Plan(snaps, FlatPolicy(retain), planNow)

	cutoff := planNow.Add(-retain)
	for _, s := range keep {
		if s.Taken.Before(cutoff) {
			t.Errorf("kept %s, which the flat window would have pruned", s.Stamp)
		}
	}
	for _, s := range prune {
		if !s.Taken.Before(cutoff) {
			t.Errorf("pruned %s, which the flat window would have kept", s.Stamp)
		}
	}
	// The snapshot sitting exactly on the boundary is the one worth naming: 25
	// snapshots at hourly spacing are inside a 24-hour window, not 24, because a
	// tie is broken toward keeping.
	if len(keep) != 25 {
		t.Errorf("kept %d, want 25 including the one exactly on the cutoff", len(keep))
	}
}

// Which snapshot a bucket keeps decides whether the kept set holds still. The
// oldest is fixed the moment the bucket's first snapshot exists; the newest
// changes every time another arrives, so keeping the newest would delete
// yesterday's keeper today for no reason but that a newer one turned up.
func TestPlanKeepsTheOldestSnapshotInEachBucket(t *testing.T) {
	// Four snapshots inside one 24-hour bucket, plus a recent one so that the
	// rule keeping the newest whatever happens is not what is being measured.
	base := planNow.Add(-5 * day).Truncate(day)
	group := []apfs.Snapshot{
		snapshotAt(base.Add(20 * time.Hour)),
		snapshotAt(base.Add(14 * time.Hour)),
		snapshotAt(base.Add(8 * time.Hour)),
		snapshotAt(base.Add(2 * time.Hour)),
	}
	snaps := append([]apfs.Snapshot{snapshotAt(planNow)}, group...)
	policy := Policy{Tiers: []Tier{{Every: day, For: 30 * day}}}

	keep, _ := Plan(snaps, policy, planNow)
	if len(keep) != 2 {
		t.Fatalf("kept %v, want the newest plus one per bucket", stamps(keep))
	}
	if want := group[len(group)-1].Stamp; keep[1].Stamp != want {
		t.Errorf("kept %s from the bucket, want the oldest in it, %s", keep[1].Stamp, want)
	}
}

// Nothing about a snapshot changes as the clock moves, so nothing about the
// decision may either. Buckets are absolute for this reason: bucketing by age
// relative to now would shift every boundary between one run and the next.
func TestPlanIsStableWhileNothingChangesBand(t *testing.T) {
	policy := Presets(6*time.Hour, 14*day)[0].Policy
	// Spaced 6 hours but offset half an hour off the tier boundaries, so
	// advancing an hour moves nothing between bands.
	snaps := history(planNow.Add(-30*time.Minute), 6*time.Hour, 200)

	before, _ := Plan(snaps, policy, planNow)
	after, _ := Plan(snaps, policy, planNow.Add(time.Hour))

	tiers := policy.Bands()
	for _, s := range snaps {
		if a, b := tierIndex(tiers, planNow.Sub(s.Taken)), tierIndex(tiers, planNow.Add(time.Hour).Sub(s.Taken)); a != b {
			t.Fatalf("the fixture is wrong: %s changed band between the two plans", s.Stamp)
		}
	}
	if got, want := strings.Join(stamps(after), ","), strings.Join(stamps(before), ","); got != want {
		t.Errorf("an hour passing changed the plan\nbefore: %s\nafter:  %s", want, got)
	}
}

// The property that matters most, stated as the simulation a real machine runs:
// a snapshot must never be deleted by one plan having been kept by the last
// one, unless something about it changed — it aged into a coarser band, or out
// of the policy altogether. Anything else is the history rewriting itself.
func TestPlanNeverPrunesASnapshotForNoNewReason(t *testing.T) {
	policy := Presets(6*time.Hour, 14*day)[1].Policy
	tiers := policy.Bands()
	const interval = 6 * time.Hour
	steps := int((400 * day) / interval)

	now := planNow.Add(-400 * day)
	var live []apfs.Snapshot
	band := map[string]int{}

	for step := 0; step < steps; step++ {
		now = now.Add(interval)
		previous := now.Add(-interval)
		live = append([]apfs.Snapshot{snapshotAt(now)}, live...)

		keep, prune := Plan(live, policy, now)
		for _, s := range prune {
			was, kept := band[s.Stamp]
			if !kept {
				continue // pruned on the first plan that ever saw it
			}
			is := tierIndex(tiers, now.Sub(s.Taken))
			if is == was {
				t.Fatalf("pruned %s at %s for no new reason: it was kept at %s and is still in band %d",
					s.Stamp, now.Format(time.RFC3339), previous.Format(time.RFC3339), is)
			}
		}
		for k := range band {
			delete(band, k)
		}
		for _, s := range keep {
			band[s.Stamp] = tierIndex(tiers, now.Sub(s.Taken))
		}
		live = keep
	}

	// And the schedule is bounded and long-reaching, which is the point of it.
	// Bounded, but the bound is now a function of the window the preset opens
	// with: at six-hourly for a fortnight that band alone holds 57.
	if len(live) > 120 {
		t.Errorf("holding %d snapshots after 400 days, which is not thinning", len(live))
	}
	if reach := now.Sub(live[len(live)-1].Taken); reach < 300*day {
		t.Errorf("oldest surviving snapshot is only %s old", reach)
	}
}

// Pruning as it goes must land where planning the whole history once would,
// because otherwise what a machine holds depends on how often the scheduled task
// happened to run.
//
// It nearly does, and the gap is worth pinning down. Nesting means a coarse band
// keeps a snapshot the finer band was already keeping, so nothing is lost in the
// middle of a band. The one bucket that can differ is the one straddling a band
// boundary: planning the whole history sees a snapshot there whose older
// bucket-mate has since moved into the next band, and keeps it, where a machine
// pruning as it went had already deleted it. So the result is a subset, short by
// at most one snapshot per boundary — and never the other way round, which would
// mean deleting something a single plan would have held on to.
func TestPruningAsItGoesKeepsASubsetOfPlanningTheWholeHistory(t *testing.T) {
	policy := Presets(6*time.Hour, 14*day)[1].Policy
	const interval = 6 * time.Hour
	steps := int((400 * day) / interval)

	now := planNow.Add(-400 * day)
	var live []apfs.Snapshot
	for step := 0; step < steps; step++ {
		now = now.Add(interval)
		live = append([]apfs.Snapshot{snapshotAt(now)}, live...)
		live, _ = Plan(live, policy, now)
	}

	// The same snapshots the loop above saw, not one more. The extra one used to
	// be beyond every preset's reach and pruned either way; a preset's reach now
	// follows the window, and at fifty-two times a fortnight it outruns the four
	// hundred days simulated here, so the difference stopped cancelling.
	whole, _ := Plan(history(now, interval, steps), policy, now)
	inWhole := map[string]bool{}
	for _, s := range whole {
		inWhole[s.Stamp] = true
	}
	for _, s := range live {
		if !inWhole[s.Stamp] {
			t.Errorf("pruning as it went kept %s, which planning the whole history did not", s.Stamp)
		}
	}
	if gap := len(whole) - len(live); gap > len(policy.Bands()) {
		t.Errorf("pruning as it went holds %d fewer snapshots than one whole plan, more than one per band boundary\ngradual: %v\nwhole:   %v",
			gap, stamps(live), stamps(whole))
	}
}

// A realistic set: six-hourly snapshots over four months, thinned by the
// shortest preset. Each band is checked for what it promises rather than for a
// total, because a total can be right by accident.
func TestPlanThinsARealisticHistoryBandByBand(t *testing.T) {
	policy := Presets(6*time.Hour, 14*day)[0].Policy
	tiers := policy.Bands()
	const interval = 6 * time.Hour
	snaps := history(planNow, interval, int((120*day)/interval))

	keep, prune := Plan(snaps, policy, planNow)

	kept := map[string]bool{}
	for _, s := range keep {
		kept[s.Stamp] = true
	}

	// Band 0 keeps everything it covers; nothing past the horizon survives.
	for _, s := range snaps {
		age := planNow.Sub(s.Taken)
		switch tierIndex(tiers, age) {
		case 0:
			if !kept[s.Stamp] {
				t.Errorf("%s is %s old and inside the keep-everything band, but was pruned", s.Stamp, age)
			}
		case -1:
			if kept[s.Stamp] {
				t.Errorf("%s is %s old, past the %s horizon, and was kept", s.Stamp, age, policy.Horizon())
			}
		}
	}

	// The thinning bands keep exactly one snapshot per bucket, counting only
	// the snapshots that band governs.
	for i, tier := range tiers {
		if tier.Every <= 0 {
			continue
		}
		perBucket := map[int64]int{}
		for _, s := range snaps {
			if tierIndex(tiers, planNow.Sub(s.Taken)) != i || !kept[s.Stamp] {
				continue
			}
			perBucket[BucketStart(s.Taken, tier.Every)]++
		}
		if len(perBucket) == 0 {
			t.Errorf("band %d (one every %s) kept nothing at all", i, tier.Every)
		}
		for start, n := range perBucket {
			if n != 1 {
				t.Errorf("band %d kept %d snapshots in the bucket starting %s, want 1",
					i, n, time.Unix(0, start))
			}
		}
	}

	// The argument for tiering, as a number. Not against the flat fortnight — a
	// preset now opens with that fortnight, so it keeps everything the fortnight
	// does — but against a flat policy reaching as far, which is what someone is
	// choosing between.
	flatSameReach, _ := Plan(snaps, FlatPolicy(91*day), planNow)
	if len(keep) >= len(flatSameReach) {
		t.Errorf("tiered kept %d, flat 91 days kept %d — tiering bought nothing", len(keep), len(flatSameReach))
	}
	oldest := planNow.Sub(keep[len(keep)-1].Taken)
	if oldest < 88*day {
		t.Errorf("oldest kept snapshot is %s old, want the reach of a 13-week policy", oldest)
	}
	if len(keep)+len(prune) != len(snaps) {
		t.Errorf("plan lost snapshots: %d + %d != %d", len(keep), len(prune), len(snaps))
	}
}

// apfs.List returns newest first, but a plan that only worked on sorted input
// would fail by deleting the wrong snapshots, which is not a failure anyone
// notices in time.
func TestPlanDoesNotCareWhatOrderSnapshotsArriveIn(t *testing.T) {
	policy := Presets(6*time.Hour, 14*day)[0].Policy
	ordered := history(planNow, 6*time.Hour, 200)

	shuffled := append([]apfs.Snapshot(nil), ordered...)
	rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	want, _ := Plan(ordered, policy, planNow)
	got, _ := Plan(shuffled, policy, planNow)
	if a, b := strings.Join(stamps(got), ","), strings.Join(stamps(want), ","); a != b {
		t.Errorf("shuffled input planned differently\nwant: %s\ngot:  %s", b, a)
	}
	// And the result is newest first whatever came in, because the caller shows
	// it in that order.
	for i := 1; i < len(got); i++ {
		if got[i].Taken.After(got[i-1].Taken) {
			t.Fatalf("keep is not newest first at %d", i)
		}
	}
}

// The caller may still be displaying the slice it passed in.
func TestPlanDoesNotTouchTheCallersSlice(t *testing.T) {
	snaps := history(planNow, 6*time.Hour, 50)
	rand.New(rand.NewSource(2)).Shuffle(len(snaps), func(i, j int) {
		snaps[i], snaps[j] = snaps[j], snaps[i]
	})
	before := strings.Join(stamps(snaps), ",")

	Plan(snaps, Presets(6*time.Hour, 14*day)[0].Policy, planNow)

	if after := strings.Join(stamps(snaps), ","); after != before {
		t.Errorf("Plan reordered its argument\nbefore: %s\nafter:  %s", before, after)
	}
}

// A policy with nothing in it must read as "keep everything", never as "no band
// covers anything, so delete the lot". A zero value, a half-decoded plist or a
// typo would otherwise destroy the whole restore history.
func TestAnEmptyPolicyPrunesNothing(t *testing.T) {
	snaps := history(planNow, 6*time.Hour, 100)
	for _, policy := range []Policy{{}, {Tiers: []Tier{}}, {Tiers: []Tier{{Every: day, For: 0}}}} {
		keep, prune := Plan(snaps, policy, planNow)
		if len(prune) != 0 || len(keep) != len(snaps) {
			t.Errorf("policy %+v pruned %d of %d snapshots", policy, len(prune), len(snaps))
		}
	}
}

// A clock corrected backwards, or a machine carried between zones, leaves
// snapshots dated ahead of now. They are not old and must not be read as
// infinitely old.
func TestPlanKeepsASnapshotDatedInTheFuture(t *testing.T) {
	snaps := []apfs.Snapshot{
		snapshotAt(planNow.Add(3 * time.Hour)),
		snapshotAt(planNow.Add(-time.Hour)),
	}
	keep, prune := Plan(snaps, FlatPolicy(2*time.Hour), planNow)
	if len(prune) != 0 {
		t.Errorf("pruned %v; a snapshot in the future is not an old one", stamps(prune))
	}
	if len(keep) != 2 {
		t.Errorf("kept %v, want both", stamps(keep))
	}
}

// Nesting is what stops a coarser band re-choosing when a snapshot ages into it,
// and it only holds if each period divides the next. A calendar month would read
// better on the settings screen and would break it, which is why the longest
// band is four weeks.
func TestPresetPeriodsNest(t *testing.T) {
	for _, preset := range Presets(6*time.Hour, 14*day) {
		var previous time.Duration
		for _, tier := range preset.Policy.Bands() {
			if tier.Every <= 0 {
				continue
			}
			if previous > 0 && tier.Every%previous != 0 {
				t.Errorf("%s: a %s bucket does not divide into a %s one, so coarsening will re-choose",
					preset.ID, previous, tier.Every)
			}
			if tier.For%tier.Every != 0 {
				t.Errorf("%s: a %s band out of %s leaves a partial bucket at the far end",
					preset.ID, tier.Every, tier.For)
			}
			previous = tier.Every
		}
	}
}

// The plist is the source of truth for what launchd will do, so a policy has to
// survive the trip through it unchanged.
func TestPolicyEncodingRoundTrips(t *testing.T) {
	policies := []Policy{FlatPolicy(14 * day), FlatPolicy(36 * time.Hour)}
	for _, preset := range Presets(6*time.Hour, 14*day) {
		policies = append(policies, preset.Policy)
	}
	for _, want := range policies {
		text := want.String()
		got, ok := ParsePolicy(text)
		if !ok {
			t.Errorf("%q would not parse back", text)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%q parsed as %+v, want %+v", text, got.Bands(), want.Bands())
		}
	}
}

// Half a policy is a different policy, and the half most easily lost is the last
// band — the one holding the oldest snapshots.
func TestParsePolicyRefusesAnythingItCannotReadWhole(t *testing.T) {
	for _, junk := range []string{
		"", "   ", "banana", "336", "24/", "/336", "24/0", "24/-1", "-6/336",
		"all/336,junk", "all/336,24/", "24//336", "all/all",
	} {
		if p, ok := ParsePolicy(junk); ok {
			t.Errorf("%q parsed as %+v", junk, p.Bands())
		}
	}
	// "0" for a keep-everything band is the obvious thing to write by hand, so
	// it is accepted alongside "all".
	p, ok := ParsePolicy(" 0/48 , 24/336 ")
	if !ok {
		t.Fatal("refused a hand-written policy")
	}
	if !p.Equal(Policy{Tiers: []Tier{{For: 48 * time.Hour}, {Every: 24 * time.Hour, For: 336 * time.Hour}}}) {
		t.Errorf("parsed as %+v", p.Bands())
	}
}

// The count on the settings screen has to come from Plan itself. A formula over
// the bands would be a second implementation of the same rules, and the moment
// it disagreed it would be the one the user had believed.
func TestRetainedCountsWhatPlanWouldKeep(t *testing.T) {
	const interval = 6 * time.Hour
	for _, policy := range []Policy{FlatPolicy(14 * day), Presets(6*time.Hour, 14*day)[0].Policy, Presets(6*time.Hour, 14*day)[1].Policy} {
		snaps := history(planNow, interval, int(policy.Horizon()/interval)+1)
		keep, _ := Plan(snaps, policy, planNow)
		if got := Retained(policy, interval, planNow); got != len(keep) {
			t.Errorf("%s: Retained says %d, Plan keeps %d", policy, got, len(keep))
		}
	}
}

// What tiering is actually worth, stated so it is true at every interval rather
// than at one.
//
// A preset now opens with the window the person chose, so it keeps everything a
// flat policy of that window keeps and then some: it cannot cost less, and the
// old claim that it did was false at two of the five intervals even before this.
// The honest comparison is against a flat policy reaching as far — which is the
// choice someone actually faces, since reach is what they are buying.
func TestTieringCostsFarLessThanFlatAtTheSameReach(t *testing.T) {
	const window = 14 * day

	for _, interval := range []time.Duration{time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour} {
		for _, preset := range Presets(interval, window) {
			tiered := Retained(preset.Policy, interval, planNow)
			flatSameReach := Retained(FlatPolicy(preset.Policy.Horizon()), interval, planNow)
			flatSameWindow := Retained(FlatPolicy(window), interval, planNow)

			if tiered >= flatSameReach {
				t.Errorf("%s at %v: %d against %d for a flat policy of the same reach — tiering bought nothing",
					preset.ID, interval, tiered, flatSameReach)
			}
			if tiered < flatSameWindow {
				t.Errorf("%s at %v: %d against %d for the flat window it opens with — it must keep at least that",
					preset.ID, interval, tiered, flatSameWindow)
			}
			if preset.Policy.Horizon() <= window {
				t.Errorf("%s reaches %s, no further than the window", preset.ID, preset.Policy.Horizon())
			}
		}
	}
}

func TestHorizonIsTheOldestAgeKept(t *testing.T) {
	// Fifty-two times the window, so it follows the choice rather than being a
	// fixed year.
	if got := Presets(6*time.Hour, 14*day)[1].Policy.Horizon(); got != 52*14*day {
		t.Errorf("horizon %s, want 52 windows", got)
	}
	if got := (Policy{}).Horizon(); got != 0 {
		t.Errorf("an empty policy reaches %s, want zero", got)
	}
}

func TestIdentifyPolicyNamesWhatIsActuallyInstalled(t *testing.T) {
	if got := IdentifyPolicy(FlatPolicy(14 * day)); got != FlatID {
		t.Errorf("flat window identified as %q", got)
	}
	if got := IdentifyPolicy(Presets(6*time.Hour, 14*day)[0].Policy); got != Presets(6*time.Hour, 14*day)[0].ID {
		t.Errorf("preset identified as %q", got)
	}
	// A hand-edited plist is reported as what it is rather than as the nearest
	// preset, because the settings screen must not claim a policy is in force
	// when a different one is.
	odd := Policy{Tiers: []Tier{{Every: 0, For: 2 * day}, {Every: 3 * day, For: 40 * day}}}
	if got := IdentifyPolicy(odd); got != "custom" {
		t.Errorf("a hand-written policy identified as %q", got)
	}
}

// listingRunner answers the commands pruning uses and records the deletions, so
// the deleting can be checked without any snapshot existing.
//
// It answers for a machine with more than one APFS volume, because that is what
// a Mac with anything plugged into it is. `tmutil localsnapshot` takes no
// arguments and writes to all of them, and a fake that knew about only one was
// how a whole class of undeletable snapshot went unnoticed.
type listingRunner struct {
	// snaps are on the data volume.
	snaps []apfs.Snapshot
	// extra are on a second volume and NOT on the first, which is the shape that
	// used to be unprunable: macOS purges the data volume's copy under space
	// pressure, and the survivor elsewhere was never listed and so never deleted.
	extra   []apfs.Snapshot
	deleted []string
}

func (l *listingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	switch {
	case name == "mount":
		return "/dev/disk3s1 on /System/Volumes/Data (apfs, local, journaled)\n" +
			"/dev/disk8s1 on /Volumes/backup (apfs, local, journaled)\n" +
			"/dev/disk3s6 on /System/Volumes/VM (apfs, local, noexec, journaled)\n", nil

	case name == "diskutil" && len(args) == 3 && args[0] == "apfs" && args[1] == "listSnapshots":
		switch args[2] {
		case "/System/Volumes/Data":
			return snapshotListing("disk3s1", l.snaps), nil
		case "/Volumes/backup":
			return snapshotListing("disk8s1", append(append([]apfs.Snapshot{}, l.snaps...), l.extra...)), nil
		case "/System/Volumes/VM":
			return "No snapshots for disk3s6\n", nil
		}
		return "", fmt.Errorf("unexpected volume %q", args[2])

	case name == "tmutil" && len(args) > 0 && args[0] == "deletelocalsnapshots":
		l.deleted = append(l.deleted, args[1])
		return "", nil
	}
	return "", fmt.Errorf("unexpected command %s %v", name, args)
}

// snapshotListing renders diskutil's block-per-snapshot format, which is what
// the volume enumeration reads. tmutil's flat list names no volume, so there
// would be nothing to tell two volumes apart by.
func snapshotListing(device string, snaps []apfs.Snapshot) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Snapshots for %s (%d found)\n", device, len(snaps))
	for _, s := range snaps {
		out.WriteString("|\n+-- 00000000-0000-0000-0000-000000000000\n")
		fmt.Fprintf(&out, "|   Name:        %s\n", s.Name)
		out.WriteString("|   XID:         1\n|   Purgeable:   Yes\n")
	}
	return out.String()
}

func TestPruneByPolicyDeletesExactlyWhatThePlanPrunes(t *testing.T) {
	snaps := history(planNow, 6*time.Hour, int((120*day)/(6*time.Hour)))
	r := &listingRunner{snaps: snaps}
	policy := Presets(6*time.Hour, 14*day)[0].Policy

	deleted, err := PruneByPolicy(context.Background(), r, policy, planNow)
	if err != nil {
		t.Fatal(err)
	}

	_, prune := Plan(snaps, policy, planNow)
	want := map[string]bool{}
	for _, s := range prune {
		want[s.Stamp] = true
	}
	if len(deleted) != len(want) {
		t.Errorf("deleted %d snapshots, the plan pruned %d", len(deleted), len(want))
	}
	for _, stamp := range r.deleted {
		if !want[stamp] {
			t.Errorf("deleted %s, which the plan kept", stamp)
		}
	}
	if len(r.deleted) != len(deleted) {
		t.Errorf("ran %d deletions but reported %d", len(r.deleted), len(deleted))
	}
	// Oldest first, so a run that fails partway leaves the same shape a
	// completed one would: thinned from the far end, not holed in the middle.
	for i := 1; i < len(r.deleted); i++ {
		if r.deleted[i] < r.deleted[i-1] {
			t.Fatalf("deleted out of order at %d: %s after %s", i, r.deleted[i], r.deleted[i-1])
		}
	}
	if len(snaps) > 0 && len(r.deleted) > 0 && r.deleted[len(r.deleted)-1] == snaps[0].Stamp {
		t.Error("deleted the newest snapshot")
	}
}

func TestPruneByPolicyDeletesNothingUnderAnEmptyPolicy(t *testing.T) {
	r := &listingRunner{snaps: history(planNow, 6*time.Hour, 100)}
	deleted, err := PruneByPolicy(context.Background(), r, Policy{}, planNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 || len(r.deleted) != 0 {
		t.Errorf("an empty policy deleted %v", r.deleted)
	}
}

func TestDescribeReadsAsASentence(t *testing.T) {
	// Built from a stated period and window, since a preset is now a function of
	// both. The sentence follows the bands, so changing them changes it — which is
	// the property that makes this description trustworthy where the hand-written
	// one was not.
	got := Describe(Presets(6*time.Hour, 14*day)[1].Policy)
	want := "One every 6 hours for 14 days, then one a week out to 26 weeks, then one every 4 weeks out to 104 weeks."
	if got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
}

// Describe builds a sentence from clauses. Each clause is a whole message rather
// than words glued together, because the order of rate and span is not the same
// in every language — and this is the test that would catch a return to gluing.
func TestTheDescribedPolicyIsTranslated(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })

	p := Policy{Tiers: []Tier{
		{Every: 0, For: 2 * day},
		{Every: day, For: 14 * day},
	}}

	i18n.SetLanguage("en")
	english := Describe(p)
	i18n.SetLanguage("de")
	german := Describe(p)

	if english == german {
		t.Fatalf("the language did not change the sentence: %q", german)
	}
	for _, want := range []string{"Everything for", "one a day"} {
		if !strings.Contains(english, want) {
			t.Errorf("English is missing %q: %q", want, english)
		}
	}
	for _, want := range []string{"Alles für", "einer pro Tag", "dann"} {
		if !strings.Contains(german, want) {
			t.Errorf("German is missing %q: %q", want, german)
		}
	}
	// The first letter is upper-cased by rune, so a sentence opening with a
	// multi-byte letter is not cut in half.
	if !strings.HasSuffix(german, ".") {
		t.Errorf("German lost its full stop: %q", german)
	}
}

// A snapshot on a second volume and not on the data volume must still be pruned.
//
// This is the bug in one test. `tmutil localsnapshot` takes no arguments, so it
// writes to every eligible mounted APFS volume at once — and pruning planned over
// the data volume's list alone. Snapshots are purgeable, so macOS reclaims them
// per volume under space pressure; a date it drops from a full data volume before
// the retention window expires survives on every other volume, where nothing was
// looking. Nothing ever asked for its deletion, so it stayed forever.
//
// Found on a real machine: an SD card at 98% full holding eight snapshots that
// existed nowhere else, one of them pinning its container's minimum size.
func TestASnapshotOnAnotherVolumeAloneIsStillPruned(t *testing.T) {
	// Old enough that no policy would keep it, and deliberately not in the data
	// volume's list — which is exactly the state macOS leaves behind.
	orphan := apfs.Snapshot{
		Name:  "com.apple.TimeMachine.2020-01-01-000000.local",
		Stamp: "2020-01-01-000000",
		Taken: planNow.Add(-2000 * day),
	}
	r := &listingRunner{snaps: history(planNow, 6*time.Hour, 40), extra: []apfs.Snapshot{orphan}}

	deleted, err := PruneByPolicy(context.Background(), r, Presets(6*time.Hour, 14*day)[0].Policy, planNow)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, s := range deleted {
		if s.Stamp == orphan.Stamp {
			found = true
		}
	}
	if !found {
		t.Errorf("a snapshot living only on a second volume was not pruned, so nothing would ever delete it. Deleted: %v", r.deleted)
	}
}

// And it is deleted once, not once per volume that holds it.
//
// `tmutil deletelocalsnapshots <date>` removes that date wherever it lives, so a
// second call for the same date is a command that can only fail — and a loop per
// volume rather than over the union would make one for every volume but the first.
func TestASnapshotOnEveryVolumeIsDeletedOnce(t *testing.T) {
	r := &listingRunner{snaps: history(planNow, 6*time.Hour, int((120*day)/(6*time.Hour)))}

	if _, err := PruneByPolicy(context.Background(), r, Presets(6*time.Hour, 14*day)[0].Policy, planNow); err != nil {
		t.Fatal(err)
	}

	seen := map[string]int{}
	for _, stamp := range r.deleted {
		seen[stamp]++
	}
	for stamp, n := range seen {
		if n != 1 {
			t.Errorf("%s was deleted %d times; it is on two volumes and one call removes it from both", stamp, n)
		}
	}
	if len(seen) == 0 {
		t.Fatal("nothing was deleted, so this test proved nothing")
	}
}
