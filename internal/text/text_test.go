package text

import "testing"

// This helper exists because the code it replaced printed "1 hours" on the
// status line. The singular case is the whole point of the test.
func TestPluralAgreesWithTheNumberItPrints(t *testing.T) {
	for _, tc := range []struct {
		n    int
		unit string
		want string
	}{
		{1, "hour", "1 hour"},
		{2, "hour", "2 hours"},
		{0, "hour", "0 hours"},
		{1, "snapshot", "1 snapshot"},
		{47, "day", "47 days"},
		{1, "thing", "1 thing"},
	} {
		if got := Plural(tc.n, tc.unit); got != tc.want {
			t.Errorf("Plural(%d, %q) = %q, want %q", tc.n, tc.unit, got, tc.want)
		}
	}
}

// Nothing counts downwards, but a negative reaching this would be a bug
// elsewhere and "-1 hour" would read as if it were fine.
func TestPluralTreatsAnythingButOneAsPlural(t *testing.T) {
	if got := Plural(-1, "hour"); got != "-1 hours" {
		t.Errorf("got %q", got)
	}
}
