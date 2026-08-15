package menubar

import (
	"bytes"
	"image/png"
	"os"
	"regexp"
	"testing"
)

// The complaint this exists to answer: a menu of findings drawn with one
// repeated icon says only that there are several. Every subject must look
// different from every other.

func allKinds() []string {
	return []string{
		KindSnapshots, KindSchedule, KindOverdue, KindTripwire,
		KindThinning, KindConflict, KindSpace, KindSimulated, KindStale,
	}
}

func TestEveryKindLooksDifferent(t *testing.T) {
	seen := map[string]string{}
	for _, kind := range allKinds() {
		data, err := Glyph(kind, LevelWarn)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		key := string(data)
		if other, clash := seen[key]; clash {
			t.Errorf("%s is drawn identically to %s", kind, other)
		}
		seen[key] = kind
	}
}

// Severity still has to be visible, or the shapes have traded one lost
// distinction for another.
func TestLevelStillChangesTheColour(t *testing.T) {
	ok, err := Glyph(KindSchedule, LevelOK)
	if err != nil {
		t.Fatal(err)
	}
	warn, _ := Glyph(KindSchedule, LevelWarn)
	bad, _ := Glyph(KindSchedule, LevelBad)

	if bytes.Equal(ok, warn) || bytes.Equal(warn, bad) || bytes.Equal(ok, bad) {
		t.Error("two levels are drawn the same")
	}
}

// macOS is handed these directly and shows nothing at all if they will not
// decode, which reads as a broken menu rather than as a missing icon.
func TestEveryGlyphIsAPNGWithSomethingInIt(t *testing.T) {
	for _, kind := range append(allKinds(), "something-added-later") {
		data, err := Glyph(kind, LevelWarn)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s is not a PNG: %v", kind, err)
		}
		if img.Bounds().Dx() != glyphPx || img.Bounds().Dy() != glyphPx {
			t.Errorf("%s is %v, want %dx%d", kind, img.Bounds(), glyphPx, glyphPx)
		}

		// An empty image is the failure that looks like success: it encodes, it
		// decodes, and the menu shows a blank space where the icon should be.
		var painted int
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					painted++
				}
			}
		}
		if painted == 0 {
			t.Errorf("%s drew nothing", kind)
		}
		// Nor should it be a solid block, which is what a runaway fill looks like.
		if painted == glyphPx*glyphPx {
			t.Errorf("%s filled the whole square", kind)
		}
	}
}

// A kind this build has never heard of still gets an icon, because findings are
// added in the service and this must not be the thing that breaks.
func TestAnUnknownKindStillDrawsSomething(t *testing.T) {
	data, err := Glyph("invented-later", LevelWarn)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("no image for an unknown kind")
	}
}

// The cross is the one glyph whose colour is fixed rather than taken from the
// level. Anything reading colour as severity has to know that.
func TestTheCrossIsAlwaysRed(t *testing.T) {
	first, err := Glyph(KindTripwire, LevelWarn)
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []Level{LevelOK, LevelBad} {
		other, err := Glyph(KindTripwire, level)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, other) {
			t.Errorf("the cross changed colour at level %s", level)
		}
	}

	img, err := png.Decode(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	want := levelColour(LevelBad)
	var opaque int
	for y := 0; y < glyphPx; y++ {
		for x := 0; x < glyphPx; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a>>8 < 250 {
				continue
			}
			opaque++
			// Within a few counts: antialiased edges are blended, so an edge
			// pixel is near the colour rather than exactly it.
			near := func(got uint32, want uint8) bool {
				d := int(got>>8) - int(want)
				return d > -8 && d < 8
			}
			if !near(r, want.R) || !near(g, want.G) || !near(b, want.B) {
				t.Fatalf("a solid pixel at %d,%d is not red: %d,%d,%d", x, y, r>>8, g>>8, b>>8)
			}
		}
	}
	if opaque == 0 {
		t.Error("the cross drew nothing solid")
	}
}

// Smaller than the others, so it does not dominate a menu whose other rows are
// not emergencies.
func TestTheCrossIsSmallerThanTheClock(t *testing.T) {
	painted := func(kind string) int {
		data, err := Glyph(kind, LevelWarn)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		var n int
		for y := 0; y < glyphPx; y++ {
			for x := 0; x < glyphPx; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					n++
				}
			}
		}
		return n
	}
	if cross, clock := painted(KindTripwire), painted(KindSchedule); cross >= clock {
		t.Errorf("the cross covers %d pixels, the clock %d — it is not the smaller", cross, clock)
	}
}

// The window draws the same nine shapes as SVG, because a menu item needs PNG
// bytes and a web view does not. The two cannot share code, so the list of kinds
// is what holds them together — and this is the Go half of that check. Its
// counterpart is frontend/src/FindingIcon.test.tsx.
func TestTheKindsMatchTheOnesTheWindowDraws(t *testing.T) {
	// Read out of the frontend rather than restated, so this fails when the two
	// drift rather than when someone forgets to update a copy of a copy.
	src, err := os.ReadFile("../../frontend/src/FindingIcon.tsx")
	if err != nil {
		t.Skipf("cannot read the window's icons: %v", err)
	}

	block := regexp.MustCompile(`(?s)findingKinds = \[(.*?)\]`).FindSubmatch(src)
	if block == nil {
		t.Fatal("findingKinds is not where this test expects it")
	}
	inWindow := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z]+)"`).FindAllSubmatch(block[1], -1) {
		inWindow[string(m[1])] = true
	}

	for _, kind := range allKinds() {
		if !inWindow[kind] {
			t.Errorf("%s is drawn in the menu bar but not in the window", kind)
		}
		delete(inWindow, kind)
	}
	for kind := range inWindow {
		t.Errorf("%s is drawn in the window but not in the menu bar", kind)
	}
}
