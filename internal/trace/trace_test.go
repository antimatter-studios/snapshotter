package trace

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
)

// The gate on verbose logging.
//
// Worth testing because its failure mode is silence: a broken gate produces no
// output and no error, which is indistinguishable from a machine with nothing to
// say. That is exactly how the command line went a long time ignoring
// logging.verbose — nobody notices the absence of lines they were not sure they
// would see.

// capture redirects the standard logger for the duration of a test.
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
		SetEnabled(false)
	})
	return &buf
}

func TestNothingIsPrintedUntilItIsSwitchedOn(t *testing.T) {
	out := capture(t)
	SetEnabled(false)
	out.Reset()

	Printf("a line nobody asked for")
	if out.Len() != 0 {
		t.Errorf("printed while off: %q", out.String())
	}

	SetEnabled(true)
	out.Reset()
	Printf("a line somebody asked for")
	if !strings.Contains(out.String(), "somebody asked for") {
		t.Errorf("printed nothing while on: %q", out.String())
	}
}

// The change is announced, and only the change. A settings file saved every few
// seconds would otherwise fill the log with the news that nothing happened.
func TestOnlyAChangeIsAnnounced(t *testing.T) {
	out := capture(t)
	SetEnabled(false)
	out.Reset()

	SetEnabled(true)
	first := out.String()
	if !strings.Contains(first, "on") {
		t.Errorf("switching on said %q", first)
	}

	out.Reset()
	SetEnabled(true)
	if out.Len() != 0 {
		t.Errorf("switching on again said %q", out.String())
	}

	out.Reset()
	SetEnabled(false)
	if !strings.Contains(out.String(), "off") {
		t.Errorf("switching off said %q", out.String())
	}
}

func TestEnabledReportsTheState(t *testing.T) {
	capture(t)

	SetEnabled(true)
	if !Enabled() {
		t.Error("switched on, reports off")
	}
	SetEnabled(false)
	if Enabled() {
		t.Error("switched off, reports on")
	}
}

// The settings watcher writes this from its own goroutine while anything logging
// reads it. The race detector is the point of this test; the assertions are
// incidental.
func TestItSurvivesBeingReadAndWrittenAtOnce(t *testing.T) {
	capture(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				SetEnabled(j%2 == 0)
				_ = Enabled()
				Printf("from %d", n)
			}
		}(i)
	}
	wg.Wait()
}
