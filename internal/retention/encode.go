package retention

import (
	"strconv"
	"strings"
	"time"
)

// A policy is written into the launchd plist and read back from it, so the two
// halves of this file are the only place its wire form is defined.

func (p Policy) String() string {
	tiers := p.normalised()
	fields := make([]string, 0, len(tiers))
	for _, t := range tiers {
		every := "all"
		if t.Every > 0 {
			every = strconv.Itoa(HoursUp(t.Every))
		}
		fields = append(fields, every+"/"+strconv.Itoa(HoursUp(t.For)))
	}
	return strings.Join(fields, ",")
}

// Describe words a policy as a sentence. Tiers are far easier to check in prose
// than as a row of numbers, and this is the string the settings screen shows
// beside the count it would retain.

// ParsePolicy reads the encoding String writes. It also accepts "0" for a
// keep-everything band, because that is the obvious thing to write by hand.
//
// It reports false rather than a partial policy. Half a policy is a different
// policy, and the half most likely to be dropped is the last one — the band
// holding the oldest snapshots, whose loss is the one that cannot be undone.
func ParsePolicy(s string) (Policy, bool) {
	var p Policy
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		every, forAge, ok := parseTier(field)
		if !ok {
			return Policy{}, false
		}
		p.Tiers = append(p.Tiers, Tier{Every: every, For: forAge})
	}
	if len(p.Tiers) == 0 {
		return Policy{}, false
	}
	return p, true
}

// parseTier reads one every/for pair, both in whole hours.
func parseTier(field string) (every, forAge time.Duration, ok bool) {
	left, right, found := strings.Cut(field, "/")
	if !found {
		return 0, 0, false
	}
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)

	if left != "all" {
		hours, err := strconv.Atoi(left)
		if err != nil || hours < 0 {
			return 0, 0, false
		}
		every = time.Duration(hours) * time.Hour
	}
	hours, err := strconv.Atoi(right)
	if err != nil || hours <= 0 {
		return 0, 0, false
	}
	return every, time.Duration(hours) * time.Hour, true
}

// Retained reports how many snapshots a policy holds once a schedule taking one
// every interval has been running longer than the policy reaches.
//
// It counts by planning a synthetic history rather than by arithmetic over the
// tiers, so the figure the settings screen shows comes from the same function
// that does the deleting and cannot drift from it. It is an estimate only in
// that a real history is not taken exactly on the interval.

// hoursUp rounds a duration up to whole hours, which is the resolution the plist
// carries. Rounding up on the reach of a band keeps a snapshot slightly longer
// than asked; rounding down would delete one slightly early.
func HoursUp(d time.Duration) int {
	hours := int(d / time.Hour)
	if d%time.Hour != 0 {
		hours++
	}
	return hours
}

// words spells a duration in the largest unit that stays exact, so a policy
// reads as "13 weeks" rather than "2184 hours".
//
// Weeks only past a month: a fortnight is a fortnight, and "2 weeks" for the
// retention everyone here already thinks of as 14 days would be a needless
// translation.
