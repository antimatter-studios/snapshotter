// Package events is how the parts of this application that run as separate
// processes tell each other what happened.
//
// The tripwire and the scheduled task are launchd agents. The window is a third
// process, usually not running when either of them acts. So nothing either agent
// learns can be held in memory for the window to show later — by the time anyone
// opens the window, the process that knew has exited.
//
// This is the smallest thing that fixes that: a file of one JSON object per line,
// appended to by whoever has something to report and read by whoever wants to
// display it. No daemon, no socket, no schema migration — a line is either
// parseable or skipped.
package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Kind says what happened. Kept as strings rather than an enum because these are
// written to a file that outlives the build that wrote them: a reader from an
// older version should skip a kind it does not know, not fail to parse the file.
const (
	// KindBulkDeletion is the tripwire firing.
	KindBulkDeletion = "bulk-deletion"
)

// Event is one thing worth telling the user about, recorded by whichever process
// saw it.
type Event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	// Where names the directories involved, commonest first. Empty for kinds
	// that do not happen anywhere in particular.
	Where []string `json:"where,omitempty"`
	// Snapshot is the restore point taken in response, when there was one. Its
	// absence is meaningful: it says the response failed.
	Snapshot string `json:"snapshot,omitempty"`
	// Note carries anything the writer wants a person to read, such as why the
	// response failed.
	Note string `json:"note,omitempty"`
}

// maxRows is how many lines the file keeps.
//
// Trimmed to the newest rather than emptied: emptying at the limit would throw
// away exactly the recent events something is displaying, so the list would go
// blank at the moment it had most to say. A hundred lines is a few kilobytes and
// more history than any screen shows.
const maxRows = 100

// mu serialises writers within one process. Between processes the file lock does
// it; this stops two goroutines in the same process interleaving.
var mu sync.Mutex

// Append records one event.
//
// Failures are returned rather than swallowed, but callers should treat them as
// unimportant: this is a convenience for the interface, and losing a line from it
// matters far less than whatever the caller was actually doing.
func Append(e Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	// Held across the write and the trim so a reader never sees the file
	// mid-rewrite and a second writer never appends into the copy being replaced.
	if err := lock(f); err != nil {
		return err
	}
	defer unlock(f)

	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return trimLocked(path)
}

// Recent returns the newest events, newest first, at most n of them.
//
// A file that cannot be read at all yields nothing and no error: an interface
// asking "what happened lately" on a machine where nothing has is not an error
// case, and neither is a first run.
func Recent(n int) ([]Event, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	all := parse(data)
	// Newest first, because every caller wants the last few and none of them
	// wants to reverse a slice to get them.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	return all, nil
}

// parse reads whole lines, skipping any it cannot understand.
//
// A half-written last line is possible if a writer died mid-append, and a line
// from a later version may carry fields this build has never heard of. Neither is
// a reason to discard the rest of the file.
func parse(data []byte) []Event {
	var out []Event
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

// trimLocked drops the oldest lines once the file is longer than maxRows. The
// caller holds both the mutex and the file lock.
func trimLocked(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= maxRows {
		return nil
	}

	kept := strings.Join(lines[len(lines)-maxRows:], "\n") + "\n"
	// Written in place rather than renamed over: a rename would leave any other
	// process that has the file open appending into an inode nobody will read
	// again. Truncate-and-write is safe here because the file lock is held.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(kept)
	return err
}

// Path is where the file lives: beside the mounts, under the user's own
// Application Support, which every process here can already write to without
// authorization.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("events: cannot find the home directory: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "Snapshotter", "events.jsonl"), nil
}

func lock(f *os.File) error   { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }
func unlock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
