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

// ModeName is what to call a policy: which of the shapes on offer it is.
//
// Named by shape rather than by reach. A name with a number in it — "yearly" —
// would be wrong for four of the five windows on offer, because a preset's reach
// follows the window chosen rather than being fixed, and that is exactly the bug
// the presets themselves used to have.
func ModeName(p Policy) string {
	switch id := IdentifyPolicy(p); id {
	case FlatID:
		return i18n.T("retention.mode.flat")
	case "custom":
		if len(p.Bands()) == 0 {
			return i18n.T("retention.mode.none")
		}
		return i18n.T("retention.mode.custom")
	default:
		// A preset's own name, which the settings screen shows beside its radio
		// button. Recovered from the policy rather than stored, so a plist written
		// months ago still knows what it is.
		bands := p.Bands()
		for _, preset := range Presets(bands[0].Every, bands[0].For) {
			if preset.ID == id {
				return preset.Name
			}
		}
		return i18n.T("retention.mode.custom")
	}
}

// Headline is the schedule in one line: which mode, how often, and how far back.
//
// The single place this sentence is built. The menu bar used to build its own from
// the interval and the retention window alone, ignoring the policy — so a tiered
// schedule was announced as "every 3 hours, kept 364 days" when only one snapshot
// every four weeks survives past the twenty-sixth. It took the horizon and called
// it the retention, which is true of a flat window and of nothing else.
//
// Hence the wording differing by kind rather than a single template: a flat window
// keeps everything for its span, and a tiered one thins towards its horizon.
// Describe gives the full sentence for somewhere with room for it.
func Headline(interval time.Duration, p Policy) string {
	mode := ModeName(p)
	bands := p.Bands()
	if len(bands) == 0 {
		return mode
	}

	key := "retention.headline.tiered"
	if p.IsFlat() {
		key = "retention.headline.flat"
	}
	return i18n.T(key,
		"Mode", mode,
		"Every", words(interval),
		"Reach", words(p.Horizon()))
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
	ID     string `json:"id"`
	Name   string `json:"name"`
	Policy Policy `json:"policy"`
}

// FlatID names the flat window in the settings screen, alongside the presets.
// It is not a preset itself because its reach is the retention the user chooses
// rather than one fixed here.
const FlatID = "flat"

// Presets are the tiered policies on offer, built from the choices the person
// actually made.
//
// They used to be fixed values whose first band was hardcoded to "everything for
// two days". The time period and flat window chosen in the interface selected
// nothing in them at all: pick a tiered preset and both settings were silently
// discarded. That is the bug this signature exists to make impossible — a preset
// cannot be constructed without them.
//
// Three bands, always. The first is the choice: every {period} for {window}. The
// two after it are multiples of that window, which is what keeps the count at
// three whatever is chosen — spans fixed in absolute days disappeared whenever
// the window already reached past them, so a fortnight's window turned a
// three-band preset into a two-band one without saying so.
//
// A band is dropped only if it would be finer than the one before it, which the
// multipliers make impossible; the check stays because the invariant is the
// thing that matters, not the arithmetic that currently satisfies it.
func Presets(period, window time.Duration) []Preset {
	base := Tier{Every: period, For: window}

	shapes := []struct {
		id   string
		name string
		// Each band beyond the first: how coarse, and how far as a multiple of
		// the window. Named for their shape rather than an absolute reach,
		// because the reach now follows the window and any number in the name
		// would be wrong for four of the five windows.
		bands []struct {
			every time.Duration
			times int
		}
	}{
		{"tiered-daily-weekly", i18n.T("retention.dailyWeekly.name"), []struct {
			every time.Duration
			times int
		}{{day, 4}, {7 * day, 13}}},
		{"tiered-weekly-monthly", i18n.T("retention.weeklyMonthly.name"), []struct {
			every time.Duration
			times int
		}{{7 * day, 13}, {28 * day, 52}}},
	}

	out := make([]Preset, 0, len(shapes))
	for _, shape := range shapes {
		tiers := []Tier{base}
		previous := base
		for _, b := range shape.bands {
			// Never finer than the band before it. A policy that refines with age
			// keeps the newest snapshots at the coarsest density, which is not a
			// thing anyone means to ask for.
			every := b.every
			if every < previous.Every {
				every = previous.Every
			}
			t := Tier{Every: every, For: window * time.Duration(b.times)}
			tiers = append(tiers, t)
			previous = t
		}
		out = append(out, Preset{ID: shape.id, Name: shape.name, Policy: Policy{Tiers: tiers}})
	}
	return out
}

// legacyPolicyIDs maps identifiers written by earlier versions onto the ones in
// use now, matched by what each shape actually did.
//
// The presets were renamed when their reach stopped being fixed — a name with a
// number in it is wrong for four of the five windows once the reach follows the
// window. The rename shipped without this, and a settings file naming
// "tiered-52-weeks" then resolved to nothing at all. That is not a cosmetic
// failure: the startup reconciliation reads this file as the intent, so an
// unresolvable name meant nothing was put back after an upgrade removed it.
//
// Old shapes, for the record: tiered-13-weeks kept everything for two days, then
// daily for a fortnight, then weekly out to thirteen weeks. tiered-52-weeks
// added a monthly band reaching a year. So the first is the daily-then-weekly
// shape and the second the weekly-then-monthly one.
var legacyPolicyIDs = map[string]string{
	"tiered-13-weeks": "tiered-daily-weekly",
	"tiered-52-weeks": "tiered-weekly-monthly",
}

// CanonicalPolicyID translates an identifier read from storage into a current
// one. Anything already current, or unrecognised, is returned unchanged.
func CanonicalPolicyID(id string) string {
	if current, ok := legacyPolicyIDs[id]; ok {
		return current
	}
	return id
}

func PolicyByID(id string, period, window time.Duration) (Policy, bool) {
	id = CanonicalPolicyID(id)
	if id == FlatID {
		return FlatPolicy(window), true
	}
	for _, p := range Presets(period, window) {
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
	// The period and window a preset was built from are recoverable from the
	// policy itself: they are its first band. So a policy can be recognised
	// without being told what it was made with, which is what keeps a plist
	// written months ago identifiable today.
	bands := p.Bands()
	if len(bands) == 0 {
		return "custom"
	}
	for _, preset := range Presets(bands[0].Every, bands[0].For) {
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
