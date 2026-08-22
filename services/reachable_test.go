package services

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every method here is bound into the window, and a bound method nothing calls is
// a capability that exists and cannot be used.
//
// That is not a hypothetical. Eleven of these were unreachable at once — among
// them "what was deleted since this snapshot", which is close to the question this
// application exists to answer. They were implemented, tested, and impossible to
// invoke, and nothing said so because nothing looked. A folder comparison with
// progress reporting and cancellation was in the same state, along with the
// event it reported progress on, which no listener had ever subscribed to.
//
// So the audit is a test. Anything added below has to be reachable or listed.

// notSurfaced are the methods deliberately without a route in, and why. A method
// here is a decision; a method in neither list is an oversight.
var notSurfaced = map[string]string{
	"Compare": "folder-level comparison, which would need a screen that largely " +
		"duplicates the browser's merged listing",
	"CompareSnapshots": "the same, between two snapshots rather than against the disk",
	"Cancel":           "cancels the above, so it has nothing to cancel yet",
	"ListLive":         "single-sided listing, superseded by the merged one the browser uses",
	"ListSnapshot":     "the same, for the other side",
	"Locate":           "finds one path across snapshots; the search screen answers this by name instead",
	"UninstallTripwire": "the health screen offers installing and starting it; removing it is a " +
		"terminal job, and a button that silences the protection is not one to put a stray click near",
}

func TestEveryBoundMethodIsReachableOrKnownNotToBe(t *testing.T) {
	// Both callers: the window through the bindings, and the menu bar, which is Go
	// and calls the same services directly. A method the tray uses is reached even
	// though no TypeScript mentions it.
	callers := windowSource(t) + traySource(t)

	for _, method := range boundMethods(t) {
		if strings.Contains(callers, "."+method+"(") {
			if why, listed := notSurfaced[method]; listed {
				t.Errorf("%s is listed as not surfaced (%q) but the window calls it — remove it from the list", method, why)
			}
			continue
		}
		if _, listed := notSurfaced[method]; !listed {
			t.Errorf("nothing in the window calls %s, and it is not listed as deliberate. "+
				"Either give it a route in or say here why it has none.", method)
		}
	}
}

// The list must not outlive what it describes either: a name left in it after the
// method is gone makes the next reader look for something that is not there.
func TestNothingIsListedThatNoLongerExists(t *testing.T) {
	have := map[string]bool{}
	for _, m := range boundMethods(t) {
		have[m] = true
	}
	for method := range notSurfaced {
		if !have[method] {
			t.Errorf("%s is listed as not surfaced, but no service has a method by that name", method)
		}
	}
}

// boundMethods lists the exported methods on every service in this package, which
// is exactly what Wails binds and the window can reach.
func boundMethods(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	exported := regexp.MustCompile(`(?m)^func \([a-z]+ \*[A-Za-z]+Service\) ([A-Z]\w*)`)

	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range exported.FindAllStringSubmatch(string(body), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no bound methods found, so this test is checking nothing")
	}
	return out
}

// traySource is the Go that stands up the window and draws the menu bar, which
// reaches these services without going through the bindings at all.
func traySource(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	for _, dir := range []string{"../cmd/snapshotter", "../internal/menubar"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			body, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			b.Write(body)
		}
	}
	return b.String()
}

// windowSource is every hand-written source file in the window, concatenated.
// Tests are excluded: a method called only from a test is still unreachable by
// anyone using the application, which is the thing being checked.
func windowSource(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	err := filepath.WalkDir(windowRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") {
			return nil
		}
		if strings.Contains(name, ".test.") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(body)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the window's source: %v", err)
	}
	return b.String()
}
