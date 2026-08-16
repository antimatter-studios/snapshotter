package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Go has no equivalent of the frontend's Record<Key, string>, which turns a
// missing translation into a compile error. These tests are that guarantee,
// written out.

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEveryCatalogueCarriesEveryKey(t *testing.T) {
	want := keysOf(en)
	for code, catalogue := range catalogues {
		if code == "en" {
			continue
		}
		got := keysOf(catalogue)
		if len(got) != len(want) {
			t.Errorf("%s has %d keys, English has %d", code, len(got), len(want))
		}
		for _, key := range want {
			if _, ok := catalogue[key]; !ok {
				t.Errorf("%s is missing %q, which would show in English mid-sentence", code, key)
			}
		}
		for _, key := range got {
			if _, ok := en[key]; !ok {
				t.Errorf("%s has %q, which English does not — nothing will ever ask for it", code, key)
			}
		}
	}
}

func TestNothingIsBlank(t *testing.T) {
	for code, catalogue := range catalogues {
		for key, text := range catalogue {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s/%s is empty, which shows as a gap rather than as a fault", code, key)
			}
			if text != strings.TrimSpace(text) {
				t.Errorf("%s/%s is padded with whitespace", code, key)
			}
		}
	}
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// A dropped placeholder renders a sentence with its value silently missing, which
// reads as finished text. The window's catalogue had exactly that fault.
func TestPlaceholdersSurviveTranslation(t *testing.T) {
	for code, catalogue := range catalogues {
		if code == "en" {
			continue
		}
		for key, want := range en {
			got := catalogue[key]
			a := placeholder.FindAllString(want, -1)
			b := placeholder.FindAllString(got, -1)
			sort.Strings(a)
			sort.Strings(b)
			// Order is not checked: a translator has to be able to move a
			// placeholder to where the sentence needs it.
			if strings.Join(a, ",") != strings.Join(b, ",") {
				t.Errorf("%s/%s has placeholders %v, English has %v", code, key, b, a)
			}
		}
	}
}

// The same truncation guard the window's catalogue needed, for the same reason:
// a translation that stops short of the full stop has usually stopped short of a
// clause, and what remains is still a grammatical sentence.
func TestNothingIsObviouslyTruncated(t *testing.T) {
	ending := func(s string) string {
		if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "…") {
			return s[len(s)-len("."):]
		}
		return ""
	}
	for code, catalogue := range catalogues {
		if code == "en" {
			continue
		}
		for key, want := range en {
			got := catalogue[key]
			if (ending(want) == "") != (ending(got) == "") {
				t.Errorf("%s/%s ends %q, English ends %q", code, key, got[max(0, len(got)-12):], want[max(0, len(want)-12):])
			}
			// German runs longer than English rather than shorter, so a
			// translation under half the length is nearly always a lost clause.
			if len(want) > 30 && len(got) < len(want)/2 {
				t.Errorf("%s/%s is %d characters against English's %d", code, key, len(got), len(want))
			}
		}
	}
}

func TestAnUnknownLanguageIsIgnoredRatherThanObeyed(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })

	SetLanguage("de")
	SetLanguage("kl")
	// A hand-edited settings file naming a language this build does not carry
	// must not empty the menu bar.
	if Language() != "de" {
		t.Errorf("an unknown language was accepted: %q", Language())
	}
}

func TestPlaceholdersAreFilledByName(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })
	SetLanguage("en")

	got := T("status.overdue.detail", "due", "09:00", "newest", "Tuesday")
	if strings.Contains(got, "{") {
		t.Errorf("a placeholder was left unfilled: %q", got)
	}
	if !strings.Contains(got, "09:00") || !strings.Contains(got, "Tuesday") {
		t.Errorf("the values did not arrive: %q", got)
	}
}

// A key with no entry returns itself, which is meant to look like a fault.
func TestAMissingKeyLooksWrongRatherThanTerse(t *testing.T) {
	if got := T("status.nothing.like.this"); got != "status.nothing.like.this" {
		t.Errorf("a missing key produced %q, which could pass for a label", got)
	}
}
