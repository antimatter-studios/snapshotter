package elevate

import (
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
