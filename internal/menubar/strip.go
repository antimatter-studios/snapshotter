// Package menubar draws the small images the menu bar menu shows.
//
// A menu is not only a list of things to click. macOS will draw an image beside
// any item, and an image can say in one glance what a sentence takes a moment to
// read — "you are covered for the last two days, with a gap on Tuesday night" is
// a shape, not a number.
package menubar

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"time"
)

// Coverage draws one cell per hour across the window, filled where a snapshot
// exists and empty where none does, oldest on the left.
//
// The gaps are the point. A count says how many restore points there are; this
// says when they are, which is the question someone actually has — a machine
// with twelve snapshots taken in one hour is not covered, and the count alone
// cannot tell you that.
//
// The image is drawn at twice its display size, because macOS scales menu images
// down and a strip drawn at 1× on a Retina display is visibly soft.
// hoursFilled marks each hour of the window in which at least one snapshot was
// taken. It is the single statement of the rule the strip draws and the caption
// describes, so the two cannot come to disagree about what a mark means.
func hoursFilled(taken []time.Time, now time.Time, window time.Duration, fallbackHours int) []bool {
	hours := int(window.Hours())
	if hours <= 0 {
		hours = fallbackHours
	}

	filled := make([]bool, hours)
	for _, t := range taken {
		age := now.Sub(t)
		if age < 0 || age >= window {
			continue
		}
		// Oldest on the left, so the newest snapshot is the rightmost cell —
		// the same direction as every other timeline a person reads.
		i := hours - 1 - int(age.Hours())
		if i >= 0 && i < hours {
			filled[i] = true
		}
	}
	return filled
}

// HoursCovered counts the hours of the window holding at least one snapshot.
//
// The menu needs this to say what the strip means. Twenty-two snapshots can fill
// three marks, and without a sentence saying so the strip reads as broken rather
// than as the answer to a different question than the one being asked.
func HoursCovered(taken []time.Time, now time.Time, window time.Duration) (covered, total int) {
	filled := hoursFilled(taken, now, window, 48)
	for _, f := range filled {
		if f {
			covered++
		}
	}
	return covered, len(filled)
}

func Coverage(taken []time.Time, now time.Time, window time.Duration, dark bool) ([]byte, error) {
	const (
		scale     = 2
		cellW     = 3 * scale // wide enough that a single hour is still visible
		cellH     = 11 * scale
		gap       = 1 * scale
		hoursWide = 48 // two days: far enough back to show a nightly rhythm
	)

	filled := hoursFilled(taken, now, window, hoursWide)
	hours := len(filled)

	width := hours*cellW + (hours-1)*gap
	img := image.NewNRGBA(image.Rect(0, 0, width, cellH))

	// Two greys rather than a colour: the level already has a colour of its own on
	// the icon beside it, and a second one here would compete with it.
	on := color.NRGBA{0x3c, 0x3c, 0x43, 0xd8}
	off := color.NRGBA{0x3c, 0x3c, 0x43, 0x30}
	if dark {
		on = color.NRGBA{0xeb, 0xeb, 0xf5, 0xd8}
		off = color.NRGBA{0xeb, 0xeb, 0xf5, 0x28}
	}

	for h := 0; h < hours; h++ {
		shade := off
		if filled[h] {
			shade = on
		}
		x0 := h * (cellW + gap)
		for x := x0; x < x0+cellW && x < width; x++ {
			for y := 0; y < cellH; y++ {
				img.SetNRGBA(x, y, shade)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
