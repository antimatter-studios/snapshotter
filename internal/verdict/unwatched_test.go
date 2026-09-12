package verdict

import "testing"

// A verdict is trusted without being re-checked, and that is only safe while the
// filesystem is being watched without interruption. If watching stops and starts
// — which it now does, because watching costs a great deal and browsing is rare —
// everything decided before the gap describes a period nobody was looking at.
func TestAGapInWatchingForgetsVerdictsButKeepsChanges(t *testing.T) {
	c := New()
	c.Put("snap", "/Users/someone/projects", Answer{Verdict: Same})
	c.Put("snap", "/Users/someone/docs", Answer{Verdict: Modified, ChangedPath: "/Users/someone/docs/a.txt"})

	if _, ok := c.Get("snap", "/Users/someone/projects"); !ok {
		t.Fatal("the verdict was not remembered in the first place")
	}

	c.Unwatched()

	if _, ok := c.Get("snap", "/Users/someone/projects"); ok {
		t.Error("a verdict survived a gap in watching, so it claims something about a period nobody saw")
	}
	if _, ok := c.Get("snap", "/Users/someone/docs"); ok {
		t.Error("a verdict survived a gap in watching")
	}
	// The recorded change stays: it is re-checked with a stat before it is
	// believed, so a stale one costs a stat rather than being wrong.
	if got, ok := c.ChangedPathUnder("snap", "/Users/someone"); !ok || got != "/Users/someone/docs/a.txt" {
		t.Errorf("a recorded change was dropped: got %q/%v", got, ok)
	}
}
