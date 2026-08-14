// Package scenario drives the application from a machine state written down
// rather than from this machine.
//
// Everything that shells out goes through apfs.Runner, and main.go is the only
// place that chooses which one. A fake Runner therefore replaces the whole
// machine at once — the snapshot listing, what diskutil knows about each
// snapshot, whether Time Machine has a destination, what launchd has loaded —
// because those are all commands and they all go through the same seam. That is
// what puts a screen into a state this Mac is not in: no snapshots at all, a
// schedule three days overdue, a rival agent installed, or any state that could
// otherwise only be produced by destroying something real.
//
// The fake answers in the shape the real tools answer. Inventing a tidier format
// would test the fake instead of the application, and the parsers are where the
// bugs have actually been: a header line taken for a snapshot name, a container
// NOTE attributed to the wrong block, a conflicting agent whose plist never
// mentions tmutil.
//
// Two parts of the state are files rather than command output — an installed
// agent is a plist in ~/Library/LaunchAgents, and a competing agent is another
// plist beside it — so a scenario also owns a sandbox directory. The plists in it
// are written by the real installer and read back by the real parser, so a
// scenario cannot claim a schedule the application would not see.
package scenario

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"snapshotter/internal/schedule"
)

const (
	// EnvName selects a built-in scenario.
	EnvName = "SNAPSHOTTER_SCENARIO"
	// EnvFile selects a scenario written as JSON, for the states no built-in
	// covers.
	EnvFile = "SNAPSHOTTER_SCENARIO_FILE"
)

// Scenario is a spec turned into the machine the application will see.
type Scenario struct {
	Spec Spec
	// Runner replaces apfs.SystemRunner() everywhere.
	Runner *Runner

	program string
	root    string
}

// Options are the seams the tests need and nothing else uses.
type Options struct {
	// Now is the clock. Ages are measured back from one reading of it.
	Now func() time.Time
	// Program is the binary the installed plists name. Defaults to this one,
	// which is what makes an installed schedule in a scenario worth reading.
	Program string
	// Root is where the sandbox goes. Defaults to a directory under the system
	// temporary directory.
	Root string
}

// FromEnv builds the scenario the environment asks for. A nil Scenario and a nil
// error mean none was asked for, which is the normal case.
//
// No filesystem work happens here: the fake machine is only the Runner until
// something asks for a Sandbox. That keeps `snapshotter list` under a scenario
// from writing anything at all.
func FromEnv() (*Scenario, error) {
	name, file := os.Getenv(EnvName), os.Getenv(EnvFile)
	switch {
	case name == "" && file == "":
		return nil, nil
	case name != "" && file != "":
		// Silently preferring one would make a stale variable in a shell look
		// like a broken scenario file.
		return nil, fmt.Errorf("scenario: %s and %s are both set; pick one", EnvName, EnvFile)
	}

	var spec Spec
	var err error
	if file != "" {
		spec, err = LoadFile(file)
	} else {
		spec, err = Load(name)
	}
	if err != nil {
		return nil, err
	}
	return New(spec, Options{})
}

// New builds the fake machine without touching the filesystem.
func New(s Spec, opt Options) (*Scenario, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	at := now()

	runner, err := newRunner(s, at, now)
	if err != nil {
		return nil, err
	}
	// Set before any sandbox exists, because the launchd state is a command
	// answer and not a file: a scenario claiming a loaded agent must say so even
	// where nothing has written a plist.
	runner.setLoaded(schedule.Label, s.Schedule.Loaded)
	runner.setLoaded(schedule.TripwireLabel, s.Tripwire.Loaded)

	return &Scenario{Spec: s, Runner: runner, program: opt.Program, root: opt.Root}, nil
}

// Sandbox is where the parts of a scenario that are files ended up.
//
// main.go uses these in place of the real paths. Redirecting them is not tidiness:
// a scenario writing its plist into the real ~/Library/LaunchAgents would outlive
// the run and start taking real snapshots on a real timer.
type Sandbox struct {
	Dir             string
	AgentDir        string
	MountRoot       string
	LogPath         string
	TripwireLogPath string
}

// sandboxDir groups every scenario's sandbox under one directory, so what is
// left behind after a run is obvious and removable in one go.
const sandboxDir = "snapshotter-scenario"

// Sandbox writes the plists the scenario claims are installed and returns where
// everything went.
//
// The directory is named after the scenario and the process, and is emptied
// first. Named after the process because a scenario is per-process state:
// `snapshotter list` in one terminal must not reset the schedule a window in
// another has installed. Emptied because a scenario that inherited the last run's
// state would stop being the state that was written down.
func (sc *Scenario) Sandbox(ctx context.Context) (Sandbox, error) {
	program := sc.program
	if program == "" {
		found, err := os.Executable()
		if err != nil {
			return Sandbox{}, fmt.Errorf("scenario: cannot find this program's path: %w", err)
		}
		program = found
	}

	root := sc.root
	if root == "" {
		root = filepath.Join(os.TempDir(), sandboxDir, fmt.Sprintf("%s-%d", sc.Spec.Name, os.Getpid()))
	}
	if err := os.RemoveAll(root); err != nil {
		return Sandbox{}, fmt.Errorf("scenario: emptying %s: %w", root, err)
	}

	box := Sandbox{
		Dir:             root,
		AgentDir:        filepath.Join(root, "LaunchAgents"),
		MountRoot:       filepath.Join(root, "mounts"),
		LogPath:         filepath.Join(root, "snapshotter.log"),
		TripwireLogPath: filepath.Join(root, "snapshotter-tripwire.log"),
	}
	if err := os.MkdirAll(box.AgentDir, 0o755); err != nil {
		return Sandbox{}, fmt.Errorf("scenario: creating %s: %w", box.AgentDir, err)
	}

	// Install loads what it writes, and the fake refuses a label it already
	// holds exactly as launchd does, so the loaded flags come off first and go
	// back on at the end. The other order would make a scenario claiming both
	// installed and loaded fail to install.
	sc.Runner.setLoaded(schedule.Label, false)
	sc.Runner.setLoaded(schedule.TripwireLabel, false)

	if sc.Spec.Schedule.Installed {
		agent := &schedule.Agent{
			Runner:   sc.Runner,
			AgentDir: box.AgentDir,
			Program:  program,
			LogPath:  box.LogPath,
			UID:      os.Getuid(),
		}
		if err := agent.Install(ctx, sc.Spec.scheduleConfig()); err != nil {
			return Sandbox{}, fmt.Errorf("scenario %s: %w", sc.Spec.Name, err)
		}
	}
	if sc.Spec.Tripwire.Installed {
		tripwire := &schedule.Tripwire{
			Runner:   sc.Runner,
			AgentDir: box.AgentDir,
			Program:  program,
			LogPath:  box.TripwireLogPath,
			UID:      os.Getuid(),
		}
		if err := tripwire.Install(ctx); err != nil {
			return Sandbox{}, fmt.Errorf("scenario %s: %w", sc.Spec.Name, err)
		}
	}
	if sc.Spec.CompetingAgent != "" {
		if err := writeCompetingAgent(box.AgentDir, sc.Spec.CompetingAgent); err != nil {
			return Sandbox{}, err
		}
	}

	sc.Runner.setLoaded(schedule.Label, sc.Spec.Schedule.Loaded)
	sc.Runner.setLoaded(schedule.TripwireLabel, sc.Spec.Tripwire.Loaded)
	return box, nil
}

// writeCompetingAgent puts another snapshot-taking LaunchAgent beside ours.
//
// The plist deliberately names only a shell script, and never tmutil or
// localsnapshot, because that is the case that caught the conflict scan out: the
// standalone agent most likely to be installed on this machine keeps its tmutil
// calls inside ~/bin/apfs-snapshot, so a scan matching "localsnapshot" alone saw
// nothing. Faking the easy case would leave the fix untested.
//
// The label needs no XML escaping because validate has already restricted it to
// characters that are also safe in a filename.
func writeCompetingAgent(dir, label string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>

	<key>ProgramArguments</key>
	<array>
		<string>/bin/sh</string>
		<string>-c</string>
		<string>$HOME/bin/apfs-snapshot</string>
	</array>

	<key>StartInterval</key>
	<integer>3600</integer>
</dict>
</plist>
`, label)
	path := filepath.Join(dir, label+".plist")
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("scenario: writing %s: %w", path, err)
	}
	return nil
}

// Banner is what a scenario says about itself at startup.
//
// A fake that can be mistaken for the machine is worse than no fake at all: this
// application exists because somebody believed they were protected and was not.
// So the banner leads with what is not real, states everything the scenario
// asserts so a surprising screen can be checked against it, and says how to turn
// it off.
func (sc *Scenario) Banner() []string {
	const rule = "========================================================================"
	return []string{
		rule,
		"SCENARIO MODE — nothing reported below describes this Mac",
		"scenario:     " + sc.Spec.Name + " — " + sc.Spec.Summary,
		"snapshots:    " + sc.describeSnapshots(),
		"schedule:     " + describeSchedule(sc.Spec),
		"tripwire:     " + describeAgent(sc.Spec.Tripwire),
		"time machine: " + describeTimeMachine(sc.Spec.TimeMachine),
		"competing:    " + describeCompeting(sc.Spec.CompetingAgent),
		"tmutil, diskutil and launchctl are not being run. Unset " + EnvName + " for real state.",
		rule,
	}
}

func (sc *Scenario) describeSnapshots() string {
	snaps := sc.Spec.Snapshots
	if len(snaps) == 0 {
		return "none"
	}
	newest, oldest := snaps[0].Age, snaps[0].Age
	for _, snap := range snaps[1:] {
		if snap.Age < newest {
			newest = snap.Age
		}
		if snap.Age > oldest {
			oldest = snap.Age
		}
	}
	return fmt.Sprintf("%d invented, newest %s ago, oldest %s ago", len(snaps), newest, oldest)
}

func describeSchedule(s Spec) string {
	if !s.Schedule.Installed && !s.Schedule.Loaded {
		return "not installed"
	}
	cfg := s.scheduleConfig()
	return fmt.Sprintf("%s, every %s, kept %s",
		describeAgent(s.Schedule.AgentSpec), Span(cfg.Interval), Span(cfg.Retention))
}

func describeAgent(a AgentSpec) string {
	switch {
	case a.Installed && a.Loaded:
		return "installed and loaded"
	case a.Installed:
		return "installed but not loaded"
	case a.Loaded:
		// launchd holding a job whose plist has been deleted is a real state,
		// and an odd enough one to spell out rather than round off.
		return "loaded with no plist on disk"
	default:
		return "not installed"
	}
}

func describeTimeMachine(configured bool) string {
	if configured {
		return "a destination is configured, so backupd thins these to roughly a day"
	}
	return "no destination"
}

func describeCompeting(label string) string {
	if label == "" {
		return "none"
	}
	return label
}
