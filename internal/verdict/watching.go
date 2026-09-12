package verdict

import (
	"context"
	"log"
	"sync"
	"time"
)

// Watching runs the filesystem watch only while somebody is browsing.
//
// The watch is not cheap. It is recursive over a home directory and every volume
// holding snapshots, it asks for every kind of event, and it delivers one call
// per event. The cache it keeps honest is read in exactly one situation: a person
// looking at a folder listing inside a snapshot.
//
// Those two facts were never connected. The watch started when the window opened
// and ran with a context nothing ever cancelled, so an application sitting in the
// menu bar watched every write on the machine for as long as it was running. On
// the machine that reported this, that was 22.5 hours of CPU across two days,
// while disk images were being built and nobody was browsing anything at all.
//
// So it starts when a verdict is wanted and stops when none has been wanted for a
// while. The cost follows the benefit instead of preceding it.
type Watching struct {
	// run is the watch itself, which must return when its context is cancelled.
	run func(context.Context)
	// idle is how long after the last question the watch keeps going.
	//
	// Not zero, because browsing is bursty: a listing is a flurry of questions,
	// then a pause while somebody reads the screen, then another flurry. Stopping
	// between them would mean starting again immediately, and every start throws
	// away the verdicts it has — so a short idle costs a little watching and a
	// long one costs the walks it was meant to save.
	idle time.Duration
	// onStart is called before each start, to forget what was decided during the
	// gap. See Cache.Unwatched.
	onStart func()

	// poll is how often the idle check runs. Separate from idle so a test can
	// drive the clock without waiting on a real one — and so the check's cost
	// stays a property of this type rather than of whatever idle is set to.
	poll func() time.Duration

	mu      sync.Mutex
	cancel  context.CancelFunc
	stopAt  time.Time
	running bool
	now     func() time.Time
}

// DefaultIdle is how long the watch outlives the last question asked of the
// cache.
//
// Two minutes: long enough to cover reading a screen and clicking into the next
// folder, short enough that closing the window stops the cost promptly.
const DefaultIdle = 2 * time.Minute

// NewWatching builds a supervisor for a watch function.
//
// run is expected to return when its context is cancelled. onStart may be nil;
// where it is not, it runs before every start — including restarts, which is the
// point of it.
func NewWatching(run func(context.Context), onStart func(), idle time.Duration) *Watching {
	if idle <= 0 {
		idle = DefaultIdle
	}
	w := &Watching{run: run, onStart: onStart, idle: idle, now: time.Now}
	w.poll = func() time.Duration { return w.idle / 4 }
	return w
}

// Wanted says a verdict has just been asked for, which starts the watch if it is
// not running and keeps it running if it is.
//
// Called on the hot path — once per folder in a listing — so it does as little as
// possible when the watch is already going: take a lock, move a deadline, and
// return.
func (w *Watching) Wanted() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stopAt = w.now().Add(w.idle)
	// Nothing to run means nothing to start. The command line builds a cache and
	// never says what to watch, and is right not to: it asks once and exits.
	if w.running || w.run == nil {
		return
	}

	// Everything decided before now was decided while nothing was watching, so it
	// describes a period nobody can account for.
	if w.onStart != nil {
		w.onStart()
	}

	// Said out loud, both ways.
	//
	// This starts and stops a recursive filesystem watch — the most expensive
	// thing this application does in the background, and the thing that cost 22.5
	// hours of CPU when it ran unconditionally. Whether it is running was
	// previously knowable only by counting threads from outside, which is no way
	// to answer "is it doing that again".
	log.Printf("watching the filesystem for changes: somebody is browsing")

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.running = true
	go w.run(ctx)
	go w.stopWhenIdle()
}

// stopWhenIdle ends the watch once nothing has asked for a verdict recently.
//
// A poll rather than a timer reset per question: Wanted is called once per folder
// in a listing, and rescheduling a timer that often is work on the hot path to
// save a check that happens twice a minute.
func (w *Watching) stopWhenIdle() {
	for {
		time.Sleep(w.poll())

		w.mu.Lock()
		if !w.running {
			w.mu.Unlock()
			return
		}
		if w.now().Before(w.stopAt) {
			w.mu.Unlock()
			continue
		}
		cancel := w.cancel
		w.cancel, w.running = nil, false
		w.mu.Unlock()

		log.Printf("no longer watching the filesystem: nothing has asked for a verdict in %s", w.idle)
		if cancel != nil {
			cancel()
		}
		return
	}
}

// Watch says what to run. It is separate from construction because the
// supervisor has to exist before Deps is copied into the services, while what it
// runs is only known once the window is being built.
//
// Calling it while the watch is going does not restart it; the next start uses
// the new function. Nothing calls it twice today.
func (w *Watching) Watch(run func(context.Context)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.run = run
}

// Running reports whether the watch is going, for tests and for anything that
// wants to say so.
func (w *Watching) Running() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}
