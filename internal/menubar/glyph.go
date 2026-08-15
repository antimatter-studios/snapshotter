package menubar

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Glyphs for findings.
//
// A finding has a level and a subject, and the level is the less useful of the
// two here: a menu listing three warnings drawn with three identical warning
// triangles has told the reader nothing except that there are three. The shape
// says what the finding is about; the colour still says how bad it is.

// Level is how severe a finding is, kept as a string so this package does not
// import the services it draws for.
type Level string

const (
	LevelOK   Level = "ok"
	LevelWarn Level = "warn"
	LevelBad  Level = "bad"
)

// The subjects a finding can have. They match the kinds the status service
// assigns; an unknown one falls back to a plain dot rather than to nothing,
// because a missing image in one row of a menu reads as a rendering fault.
const (
	KindSnapshots = "snapshots"
	KindSchedule  = "schedule"
	KindOverdue   = "overdue"
	KindTripwire  = "tripwire"
	KindThinning  = "thinning"
	KindConflict  = "conflict"
	KindSpace     = "space"
	KindSimulated = "simulated"
)

// glyphPx is the drawn size: 16 points at 2×, which is what a menu item's image
// is given on a Retina display.
const glyphPx = 32

// Status draws the overall level as a single dot, for the headline row.
//
// Deliberately smaller than a finding's glyph and much smaller than the menu bar
// icon it echoes: the headline is a summary, and a summary does not need to
// shout over the rows beneath it.
func Status(level Level) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, glyphPx, glyphPx))
	disc(img, 16, 16, 5.5, levelColour(level))

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Glyph draws the icon for one finding.
func Glyph(kind string, level Level) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, glyphPx, glyphPx))
	c := levelColour(level)

	switch kind {
	case KindSnapshots:
		// A restore point: the thing there are none of.
		disc(img, 16, 16, 7, c)

	case KindSchedule:
		// A clock, because the finding is about a timer.
		ring(img, 16, 16, 11, 2.4, c)
		line(img, 16, 16, 16, 8, 2.2, c)  // hour hand, straight up
		line(img, 16, 16, 21, 18, 2.2, c) // minute hand

	case KindOverdue:
		// The same clock with its hands past the hour, so "late" is legible as a
		// shape rather than only as a colour.
		ring(img, 16, 16, 11, 2.4, c)
		line(img, 16, 16, 16, 24, 2.2, c)
		line(img, 16, 16, 23, 13, 2.2, c)

	case KindTripwire:
		// A cross: the watcher is off. An earlier attempt drew the thing itself —
		// a wire with something resting on it — which at 16pt reads as a sunset.
		// What the finding says is that something is absent, and a cross says
		// absent without needing to be recognised first.
		//
		// Always red, and deliberately the one glyph whose colour is not the
		// level's. A cross is already the strongest shape here; drawn in the same
		// amber as everything else it reads as decoration, and red is what the
		// shape means. Smaller than the rest for the same reason — at full size it
		// dominates a menu whose other rows are not emergencies.
		red := levelColour(LevelBad)
		line(img, 11, 11, 21, 21, 2.4, red)
		line(img, 21, 11, 11, 21, 2.4, red)

	case KindThinning:
		// Bars getting shorter to the right: history being thinned out.
		bar(img, 5, 8, 4, 18, c)
		bar(img, 14, 13, 4, 13, c)
		bar(img, 23, 18, 4, 8, c)

	case KindConflict:
		// Two things overlapping, which is exactly the complaint.
		ring(img, 12, 16, 8, 2.4, c)
		ring(img, 20, 16, 8, 2.4, c)

	case KindSpace:
		// A gauge close to full.
		ring(img, 16, 16, 11, 2.4, c)
		wedge(img, 16, 16, 8, -math.Pi/2, math.Pi, c)

	case KindSimulated:
		// A dashed outline: present, but not real.
		dashedRing(img, 16, 16, 11, 2.4, c)

	default:
		disc(img, 16, 16, 6, c)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// levelColour keeps severity visible now that shape carries the subject. These
// are the system colours macOS uses for the same three meanings, so they read
// the way everything else on the machine reads.
func levelColour(level Level) color.NRGBA {
	switch level {
	case LevelOK:
		return color.NRGBA{0x34, 0xc7, 0x59, 0xff}
	case LevelBad:
		return color.NRGBA{0xff, 0x3b, 0x30, 0xff}
	default:
		return color.NRGBA{0xff, 0x9f, 0x0a, 0xff}
	}
}

// set blends one pixel, so edges are smooth rather than stepped — a 16pt glyph
// drawn without it looks like a mistake at this size.
func set(img *image.NRGBA, x, y int, c color.NRGBA, alpha float64) {
	if x < 0 || y < 0 || x >= glyphPx || y >= glyphPx || alpha <= 0 {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	old := img.NRGBAAt(x, y)
	a := alpha + float64(old.A)/255*(1-alpha)
	if a == 0 {
		return
	}
	blend := func(n, o uint8) uint8 {
		return uint8((float64(n)*alpha + float64(o)*(float64(old.A)/255)*(1-alpha)) / a)
	}
	img.SetNRGBA(x, y, color.NRGBA{blend(c.R, old.R), blend(c.G, old.G), blend(c.B, old.B), uint8(a * 255)})
}

// coverage antialiases by how far a pixel is inside an edge.
func coverage(d float64) float64 { return 0.5 - d }

func disc(img *image.NRGBA, cx, cy, r float64, c color.NRGBA) {
	for y := int(cy-r) - 1; y <= int(cy+r)+1; y++ {
		for x := int(cx-r) - 1; x <= int(cx+r)+1; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) - r
			set(img, x, y, c, coverage(d))
		}
	}
}

func ring(img *image.NRGBA, cx, cy, r, w float64, c color.NRGBA) {
	for y := int(cy-r) - 2; y <= int(cy+r)+2; y++ {
		for x := int(cx-r) - 2; x <= int(cx+r)+2; x++ {
			d := math.Abs(math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)-r) - w/2
			set(img, x, y, c, coverage(d))
		}
	}
}

// dashedRing is a ring with gaps, for something that is only notionally there.
func dashedRing(img *image.NRGBA, cx, cy, r, w float64, c color.NRGBA) {
	for y := int(cy-r) - 2; y <= int(cy+r)+2; y++ {
		for x := int(cx-r) - 2; x <= int(cx+r)+2; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			// Eight dashes: enough to read as dashed at 16pt without closing up.
			if int(math.Floor((math.Atan2(dy, dx)+math.Pi)/(math.Pi/8)))%2 == 1 {
				continue
			}
			d := math.Abs(math.Hypot(dx, dy)-r) - w/2
			set(img, x, y, c, coverage(d))
		}
	}
}

func line(img *image.NRGBA, x0, y0, x1, y1, w float64, c color.NRGBA) {
	minX, maxX := int(math.Min(x0, x1))-2, int(math.Max(x0, x1))+2
	minY, maxY := int(math.Min(y0, y1))-2, int(math.Max(y0, y1))+2
	dx, dy := x1-x0, y1-y0
	length2 := dx*dx + dy*dy
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5-x0, float64(y)+0.5-y0
			t := 0.0
			if length2 > 0 {
				t = math.Max(0, math.Min(1, (px*dx+py*dy)/length2))
			}
			d := math.Hypot(px-t*dx, py-t*dy) - w/2
			set(img, x, y, c, coverage(d))
		}
	}
}

func bar(img *image.NRGBA, x, y, w, h float64, c color.NRGBA) {
	for py := int(y); py < int(y+h); py++ {
		for px := int(x); px < int(x+w); px++ {
			set(img, px, py, c, 1)
		}
	}
}

// wedge fills a pie slice, for a gauge.
func wedge(img *image.NRGBA, cx, cy, r, from, sweep float64, c color.NRGBA) {
	for y := int(cy-r) - 1; y <= int(cy+r)+1; y++ {
		for x := int(cx-r) - 1; x <= int(cx+r)+1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if math.Hypot(dx, dy) > r {
				continue
			}
			a := math.Atan2(dy, dx) - from
			for a < 0 {
				a += 2 * math.Pi
			}
			if a <= sweep {
				set(img, x, y, c, 1)
			}
		}
	}
}
