package menubar

import (
	"embed"
	"fmt"
)

// The icons beside a finding in the menu bar.
//
// A finding has a level and a subject, and the level is the less useful of the
// two here: a menu listing three warnings drawn with three identical warning
// triangles has told the reader nothing except that there are three. The icon
// says what the finding is about; its colour says how bad it is.
//
// These are Lucide (lucide.dev, ISC), rendered to PNG by
// build/icons/findings.sh, because macOS wants image bytes for a menu item
// rather than SVG. The window draws the same icons as lucide-react components
// from the same pack, so the two surfaces cannot disagree about what a clock
// means.
//
// An earlier version of this file drew the shapes by hand out of circles, arcs
// and an antialiaser. It was more code, more tests and worse icons — one of them
// read as a sunset — for something two thousand designed icons already covered.

// Level is how severe a finding is, kept as a string so this package does not
// import the services it draws for.
type Level string

const (
	LevelOK   Level = "ok"
	LevelWarn Level = "warn"
	LevelBad  Level = "bad"
)

// The subjects a finding can have, matching the kinds the status service
// assigns.
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

// Kinds lists every subject that has an icon, so the window's set can be checked
// against this one without either side restating the other.
func Kinds() []string {
	return []string{
		KindSnapshots, KindSchedule, KindOverdue, KindTripwire,
		KindThinning, KindConflict, KindSpace, KindSimulated, KindStale,
	}
}

//go:embed all:icons
var icons embed.FS

// Glyph returns the icon for one finding.
//
// A kind with no icon of its own falls back rather than returning nothing:
// findings are added in the service, and a menu row with a blank where its icon
// should be reads as a rendering fault rather than as a new kind of finding.
func Glyph(kind string, level Level) ([]byte, error) {
	switch level {
	case LevelOK, LevelWarn, LevelBad:
	default:
		// An unrecognised level is not health. Same reasoning as the menu bar
		// icon: something unaccounted for should look like something to check.
		level = LevelBad
	}

	if data, err := icons.ReadFile(iconPath(kind, level)); err == nil {
		return data, nil
	}
	data, err := icons.ReadFile(iconPath(KindSnapshots, level))
	if err != nil {
		return nil, fmt.Errorf("menubar: no icon for %q at %s, and no fallback either: %w", kind, level, err)
	}
	return data, nil
}

// Status returns the icon for the headline row, which summarises the level
// rather than naming a subject of its own.
func Status(level Level) ([]byte, error) { return Glyph(KindSnapshots, level) }

func iconPath(kind string, level Level) string {
	return fmt.Sprintf("icons/%s-%s.png", kind, level)
}
