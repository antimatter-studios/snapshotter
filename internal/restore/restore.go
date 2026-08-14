// Package restore copies files out of a mounted snapshot back onto the live
// filesystem.
//
// A restored copy is an ordinary file: it no longer depends on the snapshot and
// survives that snapshot being pruned. Nothing here writes to the snapshot,
// which is mounted read-only in any case.
package restore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Mode decides what happens when something already exists at the destination.
type Mode string

const (
	// SideBySide never touches the existing file. The restored copy lands
	// beside it with a suffix naming the snapshot it came from. This is the
	// default because a restore is usually an act of comparison, and the file
	// on disk may be the one worth keeping.
	SideBySide Mode = "sideBySide"
	// Replace puts the restored copy at the original path, after moving
	// whatever was there to a .bak- copy. Nothing is deleted.
	Replace Mode = "replace"
)

// Options tunes a restore.
type Options struct {
	Mode Mode
	// Tag names the restored copy, and is expected to be the snapshot's date
	// stamp so the file records where it came from. Defaults to the current
	// time when empty.
	Tag string
	Now time.Time
}

// Result reports what a restore produced.
type Result struct {
	// Destination is where the restored copy actually landed, which is not the
	// requested path in SideBySide mode.
	Destination string `json:"destination"`
	// BackedUp is where a pre-existing file was moved to, if any.
	BackedUp string `json:"backedUp,omitempty"`
	Files    int    `json:"files"`
	Dirs     int    `json:"dirs"`
	Bytes    int64  `json:"bytes"`
}

// Restore copies source (inside a mounted snapshot) to target (a live path).
func Restore(ctx context.Context, source, target string, opt Options) (Result, error) {
	var res Result

	info, err := os.Lstat(source)
	if err != nil {
		return res, fmt.Errorf("restore: reading %s: %w", source, err)
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	if opt.Mode == "" {
		opt.Mode = SideBySide
	}

	dest, backedUp, err := resolveDestination(target, opt)
	if err != nil {
		return res, err
	}
	res.Destination = dest
	res.BackedUp = backedUp

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return res, fmt.Errorf("restore: creating %s: %w", filepath.Dir(dest), err)
	}
	if err := copyAny(ctx, source, dest, info, &res); err != nil {
		return res, err
	}
	return res, nil
}

// resolveDestination applies the mode's rule about existing files. Neither mode
// destroys anything: SideBySide leaves the original untouched, Replace moves it
// aside first.
func resolveDestination(target string, opt Options) (dest, backedUp string, err error) {
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return target, "", nil
	}

	tag := opt.Tag
	if tag == "" {
		tag = opt.Now.Format("2006-01-02-150405")
	}

	switch opt.Mode {
	case Replace:
		backup := uniquePath(target + ".bak-" + tag)
		if err := os.Rename(target, backup); err != nil {
			return "", "", fmt.Errorf("restore: moving the existing %s aside: %w", target, err)
		}
		return target, backup, nil
	default:
		return uniquePath(target + ".restored-" + tag), "", nil
	}
}

// uniquePath appends a counter until the path is free, so a second restore of
// the same snapshot cannot silently overwrite the first.
func uniquePath(path string) string {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return path
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", path, i)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func copyAny(ctx context.Context, source, dest string, info os.FileInfo, res *Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(source)
		if err != nil {
			return fmt.Errorf("restore: reading link %s: %w", source, err)
		}
		if err := os.Symlink(target, dest); err != nil {
			return fmt.Errorf("restore: writing link %s: %w", dest, err)
		}
		res.Files++
		return nil

	case info.IsDir():
		return copyDir(ctx, source, dest, info, res)

	case info.Mode().IsRegular():
		return copyFile(ctx, source, dest, info, res)

	default:
		// Sockets, devices and fifos carry no recoverable content.
		return nil
	}
}

func copyDir(ctx context.Context, source, dest string, info os.FileInfo, res *Result) error {
	if err := os.MkdirAll(dest, info.Mode().Perm()); err != nil {
		return fmt.Errorf("restore: creating %s: %w", dest, err)
	}
	res.Dirs++

	items, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("restore: reading %s: %w", source, err)
	}
	for _, item := range items {
		childInfo, err := item.Info()
		if err != nil {
			return fmt.Errorf("restore: reading %s: %w", filepath.Join(source, item.Name()), err)
		}
		if err := copyAny(ctx, filepath.Join(source, item.Name()), filepath.Join(dest, item.Name()), childInfo, res); err != nil {
			return err
		}
	}
	// Directory timestamps are set after its contents, which would otherwise
	// bump them again.
	_ = os.Chtimes(dest, info.ModTime(), info.ModTime())
	return nil
}

func copyFile(ctx context.Context, source, dest string, info os.FileInfo, res *Result) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("restore: opening %s: %w", source, err)
	}
	defer in.Close()

	// O_EXCL: a destination appearing between the check and the write is a
	// surprise worth failing on rather than overwriting.
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("restore: creating %s: %w", dest, err)
	}

	written, err := io.Copy(out, &contextReader{ctx: ctx, r: in})
	if closeErr := out.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(dest)
		return fmt.Errorf("restore: writing %s: %w", dest, err)
	}

	res.Files++
	res.Bytes += written
	_ = os.Chtimes(dest, info.ModTime(), info.ModTime())
	return nil
}

// contextReader aborts a long copy when the context is cancelled.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Describe renders a one-line summary for the UI.
func (r Result) Describe() string {
	var parts []string
	if r.Files > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s)", r.Files))
	}
	if r.Dirs > 0 {
		parts = append(parts, fmt.Sprintf("%d folder(s)", r.Dirs))
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing")
	}
	return fmt.Sprintf("restored %s to %s", strings.Join(parts, ", "), r.Destination)
}
