package elevate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAppleScriptStringEscapesQuotesAndBackslashes(t *testing.T) {
	got := appleScriptString.Replace(`/sbin/mount_apfs -s "x\y" /vol`)
	want := `/sbin/mount_apfs -s \"x\\y\" /vol`
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestAuthorizationStatementIncludesThePrompt(t *testing.T) {
	got := authorizationStatement("/sbin/mount_apfs x", "Snapshotter is opening a snapshot.")
	want := `do shell script "/sbin/mount_apfs x" with prompt "Snapshotter is opening a snapshot." with administrator privileges`
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

// Without a reason the clause is left out entirely rather than emitted empty: an
// empty prompt is a worse dialog than the system's default one.
func TestAuthorizationStatementOmitsAnEmptyPrompt(t *testing.T) {
	got := authorizationStatement("ls", "")
	if strings.Contains(got, "with prompt") {
		t.Errorf("emitted an empty prompt clause: %s", got)
	}
	if got != `do shell script "ls" with administrator privileges` {
		t.Errorf("unexpected statement: %s", got)
	}
}

// The reason is user-facing text and will contain punctuation; an unescaped
// quote in it would break the AppleScript rather than the prompt.
func TestAuthorizationStatementEscapesTheReason(t *testing.T) {
	got := authorizationStatement("ls", `the "Documents" folder`)
	if !strings.Contains(got, `\"Documents\"`) {
		t.Errorf("did not escape quotes in the reason: %s", got)
	}
}

// What matters here is not running osascript — no test can answer an
// authorization dialog — but what this package concludes from what osascript
// says. Getting that wrong turns a cancelled prompt into an error banner, or an
// error into silence.

func TestADismissedPromptIsADecisionRatherThanAFailure(t *testing.T) {
	original := runOsascript
	t.Cleanup(func() { runOsascript = original })

	for _, out := range []string{
		"execution error: User canceled. (-128)",
		"  User canceled.  ",
		"error -128",
	} {
		runOsascript = func(context.Context, string) (string, error) {
			return out, errors.New("exit status 1")
		}
		_, err := Osascript{}.RunPrivileged(context.Background(), "true", "because")
		if !errors.Is(err, ErrCancelled) {
			t.Errorf("%q was reported as a fault rather than a cancellation: %v", out, err)
		}
	}
}

func TestARealFailureCarriesWhatTheToolSaid(t *testing.T) {
	original := runOsascript
	t.Cleanup(func() { runOsascript = original })

	runOsascript = func(context.Context, string) (string, error) {
		return "mount_apfs: Operation not permitted", errors.New("exit status 77")
	}

	out, err := Osascript{}.RunPrivileged(context.Background(), "mount", "because")
	if err == nil {
		t.Fatal("a failure was swallowed")
	}
	if errors.Is(err, ErrCancelled) {
		t.Error("a real failure was mistaken for a cancellation")
	}
	// classifyMount upstream reads this text to tell a TCC refusal from an
	// ownership problem, so losing it would lose the diagnosis.
	if !strings.Contains(err.Error(), "Operation not permitted") {
		t.Errorf("the tool's own words were dropped: %v", err)
	}
	if !strings.Contains(out, "Operation not permitted") {
		t.Errorf("the output was not returned for classification: %q", out)
	}
}

func TestSuccessReturnsTheTrimmedOutput(t *testing.T) {
	original := runOsascript
	t.Cleanup(func() { runOsascript = original })

	runOsascript = func(context.Context, string) (string, error) {
		return "  done  \n", nil
	}
	out, err := Osascript{}.RunPrivileged(context.Background(), "true", "because")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "done" {
		t.Errorf("want trimmed output, got %q", out)
	}
}

// The statement handed to osascript must be the one built from the script and
// the reason, or the dialog says something other than what will run.
func TestThePromptedStatementIsTheOneThatRuns(t *testing.T) {
	original := runOsascript
	t.Cleanup(func() { runOsascript = original })

	var seen string
	runOsascript = func(_ context.Context, stmt string) (string, error) {
		seen = stmt
		return "", nil
	}
	if _, err := (Osascript{}).RunPrivileged(context.Background(), "/sbin/mount_apfs x", "Snapshotter is opening a snapshot."); err != nil {
		t.Fatal(err)
	}
	if seen != authorizationStatement("/sbin/mount_apfs x", "Snapshotter is opening a snapshot.") {
		t.Errorf("ran something other than what it built: %q", seen)
	}
}
