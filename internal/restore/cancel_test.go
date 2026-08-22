package restore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// Stopping a restore that is already copying.
//
// A restore of a large file is a long copy, and the only way to interrupt one is
// to check between reads. This wrapper is that check. Without it a cancelled
// restore keeps writing until the file is finished, so the window says it stopped
// and the disk says otherwise.

func TestReadingStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &contextReader{ctx: ctx, r: strings.NewReader(strings.Repeat("x", 1000))}

	buf := make([]byte, 10)
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("reading before cancellation: %v", err)
	}

	cancel()

	if _, err := r.Read(buf); !errors.Is(err, context.Canceled) {
		t.Errorf("reading after cancellation returned %v, want context.Canceled", err)
	}
}

// The check happens before the read, not after, so a cancelled restore does not
// copy one more block on its way out.
func TestNothingIsReadOnceCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	counted := &countingReader{r: strings.NewReader("some contents")}
	r := &contextReader{ctx: ctx, r: counted}

	if _, err := r.Read(make([]byte, 4)); err == nil {
		t.Error("a cancelled reader read anyway")
	}
	if counted.reads != 0 {
		t.Errorf("the underlying reader was read %d times after cancellation", counted.reads)
	}
}

// An uncancelled read passes straight through, including the end of the file,
// which must arrive as io.EOF rather than as an error about the context.
func TestAnUncancelledReadPassesThroughToTheEnd(t *testing.T) {
	r := &contextReader{ctx: context.Background(), r: bytes.NewReader([]byte("abc"))}

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading to the end: %v", err)
	}
	if string(got) != "abc" {
		t.Errorf("read %q", got)
	}
}

// A deadline that has passed reads as its own reason, so a restore stopped by a
// timeout does not report itself as cancelled by a person.
func TestAnExpiredDeadlineSaysSo(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	r := &contextReader{ctx: ctx, r: strings.NewReader("contents")}
	if _, err := r.Read(make([]byte, 4)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want a deadline error", err)
	}
}

type countingReader struct {
	r     io.Reader
	reads int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}
