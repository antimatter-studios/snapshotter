package i18n

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// go-i18n owns lookup, fallback, template data and plural selection, so none of
// that is tested here.
//
// What remains is the message files themselves, which no library can check: a
// dropped clause, a placeholder that did not survive translation, a language
// carrying a message nothing will ever ask for.

type entry struct {
	One   string `json:"one"`
	Other string `json:"other"`
}

func load(t *testing.T, code string) map[string]entry {
	t.Helper()

	data, err := locales.ReadFile("locales/" + code + ".json")
	if err != nil {
		t.Fatalf("reading %s: %v", code, err)
	}
	var out map[string]entry
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parsing %s: %v", code, err)
	}
	return out
}

func ids(m map[string]entry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEveryLanguageCarriesEveryMessage(t *testing.T) {
	english := load(t, "en")
	for _, code := range Languages {
		if code == "en" {
			continue
		}
		other := load(t, code)
		if strings.Join(ids(other), ",") != strings.Join(ids(english), ",") {
			for _, id := range ids(english) {
				if _, ok := other[id]; !ok {
					t.Errorf("%s is missing %q", code, id)
				}
			}
			for _, id := range ids(other) {
				if _, ok := english[id]; !ok {
					t.Errorf("%s has %q, which English does not", code, id)
				}
			}
		}
	}
}

// joiners are the messages whose whitespace is the point: they are placed
// between two clauses and have to carry their own spacing, which differs by
// language. Everything else with a space at its edge is a mistake.
var joiners = map[string]bool{"retention.join": true}

func TestNothingIsBlankOrPadded(t *testing.T) {
	for _, code := range Languages {
		for id, m := range load(t, code) {
			if joiners[id] {
				continue
			}
			if strings.TrimSpace(m.Other) == "" {
				t.Errorf("%s/%s is empty, which shows as a gap rather than a fault", code, id)
			}
			if m.Other != strings.TrimSpace(m.Other) {
				t.Errorf("%s/%s is padded with whitespace", code, id)
			}
		}
	}
}

var templateAction = regexp.MustCompile(`\{\{\.(\w+)\}\}`)

// A dropped placeholder renders a sentence with its value silently missing, which
// reads as finished text. The window's catalogue had exactly that fault.
func TestPlaceholdersSurviveTranslation(t *testing.T) {
	english := load(t, "en")
	for _, code := range Languages {
		if code == "en" {
			continue
		}
		other := load(t, code)
		for id, want := range english {
			a := templateAction.FindAllString(want.Other, -1)
			b := templateAction.FindAllString(other[id].Other, -1)
			sort.Strings(a)
			sort.Strings(b)
			// Order is not checked: a translator must be free to move a value to
			// where the sentence needs it.
			if strings.Join(a, ",") != strings.Join(b, ",") {
				t.Errorf("%s/%s has %v, English has %v", code, id, b, a)
			}
		}
	}
}

// The same truncation guard the window's catalogue needed: a translation that
// stops short of the full stop has usually stopped short of a clause, and what
// remains is still a grammatical sentence.
// abbreviated are the messages whose final full stop belongs to an abbreviation
// rather than to a sentence. German writes "vor 5 Min." with the point, because
// Min. is short for Minuten; dropping it to match English's "5m ago" would be
// wrong German rather than consistent punctuation.
var abbreviated = map[string]bool{
	"cli.minutesAgo": true,
	"cli.hoursAgo":   true,
	"cli.daysAgo":    true,
}

func TestNothingIsObviouslyTruncated(t *testing.T) {
	ends := func(s string) bool { return strings.HasSuffix(s, ".") || strings.HasSuffix(s, "…") }

	english := load(t, "en")
	for _, code := range Languages {
		if code == "en" {
			continue
		}
		other := load(t, code)
		for id, want := range english {
			got := other[id].Other
			if ends(want.Other) != ends(got) && !abbreviated[id] {
				t.Errorf("%s/%s ends differently from English: %q", code, id, got)
			}
			// German runs longer than English rather than shorter.
			if len(want.Other) > 30 && len(got) < len(want.Other)/2 {
				t.Errorf("%s/%s is %d characters against English's %d", code, id, len(got), len(want.Other))
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

func TestTemplateDataArrives(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })
	SetLanguage("en")

	got := T("status.overdue.detail", "due", "09:00", "newest", "Tuesday")
	if strings.Contains(got, "{{") {
		t.Errorf("a placeholder was left unfilled: %q", got)
	}
	if !strings.Contains(got, "09:00") || !strings.Contains(got, "Tuesday") {
		t.Errorf("the values did not arrive: %q", got)
	}
}

func TestATranslationIsActuallyUsed(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })

	SetLanguage("en")
	english := T("status.covered")
	SetLanguage("de")
	german := T("status.covered")

	if english == german {
		t.Errorf("the language did not change the answer: %q", german)
	}
	if german == "status.covered" {
		t.Error("the German message was not found, so the id came back instead")
	}
}

// A message id with no entry returns itself, which is meant to look like a fault.
func TestAMissingMessageLooksWrongRatherThanTerse(t *testing.T) {
	if got := T("status.nothing.like.this"); got != "status.nothing.like.this" {
		t.Errorf("a missing id produced %q, which could pass for a label", got)
	}
}

// The reason for the whole library: Spanish and English pluralise differently,
// and a rule per language is not something to reimplement.
func TestPluralFormsAreChosenPerLanguage(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })

	for _, c := range []struct{ code, one, many string }{
		{"en", "1 snapshot", "5 snapshots"},
		{"de", "1 Snapshot", "5 Snapshots"},
		{"es", "1 snapshot", "5 snapshots"},
		{"fr", "1 snapshot", "5 snapshots"},
	} {
		SetLanguage(c.code)
		if got := N("count.snapshots", 1); got != c.one {
			t.Errorf("%s singular: got %q, want %q", c.code, got, c.one)
		}
		if got := N("count.snapshots", 5); got != c.many {
			t.Errorf("%s plural: got %q, want %q", c.code, got, c.many)
		}
	}
}

// Zero takes the plural form in English and German, and the singular in French —
// the kind of rule that made hand-rolling this a mistake.
func TestZeroFollowsTheLanguageRatherThanEnglish(t *testing.T) {
	t.Cleanup(func() { SetLanguage("en") })

	SetLanguage("en")
	if got := N("count.days", 0); got != "0 days" {
		t.Errorf("English zero: got %q", got)
	}
	SetLanguage("fr")
	if got := N("count.days", 0); got != "0 jour" {
		t.Errorf("French treats zero as singular; got %q", got)
	}
}
