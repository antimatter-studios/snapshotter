// Package trace is logging that is off unless somebody asks for it.
//
// It exists because a folder reported "could not check" and the application knew
// exactly why and threw it away — and three separate explanations for that were
// all wrong, each a guess made from outside a process that had the answer inside
// it. Anything that decides something a person will read should be able to say
// how it decided, on request.
//
// Off by default: this logs per directory and per file, which on a home folder
// is tens of thousands of lines. It is a thing to turn on when something is
// wrong, not a thing to leave running.
package trace

import (
	"log"
	"sync/atomic"
)

// on is read on every call from several goroutines and written when the settings
// file changes, so it is atomic rather than a plain bool.
var on atomic.Bool

// SetEnabled turns verbose logging on or off. Called at startup and whenever the
// settings file changes, so it can be turned on without restarting — which
// matters, because restarting to look at a problem is how you lose the problem.
func SetEnabled(enabled bool) {
	if on.Swap(enabled) != enabled {
		// Logged at both edges, and not through Printf below, so the transition
		// is visible even in a log that is otherwise empty.
		if enabled {
			log.Print("verbose logging on")
		} else {
			log.Print("verbose logging off")
		}
	}
}

// Enabled reports whether anything would be logged, for callers that would have
// to do real work to build a message.
func Enabled() bool { return on.Load() }

// Printf logs when verbose logging is on, and costs a single atomic load when it
// is not.
func Printf(format string, args ...any) {
	if on.Load() {
		log.Printf(format, args...)
	}
}
