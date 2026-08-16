package menubar

import (
	"bytes"
	"image/png"
	"os"
	"regexp"
	"testing"
)

// The complaint these exist to answer: a menu of findings drawn with one
// repeated icon says only that there are several. Every subject must look
// different from every other, and every one must actually be there — an icon
// that fails to load is a blank space in a menu row, which reads as a broken
// application rather than a missing picture.

func TestEveryKindHasItsOwnIcon(t *testing.T) {
	seen := map[string]string{}
	for _, kind := range Kinds() {
		data, err := Glyph(kind, LevelWarn)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if other, clash := seen[string(data)]; clash {
			t.Errorf("%s and %s are the same image", kind, other)
		}
		seen[string(data)] = kind
	}
}

// Severity still has to be visible, or the icons have traded one lost
// distinction for another. The cross is the exception and has its own test.
func TestLevelChangesTheColour(t *testing.T) {
	ok, err := Glyph(KindSchedule, LevelOK)
	if err != nil {
		t.Fatal(err)
	}
	warn, _ := Glyph(KindSchedule, LevelWarn)
	bad, _ := Glyph(KindSchedule, LevelBad)

	if bytes.Equal(ok, warn) || bytes.Equal(warn, bad) || bytes.Equal(ok, bad) {
		t.Error("two levels render the same image")
	}
}

// macOS is handed these bytes directly and shows nothing at all if they will not
// decode.
func TestEveryIconIsAUsablePNG(t *testing.T) {
	for _, kind := range Kinds() {
		for _, level := range []Level{LevelOK, LevelWarn, LevelBad} {
			data, err := Glyph(kind, level)
			if err != nil {
				t.Fatalf("%s/%s: %v", kind, level, err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s/%s is not a PNG: %v", kind, level, err)
			}
			b := img.Bounds()
			if b.Dx() != 32 || b.Dy() != 32 {
				t.Errorf("%s/%s is %dx%d, want 32x32", kind, level, b.Dx(), b.Dy())
			}

			// An empty image is the failure that looks like success: it decodes,
			// it renders, and the menu shows a gap.
			var painted int
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
						painted++
					}
				}
			}
			if painted == 0 {
				t.Errorf("%s/%s is blank", kind, level)
			}
		}
	}
}

// The cross marks something absent, which is the one state worth breaking the
// palette for. It is red at every level.
func TestTheCrossIsAlwaysRed(t *testing.T) {
	first, err := Glyph(KindTripwire, LevelWarn)
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []Level{LevelOK, LevelBad} {
		other, _ := Glyph(KindTripwire, level)
		if !bytes.Equal(first, other) {
			t.Errorf("the cross changed at level %s", level)
		}
	}
}

// A kind this build has never heard of still gets an icon, because findings are
// added in the service and this must not be the thing that breaks.
func TestAnUnknownKindFallsBackRatherThanFailing(t *testing.T) {
	data, err := Glyph("invented-later", LevelWarn)
	if err != nil {
		t.Fatalf("an unknown kind produced no icon: %v", err)
	}
	if len(data) == 0 {
		t.Error("the fallback is empty")
	}
	// An unknown LEVEL must not fail either, and must not read as health.
	if _, err := Glyph(KindSchedule, Level("something-new")); err != nil {
		t.Errorf("an unknown level produced no icon: %v", err)
	}
}

// The window draws the same icons as lucide-react components, because a menu
// item takes image bytes and a web view takes SVG. Nothing can share code across
// that line, so the list of kinds is what holds the two together — and the
// generator that renders these PNGs keys off the same list.
func TestTheKindsMatchTheOnesTheWindowDraws(t *testing.T) {
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
	for _, kind := range Kinds() {
		if !inWindow[kind] {
			t.Errorf("%s has a menu bar icon but the window does not draw it", kind)
		}
		delete(inWindow, kind)
	}
	for kind := range inWindow {
		t.Errorf("%s is drawn in the window but has no menu bar icon", kind)
	}
}

// The generator is the only thing that writes these files, so a kind added to
// the const block above without a line in the script would silently ship the
// fallback icon.
func TestTheGeneratorCoversEveryKind(t *testing.T) {
	script, err := os.ReadFile("../../build/icons/findings.sh")
	if err != nil {
		t.Skipf("cannot read the generator: %v", err)
	}
	for _, kind := range Kinds() {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(kind) + `:`).Match(script) {
			t.Errorf("%s has no icon named in build/icons/findings.sh", kind)
		}
	}
}
