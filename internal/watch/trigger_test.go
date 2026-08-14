package watch

import (
	"testing"
	"time"
)

var start = time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)

// feed reports n deletions spaced apart, returning how many fired.
func feed(t *Trigger, from time.Time, n int, gap time.Duration) (fired int, at time.Time) {
	at = from
	for i := 0; i < n; i++ {
		if t.Deletion(at) {
			fired++
		}
		at = at.Add(gap)
	}
	return fired, at
}

func TestBelowTheThresholdNothingFires(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)
	if fired, _ := feed(tr, start, 9, time.Millisecond); fired != 0 {
		t.Fatalf("fired %d times below the threshold", fired)
	}
}

func TestABurstFires(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)
	fired, _ := feed(tr, start, 10, time.Millisecond)
	if fired != 1 {
		t.Fatalf("fired %d times, want exactly 1", fired)
	}
}

// Ordinary work deletes steadily. Spread widely enough, it must never trip the
// wire however long it runs, or the feature becomes a snapshot generator.
func TestASlowTrickleNeverFires(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)
	// One deletion every 200ms: five per window, half the threshold, forever.
	if fired, _ := feed(tr, start, 500, 200*time.Millisecond); fired != 0 {
		t.Fatalf("a steady trickle fired %d times", fired)
	}
}

// The cooldown is what stops one long deletion filling the disk with snapshots
// of a disk that is being emptied.
func TestTheCooldownHoldsOffASecondSnapshot(t *testing.T) {
	tr := NewTrigger(10, time.Second, 10*time.Minute)

	fired, at := feed(tr, start, 10, time.Millisecond)
	if fired != 1 {
		t.Fatalf("first burst fired %d times, want 1", fired)
	}
	// A second burst immediately afterwards is inside the cooldown.
	if fired, at = feed(tr, at, 10, time.Millisecond); fired != 0 {
		t.Fatalf("a burst inside the cooldown fired %d times", fired)
	}
	// Past the cooldown it should fire again.
	if fired, _ = feed(tr, at.Add(11*time.Minute), 10, time.Millisecond); fired != 1 {
		t.Fatalf("a burst after the cooldown fired %d times, want 1", fired)
	}
}

// Old deletions must leave the window, or a long enough run of anything
// eventually reaches the threshold by accumulation.
func TestTheWindowForgets(t *testing.T) {
	tr := NewTrigger(10, time.Second, time.Minute)

	feed(tr, start, 9, time.Millisecond)
	if n := tr.Pending(start.Add(9 * time.Millisecond)); n != 9 {
		t.Fatalf("pending = %d, want 9", n)
	}
	if n := tr.Pending(start.Add(2 * time.Second)); n != 0 {
		t.Fatalf("pending after the window = %d, want 0", n)
	}
	// Those nine are stale, so nine more must not add up to eighteen.
	if fired, _ := feed(tr, start.Add(2*time.Second), 9, time.Millisecond); fired != 0 {
		t.Fatal("stale deletions counted toward a new burst")
	}
}

func TestZeroFieldsFallBackToTheDefaults(t *testing.T) {
	tr := NewTrigger(0, 0, 0)
	if tr.Threshold != DefaultThreshold || tr.Window != DefaultWindow || tr.Cooldown != DefaultCooldown {
		t.Fatalf("got %d/%v/%v, want the defaults", tr.Threshold, tr.Window, tr.Cooldown)
	}
}
