package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"snapshotter/internal/config"
	"snapshotter/internal/schedule"
)

// ScheduleService installs and inspects the timer that takes snapshots.
type ScheduleService struct{ Deps }

// NewScheduleService builds the service.
func NewScheduleService(d Deps) *ScheduleService { return &ScheduleService{Deps: d} }

// ScheduleView is the schedule as the settings screen shows it.
type ScheduleView struct {
	Installed bool `json:"installed"`
	Loaded    bool `json:"loaded"`
	// IntervalHours and RetentionDays are the units the interface offers,
	// rather than the durations stored underneath.
	IntervalHours float64  `json:"intervalHours"`
	RetentionDays float64  `json:"retentionDays"`
	PlistPath     string   `json:"plistPath"`
	LogPath       string   `json:"logPath"`
	Conflicts     []string `json:"conflicts"`
	// MaxSnapshots is how many the schedule will hold at this interval and
	// retention, which is the number worth seeing before committing to a
	// setting. Counted by planning a history rather than by dividing the window
	// by the interval, because with a tiered policy those are different numbers
	// and only the first one is true.
	MaxSnapshots int `json:"maxSnapshots"`
	// PolicyID names the retention policy in force, so the settings screen can
	// preselect it: "flat", a preset's identifier, or "custom" for a plist
	// somebody edited by hand.
	PolicyID string `json:"policyId"`
	// PolicySummary is the policy as a sentence, worded in one place so the
	// interface never has to word durations a second time.
	PolicySummary string `json:"policySummary"`
	// ReachDays is how far back the retained history goes. For a flat window it
	// is RetentionDays; for a tiered policy it is much further, and it is the
	// number the whole argument for tiering rests on.
	ReachDays float64    `json:"reachDays"`
	Tiers     []TierView `json:"tiers"`
}

// TierView is one band of a retention policy in the units the interface uses.
type TierView struct {
	// EveryHours is zero for a band that keeps every snapshot it covers.
	EveryHours float64 `json:"everyHours"`
	ForHours   float64 `json:"forHours"`
}

// PolicyOption is one retention policy on offer, with what it would actually do
// at the interval currently chosen.
//
// The numbers are the point. "Hourly for a day, daily for a week" tells nobody
// whether they end up with more or fewer restore points than they have now,
// which is the only question anyone actually has about retention.
type PolicyOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Why decides it for someone who will not read the bands; Summary is the
	// bands themselves, for someone who will.
	Why     string     `json:"why"`
	Summary string     `json:"summary"`
	Tiers   []TierView `json:"tiers"`
	// Retained is how many snapshots the policy holds once the schedule has been
	// running longer than the policy reaches, counted by planning a history at
	// this interval so it cannot drift from the code that deletes.
	Retained int `json:"retained"`
	// ReachDays is the age of the oldest snapshot still kept.
	ReachDays float64 `json:"reachDays"`
}

// Status reports what launchd currently has.
func (s *ScheduleService) Status(ctx context.Context) (ScheduleView, error) {
	st, err := s.Agent.Status(ctx)
	if err != nil {
		return ScheduleView{}, err
	}
	return viewOf(st), nil
}

// Install writes and loads the schedule with a flat retention window.
//
// It stays as it was, taking the two numbers and nothing else, because the
// one-click fix in the Health panel calls it and installing a schedule must not
// require an opinion about tiering first.
func (s *ScheduleService) Install(ctx context.Context, intervalHours, retentionDays float64) (ScheduleView, error) {
	return s.InstallPolicy(ctx, intervalHours, retentionDays, schedule.FlatID)
}

// InstallPolicy writes and loads the schedule with a chosen retention policy.
// retentionDays is the flat window, and is used only when policyID names it —
// every other policy carries its own bands.
func (s *ScheduleService) InstallPolicy(ctx context.Context, intervalHours, retentionDays float64, policyID string) (ScheduleView, error) {
	// Recorded as intent before anything is installed. launchd remains the truth
	// about what is running; this is what the settings screen should show on a
	// machine where nothing is installed yet, and what a second installation should
	// inherit rather than asking again.
	if cfg, err := config.Load(); err == nil {
		cfg.Schedule.Enabled = true
		cfg.Schedule.IntervalHours = intervalHours
		cfg.Schedule.RetentionDays = retentionDays
		if policyID != "" {
			cfg.Schedule.Policy = policyID
		}
		if err := config.Save(cfg); err != nil {
			log.Printf("schedule: recording the choice in the configuration: %v", err)
		}
	}
	policy, ok := schedule.PolicyByID(policyID, days(retentionDays))
	if !ok {
		return ScheduleView{}, fmt.Errorf("services: %q is not a retention policy", policyID)
	}
	cfg := schedule.Config{
		Interval:  time.Duration(intervalHours * float64(time.Hour)),
		Retention: days(retentionDays),
		Policy:    policy,
	}
	if err := s.Agent.Install(ctx, cfg); err != nil {
		return ScheduleView{}, err
	}
	return s.Status(ctx)
}

// Policies reports the retention policies on offer, each with what it would
// retain at the interval and flat window currently chosen. The settings screen
// calls it as those change, so the numbers beside each choice are always the
// numbers for the schedule being configured.
func (s *ScheduleService) Policies(intervalHours, retentionDays float64) []PolicyOption {
	interval := time.Duration(intervalHours * float64(time.Hour))
	now := time.Now()

	options := []PolicyOption{optionOf(
		schedule.FlatID, "Flat window",
		"Every snapshot inside the window and nothing outside it — what this schedule has always done.",
		schedule.FlatPolicy(days(retentionDays)), interval, now,
	)}
	for _, preset := range schedule.Presets {
		options = append(options, optionOf(preset.ID, preset.Name, preset.Why, preset.Policy, interval, now))
	}
	return options
}

func optionOf(id, name, why string, policy schedule.Policy, interval time.Duration, now time.Time) PolicyOption {
	return PolicyOption{
		ID:        id,
		Name:      name,
		Why:       why,
		Summary:   policy.Describe(),
		Tiers:     tierViews(policy),
		Retained:  schedule.Retained(policy, interval, now),
		ReachDays: inDays(policy.Horizon()),
	}
}

// The interface talks in days and durations are hours, so the conversion runs in
// both directions and in several views. It is written once each way.
const hoursPerDay = 24

// days turns the unit the interface offers into a duration.
func days(n float64) time.Duration { return time.Duration(n * hoursPerDay * float64(time.Hour)) }

// inDays turns a duration back into the unit the interface offers.
func inDays(d time.Duration) float64 { return d.Hours() / hoursPerDay }

// Uninstall stops the schedule. Snapshots already taken are left alone.
func (s *ScheduleService) Uninstall(ctx context.Context) (ScheduleView, error) {
	if err := s.Agent.Uninstall(ctx); err != nil {
		return ScheduleView{}, err
	}
	// Recorded, or the next launch would helpfully put back the schedule that
	// was just deliberately removed.
	if cfg, err := config.Load(); err == nil {
		cfg.Schedule.Enabled = false
		if err := config.Save(cfg); err != nil {
			log.Printf("schedule: recording the removal: %v", err)
		}
	}
	return s.Status(ctx)
}

// Log returns the tail of the agent's log, so a schedule that is failing
// silently can be seen to be failing.
func (s *ScheduleService) Log(maxBytes int64) (string, error) {
	return tailFile(s.Agent.LogPath, maxBytes, "The scheduled task has not written anything yet.")
}

// defaultLogTailBytes is how much of a log is returned when the caller does not
// say. Enough to hold weeks of a scheduled task's one line per run, and small
// enough to hand to a web view as a single string.
//
// Callers ask for this by passing nothing rather than by naming a size of their
// own, so every screen shows the same amount of the same log.
const defaultLogTailBytes = 64 * 1024

// tailFile returns the last maxBytes of a log, or empty if it says nothing yet.
// Shared by the two agents so their behaviour cannot drift apart.
func tailFile(path string, maxBytes int64, absent string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return absent, nil
		}
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = defaultLogTailBytes
	}
	offset := int64(0)
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return "", err
	}
	buf := make([]byte, info.Size()-offset)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	return string(buf[:n]), nil
}

func viewOf(st schedule.Status) ScheduleView {
	policy := st.Config.EffectivePolicy()
	v := ScheduleView{
		Installed:     st.Installed,
		Loaded:        st.Loaded,
		IntervalHours: st.Config.Interval.Hours(),
		RetentionDays: inDays(st.Config.Retention),
		PlistPath:     st.PlistPath,
		LogPath:       st.LogPath,
		Conflicts:     st.Conflicts,
		PolicyID:      schedule.IdentifyPolicy(policy),
		PolicySummary: policy.Describe(),
		ReachDays:     inDays(policy.Horizon()),
		Tiers:         tierViews(policy),
	}
	v.MaxSnapshots = schedule.Retained(policy, st.Config.Interval, time.Now())
	return v
}

func tierViews(policy schedule.Policy) []TierView {
	bands := policy.Bands()
	views := make([]TierView, 0, len(bands))
	for _, band := range bands {
		views = append(views, TierView{EveryHours: band.Every.Hours(), ForHours: band.For.Hours()})
	}
	return views
}

// Describe renders the schedule as a sentence for the settings screen.
func (v ScheduleView) Describe() string {
	if !v.Installed {
		return "No schedule installed. Nothing is taking snapshots automatically."
	}
	state := "installed but not running"
	if v.Loaded {
		state = "running"
	}
	return fmt.Sprintf("A snapshot every %g hours. %s About %d snapshots, reaching back %g days — %s.",
		v.IntervalHours, v.PolicySummary, v.MaxSnapshots, v.ReachDays, state)
}

// TripwireView is the bulk-deletion watcher as the settings screen shows it.
type TripwireView struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	PlistPath string `json:"plistPath"`
	LogPath   string `json:"logPath"`
}

// TripwireStatus reports whether the watcher is installed and running.
func (s *ScheduleService) TripwireStatus(ctx context.Context) (TripwireView, error) {
	st, err := s.Tripwire.Status(ctx)
	if err != nil {
		return TripwireView{}, err
	}
	return TripwireView{
		Installed: st.Installed, Running: st.Loaded,
		PlistPath: st.PlistPath, LogPath: st.LogPath,
	}, nil
}

// InstallTripwire starts the watcher and keeps it started across logins.
func (s *ScheduleService) InstallTripwire(ctx context.Context) (TripwireView, error) {
	if err := s.Tripwire.Install(ctx); err != nil {
		return TripwireView{}, err
	}
	if cfg, err := config.Load(); err == nil {
		cfg.Tripwire.Enabled = true
		if err := config.Save(cfg); err != nil {
			log.Printf("tripwire: recording the choice: %v", err)
		}
	}
	return s.TripwireStatus(ctx)
}

// UninstallTripwire stops the watcher. Snapshots it already took are left alone.
func (s *ScheduleService) UninstallTripwire(ctx context.Context) (TripwireView, error) {
	if err := s.Tripwire.Uninstall(ctx); err != nil {
		return TripwireView{}, err
	}
	if cfg, err := config.Load(); err == nil {
		cfg.Tripwire.Enabled = false
		if err := config.Save(cfg); err != nil {
			log.Printf("tripwire: recording the removal: %v", err)
		}
	}
	return s.TripwireStatus(ctx)
}

// TripwireLog returns the tail of the watcher's log, which is the only way to
// see that it is running and what it has reacted to.
func (s *ScheduleService) TripwireLog(maxBytes int64) (string, error) {
	return tailFile(s.Tripwire.LogPath, maxBytes,
		"The bulk-deletion watcher has not written anything yet.")
}

// Restored says what Restore put back, so the caller can tell someone.
type Restored struct {
	Schedule bool `json:"schedule"`
	Tripwire bool `json:"tripwire"`
}

// Any reports whether anything needed restoring.
func (r Restored) Any() bool { return r.Schedule || r.Tripwire }

// Restore reinstalls whatever the settings say was asked for and launchd no
// longer has.
//
// A launchd job is not durable in the way people assume. Upgrading through
// Homebrew removes it, because the cask's uninstall stanza unloads both agents
// before the new version is staged; so does anything else that tidies
// ~/Library/LaunchAgents. The failure is silent and it is the worst one this
// application has: the window still shows the interval that was chosen, the
// settings file still records it, and nothing is taking snapshots.
//
// So the settings file is treated as the intent and launchd as the current
// state, and this reconciles the second to the first. It only ever ADDS: a
// schedule that was deliberately removed sets Enabled to false, and is not put
// back.
func (s *ScheduleService) Restore(ctx context.Context) (Restored, error) {
	var out Restored

	cfg, err := config.Load()
	if err != nil {
		// A settings file that will not parse says nothing about what was wanted,
		// and guessing would install a schedule nobody asked for.
		return out, err
	}

	if cfg.Schedule.Enabled {
		st, err := s.Agent.Status(ctx)
		if err != nil {
			return out, err
		}
		if !st.Installed {
			if _, err := s.InstallPolicy(ctx, cfg.Schedule.IntervalHours, cfg.Schedule.RetentionDays, cfg.Schedule.Policy); err != nil {
				return out, err
			}
			out.Schedule = true
		}
	}

	if cfg.Tripwire.Enabled {
		st, err := s.Tripwire.Status(ctx)
		if err != nil {
			return out, err
		}
		if !st.Installed {
			if _, err := s.InstallTripwire(ctx); err != nil {
				return out, err
			}
			out.Tripwire = true
		}
	}

	return out, nil
}
