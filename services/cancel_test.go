package services

import (
	"context"
	"sync"
	"testing"
)

// One walk at a time, and the bookkeeping that makes that true.
//
// A comparison walks a tree, which takes as long as the tree is big. Starting a
// second one has to stop the first, or two walks compete for the disk and the
// window shows whichever finishes last — which is not necessarily the one that
// was asked for. The run identifier exists so a walk that finishes after being
// replaced does not clear the newer one's cancellation.

func TestCancellingWithNothingRunningIsHarmless(t *testing.T) {
	d := &DiffService{}

	// Called whenever the window closes a comparison, whether one was running or
	// not. It must not panic on the common case.
	d.Cancel()
	d.Cancel()
}

func TestStartingAWalkCancelsTheOneBefore(t *testing.T) {
	d := &DiffService{}

	firstCancelled := false
	first := func() { firstCancelled = true }
	id1 := d.replaceRunning(first)

	secondCancelled := false
	second := func() { secondCancelled = true }
	id2 := d.replaceRunning(second)

	if !firstCancelled {
		t.Error("starting a second walk left the first one running")
	}
	if secondCancelled {
		t.Error("the second walk was cancelled as it started")
	}
	if id2 <= id1 {
		t.Errorf("run ids did not advance: %d then %d", id1, id2)
	}
}

// The case the identifier exists for. A walk that was already replaced finishes
// late and must not clear the cancellation belonging to the one that replaced it,
// or Cancel afterwards does nothing and the newer walk runs to completion
// unstoppably.
func TestALateFinishDoesNotDisarmTheCurrentWalk(t *testing.T) {
	d := &DiffService{}

	stale := d.replaceRunning(func() {})
	current := d.replaceRunning(func() {})
	if stale == current {
		t.Fatal("the two runs share an identifier")
	}

	// The stale walk finishes now, after being replaced.
	d.finished(stale, func() {})

	cancelled := false
	d.mu.Lock()
	armed := d.cancel != nil
	d.mu.Unlock()
	if !armed {
		t.Fatal("a late finish cleared the current walk's cancellation")
	}

	// And the current one is still stoppable.
	d.mu.Lock()
	d.cancel = func() { cancelled = true }
	d.mu.Unlock()
	d.Cancel()
	if !cancelled {
		t.Error("the current walk could not be cancelled")
	}
}

func TestFinishingTheCurrentWalkDisarmsIt(t *testing.T) {
	d := &DiffService{}

	id := d.replaceRunning(func() {})
	d.finished(id, func() {})

	d.mu.Lock()
	armed := d.cancel != nil
	d.mu.Unlock()
	if armed {
		t.Error("the walk finished and its cancellation is still armed")
	}
}

// The window can close a comparison while the walk is starting another, so these
// are reached from more than one goroutine. The race detector is the assertion.
func TestTheBookkeepingSurvivesConcurrentUse(t *testing.T) {
	d := &DiffService{}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, cancel := context.WithCancel(context.Background())
				id := d.replaceRunning(cancel)
				d.Cancel()
				d.finished(id, cancel)
			}
		}()
	}
	wg.Wait()
}
