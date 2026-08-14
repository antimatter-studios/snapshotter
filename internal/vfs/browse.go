package vfs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one item in a directory listing, from either the live filesystem or
// a mounted snapshot.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Mode    string    `json:"mode"`
	// Symlink holds the link target for symbolic links. Links are reported
	// rather than followed, so a link into an uncovered volume cannot silently
	// take a listing outside the snapshot.
	Symlink string `json:"symlink,omitempty"`
	// Unreadable marks an entry whose metadata could not be read, usually
	// because macOS privacy protection covers it. The name is still shown.
	Unreadable bool `json:"unreadable,omitempty"`
}

// ListDir reads one directory without descending into it.
func ListDir(dir string) ([]Entry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("vfs: reading %s: %w", dir, err)
	}

	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		full := filepath.Join(dir, item.Name())
		e := Entry{Name: item.Name(), Path: full, IsDir: item.IsDir()}

		info, err := item.Info()
		if err != nil {
			e.Unreadable = true
			entries = append(entries, e)
			continue
		}
		e.Size = info.Size()
		e.ModTime = info.ModTime()
		e.Mode = info.Mode().Perm().String()

		if info.Mode()&os.ModeSymlink != 0 {
			e.IsDir = false
			if target, err := os.Readlink(full); err == nil {
				e.Symlink = target
			}
		}
		entries = append(entries, e)
	}

	sortEntries(entries)
	return entries, nil
}

// Stat describes a single path.
func Stat(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, fmt.Errorf("vfs: reading %s: %w", path, err)
	}
	e := Entry{
		Name:    filepath.Base(path),
		Path:    path,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Mode:    info.Mode().Perm().String(),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		e.IsDir = false
		if target, err := os.Readlink(path); err == nil {
			e.Symlink = target
		}
	}
	return e, nil
}

// Exists reports whether a path is present, without distinguishing why not.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// sortEntries puts directories first, then orders case-insensitively, which is
// how the Finder presents a folder.
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})
}
