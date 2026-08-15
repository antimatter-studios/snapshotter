package menubar

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"
)

// The strip is read at a glance and never read carefully, so what matters is
// that a gap looks like a gap: an hour with no snapshot must not be drawn the
// same as an hour with one.

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("what was produced is not a PNG: %v", err)
	}
	return img
}

// macOS is handed these bytes directly and fails silently on anything it cannot
// read, which shows up as a menu item with no image rather than as an error.
func TestItProducesAPNG(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	data, err := Coverage([]time.Time{now.Add(-time.Hour)}, now, 48*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[1:4]) != "PNG" {
		t.Fatal("not a PNG")
	}
	img := decode(t, data)
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Error("the image has no size")
	}
}

// column reports the colour of a cell, by sampling the middle of its band.
func column(t *testing.T, img image.Image, hour, hours int) (r, g, b, a uint32) {
	t.Helper()
	width := img.Bounds().Dx()
	x := (hour*width)/hours + width/(hours*3)
	return img.At(x, img.Bounds().Dy()/2).RGBA()
}

// The whole point: an hour that has a snapshot must be drawn differently from
// one that does not.
func TestAnHourWithASnapshotIsDrawnDifferentlyFromOneWithout(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	const hours = 48

	// One snapshot, an hour ago: the newest cells differ from the oldest.
	data, err := Coverage([]time.Time{now.Add(-90 * time.Minute)}, now, hours*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, data)

	_, _, _, newA := column(t, img, hours-2, hours) // the hour that has one
	_, _, _, oldA := column(t, img, 0, hours)       // two days ago, which does not
	if newA == oldA {
		t.Error("a covered hour is indistinguishable from an uncovered one")
	}
	if newA <= oldA {
		t.Error("the covered hour is the fainter of the two")
	}
}

// Newest on the right, like every other timeline. Drawn the other way round, a
// machine that has just been covered looks like one that was covered two days
// ago and has been neglected since.
func TestTheNewestSnapshotIsOnTheRight(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	const hours = 48

	data, err := Coverage([]time.Time{now.Add(-30 * time.Minute)}, now, hours*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, data)

	_, _, _, rightA := column(t, img, hours-1, hours)
	_, _, _, leftA := column(t, img, 0, hours)
	if rightA <= leftA {
		t.Error("the newest snapshot was not drawn at the right-hand end")
	}
}

// Anything outside the window is not drawn rather than clamped to an end, which
// would invent coverage the machine does not have.
func TestSnapshotsOutsideTheWindowAreNotDrawn(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	const hours = 48

	empty, err := Coverage(nil, now, hours*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	old, err := Coverage([]time.Time{
		now.Add(-72 * time.Hour), // older than the window
		now.Add(time.Hour),       // in the future, which a clock change can produce
	}, now, hours*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(empty, old) {
		t.Error("a snapshot outside the window changed the picture")
	}
}

// The menu is drawn on whichever background the system is using, and a strip
// drawn for the wrong one is either invisible or a black box.
func TestLightAndDarkAreDrawnDifferently(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	snaps := []time.Time{now.Add(-time.Hour)}

	light, err := Coverage(snaps, now, 48*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	dark, err := Coverage(snaps, now, 48*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(light, dark) {
		t.Error("the same image is used on both backgrounds")
	}
}

// A zero window would otherwise divide by zero or produce a zero-width image,
// and it arrives from a settings file somebody edited.
func TestAnEmptyWindowStillProducesSomething(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	data, err := Coverage(nil, now, 0, false)
	if err != nil {
		t.Fatalf("a zero window failed: %v", err)
	}
	if img := decode(t, data); img.Bounds().Dx() == 0 {
		t.Error("a zero window produced a zero-width image")
	}
}
