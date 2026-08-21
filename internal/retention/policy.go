// Package retention decides which snapshots a policy keeps.
//
// It is arithmetic on timestamps and nothing else. It does not know what a
// snapshot is, that they live on APFS, that deleting one requires a privileged
// command, or that any of this is ever described to a person in German — which is
// the point: the rule that decides what gets destroyed can be tested exhaustively,
// in milliseconds, with no filesystem and no language set.
//
// Everything it exports is a pure function of its arguments. The package that
// owns snapshots converts to and from timestamps at the boundary.
package retention

import (
	"sort"
	"time"
)

// Tier is one band of a retention policy: keep one snapshot per Every-long
// bucket, for snapshots up to For old.
//
// Every of zero means keep every snapshot the band covers, which is how the flat
// window this replaces is expressed — see FlatPolicy.
type Tier struct {
	Every time.Duration `json:"every"`
	For   time.Duration `json:"for"`
}

// Policy is how snapshots thin out as they age: everything recent, then one a
// day, then one a week. Tiers may be given in any order; Plan sorts them.
type Policy struct {
	Tiers []Tier `json:"tiers"`
}

// FlatPolicy is the behaviour that existed before tiering: keep everything
// inside the window and nothing outside it.
//
// It exists so no installed schedule changes meaning. A flat window is a policy
// with one keep-everything tier, and Plan under it agrees with apfs.Prune to the
// boundary, which TestFlatPolicyMatchesTheWindowItReplaces holds it to.
func FlatPolicy(retain time.Duration) Policy {
	return Policy{Tiers: []Tier{{Every: 0, For: retain}}}
}

// Plan divides snapshots into the ones a policy keeps and the ones it prunes,
// both newest first. It changes nothing: the caller deletes, or does not.
//
// Being a pure function of its arguments — now included — is the point. Deletion
// is irreversible and a snapshot cannot be recreated, because it records a past
// state of a disk that has since moved on. A retention bug is therefore only
// ever discovered by the person who needed the snapshot it deleted, so the
// decision is made somewhere it can be tested exhaustively and cheaply.
// normalised drops the tiers that cover nothing and orders the rest by reach.
//
// The order is not the caller's to get right. A policy stored back to front
// would otherwise apply the coarsest thinning to the newest snapshots and delete
// this morning's work.
func (p Policy) normalised() []Tier {
	out := make([]Tier, 0, len(p.Tiers))
	for _, t := range p.Tiers {
		if t.For <= 0 {
			continue
		}
		if t.Every < 0 {
			t.Every = 0
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].For < out[j].For })
	return out
}

// Bands returns the tiers in the order Plan applies them — finest first, with
// any that cover nothing dropped. That is also the order they read in, which is
// why the interface is given this rather than the raw field.
func (p Policy) Bands() []Tier { return p.normalised() }

// Horizon is the age of the oldest snapshot a policy still keeps: how far back
// the history reaches. Zero means the policy prunes nothing.
func (p Policy) Horizon() time.Duration {
	var furthest time.Duration
	for _, t := range p.normalised() {
		if t.For > furthest {
			furthest = t.For
		}
	}
	return furthest
}

// IsFlat reports whether a policy is the old flat window: one band, keeping
// everything inside it.
func (p Policy) IsFlat() bool {
	tiers := p.normalised()
	return len(tiers) == 1 && tiers[0].Every <= 0
}

// Equal compares policies by what they would do rather than by how they were
// written, so a preset recovered from a plist still matches the preset.
func (p Policy) Equal(other Policy) bool {
	a, b := p.normalised(), other.normalised()
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// String encodes a policy the way the plist carries it: comma-separated
// every/for pairs in whole hours, with "all" for a band that keeps everything.
//
// Whole hours because the retention value it sits beside in that plist is
// already in hours, and because a launchd plist is read by people as often as by
// programs. Both halves round up, so any loss in the encoding keeps more than was
// asked for rather than less.
