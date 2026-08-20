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

// Cells is how many periods the strip shows. Sixteen because that is a readable
// number of marks in a menu, and because at the default schedule it comes to two
// days of history.
const Cells = 16

// periodsFilled marks each scheduled period in which at least one snapshot was
// taken, oldest first.
//
// One cell per period rather than per hour, and that is the whole point. An
// hourly cell measured against a schedule nobody configured: on a three-hourly
// schedule two cells in every three are empty however well the machine is doing,
// so a healthy strip and a failing one looked nearly the same. Measured in
// periods, a working schedule is solid and every gap is a snapshot that genuinely
// did not happen.
//
// It is the single statement of the rule the strip draws and the caption
// describes, so the two cannot come to disagree about what a mark means.
func periodsFilled(taken []time.Time, now time.Time, period time.Duration, cells int) []bool {
	if period <= 0 {
		period = time.Hour
	}
	if cells <= 0 {
		cells = Cells
	}
	window := period * time.Duration(cells)

	filled := make([]bool, cells)
	for _, t := range taken {
		age := now.Sub(t)
		if age < 0 || age >= window {
			continue
		}
		// Oldest on the left, so the newest snapshot is the rightmost cell —
		// the same direction as every other timeline a person reads.
		i := cells - 1 - int(age/period)
		if i >= 0 && i < cells {
			filled[i] = true
		}
	}
	return filled
}

// PeriodsCovered counts the scheduled periods holding at least one snapshot.
//
// The menu needs this to say what the strip means: without a sentence naming the
// unit, a strip reads as broken rather than as the answer to a different question
// than the one being asked.
func PeriodsCovered(taken []time.Time, now time.Time, period time.Duration, cells int) (covered, total int) {
	filled := periodsFilled(taken, now, period, cells)
	for _, f := range filled {
		if f {
			covered++
		}
	}
	return covered, len(filled)
}

// Coverage draws one cell per scheduled period, filled where a snapshot exists
// and empty where none does, oldest on the left.
//
// The gaps are the point, and they only mean something because the cell is the
// schedule's own period: an empty cell is a snapshot that was due and did not
// happen. A count says how many restore points there are; this says whether the
// schedule is keeping its promise.
//
// The image is drawn at twice its display size, because macOS scales menu images
// down and a strip drawn at 1× on a Retina display is visibly soft.
func Coverage(taken []time.Time, now time.Time, period time.Duration, cells int, dark bool) ([]byte, error) {
	const (
		scale = 2
		// Wider than the hourly strip's cells were: there are a third as many of
		// them now, and a mark standing for three hours of work deserves the room.
		cellW = 8 * scale
		cellH = 11 * scale
		gap   = 1 * scale
	)

	filled := periodsFilled(taken, now, period, cells)
	count := len(filled)

	width := count*cellW + (count-1)*gap
	img := image.NewNRGBA(image.Rect(0, 0, width, cellH))

	// Two greys rather than a colour: the level already has a colour of its own on
	// the icon beside it, and a second one here would compete with it.
	on := color.NRGBA{0x3c, 0x3c, 0x43, 0xd8}
	off := color.NRGBA{0x3c, 0x3c, 0x43, 0x30}
	if dark {
		on = color.NRGBA{0xeb, 0xeb, 0xf5, 0xd8}
		off = color.NRGBA{0xeb, 0xeb, 0xf5, 0x28}
	}

	for c := 0; c < count; c++ {
		shade := off
		if filled[c] {
			shade = on
		}
		x0 := c * (cellW + gap)
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
