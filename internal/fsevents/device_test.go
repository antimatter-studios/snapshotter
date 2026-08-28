package fsevents

import (
	"os"
	"syscall"
)

// sameDevice reports whether two paths are on one volume, which is how a test
// finds where a directory's volume begins.
func sameDevice(a, b os.FileInfo) bool {
	x, ok := a.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	y, ok := b.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return x.Dev == y.Dev
}
