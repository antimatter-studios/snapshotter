package i18n

import "math"

// Span words a length of time in the largest unit that stays honest.
//
// One implementation because there were three: services/status.go worded the
// cover figure, internal/cli worded the same figure for the terminal, and the
// window worded it again in TypeScript. All three carried the same two thresholds
// and the same three phrasings, and when the strings were translated only two of
// the three were found — the terminal kept printing "4 days" inside a German
// sentence until someone ran it.
//
// The thresholds are the point of the rule: "0.3 days" reads as a rounding error
// and "7 hours" reads as a fact, so hours are used until there are enough days to
// be worth saying.
func Span(hours float64) string {
	switch {
	case hours >= hoursBeforeDays:
		return N("count.days", int(math.Round(hours/hoursPerDay)))
	case hours >= 1:
		// Rounded first, then pluralised against what will actually be printed:
		// 1.4 hours prints as "1 hour", and pluralising the unrounded value would
		// have called it "1 hours".
		return N("count.hours", int(math.Round(hours)))
	default:
		return T("count.underAnHour")
	}
}

const (
	hoursPerDay = 24
	// Two days, not one: a single day expressed in days loses the detail that
	// "30 hours" carries.
	hoursBeforeDays = 48
)
