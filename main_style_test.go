package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The native window and the web view have to agree on two values, and nothing in
// either toolchain can make them share a definition: one is compiled into a Go
// binary, the other is parsed by WebKit at run time. Both mismatches are visible
// and neither is detectable from inside the application, so they are checked
// here instead.

const stylesheet = "frontend/src/styles.css"

// rootToken reads one custom property from the :root block.
func rootToken(t *testing.T, name string) string {
	t.Helper()

	css, err := os.ReadFile(stylesheet)
	if err != nil {
		t.Fatalf("reading the stylesheet: %v", err)
	}
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*%s:\s*([^;]+);`, regexp.QuoteMeta(name)))
	match := re.FindSubmatch(css)
	if match == nil {
		t.Fatalf("%s does not define %s", stylesheet, name)
	}
	return string(match[1])
}

// A mismatch here is a flash of the wrong colour on every launch: the window is
// on screen before the web view has painted anything.
func TestTheWindowBackgroundMatchesTheStylesheet(t *testing.T) {
	if got := rootToken(t, "--bg"); got != windowBackgroundHex {
		t.Errorf("main.go paints %s, the stylesheet paints %s", windowBackgroundHex, got)
	}
}

// A mismatch here puts the traffic lights on top of the title.
func TestTheTitleBarHeightMatchesTheStylesheet(t *testing.T) {
	raw := rootToken(t, "--titlebar-height")
	px := regexp.MustCompile(`^(\d+)px$`).FindStringSubmatch(raw)
	if px == nil {
		t.Fatalf("--titlebar-height is %q, which this test cannot compare to a Go int", raw)
	}
	got, err := strconv.Atoi(px[1])
	if err != nil {
		t.Fatal(err)
	}
	if got != titleBarHeight {
		t.Errorf("main.go reserves %dpx, the stylesheet pads %dpx", titleBarHeight, got)
	}
}

// The hex constant is what the test above compares against, and the components
// are what Wails is actually given. If those two disagree the check above passes
// while the window still paints the wrong colour.
func TestTheBackgroundComponentsMatchTheirOwnHex(t *testing.T) {
	got := fmt.Sprintf("#%02x%02x%02x", windowBackground.r, windowBackground.g, windowBackground.b)
	if got != windowBackgroundHex {
		t.Errorf("the components are %s but the hex says %s", got, windowBackgroundHex)
	}
}
