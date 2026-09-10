// Package audit records every destructive act this application performs.
//
// It exists because of a question that could not be answered. Three snapshots
// disappeared overnight and nothing in this application could say whether it had
// deleted them. The scheduled task logs to a file launchd captures, so its
// prunes were on record — but the window writes nowhere at all, so a deletion
// from a row button, or the sweep that follows taking a snapshot of one disk,
// left no trace anywhere. "The log shows no deletion" therefore proved nothing,
// and answering the user's question needed the system log and a day of work.
//
// The record has to be written where the deleting happens, not where it is
// requested. Callers come and go — a button, a scheduled run, a sweep, whatever
// is added next — and a record kept at each call site is a record that a new
// call site silently omits. So apfs.Delete and apfs.DeleteOn write it, and there
// is no way to remove a snapshot through this application that does not.
//
// Writing it must never prevent or fail the act it describes. A disk that will
// not take the line is a worse reason to leave a snapshot undeleted than it is
// to lose the line, and a caller that treated an audit failure as an operation
// failure would report a deletion that in fact happened.
package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Path is the file, under the user's own log directory beside the two the
// launchd agents write. A fixed name rather than a configured one: this is the
// record somebody reaches for when they distrust the application, and a location
// that could be moved by a setting is a location that can be moved out of the
// way.
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", "snapshotter-audit.log")
}

// mu serialises the appends. Several deletions can be in flight at once — a
// batch unmount, a sweep across volumes — and interleaved partial lines would
// corrupt the one record that is supposed to be trustworthy.
var mu sync.Mutex

// now is replaceable so tests can assert on the timestamp rather than parse
// whatever the clock said.
var now = time.Now

// Deleted records the removal of a snapshot, whether or not it worked.
//
// A failed deletion is recorded too. "It was attempted and refused" is a
// different fact from "it was never attempted", and only one of them means the
// snapshot should still be there — which is exactly the distinction somebody
// reading this file after a surprise needs to draw.
func Deleted(what, where, why string, err error) {
	outcome := "ok"
	if err != nil {
		outcome = "FAILED: " + strings.ReplaceAll(err.Error(), "\n", " ")
	}
	write(fmt.Sprintf("deleted %s on %s (%s) %s", what, where, why, outcome))
}

// Note records something destructive that is not a deletion, for whatever is
// added later. Kept deliberately vague in shape and specific in use.
func Note(text string) { write(text) }

func write(line string) {
	path := Path()
	if path == "" {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// Best effort throughout, and silent on failure. The alternative is logging
	// about the failure to log, which reaches a place nobody is reading either.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", now().Format("2006-01-02 15:04:05"), line)
}
