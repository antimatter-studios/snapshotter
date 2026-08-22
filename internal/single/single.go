// Package single keeps one window at a time.
package single

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Why a lock and not a search of the process table:
//
// This started as `pgrep -f /Applications/Snapshotter.app/...`, which finds only
// the installed copy — so a development build refused to start beside the
// installed one, and two installed copies, or two development builds, were not
// prevented at all. The path was the thing being matched, and the path is exactly
// what differs between the copies that need catching.
//
// A lock catches all of them, whatever they were built from and wherever they
// live. It also releases when the holder dies, including on a crash, which a pid
// file does not: a stale pid file either blocks a machine until someone deletes it
// or has to be validated against a process table, and validating it reintroduces
// the guessing the file was meant to replace.
//
// Why it matters more than tidiness: two copies put two identical icons in the
// menu bar, and only the one in /Applications holds the Full Disk Access grant, so
// the working copy looks the same and cannot mount anything. A copy left running
// is invisible — one sat at 300% CPU for nineteen hours on the author's machine
// before anyone noticed.

// ErrAlreadyRunning reports that another window holds the lock. Held is what it
// could learn about the holder, which may be empty.
type ErrAlreadyRunning struct {
	// Held names the executable the running copy was launched from, when the lock
	// file says. It is the answer to "which of these two is the one I am looking
	// at", which is the question the menu bar cannot answer.
	Held string
	PID  int
}

func (e *ErrAlreadyRunning) Error() string {
	var b strings.Builder
	b.WriteString("Snapshotter is already running")
	if e.PID > 0 {
		fmt.Fprintf(&b, " (process %d", e.PID)
		if e.Held != "" {
			fmt.Fprintf(&b, ", from %s", e.Held)
		}
		b.WriteString(")")
	}
	b.WriteString(".\nTwo copies put two identical icons in the menu bar, and only the one in " +
		"/Applications holds the Full Disk Access grant — so the second looks the same and cannot " +
		"mount anything.\nQuit the running one first, or set SNAPSHOTTER_ALLOW_SECOND_COPY=1 if that " +
		"is what you meant.")
	return b.String()
}

// AllowSecondCopy is the escape hatch, for the case where two really are wanted —
// comparing two builds side by side, most often.
const AllowSecondCopy = "SNAPSHOTTER_ALLOW_SECOND_COPY"

// Hold takes the lock, and returns the function that gives it up.
//
// The returned release is safe to call once; the lock is also released by the
// process exiting, which is what makes a crash recoverable without anyone
// deleting a file.
func Hold(path string) (func(), error) {
	if os.Getenv(AllowSecondCopy) == "1" {
		return func() {}, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("single: preparing %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("single: opening %s: %w", path, err)
	}

	// Non-blocking: the point is to refuse and say so, not to wait behind a window
	// that may be open for days.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		held, pid := describeHolder(f)
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &ErrAlreadyRunning{Held: held, PID: pid}
		}
		// Some other filesystem trouble. Reported as itself rather than as a second
		// copy, because refusing to start over an unreadable lock file would be a
		// worse failure than the one being prevented.
		return nil, fmt.Errorf("single: locking %s: %w", path, err)
	}

	// Who we are, for whoever is refused next. Written after the lock is held, so
	// two processes cannot interleave their answers.
	if err := f.Truncate(0); err == nil {
		self, _ := os.Executable()
		fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), self)
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// Path is where the lock lives, given the directory the application keeps its
// settings in.
//
// Beside the settings rather than in a temporary directory: this has to be the
// same file for every copy of the application belonging to one person, and a
// per-user path that already exists and is already respected by an environment
// variable is the one place guaranteed to be exactly that.
func Path(configDir string) string { return filepath.Join(configDir, "window.lock") }

// describeHolder reads what the holder wrote about itself. Anything unreadable or
// half-written yields nothing, because a partial answer here is only ever used to
// make a message friendlier.
func describeHolder(f *os.File) (string, int) {
	buf := make([]byte, 4096)
	n, err := f.ReadAt(buf, 0)
	if n == 0 && err != nil {
		return "", 0
	}
	lines := strings.Split(strings.TrimSpace(string(buf[:n])), "\n")
	pid := 0
	if len(lines) > 0 {
		pid, _ = strconv.Atoi(strings.TrimSpace(lines[0]))
	}
	held := ""
	if len(lines) > 1 {
		held = strings.TrimSpace(lines[1])
	}
	return held, pid
}
