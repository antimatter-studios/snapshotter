// Package schedule installs and inspects the LaunchAgent that takes snapshots
// on a timer.
//
// macOS only schedules local snapshots on its own when Time Machine has a
// destination configured. Without one, nothing takes them, which is why this
// agent exists at all. It runs as the ordinary user because tmutil needs no
// privileges: only mounting does.
package schedule

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"snapshotter/internal/apfs"
)

// Label identifies this application's agent to launchd.
const Label = "com.christhomas.snapshotter"

// conflictMarkers are strings that give away another agent taking local
// snapshots. Two such agents would double the snapshot rate and apply two
// retention windows to one shared set.
//
// A plist that drives a shell script names only that script, so matching on
// "localsnapshot" alone misses the very agent most likely to be installed
// here: the standalone com.christhomas.apfs-snapshot one, whose tmutil calls
// are inside ~/bin/apfs-snapshot rather than the plist.
var conflictMarkers = []string{"localsnapshot", "apfs-snapshot", "tmutil"}

// Config is the schedule the user asked for.
type Config struct {
	// Interval is how often a snapshot is taken. launchd fires a missed
	// interval on wake, so a sleeping Mac catches up rather than skipping.
	Interval time.Duration `json:"interval"`
	// Retention is how far back the schedule keeps anything: the flat window
	// where there is no policy, and the policy's horizon where there is one. It
	// is an upper bound either way, because macOS reclaims purgeable snapshots
	// under space pressure regardless.
	Retention time.Duration `json:"retention"`
	// Policy is how snapshots thin out as they age. An empty policy means the
	// flat window Retention describes, which is what every schedule installed
	// before tiering existed carries — so a zero value here changes nothing.
	Policy Policy `json:"policy"`
}

// DefaultConfig is six-hourly snapshots kept for a fortnight.
//
// Six hours rather than hourly: the aim is days of depth against an accidental
// deletion, not intra-day granularity, and each snapshot pins another
// generation of any large file rewritten between them.
//
// Flat rather than tiered, still. Tiering reaches further for the same count and
// the settings screen argues for it, but making it the default would change what
// the one-click fix in Health installs, and a retention default that changes
// under someone is a retention default that deletes something they expected to
// find.
var DefaultConfig = Config{Interval: 6 * time.Hour, Retention: 14 * 24 * time.Hour}

// EffectivePolicy is the policy this schedule actually applies: the one it
// carries, or the equivalent flat window if it carries none.
//
// Every reader goes through here so nothing has to decide which of the two
// fields to believe. A Config with neither a policy nor a retention yields a
// policy that prunes nothing, which Install refuses and Plan treats as keep
// everything — both of which are better than a guess.
func (c Config) EffectivePolicy() Policy {
	if len(c.Policy.normalised()) > 0 {
		return c.Policy
	}
	return FlatPolicy(c.Retention)
}

// withPolicy sets the policy and makes Retention agree with it, since Retention
// is the horizon and two fields disagreeing about how far back a schedule
// reaches would eventually be resolved by deleting something.
func (c Config) withPolicy(p Policy) Config {
	c.Policy = p
	c.Retention = p.Horizon()
	return c
}

// Status describes what launchd currently has.
type Status struct {
	Installed bool   `json:"installed"`
	Loaded    bool   `json:"loaded"`
	Config    Config `json:"config"`
	PlistPath string `json:"plistPath"`
	LogPath   string `json:"logPath"`
	// Program is the binary the installed plist names, and ProgramMissing says it
	// is no longer there.
	//
	// launchd does not mind. It keeps the job loaded and fails to exec it once an
	// interval, forever, while everything reading the plist goes on reporting a
	// schedule that is installed and running. That is the quietest way this
	// application can stop working, so it is read back rather than assumed.
	Program        string `json:"program"`
	ProgramMissing bool   `json:"programMissing"`
	// Conflicts names other LaunchAgents that also take local snapshots.
	Conflicts []string `json:"conflicts"`
}

// Agent manages one LaunchAgent.
type Agent struct {
	Runner apfs.Runner
	// AgentDir is the user's LaunchAgents directory.
	AgentDir string
	// Program is the binary launchd runs, invoked with SnapshotArgs.
	Program string
	LogPath string
	// UID is the user launchd runs the agent for.
	UID int
}

// SnapshotArgs is the flag that makes the application take a snapshot and exit
// instead of opening a window.
var SnapshotArgs = []string{"--take-snapshot"}

func (a *Agent) plistPath() string { return filepath.Join(a.AgentDir, Label+".plist") }

func (a *Agent) serviceTarget() string { return fmt.Sprintf("gui/%d/%s", a.UID, Label) }

func (a *Agent) domainTarget() string { return fmt.Sprintf("gui/%d", a.UID) }

// Status reports what is installed, loaded and configured.
func (a *Agent) Status(ctx context.Context) (Status, error) {
	st := Status{PlistPath: a.plistPath(), LogPath: a.LogPath, Config: DefaultConfig}

	data, err := os.ReadFile(a.plistPath())
	if err == nil {
		st.Installed = true
		st.Config = parseConfig(string(data))
		if program, ok := elementAfterKey(string(data), "ProgramArguments", "string"); ok {
			st.Program = program
			if _, statErr := os.Stat(program); statErr != nil && os.IsNotExist(statErr) {
				st.ProgramMissing = true
			}
		}
	} else if !os.IsNotExist(err) {
		return st, fmt.Errorf("schedule: reading %s: %w", a.plistPath(), err)
	}

	if _, err := a.Runner.Run(ctx, "launchctl", "print", a.serviceTarget()); err == nil {
		st.Loaded = true
	}

	conflicts, err := a.conflicts()
	if err != nil {
		return st, err
	}
	st.Conflicts = conflicts
	return st, nil
}

// Install writes the plist and loads it, replacing any previous version.
func (a *Agent) Install(ctx context.Context, cfg Config) error {
	if cfg.Interval < time.Minute {
		return fmt.Errorf("schedule: interval %s is too short", cfg.Interval)
	}
	// Resolved before the checks below, so a tiered policy is validated on how
	// far it actually reaches rather than on a Retention field the caller may not
	// have filled in.
	cfg = cfg.withPolicy(cfg.EffectivePolicy())
	if cfg.Retention < cfg.Interval {
		return fmt.Errorf("schedule: retention %s is shorter than the interval %s, which would delete every snapshot as it is taken", cfg.Retention, cfg.Interval)
	}
	if err := os.MkdirAll(a.AgentDir, 0o755); err != nil {
		return fmt.Errorf("schedule: creating %s: %w", a.AgentDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(a.LogPath), 0o755); err != nil {
		return fmt.Errorf("schedule: creating the log directory: %w", err)
	}

	plist, err := a.render(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.plistPath(), []byte(plist), 0o644); err != nil {
		return fmt.Errorf("schedule: writing %s: %w", a.plistPath(), err)
	}

	// launchd refuses to bootstrap a label it already has, so an existing
	// service is booted out first. Failure here is expected when nothing is
	// loaded, and is not worth reporting.
	_, _ = a.Runner.Run(ctx, "launchctl", "bootout", a.serviceTarget())

	if out, err := a.Runner.Run(ctx, "launchctl", "bootstrap", a.domainTarget(), a.plistPath()); err != nil {
		return fmt.Errorf("schedule: loading the agent: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// Uninstall unloads the agent and removes its plist. Snapshots already taken
// are left alone.
func (a *Agent) Uninstall(ctx context.Context) error {
	_, _ = a.Runner.Run(ctx, "launchctl", "bootout", a.serviceTarget())
	if err := os.Remove(a.plistPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("schedule: removing %s: %w", a.plistPath(), err)
	}
	return nil
}

// ours reports whether a plist filename is one this application installed.
//
// Both have to be excluded from the conflict scan, not just the interval agent.
// The tripwire genuinely does take snapshots, so the marker logic would be right
// about it and wrong about what it means: reporting our own watcher to the user
// as a competing agent would send them to disable the thing protecting them.
func ours(plistName string) bool {
	return plistName == Label+".plist" || plistName == TripwireLabel+".plist"
}

// conflicts finds other LaunchAgents that also run tmutil localsnapshot.
func (a *Agent) conflicts() ([]string, error) {
	items, err := os.ReadDir(a.AgentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("schedule: reading %s: %w", a.AgentDir, err)
	}

	var found []string
	for _, item := range items {
		name := item.Name()
		if item.IsDir() || !strings.HasSuffix(name, ".plist") || ours(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(a.AgentDir, name))
		if err != nil {
			continue
		}
		text := string(data)
		for _, marker := range conflictMarkers {
			if strings.Contains(text, marker) {
				found = append(found, strings.TrimSuffix(name, ".plist"))
				break
			}
		}
	}
	return found, nil
}

// render produces the LaunchAgent property list.
func (a *Agent) render(cfg Config) (string, error) {
	args := append([]string{a.Program}, SnapshotArgs...)
	var argXML strings.Builder
	for _, arg := range args {
		escaped, err := escape(arg)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&argXML, "\t\t<string>%s</string>\n", escaped)
	}
	logPath, err := escape(a.LogPath)
	if err != nil {
		return "", err
	}
	policy := cfg.EffectivePolicy()
	policyText, err := escape(policy.String())
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>

	<key>ProgramArguments</key>
	<array>
%s	</array>

	<!-- launchd fires a missed interval when the machine wakes, so a Mac that
	     slept through one still gets its snapshot. -->
	<key>StartInterval</key>
	<integer>%d</integer>

	<key>RunAtLoad</key>
	<true/>

	<key>EnvironmentVariables</key>
	<dict>
		<!-- The horizon of the policy below, in hours, and the whole of what a
		     build predating tiered retention understands. Writing the horizon
		     rather than some inner band means such a build keeps a superset of
		     what the policy asks for: it prunes at the far edge and no closer,
		     which errs toward keeping rather than toward deleting. -->
		<key>%s</key>
		<string>%d</string>
		<!-- every/for pairs in whole hours, "all" meaning keep every snapshot
		     the band covers. This plist is what launchd will actually run, so it
		     is the source of truth for the retention in force. -->
		<key>%s</key>
		<string>%s</string>
	</dict>

	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, Label, argXML.String(), int(cfg.Interval.Seconds()),
		retentionEnv, hoursUp(policy.Horizon()), policyEnv, policyText,
		logPath, logPath), nil
}

// retentionEnv carries the retention window to the scheduled run, so changing
// it is a matter of rewriting the plist rather than rebuilding the binary.
const retentionEnv = "SNAPSHOTTER_RETENTION_HOURS"

// policyEnv carries the tiered policy the same way. It is a second variable
// rather than a replacement so that an agent installed by an earlier build, which
// has only retentionEnv, keeps working unchanged.
const policyEnv = "SNAPSHOTTER_RETENTION_POLICY"

// RetentionFromEnv reads the retention window a scheduled run should apply.
//
// With a tiered policy installed this is the policy's horizon — how far back
// anything is kept — so a caller that only understands a flat window prunes at
// the far edge of the policy and never inside it.
func RetentionFromEnv() time.Duration {
	raw := os.Getenv(retentionEnv)
	if raw == "" {
		return DefaultConfig.Retention
	}
	var hours int
	if _, err := fmt.Sscanf(raw, "%d", &hours); err != nil || hours <= 0 {
		return DefaultConfig.Retention
	}
	return time.Duration(hours) * time.Hour
}

// PolicyFromEnv reads the retention policy a scheduled run should apply.
//
// An agent installed before tiering carries only retentionEnv, which is read as
// the equivalent flat policy so that plist goes on doing exactly what it did.
//
// A policy that is set but unreadable returns an error and a policy that prunes
// nothing. Deleting on a guess is not an option here: pruning too little is
// corrected by the next run once the plist is fixed, and pruning too much cannot
// be corrected at all.
func PolicyFromEnv() (Policy, error) {
	raw := strings.TrimSpace(os.Getenv(policyEnv))
	if raw == "" {
		return FlatPolicy(RetentionFromEnv()), nil
	}
	policy, ok := ParsePolicy(raw)
	if !ok {
		return Policy{}, fmt.Errorf("schedule: %s=%q is not a retention policy, so nothing will be pruned", policyEnv, raw)
	}
	return policy, nil
}

// parseConfig recovers the schedule from an installed plist, so the UI shows
// what launchd will actually do rather than what was last requested.
func parseConfig(plist string) Config {
	cfg := DefaultConfig
	if seconds, ok := intAfterKey(plist, "StartInterval"); ok && seconds > 0 {
		cfg.Interval = time.Duration(seconds) * time.Second
	}
	if raw, ok := elementAfterKey(plist, policyEnv, "string"); ok {
		if policy, ok := ParsePolicy(raw); ok {
			return cfg.withPolicy(policy)
		}
	}
	// No policy in the plist, or one that will not parse: fall back to the flat
	// window. An agent installed before tiering has only this value, and reading
	// it as a single keep-everything band preserves its meaning exactly rather
	// than approximating it.
	if hours, ok := intAfterKey(plist, retentionEnv); ok && hours > 0 {
		cfg.Retention = time.Duration(hours) * time.Hour
	}
	return cfg.withPolicy(FlatPolicy(cfg.Retention))
}

// intAfterKey pulls the number out of the element following <key>name</key>,
// which is either <integer>n</integer> or the <string>n</string> used for
// environment variables.
func intAfterKey(plist, name string) (int, bool) {
	for _, tag := range []string{"integer", "string"} {
		text, ok := elementAfterKey(plist, name, tag)
		if !ok {
			continue
		}
		var value int
		if _, err := fmt.Sscanf(text, "%d", &value); err == nil {
			return value, true
		}
	}
	return 0, false
}

// elementAfterKey returns the text of the first tag element following
// <key>name</key>. The plist is read rather than unmarshalled because only three
// values are wanted out of it and a full plist decoder is a dependency; the key
// is matched with its closing tag so one variable's name cannot match another's
// prefix.
func elementAfterKey(plist, name, tag string) (string, bool) {
	idx := strings.Index(plist, "<key>"+name+"</key>")
	if idx < 0 {
		return "", false
	}
	rest := plist[idx:]
	open := "<" + tag + ">"
	start := strings.Index(rest, open)
	if start < 0 {
		return "", false
	}
	end := strings.Index(rest[start:], "</"+tag+">")
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[start+len(open) : start+end]), true
}

func escape(s string) (string, error) {
	var buf strings.Builder
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return "", fmt.Errorf("schedule: escaping %q: %w", s, err)
	}
	return buf.String(), nil
}
