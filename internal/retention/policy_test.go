package retention

import (
	"math/rand"
	"testing"
	"time"
)

// The queries a policy answers about itself. Covered only through the packages
// that use them until now, which is how "Horizon is the furthest band" stayed an
// assumption rather than an assertion.

// normalised is what every other method is built on, so its two jobs are worth
// stating outright: drop the bands that cover nothing, and order the rest by
// reach. The order is not the caller's to get right — a policy stored back to
// front would apply the coarsest thinning to the newest snapshots.
func TestBandsAreOrderedByReachAndEmptyOnesDropped(t *testing.T) {
	p := Policy{Tiers: []Tier{
		{Every: 7 * day, For: 91 * day},
		{Every: 0, For: 0}, // covers nothing
		{Every: day, For: 14 * day},
		{Every: time.Hour, For: -time.Hour}, // negative, also nothing
	}}

	bands := p.Bands()
	if len(bands) != 2 {
		t.Fatalf("kept %d bands, want the 2 that cover anything: %v", len(bands), bands)
	}
	if !(bands[0].For < bands[1].For) {
		t.Errorf("bands are not ordered by reach: %v", bands)
	}
}

// Horizon is how far back the history reaches, which is the number the interface
// shows as "reaching back". It must be the furthest band, whatever order they
// were given in.
func TestHorizonIsTheFurthestBand(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for i := 0; i < 1000; i++ {
		p, _, _ := generate(r)

		var furthest time.Duration
		for _, b := range p.Tiers {
			if b.For > furthest {
				furthest = b.For
			}
		}
		if got := p.Horizon(); got != furthest {
			t.Fatalf("case %d: horizon %v, furthest band %v", i, got, furthest)
		}
	}
}

// An empty policy prunes nothing, so its horizon is zero rather than infinite.
// The other reading would make a zero value delete everything ever taken.
func TestAnEmptyPolicyReachesNowhere(t *testing.T) {
	if got := (Policy{}).Horizon(); got != 0 {
		t.Errorf("horizon %v, want 0", got)
	}
	if bands := (Policy{}).Bands(); len(bands) != 0 {
		t.Errorf("bands %v, want none", bands)
	}
}

// IsFlat decides whether the interface shows the flat window or a preset, so a
// policy that keeps everything for a window must be recognised as one however it
// was built.
func TestIsFlatRecognisesAKeepEverythingWindow(t *testing.T) {
	if !FlatPolicy(14 * day).IsFlat() {
		t.Error("a flat policy was not recognised as flat")
	}
	twoBands := Policy{Tiers: []Tier{{Every: 0, For: 2 * day}, {Every: day, For: 14 * day}}}
	if twoBands.IsFlat() {
		t.Error("a two-band policy was called flat")
	}
	if (Policy{}).IsFlat() {
		t.Error("an empty policy was called flat")
	}
}

// Equal compares what a policy does, not how it was written: the same bands in a
// different order are the same policy, because that is how they will be applied.
func TestEqualComparesBehaviourNotSpelling(t *testing.T) {
	a := Policy{Tiers: []Tier{{Every: day, For: 14 * day}, {Every: 7 * day, For: 91 * day}}}
	backToFront := Policy{Tiers: []Tier{{Every: 7 * day, For: 91 * day}, {Every: day, For: 14 * day}}}
	withNothing := Policy{Tiers: []Tier{
		{Every: day, For: 14 * day},
		{Every: 0, For: 0},
		{Every: 7 * day, For: 91 * day},
	}}

	if !a.Equal(backToFront) {
		t.Error("the same bands in another order compared unequal")
	}
	if !a.Equal(withNothing) {
		t.Error("a band covering nothing changed the comparison")
	}
	if a.Equal(FlatPolicy(14 * day)) {
		t.Error("a tiered policy compared equal to a flat one")
	}
}

// Equality has to be reflexive and symmetric, or IdentifyPolicy will name the
// same policy differently depending on which side it is on.
func TestEqualIsReflexiveAndSymmetric(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	for i := 0; i < 1000; i++ {
		a, _, _ := generate(r)
		b, _, _ := generate(r)

		if !a.Equal(a) {
			t.Fatalf("case %d: a policy did not equal itself", i)
		}
		if a.Equal(b) != b.Equal(a) {
			t.Fatalf("case %d: equality is not symmetric", i)
		}
	}
}

// FlatPolicy is one band keeping everything, which is the shape the rest of the
// code recognises as flat.
func TestFlatPolicyIsOneKeepEverythingBand(t *testing.T) {
	p := FlatPolicy(14 * day)
	bands := p.Bands()
	if len(bands) != 1 {
		t.Fatalf("built %d bands, want 1: %v", len(bands), bands)
	}
	if bands[0].Every != 0 {
		t.Errorf("band keeps one per %v, want everything", bands[0].Every)
	}
	if bands[0].For != 14*day {
		t.Errorf("band reaches %v, want 14 days", bands[0].For)
	}
}
