package menubar

import (
	"bytes"
	"testing"
)

// The glyphs the menu bar itself shows, one per health level.
//
// They moved into this package today, from beside main, because go:embed cannot
// reach across directories and that was the only thing keeping the entry point in
// the repository root. Their tests did not come with them: the ones next to main
// call this function, but a package's own coverage does not count what another
// package exercises, so these had none of their own.
//
// Worth having directly, because the failure is silent. A menu bar item with no
// icon looks like the application failed to start, and nothing logs it.

func TestEveryLevelHasItsOwnGlyph(t *testing.T) {
	seen := map[string]string{}
	for _, level := range []Level{LevelOK, LevelWarn, LevelBad} {
		data := TrayIcon(level)
		if len(data) == 0 {
			t.Errorf("%s has no glyph", level)
			continue
		}
		key := string(data)
		if other, ok := seen[key]; ok {
			t.Errorf("%s and %s draw the same picture", level, other)
		}
		seen[key] = string(level)
	}
}

// The runtime hands these straight to macOS, which fails silently on anything
// that is not an image — so a truncated or wrong-format file shows as a missing
// icon rather than as an error.
func TestEveryGlyphIsAPNG(t *testing.T) {
	for _, level := range []Level{LevelOK, LevelWarn, LevelBad} {
		data := TrayIcon(level)
		if len(data) < 8 || !bytes.Equal(data[1:4], []byte("PNG")) {
			t.Errorf("%s is not a PNG: % x", level, data[:min(8, len(data))])
		}
	}
}

// A level this package has not heard of must read as something to look at rather
// than as health. A level added to the services and forgotten here would
// otherwise show a healthy menu bar for a machine nobody has checked.
func TestAnUnknownLevelShowsTheWorstGlyph(t *testing.T) {
	got := TrayIcon(Level("something-new"))

	if bytes.Equal(got, TrayIcon(LevelOK)) {
		t.Error("an unknown level showed the healthy glyph")
	}
	if !bytes.Equal(got, TrayIcon(LevelBad)) {
		t.Error("an unknown level should show the worst glyph available")
	}
}

// The dot drawn beside the headline, which is a different image from the menu bar
// icon: that one is sized for the menu bar and dominates a menu row.
func TestStatusDrawsADotPerLevel(t *testing.T) {
	seen := map[string]bool{}
	for _, level := range []Level{LevelOK, LevelWarn, LevelBad} {
		data, err := Status(level)
		if err != nil {
			t.Errorf("%s: %v", level, err)
			continue
		}
		if len(data) < 8 || !bytes.Equal(data[1:4], []byte("PNG")) {
			t.Errorf("%s did not produce a PNG", level)
		}
		if seen[string(data)] {
			t.Errorf("%s draws the same dot as another level", level)
		}
		seen[string(data)] = true
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
