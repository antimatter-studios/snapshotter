package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"snapshotter/internal/diffs"
	"snapshotter/internal/i18n"
)

// Words this package chooses and the window has to recognise.
//
// A status, a finding's action, a finding's level: each is a string invented here
// and read there, across a boundary no compiler crosses. Every one of them fails
// the same silent way — the window renders something that is not wrong so much as
// unfinished. A status with no catalogue entry shows its own key as its label. An
// action nothing branches on gives a finding no button, which reads as a problem
// with no fix. A level with no rule renders unstyled, which reads as a rendering
// fault.
//
// The precedent is menubar's glyph test, which cross-checks the finding icons the
// same way and for the same reason: two implementations of one vocabulary cannot
// share code, so the list is what holds them together.

const windowRoot = "../frontend/src"

func readWindowFile(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(windowRoot, name))
	if err != nil {
		t.Fatalf("reading the window's %s: %v", name, err)
	}
	return string(body)
}

// everyStatus is what a comparison can report about one row. Listed here rather
// than derived, because the point is to notice when the set changes.
var everyStatus = []diffs.Status{
	diffs.Same,
	diffs.Modified,
	diffs.OnlyInSnapshot,
	diffs.OnlyOnDisk,
	diffs.TypeChanged,
	diffs.NotExamined,
}

func TestEveryStatusHasAWordInEveryLanguage(t *testing.T) {
	for _, code := range []string{"en", "de", "es", "fr"} {
		body, err := os.ReadFile(filepath.Join(windowRoot, "locales", code+".json"))
		if err != nil {
			t.Fatalf("reading the %s catalogue: %v", code, err)
		}
		var catalogue map[string]string
		if err := json.Unmarshal(body, &catalogue); err != nil {
			t.Fatalf("parsing the %s catalogue: %v", code, err)
		}

		for _, status := range everyStatus {
			// The window looks the status up by name: t(`status.${status}`). A missing
			// entry puts "status.typeChanged" in the column where a word belongs.
			key := "status." + string(status)
			if catalogue[key] == "" {
				t.Errorf("%s has no %q, so a row with that status shows the key as its label", code, key)
			}
		}
	}
}

func TestEveryStatusThisPackageCanReportIsOneTheWindowDraws(t *testing.T) {
	// StatusIcon carries the marks. A status missing from its registry draws no
	// mark at all, which is how it shipped once: two statuses had marks and the
	// rest silently had none.
	registry := readWindowFile(t, "StatusIcon.tsx")

	for _, status := range everyStatus {
		if !strings.Contains(registry, string(status)) {
			t.Errorf("StatusIcon.tsx does not mention %q, so such a row gets no mark", status)
		}
	}
}

// findingActions are the fixes this package offers. Each is a button in the
// window, and the button is the whole value of the finding: a problem you can
// read but not act on from where you read it is just anxiety.
func TestEveryFindingActionHasAButtonInTheWindow(t *testing.T) {
	health := readWindowFile(t, "Health.tsx")

	for _, action := range actionsEmitted(t) {
		// The window writes `f.action === "install-schedule"`, so the quoted string
		// appearing there is what makes the button exist.
		if !strings.Contains(health, `"`+action+`"`) {
			t.Errorf("nothing in Health.tsx branches on %q, so that finding arrives with no way to act on it", action)
		}
	}
}

func TestEveryFindingLevelHasSomethingToLookLike(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join(windowRoot, "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)

	for _, level := range levelsOnFindings(t) {
		if !strings.Contains(css, ".finding."+string(level)) {
			t.Errorf("styles.css has no .finding.%s, so such a finding renders unstyled", level)
		}
	}
	// The headline verdict takes every level including ok, which is the one a
	// finding never takes.
	for _, level := range []Level{LevelOK, LevelInfo, LevelWarn, LevelBad} {
		if !strings.Contains(css, ".verdict."+string(level)) {
			t.Errorf("styles.css has no .verdict.%s", level)
		}
	}
}

// actionsEmitted reads the actions out of this package's own source, so a new one
// is picked up without anyone remembering to list it here.
func actionsEmitted(t *testing.T) []string {
	t.Helper()

	body, err := os.ReadFile("status.go")
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`Action:\s*"([a-z-]+)"`).FindAllStringSubmatch(string(body), -1)
	if len(found) == 0 {
		t.Fatal("no actions found in status.go, so this test is checking nothing")
	}

	seen := map[string]bool{}
	var out []string
	for _, m := range found {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func levelsOnFindings(t *testing.T) []Level {
	t.Helper()

	body, err := os.ReadFile("status.go")
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`Level:\s*(Level[A-Za-z]+)`).FindAllStringSubmatch(string(body), -1)
	if len(found) == 0 {
		t.Fatal("no finding levels found in status.go, so this test is checking nothing")
	}

	byName := map[string]Level{"LevelOK": LevelOK, "LevelInfo": LevelInfo, "LevelWarn": LevelWarn, "LevelBad": LevelBad}
	seen := map[Level]bool{}
	var out []Level
	for _, m := range found {
		level, ok := byName[m[1]]
		if !ok {
			t.Fatalf("status.go uses a level this test does not know: %s", m[1])
		}
		if !seen[level] {
			seen[level] = true
			out = append(out, level)
		}
	}
	return out
}

// Notes are prose, and prose crossing this boundary has to be translated before
// it leaves. The window shows Note verbatim — it has no way to know what it says
// — so an untranslated one arrives intact in the middle of a German screen. Every
// note this package can produce used to be English; two of search's four were
// already translated and two were not, which is how it went unnoticed.
func TestEveryNoteFollowsTheLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })

	svc, seed := compareFixture(t, map[string]string{"solid.bin": "head\x00tail"})
	binary := filepath.Join(seed, "solid.bin")

	i18n.SetLanguage("en")
	english := versionsOf(t, svc, binary).Note
	if english == "" {
		t.Fatal("a binary file came back with nothing said about it")
	}

	i18n.SetLanguage("de")
	german := versionsOf(t, svc, binary).Note
	if german == english {
		t.Errorf("the note is the same in German as in English: %q", german)
	}
	if german == "" || strings.HasPrefix(german, "diff.") {
		t.Errorf("the German note came back as %q", german)
	}
}

// The other half of the same rule: a note that is a key rather than a sentence
// means the catalogue is missing an entry, and it renders as "diff.looksBinary"
// where a sentence belongs.
func TestEveryNoteThisPackageAsksForExists(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("en") })

	body, err := os.ReadFile("diff.go")
	if err != nil {
		t.Fatal(err)
	}
	searchBody, err := os.ReadFile("search.go")
	if err != nil {
		t.Fatal(err)
	}
	asked := regexp.MustCompile(`i18n\.[TN]\("([a-zA-Z.]+)"`).FindAllStringSubmatch(string(body)+string(searchBody), -1)
	if len(asked) == 0 {
		t.Fatal("no catalogue lookups found, so this test is checking nothing")
	}

	for _, code := range []string{"en", "de", "es", "fr"} {
		i18n.SetLanguage(code)
		for _, m := range asked {
			if got := i18n.T(m[1]); got == m[1] {
				t.Errorf("%s has no entry for %q, so it renders as its own key", code, m[1])
			}
		}
	}
}

// What the schedule does is said in four places: the menu bar's line, its
// tooltip, the window's figure grid, and the settings screen's summary. All four
// read it from internal/schedule.
//
// Three of them used to build their own. The menu bar said "every 3 hours, kept
// 364 days" and the figure grid said "Every 3h, kept 14d", both from the interval
// and the retention window alone — which ignores the policy, so both were wrong
// for every tiered schedule: they read the horizon as the retention, which is
// true of a flat window and of nothing else. The settings screen, reading the
// policy properly, said something different and correct. Nothing noticed, because
// no two of them were ever compared.
//
// This is that comparison. It is a text search rather than a type check because
// the divergence crossed a language boundary, which is exactly where a compiler
// stops looking.
func TestNoScreenWordsTheScheduleItself(t *testing.T) {
	// The shapes of a hand-built claim: an interval and a retention glued together
	// in a template literal or a format string.
	claims := []*regexp.Regexp{
		regexp.MustCompile(`kept \$\{`),
		regexp.MustCompile("kept \\${?[a-zA-Z.]*[Rr]etention"),
		regexp.MustCompile(`[Ee]very \$\{[a-zA-Z.]*[Ii]nterval`),
	}

	for _, where := range []struct{ name, body string }{
		{"the window", windowSource(t)},
		{"the menu bar", traySource(t)},
	} {
		for _, claim := range claims {
			if found := claim.FindString(where.body); found != "" {
				t.Errorf("%s builds its own account of the schedule (%q). "+
					"It has to come from internal/schedule, or it will disagree with the other places that say it.",
					where.name, found)
			}
		}
	}
}

// And the fields carrying it are actually populated, or every reader shows a
// blank where the schedule should be.
func TestTheScheduleWordingReachesTheReaders(t *testing.T) {
	window := windowSource(t)

	for _, field := range []string{"scheduleHeadline", "retentionMode", "policySummary"} {
		if !strings.Contains(window, field) {
			t.Errorf("nothing in the window reads %s, so the wording it carries goes nowhere", field)
		}
	}
	if !strings.Contains(traySource(t), "ScheduleHeadline") {
		t.Error("the menu bar does not read ScheduleHeadline")
	}
}

// Every message in the window's catalogue has to be reached by something.
//
// This exists because the opposite of a missing translation has now happened five
// times here, and no test could see any of it: a key was added and translated into
// four languages, and the English stayed written into the markup. The screen looked
// finished in English and was hardcoded in every other language.
//
// Nothing catches that. An unused message does not render wrong, does not throw,
// and leaves the four catalogues perfectly consistent with each other — the
// i18n tests all pass, because they compare the catalogues to each other rather
// than to the code. A key nothing calls is either dead weight or, far more often,
// a translation somebody wired up and then didn't.
func TestNothingInTheWindowsCatalogueGoesUnasked(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(windowRoot, "locales", "en.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue map[string]string
	if err := json.Unmarshal(body, &catalogue); err != nil {
		t.Fatal(err)
	}
	source := windowSource(t)

	// Prefixes whose keys are assembled at runtime — t(`status.${status}`) — so the
	// whole key never appears in the source. Each is a hole in this test, so each
	// names the file that builds it.
	builtAtRuntime := []string{
		"status.",         // Browser.tsx: one per row verdict
		"tripwire.level.", // Schedule.tsx: one per sensitivity
		"count.",          // passed with { count }, which i18next suffixes itself
		"age.",            // the same, from format.ts
	}

	plural := regexp.MustCompile(`_(one|other|zero|two|few|many)$`)
	var unused []string
	seen := map[string]bool{}
	for key := range catalogue {
		stem := plural.ReplaceAllString(key, "")
		if seen[stem] {
			continue
		}
		seen[stem] = true

		exempt := false
		for _, prefix := range builtAtRuntime {
			if strings.HasPrefix(stem, prefix) {
				exempt = true
				break
			}
		}
		if exempt {
			continue
		}
		if !strings.Contains(source, `"`+stem+`"`) && !strings.Contains(source, "`"+stem+"`") {
			unused = append(unused, stem)
		}
	}

	sort.Strings(unused)
	for _, key := range unused {
		// Named one by one: "fourteen unused keys" sends someone hunting, and the
		// whole point is that the key names the screen it was meant for.
		t.Errorf("nothing asks for %q — either it is dead, or the English is still written into the markup", key)
	}
}

// And the exemptions must correspond to real code. One left behind after its call
// site went away would hide a whole family of unused keys.
func TestEveryRuntimeBuiltPrefixIsReallyBuilt(t *testing.T) {
	source := windowSource(t)

	for _, prefix := range []string{"status.", "tripwire.level.", "count.", "age."} {
		if !strings.Contains(source, "`"+prefix) && !strings.Contains(source, `"`+prefix) {
			t.Errorf("%q is exempted from the unused-key check and nothing builds it", prefix)
		}
	}
}
