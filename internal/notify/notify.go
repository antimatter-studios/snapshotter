// Package notify posts macOS notifications.
//
// The application's failures are quiet ones. A schedule that stops firing and a
// watcher that has died both look exactly like everything being fine, and the
// loss this application exists to prevent happened to someone who believed they
// were covered. A notification is the difference between a lapse you have to go
// looking for and one that comes to find you.
package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Title is what every notification is attributed to.
const Title = "Snapshotter"

// appleScriptString escapes a Go string for an AppleScript literal. Only
// backslash and double quote are special there.
var appleScriptString = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// Send posts a notification. It is best-effort: a machine with notifications
// switched off is not a reason to fail the work that prompted one, so the error
// is returned for logging rather than for handling.
func Send(ctx context.Context, subtitle, body string) error {
	out, err := run(ctx, script(subtitle, body))
	if err != nil {
		return fmt.Errorf("notify: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// script builds the AppleScript.
//
// Separated from running it so the escaping can be tested, which is not
// hypothetical: the text here is assembled from error messages and file names,
// and a single unescaped quote turns a notification into a syntax error at the
// moment somebody most needs to be told something.
func script(subtitle, body string) string {
	return fmt.Sprintf(
		`display notification "%s" with title "%s" subtitle "%s"`,
		appleScriptString.Replace(body),
		appleScriptString.Replace(Title),
		appleScriptString.Replace(subtitle),
	)
}

// run is a variable so a test can observe what would be posted without a
// notification appearing on the machine running the tests.
var run = func(ctx context.Context, script string) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	return string(out), err
}
