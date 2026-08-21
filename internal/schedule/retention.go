package schedule

import (
	"context"
	"fmt"
	"snapshotter/internal/i18n"
	"sort"
	"strings"
	"time"
	"unicode"

	"snapshotter/internal/apfs"
	"snapshotter/internal/retention"
)

// day is spelled out because a retention policy is discussed in days and weeks
// and reads as noise in hours.
const day = 24 * time.Hour

// The retention rules live in internal/retention, which knows nothing about
// snapshots, filesystems or languages. This package is the boundary: it converts
// between the two, and holds the parts that genuinely need a snapshot, a
// translation or a command runner.
//
// Aliased rather than wrapped so that every existing reference to
// schedule.Policy keeps working and there is exactly one definition of what a
// policy is.
type (
	Tier   = retention.Tier
	Policy = retention.Policy
)

// BucketStart identifies the period a snapshot falls in.
func BucketStart(taken time.Time, every time.Duration) int64 {
	return retention.BucketStart(taken, every)
}

// ParsePolicy reads a policy back from the form String writes.
func ParsePolicy(text string) (Policy, bool) { return retention.ParsePolicy(text) }

// hoursUp rounds a duration up to whole hours, the resolution the plist carries.
func hoursUp(d time.Duration) int { return retention.HoursUp(d) }

// FlatPolicy keeps everything for a window.
func FlatPolicy(retain time.Duration) Policy { return retention.FlatPolicy(retain) }

// Plan decides which snapshots a policy keeps, newest first.
//
// The sort here is the tie-break the domain cannot know about: two snapshots
// taken in the same instant are ordered by their stamp, and retention.Plan sorts
// stably by time alone, so that order survives.
func Plan(snaps []apfs.Snapshot, policy Policy, now time.Time) (keep, prune []apfs.Snapshot) {
	if len(snaps) == 0 {
		return nil, nil
	}

	ordered := append([]apfs.Snapshot(nil), snaps...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Taken.Equal(ordered[j].Taken) {
			return ordered[i].Stamp > ordered[j].Stamp
		}
		return ordered[i].Taken.After(ordered[j].Taken)
	})

	taken := make([]time.Time, len(ordered))
	for i, s := range ordered {
		taken[i] = s.Taken
	}

	keepAt, pruneAt := retention.Plan(taken, policy, now)
	for _, i := range keepAt {
		keep = append(keep, ordered[i])
	}
	for _, i := range pruneAt {
		prune = append(prune, ordered[i])
	}
	return keep, prune
}

func Describe(p Policy) string {
	tiers := p.Bands()
	if len(tiers) == 0 {
		return i18n.T("retention.nothingPruned")
	}
	// Each clause is a whole message rather than words glued together, because
	// the order of rate and span is not the same in every language and gluing
	// fixes English's.
	parts := make([]string, 0, len(tiers))
	for i, t := range tiers {
		switch {
		case t.Every <= 0:
			parts = append(parts, i18n.T("retention.clause.everythingFor", "Span", words(t.For)))
		case i == 0:
			parts = append(parts, i18n.T("retention.clause.rateFor", "Rate", everyPhrase(t.Every), "Span", words(t.For)))
		default:
			parts = append(parts, i18n.T("retention.clause.rateOutTo", "Rate", everyPhrase(t.Every), "Span", words(t.For)))
		}
	}
	sentence := strings.Join(parts, i18n.T("retention.join"))
	// By rune, not by byte: a sentence starting with "Ü" or "É" would otherwise be
	// cut in half by slicing one byte off the front.
	letters := []rune(sentence)
	if len(letters) == 0 {
		return ""
	}
	letters[0] = unicode.ToUpper(letters[0])
	return string(letters) + "."
}

// everyPhrase words a bucket period as a rate. The three common ones get the
// phrasing a person would use, because "one every 1 day" is the sort of thing
// that makes a settings screen look generated.
func everyPhrase(every time.Duration) string {
	switch every {
	case time.Hour:
		return i18n.T("retention.everyHour")
	case day:
		return i18n.T("retention.everyDay")
	case 7 * day:
		return i18n.T("retention.everyWeek")
	default:
		return i18n.T("retention.everyOther", "Span", words(every))
	}
}

func words(d time.Duration) string {
	hours := int((d + time.Minute/2) / time.Hour)
	switch {
	case hours >= 4*hoursPerWeek && hours%hoursPerWeek == 0:
		return i18n.N("count.weeks", hours/hoursPerWeek)
	case hours >= hoursPerDay && hours%hoursPerDay == 0:
		return i18n.N("count.days", hours/hoursPerDay)
	default:
		return i18n.N("count.hours", hours)
	}
}

const (
	hoursPerDay  = 24
	hoursPerWeek = 7 * hoursPerDay
)

func Retained(policy Policy, interval time.Duration, now time.Time) int {
	if interval <= 0 {
		return 0
	}
	horizon := policy.Horizon()
	if horizon <= 0 {
		return 0
	}
	// A short interval against a long reach could ask for millions of synthetic
	// snapshots. The cap is far above any schedule this application installs —
	// hourly for five years is 43,800 — and stops a hand-edited plist turning a
	// settings screen into a hang.
	const maxSynthetic = 100_000
	count := int(horizon/interval) + 1
	if count > maxSynthetic {
		count = maxSynthetic
	}
	snaps := make([]apfs.Snapshot, 0, count)
	for i := 0; i < count; i++ {
		snaps = append(snaps, apfs.Snapshot{Taken: now.Add(-time.Duration(i) * interval)})
	}
	keep, _ := Plan(snaps, policy, now)
	return len(keep)
}

// Preset is a named policy the settings screen offers.
type Preset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Why is the one line that decides it for someone who does not want to read
	// the tiers.
	Why    string `json:"why"`
	Policy Policy `json:"policy"`
}

// FlatID names the flat window in the settings screen, alongside the presets.
// It is not a preset itself because its reach is the retention the user chooses
// rather than one fixed here.
const FlatID = "flat"

// Presets are the tiered policies on offer.
//
// Every period is a whole multiple of the one before it: a day is 24 hours, a
// week is 7 days, and the longest band is 4 weeks rather than a calendar month.
// That is what makes the buckets nest, and nesting is what stops coarsening
// re-choosing: when a snapshot ages out of the daily band into the weekly one,
// the snapshot the weekly band keeps is one the daily band was already keeping,
// so no snapshot is deleted merely because the granularity changed. A calendar
// month is not a whole number of weeks and would break that for the sake of a
// tidier label. TestPresetPeriodsNest holds this.
//
// Each band's reach is a whole number of its own periods too, so the oldest
// bucket in a band is a full one rather than a stub.
// A function rather than a value: a package-level slice is built at init, before
// any language has been chosen, and would keep English names for the life of the
// process. Called where it is used, which is once per settings screen.
func Presets() []Preset {
	return []Preset{
		{
			ID:   "tiered-13-weeks",
			Name: i18n.T("retention.tiered13.name"),
			Why:  i18n.T("retention.tiered13.why"),
			Policy: Policy{Tiers: []Tier{
				{Every: 0, For: 2 * day},
				{Every: day, For: 14 * day},
				{Every: 7 * day, For: 91 * day},
			}},
		},
		{
			ID:   "tiered-52-weeks",
			Name: i18n.T("retention.tiered52.name"),
			Why:  i18n.T("retention.tiered52.why"),
			Policy: Policy{Tiers: []Tier{
				{Every: 0, For: 2 * day},
				{Every: day, For: 14 * day},
				{Every: 7 * day, For: 56 * day},
				{Every: 28 * day, For: 364 * day},
			}},
		},
	}
}

// PolicyByID resolves what the settings screen sends back. The flat window needs
// the retention the user chose, since that is the only part of it not fixed
// here.
func PolicyByID(id string, flatRetention time.Duration) (Policy, bool) {
	if id == FlatID {
		return FlatPolicy(flatRetention), true
	}
	for _, p := range Presets() {
		if p.ID == id {
			return p.Policy, true
		}
	}
	return Policy{}, false
}

// IdentifyPolicy names a policy: a preset's identifier, FlatID for a single
// keep-everything band, or "custom" for anything else.
//
// Reporting a hand-edited plist as "custom" rather than as the nearest preset
// keeps the settings screen honest about what launchd will actually do.
func IdentifyPolicy(p Policy) string {
	for _, preset := range Presets() {
		if preset.Policy.Equal(p) {
			return preset.ID
		}
	}
	if p.IsFlat() {
		return FlatID
	}
	return "custom"
}

// PruneByPolicy lists the snapshots on a volume, plans them, and deletes what
// the plan prunes, returning the deleted ones.
//
// It sits beside apfs.Prune, which implements the flat window, so the scheduled
// run has one call to make whichever kind of retention is configured. The
// planning is separate and pure; this is only the part that talks to tmutil.
func PruneByPolicy(ctx context.Context, r apfs.Runner, volume string, policy Policy, now time.Time) ([]apfs.Snapshot, error) {
	snaps, err := apfs.List(ctx, r, volume)
	if err != nil {
		return nil, err
	}
	_, prune := Plan(snaps, policy, now)

	// Oldest first, so a run that fails partway leaves the same shape a
	// completed one would — thinned from the far end — rather than a history
	// with holes recently punched in the middle of it.
	var deleted []apfs.Snapshot
	for i := len(prune) - 1; i >= 0; i-- {
		if err := apfs.Delete(ctx, r, prune[i].Stamp); err != nil {
			return deleted, fmt.Errorf("schedule: pruning by policy %s: %w", policy, err)
		}
		deleted = append(deleted, prune[i])
	}
	return deleted, nil
}
