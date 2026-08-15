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
		ReachDays: policy.Horizon().Hours() / 24,
	}
}

// days turns the unit the interface offers into a duration.
func days(n float64) time.Duration { return time.Duration(n * 24 * float64(time.Hour)) }

// Uninstall stops the schedule. Snapshots already taken are left alone.
func (s *ScheduleService) Uninstall(ctx context.Context) (ScheduleView, error) {
	if err := s.Agent.Uninstall(ctx); err != nil {
		return ScheduleView{}, err
	}
	return s.Status(ctx)
}

// Log returns the tail of the agent's log, so a schedule that is failing
// silently can be seen to be failing.
func (s *ScheduleService) Log(maxBytes int64) (string, error) {
	return tailFile(s.Agent.LogPath, maxBytes, "The scheduled task has not written anything yet.")
}

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
		maxBytes = 64 * 1024
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
		RetentionDays: st.Config.Retention.Hours() / 24,
		PlistPath:     st.PlistPath,
		LogPath:       st.LogPath,
		Conflicts:     st.Conflicts,
		PolicyID:      schedule.IdentifyPolicy(policy),
		PolicySummary: policy.Describe(),
		ReachDays:     policy.Horizon().Hours() / 24,
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
	return s.TripwireStatus(ctx)
}

// UninstallTripwire stops the watcher. Snapshots it already took are left alone.
func (s *ScheduleService) UninstallTripwire(ctx context.Context) (TripwireView, error) {
	if err := s.Tripwire.Uninstall(ctx); err != nil {
		return TripwireView{}, err
	}
	return s.TripwireStatus(ctx)
}

// TripwireLog returns the tail of the watcher's log, which is the only way to
// see that it is running and what it has reacted to.
func (s *ScheduleService) TripwireLog(maxBytes int64) (string, error) {
	return tailFile(s.Tripwire.LogPath, maxBytes,
		"The bulk-deletion watcher has not written anything yet.")
}
