// Package elevate runs a shell command with administrator rights.
//
// Mounting is the only operation in this application that needs root. Listing,
// creating and deleting snapshots all go through tmutil, which is a client of
// the privileged backupd daemon and so needs nothing. mount(2) has no such
// broker: attaching a filesystem to the global namespace is a privileged
// operation regardless of who owns the files inside it.
package elevate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrCancelled reports that the user dismissed the authorization dialog. It is
// an ordinary outcome, not a failure, and the UI should treat it as such.
var ErrCancelled = errors.New("elevate: authorization cancelled")

// Elevator runs a shell script as root.
//
// reason is shown to the user in the authorization dialog and is not optional in
// spirit: a password prompt that does not say what it is for teaches people to
// approve prompts without reading them, which is the habit every credential
// phishing attempt relies on. Callers describe the specific thing about to
// happen, not the mechanism.
type Elevator interface {
	RunPrivileged(ctx context.Context, script, reason string) (string, error)
}

// Osascript asks macOS for authorization with the system's own dialog, via
// AppleScript's `with administrator privileges`.
//
// One prompt is raised per call, so callers batch their work into a single
// script rather than issuing one call per snapshot.
type Osascript struct{}

// runOsascript is a variable so the classification below can be tested. The real
// implementation raises an authorization dialog, which no test can answer — and a
// test suite that could would be a test suite able to mount filesystems on the
// machine running it.
var runOsascript = func(ctx context.Context, statement string) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", statement).CombinedOutput()
	return string(out), err
}

// appleScriptString escapes a Go string for use as an AppleScript literal.
// Only backslash and double quote are special there.
var appleScriptString = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// authorizationStatement builds the AppleScript that raises the dialog.
//
// Without a `with prompt` clause macOS falls back to naming the process it can
// see, which is osascript — so the user is asked for an administrator password
// by something they have never heard of, on behalf of an application it does not
// mention. The clause is the only part of that dialog this application controls;
// it cannot set the icon or the title, so the reason has to carry all of it.
//
// macOS appends its own "Enter your password to allow this.", so a reason reads
// as a statement of what is about to happen rather than as a request.
func authorizationStatement(script, reason string) string {
	quoted := appleScriptString.Replace(script)
	if reason == "" {
		return fmt.Sprintf(`do shell script "%s" with administrator privileges`, quoted)
	}
	return fmt.Sprintf(`do shell script "%s" with prompt "%s" with administrator privileges`,
		quoted, appleScriptString.Replace(reason))
}

func (Osascript) RunPrivileged(ctx context.Context, script, reason string) (string, error) {
	out, err := runOsascript(ctx, authorizationStatement(script, reason))
	text := strings.TrimSpace(out)
	if err != nil {
		// osascript reports a dismissed dialog as error -128, which is a
		// decision rather than a fault.
		if strings.Contains(text, "-128") || strings.Contains(text, "User canceled") {
			return text, ErrCancelled
		}
		return text, fmt.Errorf("elevate: %w: %s", err, text)
	}
	return text, nil
}
