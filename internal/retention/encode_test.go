package retention

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// The wire form. A policy is written into a launchd plist and read back from it,
// so a policy that does not survive the trip is a schedule that quietly starts
// keeping something other than what was chosen.
//
// These were covered only through the package that owns snapshots, which means
// the round trip was exercised incidentally rather than asserted.

// The property that matters: what is written can be read back unchanged. Checked
// against generated policies rather than a handful of examples, because the
// failure mode is a band whose numbers happen not to round-trip.
func TestAPolicySurvivesBeingWrittenAndReadBack(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 2000; i++ {
		p, _, _ := generate(r)

		back, ok := ParsePolicy(p.String())
		if !ok {
			t.Fatalf("case %d: %q could not be read back", i, p.String())
		}
		// Against the normalised original: String writes the bands in the order
		// the policy applies them and drops the ones that cover nothing, so that
		// is the policy the text describes.
		want := Policy{Tiers: p.Bands()}
		if !back.Equal(want) {
			t.Fatalf("case %d: %q read back as %v, want %v", i, p.String(), back.Bands(), want.Bands())
		}
	}
}

// Durations are written as whole hours, so anything finer is rounded up. Rounding
// down would shorten a band, and a band that reaches less far than asked for
// deletes a snapshot the person expected to keep.
func TestFractionsOfAnHourRoundUp(t *testing.T) {
	for _, c := range []struct {
		in   time.Duration
		want int
	}{
		{0, 0},
		{time.Minute, 1},
		{59 * time.Minute, 1},
		{time.Hour, 1},
		{time.Hour + time.Second, 2},
		{25 * time.Hour, 25},
	} {
		if got := HoursUp(c.in); got != c.want {
			t.Errorf("HoursUp(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// A keep-everything band is written as "all" rather than as a number, because
// zero hours would read back as a band that keeps one snapshot per zero-length
// period, which is not a thing.
func TestAKeepEverythingBandRoundTrips(t *testing.T) {
	p := Policy{Tiers: []Tier{{Every: 0, For: 48 * time.Hour}}}
	text := p.String()
	if !strings.HasPrefix(text, "all/") {
		t.Errorf("wrote %q, want it to start all/", text)
	}
	back, ok := ParsePolicy(text)
	if !ok {
		t.Fatalf("%q could not be read back", text)
	}
	if got := back.Bands()[0].Every; got != 0 {
		t.Errorf("read back with Every = %v, want 0", got)
	}
}

// Anything it cannot read whole is refused, rather than half-decoded into a
// policy that deletes on a rule nobody wrote.
func TestNonsenseIsRefusedRatherThanGuessedAt(t *testing.T) {
	for _, s := range []string{
		"", ",", "24", "24/", "/48", "24/48/72",
		"twelve/48", "24/forty", "-24/48", "24/-48", "all/", "all",
	} {
		if p, ok := ParsePolicy(s); ok {
			t.Errorf("%q was accepted as %v", s, p.Bands())
		}
	}
}

// Round-tripping is not enough on its own: a format that wrote every policy as
// the same text would round-trip and lose everything. Different policies must
// produce different text.
func TestDifferentPoliciesWriteDifferently(t *testing.T) {
	seen := map[string]string{}
	r := rand.New(rand.NewSource(4))
	for i := 0; i < 500; i++ {
		p, _, _ := generate(r)
		text := p.String()
		key := ""
		for _, b := range p.Bands() {
			key += b.Every.String() + "/" + b.For.String() + ";"
		}
		if prev, ok := seen[text]; ok && prev != key {
			t.Fatalf("%q describes both %s and %s", text, prev, key)
		}
		seen[text] = key
	}
}
