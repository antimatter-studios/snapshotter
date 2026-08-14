package scenario

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"snapshotter/internal/schedule"
)

// Span is a length of time written the way a person writes one: "90m", "6h",
// "3d", "2w".
//
// Every time in a scenario is relative — how long ago a snapshot was taken, how
// often the schedule fires, how long snapshots are kept — so a scenario written
// today still describes the same machine in a year. Absolute dates would make
// "the newest snapshot is far too old" quietly become "the newest snapshot is
// from 2026" and stop testing anything.
//
// time.Duration is not used directly because it marshals as a count of
// nanoseconds, which nobody can write into a file or read back out of one.
type Span time.Duration

// Duration is the span as the standard library sees it.
func (s Span) Duration() time.Duration { return time.Duration(s) }

// String writes the span back in the largest whole unit that fits, so a spec
// round-trips through JSON as "2w" rather than as "336h0m0s".
func (s Span) String() string {
	d := time.Duration(s)
	if d <= 0 {
		return d.String()
	}
	switch {
	case d%(7*24*time.Hour) == 0:
		return fmt.Sprintf("%dw", d/(7*24*time.Hour))
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// ParseSpan reads a span.
//
// time.ParseDuration handles the units it knows and is extended here with days
// and weeks, which it omits because a calendar day is not always 24 hours. Here
// it is: launchd counts a retention window in seconds, so "14d" means fourteen
// times twenty-four hours to the application too.
func ParseSpan(text string) (Span, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("scenario: an empty span says nothing; write something like 6h or 3d")
	}

	unit := time.Duration(0)
	switch {
	case strings.HasSuffix(text, "d"):
		unit, text = 24*time.Hour, strings.TrimSuffix(text, "d")
	case strings.HasSuffix(text, "w"):
		unit, text = 7*24*time.Hour, strings.TrimSuffix(text, "w")
	}

	var d time.Duration
	if unit > 0 {
		count, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, fmt.Errorf("scenario: %q is not a number of days or weeks", text)
		}
		d = time.Duration(count * float64(unit))
	} else {
		parsed, err := time.ParseDuration(text)
		if err != nil {
			return 0, fmt.Errorf("scenario: %q is not a span: %w", text, err)
		}
		d = parsed
	}

	// A negative or zero age would put a snapshot in the future or at this
	// instant, which no listing can contain, and a zero interval would be
	// refused by the installer several steps later with a worse message.
	if d <= 0 {
		return 0, fmt.Errorf("scenario: %q is not a length of time in the past", text)
	}
	return Span(d), nil
}

// UnmarshalJSON reads a span from its written form.
func (s *Span) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("scenario: a span is written as a string like \"6h\": %w", err)
	}
	parsed, err := ParseSpan(text)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// MarshalJSON exists so a spec cannot be written out in a form it could not be
// read back in. Without it the type would marshal as a nanosecond count and
// unmarshal only from a string.
func (s Span) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// SnapshotSpec is one snapshot the machine is to be holding.
type SnapshotSpec struct {
	// Age is how long ago it was taken.
	Age Span `json:"age"`
	// Purgeable defaults to true when omitted, because every Time Machine local
	// snapshot on a real machine is purgeable and the interesting case is the
	// rare one that is not. A pointer is the only way for JSON to tell an
	// omitted field from a false one.
	Purgeable *bool `json:"purgeable,omitempty"`
	// LimitsContainer marks the snapshot diskutil blames for the container's
	// minimum size. diskutil names at most one, and the interface reports it as
	// "the one worth deleting first", so more than one is refused rather than
	// left to pick arbitrarily.
	LimitsContainer bool `json:"limitsContainer,omitempty"`
}

// purgeable resolves the default.
func (s SnapshotSpec) purgeable() bool { return s.Purgeable == nil || *s.Purgeable }

// AgentSpec is what launchd has for one of this application's agents.
//
// Installed and Loaded are separate because they fail separately, and the
// difference is a finding the user sees: a plist on disk that launchd has not
// loaded takes no snapshots at all while looking configured.
type AgentSpec struct {
	Installed bool `json:"installed"`
	Loaded    bool `json:"loaded"`
}

// ScheduleSpec is the interval agent, which also carries a configuration.
type ScheduleSpec struct {
	AgentSpec
	// Interval and Retention are written into the plist by the real installer
	// and read back by the real parser, so what a scenario claims here is what
	// the application shows. Zero means schedule.DefaultConfig, so a scenario
	// states an interval only when the interval is the point.
	Interval  Span `json:"interval,omitempty"`
	Retention Span `json:"retention,omitempty"`
}

// Spec is a machine state written down.
type Spec struct {
	// Name selects the scenario and names its sandbox directory.
	Name string `json:"name"`
	// Summary is the one line the startup banner shows, so a scenario says what
	// it is for at the moment somebody is wondering why the screen looks wrong.
	Summary   string         `json:"summary"`
	Snapshots []SnapshotSpec `json:"snapshots"`
	// TimeMachine configures a backup destination. It changes nothing about the
	// snapshots and everything about what retention means: backupd thins local
	// snapshots to roughly a day on each cycle, so any longer window the
	// settings screen shows is a promise the system breaks.
	TimeMachine bool         `json:"timeMachine"`
	Schedule    ScheduleSpec `json:"schedule"`
	Tripwire    AgentSpec    `json:"tripwire"`
	// CompetingAgent is the launchd label of another agent that also takes local
	// snapshots. Two of them double the rate and apply two retention windows to
	// one shared set, which is the condition the conflict scan exists to find.
	CompetingAgent string `json:"competingAgent,omitempty"`
	// VolumeBytes and FreeBytes describe the disk. Both zero means report the real
	// one, because most scenarios have no opinion about space and inventing a
	// figure would be one more thing on screen that is not true.
	//
	// They exist because free space does not arrive through Runner — statfs(2) is a
	// syscall — so without them a scenario could describe any machine except a
	// nearly full one, which is the state the space warning exists for.
	VolumeBytes uint64 `json:"volumeBytes,omitempty"`
	FreeBytes   uint64 `json:"freeBytes,omitempty"`
}

// Space returns the disk figures this spec describes, or nil to let the caller
// ask the kernel. Nil rather than a function reporting the real values, so the
// decision is visible at the call site instead of hidden behind an indirection.
func (s Spec) Space() func(string) (uint64, uint64, error) {
	if s.VolumeBytes == 0 && s.FreeBytes == 0 {
		return nil
	}
	total, free := s.VolumeBytes, s.FreeBytes
	return func(string) (uint64, uint64, error) { return total, free, nil }
}

// nameRule keeps a scenario name usable as a path component. The sandbox is
// named after the scenario and removed by that path on every start, so a name
// containing ".." or a slash would aim a recursive delete somewhere else.
var nameRule = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// labelRule keeps a launchd label usable both as a filename and as XML text
// without escaping, which is why the plist writer below needs neither.
var labelRule = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validate rejects a spec that could not describe a real machine, or that would
// describe one the application cannot see.
func (s Spec) validate() error {
	if !nameRule.MatchString(s.Name) {
		return fmt.Errorf("scenario: %q is not a usable name; lower-case words joined by hyphens", s.Name)
	}

	limiting := 0
	for i, snap := range s.Snapshots {
		if snap.Age <= 0 {
			return fmt.Errorf("scenario %s: snapshot %d has no age", s.Name, i)
		}
		if snap.LimitsContainer {
			limiting++
		}
	}
	if limiting > 1 {
		return fmt.Errorf("scenario %s: %d snapshots limit the container; diskutil names at most one", s.Name, limiting)
	}

	if s.CompetingAgent != "" {
		if !labelRule.MatchString(s.CompetingAgent) {
			return fmt.Errorf("scenario %s: %q is not a launchd label", s.Name, s.CompetingAgent)
		}
		// The conflict scan skips this application's own plists on purpose — the
		// tripwire genuinely does take snapshots, and reporting it as a rival
		// would send the user to disable the thing protecting them. So a
		// scenario naming one of our labels would claim a conflict that nothing
		// can ever see.
		if s.CompetingAgent == schedule.Label || s.CompetingAgent == schedule.TripwireLabel {
			return fmt.Errorf("scenario %s: %s is this application's own agent, so the conflict scan ignores it", s.Name, s.CompetingAgent)
		}
	}
	return nil
}

// scheduleConfig is what the real installer is asked to write.
func (s Spec) scheduleConfig() schedule.Config {
	cfg := schedule.DefaultConfig
	if s.Schedule.Interval > 0 {
		cfg.Interval = s.Schedule.Interval.Duration()
	}
	if s.Schedule.Retention > 0 {
		cfg.Retention = s.Schedule.Retention.Duration()
	}
	return cfg
}

// Load returns a built-in scenario by name.
func Load(name string) (Spec, error) {
	spec, ok := builtIns()[name]
	if !ok {
		return Spec{}, fmt.Errorf("scenario: there is no built-in scenario %q; the built-ins are %s",
			name, strings.Join(Names(), ", "))
	}
	return spec, nil
}

// LoadFile reads a scenario written as JSON.
func LoadFile(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("scenario: reading %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	// A misspelt key that silently did nothing would be the worst failure this
	// file format could have: the scenario would run, look plausible, and not be
	// the one that was written.
	dec.DisallowUnknownFields()
	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		return Spec{}, fmt.Errorf("scenario: reading %s: %w", path, err)
	}
	return spec, nil
}

// Names lists the built-in scenarios.
func Names() []string {
	all := builtIns()
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// builtIns is a function rather than a package variable so a caller cannot edit
// the scenario everybody else is about to load.
func builtIns() map[string]Spec {
	running := AgentSpec{Installed: true, Loaded: true}
	sixHourly := ScheduleSpec{AgentSpec: running, Interval: Span(6 * time.Hour), Retention: Span(14 * 24 * time.Hour)}

	return map[string]Spec{
		"empty": {
			Name:    "empty",
			Summary: "no snapshots, and nothing taking any",
		},
		"healthy": {
			Name:      "healthy",
			Summary:   "schedule and tripwire running, two days of cover",
			Snapshots: every(10, Span(time.Hour), Span(6*time.Hour)),
			Schedule:  sixHourly,
			Tripwire:  running,
		},
		"full-disk": {
			Name: "full-disk",
			// Retention stops being a promise here. Snapshots are purgeable, so
			// macOS reclaims the oldest under space pressure rather than failing a
			// write, and no policy survives that — which is worth being able to
			// look at without filling a real disk to see it.
			Summary:     "healthy schedule, 3% of the disk free",
			Snapshots:   every(12, Span(30*time.Minute), Span(6*time.Hour)),
			Schedule:    sixHourly,
			Tripwire:    running,
			VolumeBytes: 994662584320,
			FreeBytes:   29839877529,
		},
		"overdue": {
			Name: "overdue",
			// The quiet failure the application exists to catch: everything
			// looks configured and no snapshot has been taken for days.
			Summary:   "schedule installed and loaded, newest snapshot three days old",
			Snapshots: every(4, Span(3*24*time.Hour), Span(6*time.Hour)),
			Schedule:  sixHourly,
			Tripwire:  running,
		},
		"time-machine": {
			Name:      "time-machine",
			Summary:   "a backup destination is configured, so the retention shown is a lie",
			Snapshots: every(10, Span(time.Hour), Span(6*time.Hour)),
			Schedule:  sixHourly,
			Tripwire:  running,
			// Everything else is well: the point is to see the one finding that
			// says the retention window will not hold.
			TimeMachine: true,
		},
		"conflict": {
			Name:      "conflict",
			Summary:   "another agent is taking snapshots too",
			Snapshots: every(10, Span(time.Hour), Span(6*time.Hour)),
			Schedule:  sixHourly,
			Tripwire:  running,
			// The agent named in DECISIONS.md as the one most likely to be
			// installed on this machine, so the scenario reproduces the real
			// collision rather than an invented one.
			CompetingAgent: "com.christhomas.apfs-snapshot",
		},
	}
}

// every builds a run of count snapshots, step apart, the newest of them newest
// ago.
//
// The oldest carries the container flag because on a real machine it usually
// does: the earliest snapshot is the one holding the container's floor up.
func every(count int, newest, step Span) []SnapshotSpec {
	snaps := make([]SnapshotSpec, 0, count)
	for i := 0; i < count; i++ {
		snaps = append(snaps, SnapshotSpec{Age: newest + Span(i)*step})
	}
	if len(snaps) > 0 {
		snaps[len(snaps)-1].LimitsContainer = true
	}
	return snaps
}
