package verdict

import (
	"fmt"
	"testing"
)

// The cost of one filesystem event, against the size of the cache.
//
// It used to scan every cached entry per event, asking of each whether it was
// the path or an ancestor of it. The watcher feeding this is recursive over a
// home directory, so a build writing a disk image delivers events by the
// thousand per second — and each one swept the whole cache under the lock. On
// the machine that found it the window sat at half a core for two days.
//
// A benchmark rather than an assertion about time, because "faster" is worth
// nothing without a number and the number has to be able to regress visibly.
func BenchmarkTouched(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("cache-of-%d", size), func(b *testing.B) {
			c := New()
			c.entries["snap"] = make(map[string]Answer, size)
			c.changed["snap"] = make(map[string]bool, size)
			for i := 0; i < size; i++ {
				p := fmt.Sprintf("/Users/someone/projects/thing-%d/src/deep/nested", i)
				c.entries["snap"][p] = Answer{Verdict: Same}
				c.changed["snap"][p] = true
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// A build writing into one of them, which is the event this gets
				// millions of.
				c.Touched("/Users/someone/projects/thing-7/src/deep/nested/out.dmg")
			}
		})
	}
}

// The read path: asking whether anything under a folder is known to differ.
//
// Called once per row of a listing, and twice per listing — once for the pass
// that reports only what is known, once for the pass that walks. It used to scan
// every recorded difference, asking of each whether it sat beneath the folder.
//
// The persisted half of this cache never did that: change_detection carries a
// generated parent column with an index on (snapshot, parent), so SQLite answers
// the same question with a lookup. This is the memory half catching up.
func BenchmarkChangedPathUnder(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("differences-%d", size), func(b *testing.B) {
			c := New()
			for i := 0; i < size; i++ {
				c.Put("snap", fmt.Sprintf("/Users/someone/projects/thing-%d", i), Answer{
					Verdict:     Modified,
					ChangedPath: fmt.Sprintf("/Users/someone/projects/thing-%d/src/deep/changed.go", i),
				})
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.ChangedPathUnder("snap", "/Users/someone/projects/thing-7")
			}
		})
	}
}
