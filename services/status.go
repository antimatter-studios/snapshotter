package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"os/exec"
	"snapshotter/internal/events"
	"snapshotter/internal/i18n"
	"syscall"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/text"
	"snapshotter/internal/version"
	"snapshotter/internal/watch"
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

	ScheduleInstalled bool `json:"scheduleInstalled"`
	// ScheduleProgram is the binary the installed plist names, and
	// ScheduleProgramMissing says it is gone. Kept separate from Installed
	// because launchd reports a job whose program has vanished as installed and
	// loaded, and it is neither working nor obviously broken.
	ScheduleProgram        string  `json:"scheduleProgram"`
	ScheduleProgramMissing bool    `json:"scheduleProgramMissing"`
	ScheduleRunning        bool    `json:"scheduleRunning"`
	IntervalHours          float64 `json:"intervalHours"`
	RetentionDays          float64 `json:"retentionDays"`
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
	KindStale     = "stale"
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
		h.ScheduleProgram = st.Program
		h.ScheduleProgramMissing = st.ProgramMissing
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
		out = append(out, findingNoSnapshots())
	}

	if !h.ScheduleInstalled {
		out = append(out, findingNoSchedule())
	} else if !h.ScheduleRunning {
		out = append(out, findingScheduleNotRunning())
	}

	// Worse than not installed, because it does not look like anything is wrong:
	// launchd holds the job, reports it loaded, and fails to start a binary that
	// is not there — once an interval, silently, forever.
	if h.ScheduleProgramMissing {
		out = append(out, staleProgramFinding(h.ScheduleProgram))
	}

	// An overdue schedule is the quiet failure this application exists to
	// prevent: it looks configured and is not working.
	if h.NextDue != nil && h.IntervalHours > 0 {
		if now.After(h.NextDue.Add(overdueGrace(h.IntervalHours))) {
			out = append(out, overdueFinding(*h.NextDue, *h.Newest))
		}
	}

	if !h.TripwireInstalled {
		out = append(out, findingNoTripwire())
	} else if !h.TripwireRunning {
		out = append(out, findingTripwireNotRunning())
	}

	if hasTMDestination {
		out = append(out, findingTimeMachineThins())
	}

	for _, agent := range conflicts {
		out = append(out, conflictingAgentFinding(agent))
	}

	if h.FreePercent > 0 && h.FreePercent < lowFreeSpacePercent {
		out = append(out, lowFreeSpaceFinding(h.FreePercent))
	}

	// Last, and only ever informational: a scenario is not a fault, but nothing
	// else on screen is true and that has to be said where it will be read.
	if h.Scenario != "" {
		out = append(out, simulatedReadingsFinding(h.Scenario))
	}
	if h.Faking {
		out = append(out, findingSimulatedMounts())
	}

	return out
}

// lowFreeSpacePercent is when free space is worth mentioning. Snapshots are
// purgeable, so below this the retention someone configured stops being a
// promise the system will keep.
const lowFreeSpacePercent = 10

// overdueGrace is how late a snapshot may be before it is worth reporting.
//
// A whole interval, because launchd starts a job late for reasons of its own — a
// sleeping machine most of all — and a warning that fires on every wake is one
// nobody reads.
func overdueGrace(intervalHours float64) time.Duration {
	return time.Duration(intervalHours) * time.Hour
}

// The findings this application can report that say the same thing every time.
//
// They are values because that is what they are: fixed prose describing a fixed
// situation. Keeping them out of findings() leaves it a list of the conditions
// themselves, which is the part worth reading in order.
// The findings, worded in whichever language is in force.
//
// Functions rather than package-level values, because a value would be built
// once at startup and keep the language chosen then — and switching language is
// meant to take effect without a relaunch. Each is called where it is used, which
// is once per health check.
func findingNoSnapshots() Finding {
	return Finding{
		Level:  LevelBad,
		Title:  i18n.T("status.noSnapshots.title"),
		Kind:   KindSnapshots,
		Detail: i18n.T("status.noSnapshots.detail"),
		Action: "take-snapshot",
	}
}

func findingNoSchedule() Finding {
	return Finding{
		Level:  LevelBad,
		Title:  i18n.T("status.noSchedule.title"),
		Kind:   KindSchedule,
		Detail: i18n.T("status.noSchedule.detail"),
		Action: "install-schedule",
	}
}

func findingScheduleNotRunning() Finding {
	return Finding{
		Level:  LevelWarn,
		Title:  i18n.T("status.scheduleNotRunning.title"),
		Kind:   KindSchedule,
		Detail: i18n.T("status.scheduleNotRunning.detail"),
		Action: "install-schedule",
	}
}

func findingNoTripwire() Finding {
	return Finding{
		Level:  LevelWarn,
		Title:  i18n.T("status.noTripwire.title"),
		Kind:   KindTripwire,
		Detail: i18n.T("status.noTripwire.detail"),
		Action: "install-tripwire",
	}
}

func findingTripwireNotRunning() Finding {
	return Finding{
		Level:  LevelWarn,
		Title:  i18n.T("status.tripwireNotRunning.title"),
		Kind:   KindTripwire,
		Detail: i18n.T("status.tripwireNotRunning.detail"),
		Action: "install-tripwire",
	}
}

func findingTimeMachineThins() Finding {
	return Finding{
		Level: LevelWarn,
		Title: i18n.T("status.timeMachineThins.title"),
		Kind:  KindThinning,
		// Untranslated: it is one sentence of Apple's own behaviour, kept beside
		// the code that knows about it rather than copied into four catalogues.
		Detail: apfs.ThinningWarning,
	}
}

func findingSimulatedMounts() Finding {
	return Finding{
		Level:  LevelWarn,
		Title:  i18n.T("status.simulatedMounts.title"),
		Kind:   KindSimulated,
		Detail: i18n.T("status.simulatedMounts.detail"),
	}
}

// The findings that name something specific — which binary has gone, which agent
// conflicts, how little room is left. Built rather than stored, because the
// particular is the useful part.

func staleProgramFinding(program string) Finding {
	return Finding{
		Level: LevelBad,
		Kind:  KindStale,
		Title: i18n.T("status.scheduleMissingBinary.title"),
		Detail: "launchd still has the job and still reports it as running, but the " +
			"program it names (" + program + ") no longer exists, so every " +
			"run fails and no snapshot is taken. Installing the schedule again points " +
			"it at this copy.",
		Action: "install-schedule",
	}
}

func overdueFinding(dueAt, newest time.Time) Finding {
	return Finding{
		Level: LevelWarn,
		Title: i18n.T("status.overdue.title"),
		Kind:  KindOverdue,
		Detail: i18n.T("status.overdue.detail",
			"due", dueAt.Format("15:04"), "newest", newest.Format("Mon 15:04")),
		Action: "show-log",
	}
}

func conflictingAgentFinding(agent string) Finding {
	return Finding{
		Level: LevelWarn,
		Title: i18n.T("status.conflict.title"),
		Kind:  KindConflict,
		Detail: agent + " looks like it takes local snapshots too. Two agents double the rate and " +
			"apply two retention windows to one shared set. Install one, not both.",
	}
}

func lowFreeSpaceFinding(freePercent float64) Finding {
	return Finding{
		Level: LevelWarn,
		Title: i18n.T("status.lowSpace.title"),
		Kind:  KindSpace,
		Detail: fmt.Sprintf("%.0f%% free. Snapshots are purgeable: macOS reclaims the oldest under "+
			"space pressure rather than failing a write, whatever retention is set.", freePercent),
	}
}

func simulatedReadingsFinding(scenario string) Finding {
	return Finding{
		Level: LevelInfo,
		Title: i18n.T("status.simulated.title"),
		Kind:  KindSimulated,
		Detail: "Scenario " + scenario + " is loaded. Every snapshot, schedule and " +
			"figure on this screen was invented to drive the interface, and none of it " +
			"describes this Mac.",
	}
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
		return LevelBad, i18n.T("status.noSnapshotsShort")
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
		i18n.T("status.fdaWarning")
}

// Warning is a bulk deletion the tripwire saw, as the window shows it.
//
// It comes from a file rather than from memory because the tripwire is a
// separate process: by the time anyone opens the window, the process that saw
// the deletion has long exited.
type Warning struct {
	At time.Time `json:"at"`
	// Where names the folders the files went from, commonest first, as full
	// paths. This is what an ignore rule is built from: "~" means nothing to a
	// comparison against a path the filesystem reported.
	Where []string `json:"where"`
	// Labels is the same list written the way a person writes it, with the home
	// directory as "~". Both are sent because the interface shows one and acts on
	// the other, and deriving either in the window would mean teaching it where
	// home is.
	Labels []string `json:"labels"`
	// Snapshot is the restore point taken in response. Empty means none was, and
	// Note says why.
	Snapshot string `json:"snapshot,omitempty"`
	Note     string `json:"note,omitempty"`
}

// RecentWarnings returns the last few bulk deletions, newest first.
//
// Never an error: a machine where nothing has happened is the ordinary case, and
// a screen that shows a red banner instead of an empty section on a healthy Mac
// has made things worse.
func (s *StatusService) RecentWarnings(limit int) []Warning {
	if limit <= 0 {
		limit = defaultWarningsShown
	}
	recent, err := events.Recent(limit)
	if err != nil {
		log.Printf("reading recent warnings: %v", err)
		return nil
	}

	out := make([]Warning, 0, len(recent))
	for _, e := range recent {
		if e.Kind != events.KindBulkDeletion {
			continue
		}
		labels := make([]string, len(e.Where))
		for i, dir := range e.Where {
			labels[i] = watch.Shorten(dir)
		}
		out = append(out, Warning{
			At: e.At, Where: e.Where, Labels: labels,
			Snapshot: e.Snapshot, Note: e.Note,
		})
	}
	return out
}

// defaultWarningsShown is how many the home screen asks for. Enough to see a
// pattern — the same folder three times is a different story from three
// different folders — without turning a summary into a log viewer.
const defaultWarningsShown = 5
