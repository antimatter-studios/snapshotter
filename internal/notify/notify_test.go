package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The text in a notification is assembled from error messages and file names,
// neither of which this package controls. A quote in either used to be enough to
// turn the notification into an AppleScript syntax error — at the moment somebody
// most needs to be told that their schedule has stopped.

func TestQuotesAndBackslashesCannotBreakTheScript(t *testing.T) {
	for _, tc := range []struct {
		name     string
		subtitle string
		body     string
		want     string
	}{
		{
			name:     "a quoted file name",
			subtitle: `Could not snapshot "Documents"`,
			body:     `it said "no"`,
			want:     `\"Documents\"`,
		},
		{
			name:     "a windows-looking path",
			subtitle: "failed",
			body:     `C:\temp\thing`,
			want:     `C:\\temp\\thing`,
		},
		{
			name:     "an attempt to end the statement early",
			subtitle: `x" with title "gotcha`,
			body:     "body",
			want:     `x\" with title \"gotcha`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := script(tc.subtitle, tc.body)
			if !strings.Contains(got, tc.want) {
				t.Errorf("want %q escaped in:\n  %s", tc.want, got)
			}
			// Three quoted arguments and nothing more: body, title, subtitle. Each
			// escaped quote contributes one character to the raw count, so removing
			// them leaves only the six that delimit the arguments. An unescaped quote
			// in the input would push it higher.
			if n := strings.Count(got, `"`) - strings.Count(got, `\"`); n != 6 {
				t.Errorf("want 6 structural quotes, got %d in:\n  %s", n, got)
			}
		})
	}
}

func TestTheScriptIsAttributedToTheApplication(t *testing.T) {
	got := script("something happened", "details")
	for _, want := range []string{"display notification", Title, "something happened", "details"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in: %s", want, got)
		}
	}
}

func TestSendPostsTheScriptItBuilt(t *testing.T) {
	original := run
	t.Cleanup(func() { run = original })

	var posted string
	run = func(_ context.Context, s string) (string, error) {
		posted = s
		return "", nil
	}

	if err := Send(context.Background(), "subtitle here", "body here"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if posted != script("subtitle here", "body here") {
		t.Errorf("posted something other than what it built:\n  %s", posted)
	}
}

// Notifications are best-effort: a machine with them switched off must not fail
// the work that prompted one. The error is returned for logging, and it has to
// carry what osascript said or it is useless.
func TestAFailureIsReportedWithWhatTheToolSaid(t *testing.T) {
	original := run
	t.Cleanup(func() { run = original })

	run = func(context.Context, string) (string, error) {
		return "  execution error: not allowed  ", errors.New("exit status 1")
	}

	err := Send(context.Background(), "s", "b")
	if err == nil {
		t.Fatal("a failure was swallowed")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("the tool's own words were dropped: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("the underlying error was dropped: %v", err)
	}
}
