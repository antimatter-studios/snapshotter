package schedule

import (
	"context"
	"fmt"
	"snapshotter/internal/i18n"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"snapshotter/internal/apfs"
)

// day is spelled out because a retention policy is discussed in days and weeks
// and reads as noise in hours.
const day = 24 * time.Hour

// Tier is one band of a retention policy: keep one snapshot per Every-long
// bucket, for snapshots up to For old.
//
// Every of zero means keep every snapshot the band covers, which is how the flat
// window this replaces is expressed — see FlatPolicy.
type Tier struct {
	Every time.Duration `json:"every"`
	For   time.Duration `json:"for"`
}

// Policy is how snapshots thin out as they age: everything recent, then one a
// day, then one a week. Tiers may be given in any order; Plan sorts them.
type Policy struct {
	Tiers []Tier `json:"tiers"`
}

// FlatPolicy is the behaviour that existed before tiering: keep everything
// inside the window and nothing outside it.
//
// It exists so no installed schedule changes meaning. A flat window is a policy
// with one keep-everything tier, and Plan under it agrees with apfs.Prune to the
// boundary, which TestFlatPolicyMatchesTheWindowItReplaces holds it to.
func FlatPolicy(retain time.Duration) Policy {
	return Policy{Tiers: []Tier{{Every: 0, For: retain}}}
}

// Plan divides snapshots into the ones a policy keeps and the ones it prunes,
// both newest first. It changes nothing: the caller deletes, or does not.
//
// Being a pure function of its arguments — now included — is the point. Deletion
// is irreversible and a snapshot cannot be recreated, because it records a past
// state of a disk that has since moved on. A retention bug is therefore only
// ever discovered by the person who needed the snapshot it deleted, so the
// decision is made somewhere it can be tested exhaustively and cheaply.
func Plan(snaps []apfs.Snapshot, policy Policy, now time.Time) (keep, prune []apfs.Snapshot) {
	if len(snaps) == 0 {
		return nil, nil
	}

	// A copy, sorted newest first. apfs.List already returns that order, but a
	// plan that quietly relied on the caller having sorted correctly would fail
	// by deleting the wrong snapshots — and sorting in place would reorder a
	// slice the caller may still be displaying.
	ordered := append([]apfs.Snapshot(nil), snaps...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Taken.Equal(ordered[j].Taken) {
			return ordered[i].Stamp > ordered[j].Stamp
		}
		return ordered[i].Taken.After(ordered[j].Taken)
	})

	tiers := policy.normalised()
	if len(tiers) == 0 {
		// A policy with nothing usable in it keeps everything. The other reading
		// — no tier covers any snapshot, so delete the lot — turns a zero value,
		// a half-decoded plist or a typo into total loss of the restore history.
		// The two failures are not comparable: pruning too little is corrected
		// by the next run, and macOS reclaims purgeable snapshots under space
		// pressure on its own.
		return ordered, nil
	}

	// One bucket per (tier period, absolute period start). The period is part of
	// the key because adjacent tiers overlap slightly at their boundary, and two
	// snapshots governed by different tiers must not be taken for one bucket.
	type bucket struct {
		every time.Duration
		start int64
	}
	claimed := make(map[bucket]bool, len(ordered))

	kept := make([]bool, len(ordered))
	// The newest snapshot is kept whatever the policy says. A policy that planned
	// away the last snapshot would leave the machine with no restore point at
	// all, which is the exact state this application exists to prevent, and no
	// setting a user can choose is worth that.
	kept[0] = true

	// Oldest first, so the first snapshot met in a bucket is the oldest one in
	// it, and the oldest is the one kept. That choice is the only one in this
	// file that is not forced, and it decides whether the kept set is stable as
	// time passes.
	//
	// Keeping the newest is unstable. The newest snapshot in a period changes
	// every time another arrives, so a snapshot kept by yesterday's plan is
	// deleted by today's for no reason other than that a newer one now shares its
	// bucket. A snapshot that survives one plan and is destroyed by the next,
	// with nothing having changed about it, means the far end of the history
	// keeps rewriting itself and nothing seen yesterday can be relied on today.
	//
	// Keeping the oldest is stable. The oldest snapshot in a bucket is fixed the
	// moment that bucket's first snapshot exists, since everything arriving later
	// is newer, so once a snapshot is its bucket's keeper it stays the keeper.
	// Within a tier, advancing now cannot change the kept set at all: the buckets
	// are absolute (see bucketStart) and so is the choice inside them.
	//
	// It also reaches further back for the same count, and it errs toward the
	// older snapshot of any pair — the one whose contents nothing still on disk
	// can approximate.
	//
	// The cost is that a snapshot taken minutes after another in the same bucket
	// is prunable at once. That is why every preset opens with a keep-everything
	// tier, and why the newest snapshot is kept unconditionally.
	for i := len(ordered) - 1; i >= 0; i-- {
		s := ordered[i]
		age := now.Sub(s.Taken)
		if age < 0 {
			// A snapshot dated in the future — a clock corrected backwards, a
			// machine moved between zones — is not old. Left as a negative age
			// it would still land in the first tier, but saying so here stops a
			// later change to tier lookup reading it as infinitely old.
			age = 0
		}
		tier, ok := tierFor(tiers, age)
		if !ok {
			continue // older than the policy reaches
		}
		if tier.Every <= 0 {
			kept[i] = true
			continue
		}
		b := bucket{every: tier.Every, start: bucketStart(s.Taken, tier.Every)}
		if claimed[b] {
			continue
		}
		claimed[b] = true
		kept[i] = true
	}

	for i, s := range ordered {
		if kept[i] {
			keep = append(keep, s)
		} else {
			prune = append(prune, s)
		}
	}
	return keep, prune
}

// bucketStart identifies the period a snapshot falls in, as an absolute instant.
//
// Time.Truncate rounds down to a multiple of the period measured from a fixed
// instant, so a boundary never moves. Both alternatives move:
//
//   - Bucketing by age relative to now (age/period) shifts every boundary as the
//     clock advances, so which snapshots share a bucket differs between one run
//     and the next and the kept set churns for no reason.
//   - Bucketing on local midnight moves the boundary twice a year. A daylight
//     saving change makes one day 23 or 25 hours long, which re-chooses that
//     day's keeper and so deletes the snapshot the previous plan committed to.
//
// The price is that a "daily" bucket is a fixed 24-hour window rather than a
// local calendar day, so the snapshot kept for a day is the first one after a UTC
// boundary rather than after local midnight. That is cosmetic — which of a day's
// snapshots is chosen — against the real question of whether the choice holds
// still.
func bucketStart(taken time.Time, every time.Duration) int64 {
	return taken.Truncate(every).UnixNano()
}

// tierFor finds the tier governing a snapshot of a given age.
//
// Tiers are ordered by reach and the first one that reaches this age wins, so
// the finest granularity covering a snapshot is the one applied. An age landing
// exactly on a boundary belongs to the finer tier, because the finer tier keeps
// more, and every tie in this file is broken toward keeping.
func tierFor(tiers []Tier, age time.Duration) (Tier, bool) {
	for _, t := range tiers {
		if age <= t.For {
			return t, true
		}
	}
	return Tier{}, false
}

// normalised drops the tiers that cover nothing and orders the rest by reach.
//
// The order is not the caller's to get right. A policy stored back to front
// would otherwise apply the coarsest thinning to the newest snapshots and delete
// this morning's work.
func (p Policy) normalised() []Tier {
	out := make([]Tier, 0, len(p.Tiers))
	for _, t := range p.Tiers {
		if t.For <= 0 {
			continue
		}
		if t.Every < 0 {
			t.Every = 0
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].For < out[j].For })
	return out
}

// Bands returns the tiers in the order Plan applies them — finest first, with
// any that cover nothing dropped. That is also the order they read in, which is
// why the interface is given this rather than the raw field.
func (p Policy) Bands() []Tier { return p.normalised() }

// Horizon is the age of the oldest snapshot a policy still keeps: how far back
// the history reaches. Zero means the policy prunes nothing.
func (p Policy) Horizon() time.Duration {
	var furthest time.Duration
	for _, t := range p.normalised() {
		if t.For > furthest {
			furthest = t.For
		}
	}
	return furthest
}

// IsFlat reports whether a policy is the old flat window: one band, keeping
// everything inside it.
func (p Policy) IsFlat() bool {
	tiers := p.normalised()
	return len(tiers) == 1 && tiers[0].Every <= 0
}

// Equal compares policies by what they would do rather than by how they were
// written, so a preset recovered from a plist still matches the preset.
func (p Policy) Equal(other Policy) bool {
	a, b := p.normalised(), other.normalised()
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// String encodes a policy the way the plist carries it: comma-separated
// every/for pairs in whole hours, with "all" for a band that keeps everything.
//
// Whole hours because the retention value it sits beside in that plist is
// already in hours, and because a launchd plist is read by people as often as by
// programs. Both halves round up, so any loss in the encoding keeps more than was
// asked for rather than less.
func (p Policy) String() string {
	tiers := p.normalised()
	fields := make([]string, 0, len(tiers))
	for _, t := range tiers {
		every := "all"
		if t.Every > 0 {
			every = strconv.Itoa(hoursUp(t.Every))
		}
		fields = append(fields, every+"/"+strconv.Itoa(hoursUp(t.For)))
	}
	return strings.Join(fields, ",")
}

// Describe words a policy as a sentence. Tiers are far easier to check in prose
// than as a row of numbers, and this is the string the settings screen shows
// beside the count it would retain.
func (p Policy) Describe() string {
	tiers := p.normalised()
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

// hoursUp rounds a duration up to whole hours, which is the resolution the plist
// carries. Rounding up on the reach of a band keeps a snapshot slightly longer
// than asked; rounding down would delete one slightly early.
func hoursUp(d time.Duration) int {
	hours := int(d / time.Hour)
	if d%time.Hour != 0 {
		hours++
	}
	return hours
}

// words spells a duration in the largest unit that stays exact, so a policy
// reads as "13 weeks" rather than "2184 hours".
//
// Weeks only past a month: a fortnight is a fortnight, and "2 weeks" for the
// retention everyone here already thinks of as 14 days would be a needless
// translation.
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

// ParsePolicy reads the encoding String writes. It also accepts "0" for a
// keep-everything band, because that is the obvious thing to write by hand.
//
// It reports false rather than a partial policy. Half a policy is a different
// policy, and the half most likely to be dropped is the last one — the band
// holding the oldest snapshots, whose loss is the one that cannot be undone.
func ParsePolicy(s string) (Policy, bool) {
	var p Policy
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		every, forAge, ok := parseTier(field)
		if !ok {
			return Policy{}, false
		}
		p.Tiers = append(p.Tiers, Tier{Every: every, For: forAge})
	}
	if len(p.Tiers) == 0 {
		return Policy{}, false
	}
	return p, true
}

// parseTier reads one every/for pair, both in whole hours.
func parseTier(field string) (every, forAge time.Duration, ok bool) {
	left, right, found := strings.Cut(field, "/")
	if !found {
		return 0, 0, false
	}
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)

	if left != "all" {
		hours, err := strconv.Atoi(left)
		if err != nil || hours < 0 {
			return 0, 0, false
		}
		every = time.Duration(hours) * time.Hour
	}
	hours, err := strconv.Atoi(right)
	if err != nil || hours <= 0 {
		return 0, 0, false
	}
	return every, time.Duration(hours) * time.Hour, true
}

// Retained reports how many snapshots a policy holds once a schedule taking one
// every interval has been running longer than the policy reaches.
//
// It counts by planning a synthetic history rather than by arithmetic over the
// tiers, so the figure the settings screen shows comes from the same function
// that does the deleting and cannot drift from it. It is an estimate only in
// that a real history is not taken exactly on the interval.
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
