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
