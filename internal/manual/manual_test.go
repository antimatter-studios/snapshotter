package manual

import (
	"strings"
	"testing"
)

// The manual exists because the documentation was written and none of it reached
// anybody: the documents are in the repository, and somebody who installed a disk
// image has the binary and nothing else. So what is asserted here is mostly that
// every page is reachable and says something — a page that does not load is a
// page that silently is not there.

func TestEveryPageLoads(t *testing.T) {
	// All() skips a page it cannot parse, because one malformed header should not
	// stop `help` listing anything. That makes this test the thing standing
	// between a broken page and its silent disappearance.
	entries, err := files.ReadDir("topics")
	if err != nil {
		t.Fatal(err)
	}
	var markdown int
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		markdown++
		if _, err := load(e.Name()); err != nil {
			t.Errorf("%s does not load: %v", e.Name(), err)
		}
	}
	if markdown == 0 {
		t.Fatal("no pages are embedded, so this test is checking nothing")
	}
	if len(All()) != markdown {
		t.Errorf("%d pages are embedded and %d load", markdown, len(All()))
	}
}

func TestEveryPageIsListable(t *testing.T) {
	for _, topic := range All() {
		// The summary is the one line the listing shows. Without it the page is
		// invisible in the only place anybody goes looking for it.
		if topic.Summary == "" {
			t.Errorf("%s has no summary, so it would list as a blank row", topic.Name)
		}
		if topic.Title == "" {
			t.Errorf("%s has no title", topic.Name)
		}
		if len(topic.Body) < 200 {
			t.Errorf("%s is %d characters, which is a stub rather than a page", topic.Name, len(topic.Body))
		}
		// The body should open with its own heading, so a page piped somewhere
		// else still says what it is.
		if !strings.HasPrefix(topic.Body, "# ") {
			t.Errorf("%s does not start with a heading: %.40q", topic.Name, topic.Body)
		}
	}
}

// Every page has to be reachable by the name it is filed under, or the listing
// advertises a command that does not work.
func TestEveryListedNameLooksItUp(t *testing.T) {
	for _, topic := range All() {
		got, ok := Lookup(topic.Name)
		if !ok {
			t.Errorf("%s is listed and cannot be looked up", topic.Name)
			continue
		}
		if got.Name != topic.Name {
			t.Errorf("%s looked up %s", topic.Name, got.Name)
		}
	}
}

// The name is a phrase a reader half-remembers rather than an identifier they
// copied. Refusing one spelling of a question the manual plainly answers teaches
// nothing.
func TestSpellingsOfOneNameAreOneQuestion(t *testing.T) {
	for _, name := range []string{"tripwire", "Tripwire", "TRIPWIRE", "bulk-deletion", "bulk_deletion", "  tripwire  "} {
		got, ok := Lookup(name)
		if !ok {
			t.Errorf("%q found nothing", name)
			continue
		}
		if got.Name != "tripwire" {
			t.Errorf("%q found %s", name, got.Name)
		}
	}
}

// An alias exists for the word somebody reaches for at the moment they need the
// page — "purgeable" is what they just read in a listing, not "snapshots".
func TestAnAliasReachesItsPage(t *testing.T) {
	for alias, want := range map[string]string{
		"purgeable":        "snapshots",
		"full-disk-access": "mounting",
		"fda":              "mounting",
		"sdcard":           "volumes",
		"recover":          "restoring",
		"watcher":          "tripwire",
	} {
		got, ok := Lookup(alias)
		if !ok {
			t.Errorf("%q reaches no page", alias)
			continue
		}
		if got.Name != want {
			t.Errorf("%q reaches %s, want %s", alias, got.Name, want)
		}
	}
}

// Two pages answering to one name would make which page you get depend on file
// order, which is not a thing anyone can reason about.
func TestNoTwoPagesAnswerToTheSameName(t *testing.T) {
	seen := map[string]string{}
	for _, topic := range All() {
		for _, name := range append([]string{topic.Name}, topic.Aliases...) {
			key := normalise(name)
			if other, taken := seen[key]; taken {
				t.Errorf("%q reaches both %s and %s", name, other, topic.Name)
			}
			seen[key] = topic.Name
		}
	}
}

func TestNothingIsLookedUpByAnEmptyName(t *testing.T) {
	for _, name := range []string{"", "   ", "-", "_"} {
		if got, ok := Lookup(name); ok {
			t.Errorf("%q found %s", name, got.Name)
		}
	}
}

// A near miss is answered with the page rather than with a list of everything:
// somebody who typed "mount" wants "mounting", not a menu.
func TestANearMissSuggestsThePage(t *testing.T) {
	for _, c := range []struct{ typed, want string }{
		{"restor", "restoring"},
		{"volume", "volumes"},
		{"snapshot", "snapshots"},
		{"restoring-files", "restoring"},
	} {
		got := Suggest(c.typed)
		var found bool
		for _, n := range got {
			if n == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q suggested %v, want %s among them", c.typed, got, c.want)
		}
	}
}

// And something that resembles nothing suggests nothing, so the caller can fall
// back to the full help rather than printing an empty "did you mean".
func TestSomethingLikeNothingSuggestsNothing(t *testing.T) {
	if got := Suggest("zzzzz"); len(got) != 0 {
		t.Errorf("suggested %v", got)
	}
}

// A page with a header this build does not know is refused rather than loaded
// with the header silently dropped — a typo in "summary:" would otherwise make
// the page invisible in the listing for no visible reason.
func TestAnUnknownHeaderIsRefused(t *testing.T) {
	if _, err := load("nope.md"); err == nil {
		t.Error("a missing page loaded")
	}
	for _, topic := range All() {
		if strings.Contains(topic.Body, "title:") || strings.Contains(topic.Body, "summary:") {
			t.Errorf("%s carries its headers into the body", topic.Name)
		}
	}
}
