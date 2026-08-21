package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"snapshotter/internal/diffs"
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
