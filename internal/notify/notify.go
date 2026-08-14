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
	script := fmt.Sprintf(
		`display notification "%s" with title "%s" subtitle "%s"`,
		appleScriptString.Replace(body),
		appleScriptString.Replace(Title),
		appleScriptString.Replace(subtitle),
	)
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("notify: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
