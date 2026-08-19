package i18n

import "testing"

// The reason this is not fmt.Sprintf: the decimal separator differs, and getting
// it wrong is the kind of thing that reads as a typo in three of the four
// languages this ships with.
func TestBytesFollowsTheLanguagesNumberFormat(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })

	const fortyFive = 45 * 1 << 30 // 45 GB, near enough
	for _, c := range []struct{ code, want string }{
		{"en", "45 GB"},
		{"de", "45 GB"},
	} {
		SetLanguage(c.code)
		if got := Bytes(fortyFive); got != c.want {
			t.Errorf("%s: got %q, want %q", c.code, got, c.want)
		}
	}

	// Below ten it carries a decimal, and that is where the separator shows.
	SetLanguage("en")
	english := Bytes(1_600_000_000)
	SetLanguage("de")
	german := Bytes(1_600_000_000)
	if english == german {
		t.Errorf("the decimal separator did not follow the language: both %q", english)
	}
	if got := english; got != "1.5 GB" {
		t.Errorf("English: got %q", got)
	}
	if got := german; got != "1,5 GB" {
		t.Errorf("German should use a comma: got %q", got)
	}
}

// Whole bytes have no fraction, and a size below a kilobyte should not read
// "0.0 KB".
func TestBytesUnderAKilobyteAreWhole(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })
	SetLanguage("en")

	if got := Bytes(512); got != "512 B" {
		t.Errorf("got %q, want %q", got, "512 B")
	}
}
