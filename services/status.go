package services

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"syscall"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/text"
	"snapshotter/internal/version"
)

// openURL hands a URL to the system to open. System Settings panes are
// addressed this way, which is the only supported means of deep-linking into
// them.
func openURL(url string) error { return exec.Command("open", url).Run() }

// StatusService answers one question: is this Mac actually protected right now.
//
// Every input already existed, scattered across the other services — the
// snapshot list, the schedule, Time Machine's state, the free space. Scattered
// is the problem: a user cannot assemble "I am covered" out of four screens, and
// the failure this application exists to prevent is believing you are covered
// when you are not.
type StatusService struct{ Deps }

// NewStatusService builds the service.
func NewStatusService(d Deps) *StatusService { return &StatusService{Deps: d} }

// Level is how worried to be, and drives nothing but presentation.
type Level string

const (
	// LevelOK means snapshots exist and something is taking more.
	LevelOK Level = "ok"
	// LevelWarn means protection exists but is degraded or is quietly lying
	// about its retention.
	LevelWarn Level = "warn"
	// LevelBad means there is no usable rollback point, or nothing is taking
	// any.
	LevelBad Level = "bad"
	// LevelInfo is something the panel should say that is not a degradation.
	//
	// It exists for one reason: a scenario has to announce itself in the findings,
	// and if that announcement escalated the verdict then the clean "nothing to
	// say" state could never be reached under a scenario — which is one of the
	// states a scenario is most useful for driving the interface into.
	LevelInfo Level = "info"
)

// Health is the whole answer, shaped for one panel and one menu.
type Health struct {
	Level    Level  `json:"level"`
	Headline string `json:"headline"`
	// Version is the build the window is talking to, which is not necessarily the
	// one someone thinks they installed: a copy in /Applications and a working
	// build in bin/ share a bundle identifier, and the launchd agents run whichever
	// path was installed. Showing it costs a line and settles that question.
	Version string `json:"version"`
	// Findings are the specific things wrong, worst first. Empty when all is
	// well, so the panel has nothing to say rather than something reassuring.
	Findings []Finding `json:"findings"`

	SnapshotCount int        `json:"snapshotCount"`
	Newest        *time.Time `json:"newest,omitempty"`
	Oldest        *time.Time `json:"oldest,omitempty"`
	// CoverageHours is the span between the oldest and newest snapshot: how far
	// back a mistake can be undone, which is the number the user actually wants.
	CoverageHours float64 `json:"coverageHours"`

	ScheduleInstalled bool    `json:"scheduleInstalled"`
	ScheduleRunning   bool    `json:"scheduleRunning"`
	IntervalHours     float64 `json:"intervalHours"`
	RetentionDays     float64 `json:"retentionDays"`
	// NextDue is when the schedule should next fire, estimated from the newest
	// snapshot. launchd fires a missed interval on wake, so a past value means
	// overdue rather than skipped.
	NextDue *time.Time `json:"nextDue,omitempty"`

	VolumeTotalBytes uint64  `json:"volumeTotalBytes"`
	VolumeFreeBytes  uint64  `json:"volumeFreeBytes"`
	FreePercent      float64 `json:"freePercent"`

	// PurgeableCount is how many snapshots macOS may delete without asking.
	PurgeableCount int `json:"purgeableCount"`
	// PinningStamp names the snapshot holding the container's minimum size up,
	// which is the one worth deleting first when space runs short.
	PinningStamp string `json:"pinningStamp,omitempty"`

	// TripwireInstalled and TripwireRunning describe the bulk-deletion watcher.
	// It is reported separately from the schedule because it covers a different
	// failure: the schedule bounds how much time you can lose, the tripwire
	// bounds how much of a single deletion you can lose.
	TripwireInstalled bool `json:"tripwireInstalled"`
	TripwireRunning   bool `json:"tripwireRunning"`

	// Faking reports that mounts are simulated and nothing under a mountpoint
	// is real.
	Faking bool `json:"faking"`
	// Scenario names the simulated machine these readings describe, empty when
	// they describe this one.
	Scenario string `json:"scenario,omitempty"`
}

// Finding is one specific thing wrong, with what to do about it.
// The kinds a finding can have. They name the subject, not the severity, so the
// menu bar can draw a different picture for each rather than repeating one.
const (
	KindSnapshots = "snapshots"
	KindSchedule  = "schedule"
	KindOverdue   = "overdue"
	KindTripwire  = "tripwire"
	KindThinning  = "thinning"
	KindConflict  = "conflict"
	KindSpace     = "space"
	KindSimulated = "simulated"
)

type Finding struct {
	Level  Level  `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Action names a button the interface can offer, empty when there is
	// nothing to click.
	Action string `json:"action,omitempty"`
	// Kind says what the finding is ABOUT, where Level says how bad it is. Two
	// findings can share a level and have nothing else in common, so anything
	// choosing an icon or an illustration keys off this rather than off Level —
	// otherwise every warning looks identical.
	Kind string `json:"kind"`
}

// Check gathers everything. It never fails on a partial answer: a system that
// cannot report its free space should still say when the last snapshot was.
func (s *StatusService) Check(ctx context.Context) (Health, error) {
	h := Health{Faking: s.Faking, Scenario: s.Scenario, Version: version.String()}

	snaps, err := apfs.List(ctx, s.Runner, s.Volume)
	if err != nil {
		return h, err
	}
	h.SnapshotCount = len(snaps)
	if len(snaps) > 0 {
		newest, oldest := snaps[0].Taken, snaps[len(snaps)-1].Taken
		h.Newest, h.Oldest = &newest, &oldest
		h.CoverageHours = newest.Sub(oldest).Hours()
	}

	if details, err := apfs.Details(ctx, s.Runner, s.Volume); err == nil {
		for _, d := range details {
			if d.Purgeable {
				h.PurgeableCount++
			}
			if d.LimitsContainer {
				h.PinningStamp = d.Stamp
			}
		}
	}

	st, err := s.Agent.Status(ctx)
	if err == nil {
		h.ScheduleInstalled = st.Installed
		h.ScheduleRunning = st.Loaded
		h.IntervalHours = st.Config.Interval.Hours()
		h.RetentionDays = inDays(st.Config.Retention)
		if st.Installed && h.Newest != nil {
			due := h.Newest.Add(st.Config.Interval)
			h.NextDue = &due
		}
	}

	if total, free, err := s.space(s.Volume); err == nil {
		h.VolumeTotalBytes, h.VolumeFreeBytes = total, free
		if total > 0 {
			h.FreePercent = float64(free) / float64(total) * 100
		}
	}

	if s.Tripwire != nil {
		if tw, err := s.Tripwire.Status(ctx); err == nil {
			h.TripwireInstalled, h.TripwireRunning = tw.Installed, tw.Loaded
		}
	}

	tm := apfs.DestinationInfo(ctx, s.Runner)
	conflicts := st.Conflicts

	h.Findings = findings(h, tm.HasDestination, conflicts, time.Now())
	h.Level, h.Headline = summarise(h)
	return h, nil
}

// space reports the volume's total and available bytes, through the injected
// seam where there is one and the kernel otherwise.
func (s *StatusService) space(volume string) (total, free uint64, err error) {
	if s.Space != nil {
		return s.Space(volume)
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(volume, &stat); err != nil {
		return 0, 0, err
	}
	// Bavail rather than Bfree: the blocks this user may actually have, which is
	// the number a warning about running out should be based on.
	return stat.Blocks * uint64(stat.Bsize), stat.Bavail * uint64(stat.Bsize), nil
}

// findings lists what is wrong, worst first. Each one is a thing the user can
// act on; conditions they cannot change are not findings.
func findings(h Health, hasTMDestination bool, conflicts []string, now time.Time) []Finding {
	var out []Finding

	if h.SnapshotCount == 0 {
		out = append(out, Finding{
			Level: LevelBad,
			Title: "There are no snapshots",
			Kind:  KindSnapshots,
			Detail: "Nothing can be rolled back to. Taking one now costs no disk space " +
				"immediately, because a snapshot only grows as the files it recorded change.",
			Action: "take-snapshot",
		})
	}
	if !h.ScheduleInstalled {
		out = append(out, Finding{
			Level: LevelBad,
			Title: "Nothing is taking snapshots automatically",
			Kind:  KindSchedule,
			Detail: "macOS only schedules local snapshots when Time Machine has a backup " +
				"destination. Without one, and without this schedule, today's snapshot is the last one.",
			Action: "install-schedule",
		})
	} else if !h.ScheduleRunning {
		out = append(out, Finding{
			Level:  LevelWarn,
			Title:  "The schedule is installed but not running",
			Kind:   KindSchedule,
			Detail: "launchd has the job on disk but has not loaded it, so no snapshot will be taken.",
			Action: "install-schedule",
		})
	}

	// An overdue schedule is the quiet failure this application exists to
	// prevent: it looks configured and is not working.
	if h.NextDue != nil && h.IntervalHours > 0 {
		grace := time.Duration(h.IntervalHours) * time.Hour
		if now.After(h.NextDue.Add(grace)) {
			out = append(out, Finding{
				Level: LevelWarn,
				Title: "The last snapshot is overdue",
				Kind:  KindOverdue,
				Detail: fmt.Sprintf("A snapshot was due at %s and the newest is still from %s. Check the scheduled task's log.",
					h.NextDue.Format("15:04"), h.Newest.Format("Mon 15:04")),
				Action: "show-log",
			})
		}
	}

	// The schedule bounds how much time a mistake can cost. It does nothing
	// about how much of one deletion gets through, and that is the gap this
	// application exists for.
	if !h.TripwireInstalled {
		out = append(out, Finding{
			Level: LevelWarn,
			Title: "Nothing is watching for bulk deletion",
			Kind:  KindTripwire,
			Detail: "A schedule limits how far back you can go; it does not stop a deletion " +
				"finishing. The watcher takes a snapshot as soon as something starts removing files " +
				"in bulk, so the rest of that deletion stays recoverable.",
			Action: "install-tripwire",
		})
	} else if !h.TripwireRunning {
		out = append(out, Finding{
			Level:  LevelWarn,
			Title:  "The bulk-deletion watcher is not running",
			Kind:   KindTripwire,
			Detail: "It is installed but launchd has not loaded it, so nothing is watching.",
			Action: "install-tripwire",
		})
	}

	if hasTMDestination {
		out = append(out, Finding{
			Level:  LevelWarn,
			Title:  "Time Machine will thin these snapshots",
			Kind:   KindThinning,
			Detail: timeMachineThinning,
		})
	}
	for _, c := range conflicts {
		out = append(out, Finding{
			Level: LevelWarn,
			Title: "Another agent is also taking snapshots",
			Kind:  KindConflict,
			Detail: c + " looks like it takes local snapshots too. Two agents double the rate and " +
				"apply two retention windows to one shared set. Install one, not both.",
		})
	}

	// Purgeable is the normal state, so it is only worth saying when the disk is
	// tight enough for macOS to act on it.
	if h.FreePercent > 0 && h.FreePercent < 10 {
		out = append(out, Finding{
			Level: LevelWarn,
			Title: "Free space is low, so retention is not guaranteed",
			Kind:  KindSpace,
			Detail: fmt.Sprintf("%.0f%% free. Snapshots are purgeable: macOS reclaims the oldest under "+
				"space pressure rather than failing a write, whatever retention is set.", h.FreePercent),
		})
	}

	// First, and worded as flatly as possible. Every other finding on this screen
	// is a fact about a real Mac; under a scenario none of them are, and a reader
	// who misses that draws conclusions about a machine that does not exist.
	if h.Scenario != "" {
		out = append(out, Finding{
			Level: LevelInfo,
			Title: "These readings are simulated",
			Kind:  KindSimulated,
			Detail: "Scenario " + h.Scenario + " is loaded. Every snapshot, schedule and " +
				"figure on this screen was invented to drive the interface, and none of it " +
				"describes this Mac.",
		})
	}

	if h.Faking {
		out = append(out, Finding{
			Level: LevelWarn,
			Title: "Mounts are simulated",
			Kind:  KindSimulated,
			Detail: "SNAPSHOTTER_FAKE_MOUNTS is set. Everything inside a snapshot is invented for " +
				"development, and Replace restores are refused. Nothing shown under a snapshot is real.",
		})
	}
	return out
}

// summarise reduces the findings to one level and one sentence, which is what
// the menu bar has room for.
func summarise(h Health) (Level, string) {
	level := LevelOK
	for _, f := range h.Findings {
		// Informational findings are things worth saying, not things wrong. Letting
		// one set the verdict would mean a simulated machine could never show a
		// clean one.
		if f.Level == LevelInfo {
			continue
		}
		if f.Level == LevelBad {
			level = LevelBad
			break
		}
		level = LevelWarn
	}

	switch {
	case h.SnapshotCount == 0:
		return LevelBad, "No snapshots — nothing to roll back to"
	case !h.ScheduleInstalled:
		return LevelBad, fmt.Sprintf("%s, but nothing is taking more", snapshotCount(h.SnapshotCount))
	case level == LevelOK:
		return LevelOK, fmt.Sprintf("%s, %s of cover", snapshotCount(h.SnapshotCount), coverage(h.CoverageHours))
	default:
		actionable := 0
		for _, f := range h.Findings {
			if f.Level != LevelInfo {
				actionable++
			}
		}
		return level, fmt.Sprintf("%s, %s of cover — %s to look at",
			snapshotCount(h.SnapshotCount), coverage(h.CoverageHours),
			text.Plural(actionable, "thing"))
	}
}

func snapshotCount(n int) string { return text.Plural(n, "snapshot") }

// coverage words the span in the largest unit that stays honest, because "0.3
// days" reads as a rounding error and "7 hours" reads as a fact.
func coverage(hours float64) string {
	switch {
	case hours >= 48:
		return text.Plural(int(math.Round(hours/hoursPerDay)), "day")
	case hours >= 1:
		// Rounded first, then pluralised against what will actually be printed:
		// 1.4 hours prints as "1 hour", and pluralising the unrounded value would
		// have called it "1 hours".
		return text.Plural(int(math.Round(hours)), "hour")
	default:
		return "under an hour"
	}
}

// OpenPrivacySettings reveals the Full Disk Access pane.
//
// It exists because the mount failure it answers is otherwise a dead end: the
// error names a permission, and the place to grant it is four levels into
// System Settings.
func (s *StatusService) OpenPrivacySettings() error {
	return openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles")
}

// MountHelp is the explanation shown when mounting is refused, kept beside the
// error it explains rather than written into the frontend.
func (s *StatusService) MountHelp() string {
	return mountmgr.ErrNeedsFullDiskAccess.Error() + ".\n\n" +
		"Granting Full Disk Access to this application may not be enough on its own: the " +
		"privileged command runs by way of osascript, and macOS checks the permission against " +
		"that rather than against the application. The reliable test is to run the same " +
		"mount_apfs command in Terminal, which usually already holds the permission."
}
