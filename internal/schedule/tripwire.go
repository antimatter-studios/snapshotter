package schedule

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TripwireLabel identifies the bulk-deletion watcher to launchd.
//
// Separate from the interval agent's label on purpose. They fail in different
// ways and are worth turning on and off independently: the schedule is a
// routine that can lapse quietly, the tripwire is a daemon that can crash.
const TripwireLabel = Label + ".tripwire"

// TripwireArgs is the flag that makes the application watch instead of opening
// a window.
var TripwireArgs = []string{"--watch"}

// Tripwire manages the LaunchAgent that runs the bulk-deletion watcher.
//
// It exists because the watcher is worthless while the window is closed, which
// is most of the time and all of the time that matters. A deletion at 3am is
// exactly the one nobody is watching.
type Tripwire struct {
	Runner   apfsRunner
	AgentDir string
	Program  string
	LogPath  string
	UID      int
}

// apfsRunner is the subset of apfs.Runner this file needs, restated locally so
// the type does not have to be imported for one method.
type apfsRunner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// TripwireStatus describes what launchd currently has.
type TripwireStatus struct {
	Installed bool   `json:"installed"`
	Loaded    bool   `json:"loaded"`
	PlistPath string `json:"plistPath"`
	LogPath   string `json:"logPath"`
}

func (t *Tripwire) plistPath() string { return filepath.Join(t.AgentDir, TripwireLabel+".plist") }

func (t *Tripwire) serviceTarget() string { return fmt.Sprintf("gui/%d/%s", t.UID, TripwireLabel) }

func (t *Tripwire) domainTarget() string { return fmt.Sprintf("gui/%d", t.UID) }

// Status reports whether the watcher is installed and running.
func (t *Tripwire) Status(ctx context.Context) (TripwireStatus, error) {
	st := TripwireStatus{PlistPath: t.plistPath(), LogPath: t.LogPath}

	if _, err := os.Stat(t.plistPath()); err == nil {
		st.Installed = true
	} else if !os.IsNotExist(err) {
		return st, fmt.Errorf("schedule: reading %s: %w", t.plistPath(), err)
	}
	if _, err := t.Runner.Run(ctx, "launchctl", "print", t.serviceTarget()); err == nil {
		st.Loaded = true
	}
	return st, nil
}

// Install writes the plist and loads it.
func (t *Tripwire) Install(ctx context.Context) error {
	if err := os.MkdirAll(t.AgentDir, 0o755); err != nil {
		return fmt.Errorf("schedule: creating %s: %w", t.AgentDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(t.LogPath), 0o755); err != nil {
		return fmt.Errorf("schedule: creating the log directory: %w", err)
	}

	plist, err := t.render()
	if err != nil {
		return err
	}
	if err := os.WriteFile(t.plistPath(), []byte(plist), 0o644); err != nil {
		return fmt.Errorf("schedule: writing %s: %w", t.plistPath(), err)
	}

	_, _ = t.Runner.Run(ctx, "launchctl", "bootout", t.serviceTarget())
	if out, err := t.Runner.Run(ctx, "launchctl", "bootstrap", t.domainTarget(), t.plistPath()); err != nil {
		return fmt.Errorf("schedule: loading the tripwire: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// Uninstall stops the watcher and removes its plist.
func (t *Tripwire) Uninstall(ctx context.Context) error {
	_, _ = t.Runner.Run(ctx, "launchctl", "bootout", t.serviceTarget())
	if err := os.Remove(t.plistPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("schedule: removing %s: %w", t.plistPath(), err)
	}
	return nil
}

// render produces the property list.
//
// KeepAlive rather than StartInterval: this one runs continuously and is only
// useful while it is running, so launchd is asked to restart it if it dies.
// That is the opposite of the interval agent, which should run and exit.
func (t *Tripwire) render() (string, error) {
	args := append([]string{t.Program}, TripwireArgs...)
	var argXML strings.Builder
	for _, arg := range args {
		escaped, err := escape(arg)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&argXML, "\t\t<string>%s</string>\n", escaped)
	}
	logPath, err := escape(t.LogPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>

	<!-- Which application these belong to.
	     Without it, System Settings has nothing to attribute the job to and falls
	     back to the name on the signing certificate — so "App Background Activity"
	     listed the developer, "Chris Thomas", rather than Snapshotter. The user
	     sees a person's name against two background items and no way to tell what
	     they are. -->
	<key>AssociatedBundleIdentifiers</key>
	<array>
		<string>%s</string>
	</array>

	<key>ProgramArguments</key>
	<array>
%s	</array>

	<!-- The watcher is only useful while it is running, so launchd restarts
	     it if it exits. Throttled so a crash loop cannot spin. -->
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>30</integer>

	<key>RunAtLoad</key>
	<true/>

	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, TripwireLabel, BundleID, argXML.String(), logPath, logPath), nil
}
