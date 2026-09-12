package verdict

import (
	"context"
	"sync"
	"testing"
	"time"
)

// The watch costs a great deal and the cache it keeps honest is read in exactly
// one situation: somebody looking at a folder listing inside a snapshot. Those
// two facts were never connected — the watch started when the window opened and
// ran with a context nothing cancelled, so an application in the menu bar watched
// every write on the machine for as long as it ran. That was 22.5 hours of CPU
// across two days on the machine that reported it, with nobody browsing.

// clock is a time source a test can move, safely, while the supervisor reads it
// from its own goroutine.
//
// A plain variable and a closure over it is the obvious way to write this and is
// a data race — the supervisor polls from a goroutine of its own, so the test
// writing the variable and the poll reading it are concurrent. Caught by -race
// rather than by inspection, which is the argument for running it.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *clock { return &clock{at: time.Now()} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// watched records what the supervisor did to it.
type watched struct {
	mu      sync.Mutex
	starts  int
	stopped int
}

func (w *watched) run(ctx context.Context) {
	w.mu.Lock()
	w.starts++
	w.mu.Unlock()
	<-ctx.Done()
	w.mu.Lock()
	w.stopped++
	w.mu.Unlock()
}

func (w *watched) counts() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.starts, w.stopped
}

func TestNothingIsWatchedUntilAVerdictIsWanted(t *testing.T) {
	var w watched
	s := NewWatching(w.run, nil, time.Hour)

	if s.Running() {
		t.Fatal("the watch started before anything asked for a verdict")
	}
	if starts, _ := w.counts(); starts != 0 {
		t.Fatalf("the watch ran %d times before it was wanted", starts)
	}

	s.Wanted()
	if !s.Running() {
		t.Error("asking for a verdict did not start the watch")
	}
}

func TestTheWatchStopsOnceNothingIsAsking(t *testing.T) {
	var w watched
	// An hour of idle, driven by a clock this test owns, and a poll fast enough
	// that the test does not wait on a real one. Timing this against the wall
	// clock made it fail on a machine that happened to be busy — which is every
	// machine this would matter on.
	s := NewWatching(w.run, nil, time.Hour)
	fake := newClock()
	s.now = fake.now
	s.poll = func() time.Duration { return time.Millisecond }

	s.Wanted()
	if !s.Running() {
		t.Fatal("asking for a verdict did not start the watch")
	}
	fake.advance(2 * time.Hour) // long past the idle deadline

	deadline := time.Now().Add(5 * time.Second)
	for s.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.Running() {
		t.Fatal("the watch was still going long after the last question")
	}
	// Waited for rather than checked on the spot: the supervisor clears the flag
	// and then cancels, so the watch function observes its context a moment
	// later. Asserting immediately made this depend on that gap being zero.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if _, stopped := w.counts(); stopped == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	_, stopped := w.counts()
	t.Errorf("the watch function did not return when its context was cancelled (stopped=%d)", stopped)
}

// Browsing is bursty — a flurry of questions, a pause while somebody reads the
// screen, another flurry — so continued asking must hold the watch open rather
// than letting it stop and start, since every start throws away its verdicts.
func TestAskingAgainKeepsTheWatchGoing(t *testing.T) {
	var w watched
	s := NewWatching(w.run, nil, time.Hour)
	fake := newClock()
	s.now = fake.now
	s.poll = func() time.Duration { return time.Millisecond }

	for i := 0; i < 10; i++ {
		s.Wanted()
		fake.advance(time.Minute) // well inside the idle window
		time.Sleep(2 * time.Millisecond)
	}
	if !s.Running() {
		t.Error("the watch stopped while questions were still arriving")
	}
	if starts, _ := w.counts(); starts != 1 {
		t.Errorf("the watch was started %d times during one burst, want 1", starts)
	}
}

// The rule that makes stopping safe at all: a verdict is trusted without being
// re-checked, so anything decided while nothing was watching describes a period
// nobody can account for.
func TestEveryStartForgetsWhatWasDecidedDuringTheGap(t *testing.T) {
	var w watched
	var forgotten int
	var mu sync.Mutex
	s := NewWatching(w.run, func() { mu.Lock(); forgotten++; mu.Unlock() }, time.Hour)
	fake := newClock()
	s.now = fake.now
	s.poll = func() time.Duration { return time.Millisecond }

	s.Wanted()
	fake.advance(2 * time.Hour)
	deadline := time.Now().Add(5 * time.Second)
	for s.Running() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	s.Wanted() // browsing resumes after the gap

	mu.Lock()
	defer mu.Unlock()
	if forgotten != 2 {
		t.Errorf("forgot the cache %d times across two starts, want 2", forgotten)
	}
}

// A nil supervisor is the command line, which asks once and exits. It must not
// panic and must not watch anything.
func TestANilSupervisorWatchesNothing(t *testing.T) {
	var s *Watching
	s.Wanted()
	if s.Running() {
		t.Error("a nil supervisor reported itself as watching")
	}
}

// A supervisor with nothing to watch yet must not claim to be watching.
//
// This is the shape of a bug that nearly shipped: Deps is passed by value into
// every service, so a supervisor attached to it after the services were built
// reached none of them. The browse service held a nil one, never asked for the
// watch, and would have served verdicts nothing was keeping honest — silently,
// because a nil supervisor is legitimate for the command line.
func TestNothingStartsUntilThereIsSomethingToRun(t *testing.T) {
	s := NewWatching(nil, nil, time.Hour)

	s.Wanted()
	if s.Running() {
		t.Fatal("started a watch with no watch function")
	}

	var w watched
	s.Watch(w.run)
	s.Wanted()
	if !s.Running() {
		t.Error("did not start once it was told what to run")
	}
}
