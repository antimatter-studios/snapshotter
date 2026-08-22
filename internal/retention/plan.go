package retention

import (
	"sort"
	"time"
)

// Plan decides which of a set of snapshot times a policy keeps.
//
// It returns positions in the slice it was given, newest first, rather than the
// times themselves: the caller owns the objects those times came from, and
// handing back indices means this package never has to know what they are.

func Plan(taken []time.Time, policy Policy, now time.Time) (keep, prune []int) {
	if len(taken) == 0 {
		return nil, nil
	}

	// Positions, sorted newest first, rather than a copy of the values. A plan
	// that relied on the caller having sorted correctly would fail by deleting the
	// wrong snapshots, and sorting the caller's slice in place would reorder
	// something it may still be displaying.
	//
	// Stable, and by time alone: two snapshots taken in the same instant keep the
	// order the caller gave them, which is how a caller with a tie-break of its own
	// — a name, an identifier — gets that tie-break honoured without this package
	// having to know what it is.
	ordered := make([]int, len(taken))
	for i := range ordered {
		ordered[i] = i
	}
	sort.SliceStable(ordered, func(a, b int) bool {
		return taken[ordered[a]].After(taken[ordered[b]])
	})

	tiers := policy.normalised()
	if len(tiers) == 0 {
		// A policy with nothing usable in it keeps everything. The other reading
		// — no tier covers any snapshot, so delete the lot — turns a zero value,
		// a half-decoded plist or a typo into total loss of the restore history.
		// The two failures are not comparable: pruning too little is corrected
		// by the next run, and macOS reclaims purgeable snapshots under space
		// pressure on its own.
		return ordered, nil
	}

	// One bucket per (tier period, absolute period start). The period is part of
	// the key because adjacent tiers overlap slightly at their boundary, and two
	// snapshots governed by different tiers must not be taken for one bucket.
	type bucket struct {
		every time.Duration
		start int64
	}
	claimed := make(map[bucket]bool, len(ordered))

	kept := make([]bool, len(ordered))
	// The newest snapshot is kept whatever the policy says. A policy that planned
	// away the last snapshot would leave the machine with no restore point at
	// all, which is the exact state this application exists to prevent, and no
	// setting a user can choose is worth that.
	kept[0] = true

	// Oldest first, so the first snapshot met in a bucket is the oldest one in
	// it, and the oldest is the one kept. That choice is the only one in this
	// file that is not forced, and it decides whether the kept set is stable as
	// time passes.
	//
	// Keeping the newest is unstable. The newest snapshot in a period changes
	// every time another arrives, so a snapshot kept by yesterday's plan is
	// deleted by today's for no reason other than that a newer one now shares its
	// bucket. A snapshot that survives one plan and is destroyed by the next,
	// with nothing having changed about it, means the far end of the history
	// keeps rewriting itself and nothing seen yesterday can be relied on today.
	//
	// Keeping the oldest is stable. The oldest snapshot in a bucket is fixed the
	// moment that bucket's first snapshot exists, since everything arriving later
	// is newer, so once a snapshot is its bucket's keeper it stays the keeper.
	// Within a tier, advancing now cannot change the kept set at all: the buckets
	// are absolute (see bucketStart) and so is the choice inside them.
	//
	// It also reaches further back for the same count, and it errs toward the
	// older snapshot of any pair — the one whose contents nothing still on disk
	// can approximate.
	//
	// The cost is that a snapshot taken minutes after another in the same bucket
	// is prunable at once. That is why every preset opens with a keep-everything
	// tier, and why the newest snapshot is kept unconditionally.
	for i := len(ordered) - 1; i >= 0; i-- {
		at := taken[ordered[i]]
		age := now.Sub(at)
		if age < 0 {
			// A snapshot dated in the future — a clock corrected backwards, a
			// machine moved between zones — is not old. Left as a negative age
			// it would still land in the first tier, but saying so here stops a
			// later change to tier lookup reading it as infinitely old.
			age = 0
		}
		tier, ok := tierFor(tiers, age)
		if !ok {
			continue // older than the policy reaches
		}
		if tier.Every <= 0 {
			kept[i] = true
			continue
		}
		b := bucket{every: tier.Every, start: BucketStart(at, tier.Every)}
		if claimed[b] {
			continue
		}
		claimed[b] = true
		kept[i] = true
	}

	for i, pos := range ordered {
		if kept[i] {
			keep = append(keep, pos)
		} else {
			prune = append(prune, pos)
		}
	}
	return keep, prune
}

// BucketStart is exported because it is part of the rule, not an implementation
// detail of it: which period a snapshot falls in is the thing that decides
// whether two snapshots compete, and a test of the policy has to be able to say
// so in the same terms the policy does.
func BucketStart(taken time.Time, every time.Duration) int64 {
	return taken.Truncate(every).UnixNano()
}

// tierFor finds the tier governing a snapshot of a given age.
//
// Tiers are ordered by reach and the first one that reaches this age wins, so
// the finest granularity covering a snapshot is the one applied. An age landing
// exactly on a boundary belongs to the finer tier, because the finer tier keeps
// more, and every tie in this file is broken toward keeping.
// tierFor finds the tier governing a snapshot of a given age.
//
// Tiers are ordered by reach and the first one that reaches this age wins, so
// the finest granularity covering a snapshot is the one applied. An age landing
// exactly on a boundary belongs to the finer tier, because the finer tier keeps
// more, and every tie in this file is broken toward keeping.
func tierFor(tiers []Tier, age time.Duration) (Tier, bool) {
	for _, t := range tiers {
		if age <= t.For {
			return t, true
		}
	}
	return Tier{}, false
}

// normalised drops the tiers that cover nothing and orders the rest by reach.
//
// The order is not the caller's to get right. A policy stored back to front
// would otherwise apply the coarsest thinning to the newest snapshots and delete
// this morning's work.
