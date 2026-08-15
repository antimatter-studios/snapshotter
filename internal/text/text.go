// Package text holds the wording helpers shared by anything that produces a
// sentence for a person to read.
//
// It exists because the same helper was written twice, in two packages, in two
// shapes — and the interesting part is that one of those shapes had already
// produced "1 hours" on screen. Counting and pluralising in the same place is
// what stops that: the number that is printed is the number that decides the
// plural.
package text

import "strconv"

// Plural renders a count and its unit: "1 hour", "3 hours".
//
// The unit is given in the singular and the "s" is added, which covers every
// unit this application counts in — hours, days, weeks, snapshots, things.
// Anything with an irregular plural should be worded by its caller rather than
// by teaching this function about English.
func Plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
