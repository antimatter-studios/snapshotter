package i18n

import "testing"

// The rule lived in three places and was tested in two of them, which is how the
// terminal's copy went untranslated: the tests agreed with each other about
// behaviour and neither noticed the third implementation existed.
func TestSpanIsWordedInTheLargestHonestUnit(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })
	SetLanguage("en")

	for _, tc := range []struct {
		hours float64
		want  string
	}{
		{0, "under an hour"},
		{0.5, "under an hour"},
		{1, "1 hour"},
		{1.4, "1 hour"}, // rounds to 1, and must not read "1 hours"
		{5, "5 hours"},
		{47, "47 hours"},
		{48, "2 days"},
		{72, "3 days"},
	} {
		if got := Span(tc.hours); got != tc.want {
			t.Errorf("%.1fh: want %q, got %q", tc.hours, tc.want, got)
		}
	}
}

// The whole reason for moving it here: the phrase has to follow the language, and
// a duration formatter that does not is invisible until someone reads the output.
func TestSpanFollowsTheLanguage(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })

	SetLanguage("de")
	if got := Span(72); got != "3 Tage" {
		t.Errorf("German: got %q, want %q", got, "3 Tage")
	}
	SetLanguage("fr")
	if got := Span(0.5); got != "moins d'une heure" {
		t.Errorf("French: got %q", got)
	}
}
