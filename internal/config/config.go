// Package config reads and writes the settings that belong to the person rather
// than to an installation.
//
// One file, at ~/.config/snapshotter/config.yaml, so every copy agrees: a build
// in bin/, a copy in /Applications, and whatever Homebrew installs are all the
// same application to the person using them, and a preference set in one should
// not be invisible to the next. Preferences used to live in the webview's
// localStorage, which is per-installation by construction.
//
// This is intent, not truth. Whether a schedule is actually running is a question
// for launchd, and the interface asks launchd — this file records what was asked
// for, which is what a settings screen should show when nothing is installed yet.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"snapshotter/internal/watch"
)

// Config is the whole of it. Kept flat and small on purpose: every field here has
// to be something a person would recognise as a choice they made.
type Config struct {
	Schedule   Schedule   `yaml:"schedule" json:"schedule"`
	Tripwire   Tripwire   `yaml:"tripwire" json:"tripwire"`
	Appearance Appearance `yaml:"appearance" json:"appearance"`
	Logging    Logging    `yaml:"logging" json:"logging"`
	Window     Window     `yaml:"window" json:"window"`
	Refresh    Refresh    `yaml:"refresh" json:"refresh"`
	Paths      Paths      `yaml:"paths" json:"paths"`
	// ChangeDetection is what comparing a snapshot with the live disk looks at.
	ChangeDetection ChangeDetection `yaml:"change_detection" json:"changeDetection"`
}

// ChangeDetection tunes what comparing a snapshot with the live disk reads.
type ChangeDetection struct {
	// Ignore lists paths not to look inside when deciding whether a folder has
	// changed.
	//
	// It is the only setting that helps the expensive direction. A folder that
	// differs is answered at the first difference; one that does not has to be
	// read in full to prove it — and most of what that reads is not anybody's
	// work. On the machine this was written for, 17,239 of a project's 19,788
	// entries were node modules: nine seconds of an SD card's reading, per
	// project, to confirm something nobody would restore.
	//
	// A bare name matches a path component at any depth, so "node_modules" means
	// all of them. Wildcards are filepath.Match, so "*.tmp" and "build-*" work. A
	// pattern containing a separator is matched against the whole path, so
	// "*/projects/*/dist" picks out one place rather than every dist on the disk.
	//
	// Deliberately not the bulk-deletion watcher's ignore list, which answers a
	// different question: that one is "deletions here do not count as a burst",
	// this one is "do not read this when comparing". They would usually hold the
	// same paths, which is why they are kept apart — sharing them would mean
	// changing what you are warned about in order to change what gets walked.
	//
	// Empty by default. A folder skipped is a folder this application will not
	// tell you about, and only the person using the machine knows which of those
	// they could not reproduce.
	Ignore []string `yaml:"ignore" json:"ignore"`
}

// Schedule is what to ask launchd for, not what launchd is currently doing.
type Schedule struct {
	// Enabled says a schedule was ASKED for, which the numbers below cannot: a
	// fresh settings file carries defaults that are indistinguishable from a
	// deliberate choice of the same values. Without it, restoring "what was
	// configured" on launch would install a schedule for someone who never
	// wanted one.
	Enabled       bool    `yaml:"enabled" json:"enabled"`
	IntervalHours float64 `yaml:"interval_hours" json:"interval_hours"`
	RetentionDays float64 `yaml:"retention_days" json:"retention_days"`
	// Policy names a retention preset — "flat" keeps everything inside the window,
	// the tiered ones thin with age. Stored by id rather than by its expansion so
	// that improving a preset improves existing configurations.
	Policy string `yaml:"policy" json:"policy"`
}

type Tripwire struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Watch lists the directories the tripwire watches, and is the whole of what
	// it watches. Nothing outside them counts, and an empty list means the
	// tripwire watches nothing at all.
	//
	// It used to watch the entire home directory, with an ignore list to quiet the
	// parts that are not anybody's work. That is the wrong way round. ~/Library is
	// machine-managed churn by the thousand — caches, container state, mail
	// indexes, every application's idea of scratch space — so the wire tripped on
	// deletions nobody had asked about and filled the disk with snapshots of them,
	// and the ignore list needed to stop it is a list of everything the machine
	// does, written after each surprise, never finished.
	//
	// Naming what to watch instead is both smaller and honest: ~/projects is one
	// line and it is the thing worth protecting. What is not listed is not
	// watched, which is a rule someone can hold in their head.
	//
	// A leading ~ is expanded, so "~/projects" behaves as written.
	Watch []string `yaml:"watch" json:"watch"`
	// Ignore lists path fragments whose deletions do not count towards a burst.
	//
	// Without it the wire is tripped by ordinary machine noise: a browser
	// clearing its cache deletes hundreds of files in seconds, which is exactly
	// the shape of the thing being watched for and none of its meaning. A warning
	// that fires on cache churn is a warning someone learns to dismiss, and then
	// dismisses the one that mattered.
	//
	// Matched as substrings of the full path, after ~ is expanded, so a fragment
	// like "/Library/Caches/" covers every application's cache without naming any
	// of them.
	//
	// Narrower in scope than it was: it now only has to quiet noise INSIDE a
	// watched directory, because everything outside one is already not watched.
	Ignore []string `yaml:"ignore" json:"ignore"`
	// Sensitivity is how readily a burst counts as one worth snapshotting:
	// "cautious", "balanced", "sensitive" or "very-sensitive". Empty means
	// balanced, which is what every build before this setting existed used.
	//
	// A name rather than a count, because the count alone is unanswerable — whether
	// two hundred files in five seconds is a lot depends entirely on what the
	// machine does all day, which is the thing being configured.
	Sensitivity string `yaml:"sensitivity" json:"sensitivity"`
}

// Logging is what the application says about itself.
type Logging struct {
	// Verbose turns on per-directory and per-file logging.
	//
	// Off by default because it is tens of thousands of lines on a home folder.
	// On, it says why a folder could not be compared — which is the question that
	// prompted it, after three wrong guesses at an answer the application already
	// had and was discarding.
	Verbose bool `yaml:"verbose" json:"verbose"`
}

type Appearance struct {
	// Theme is "system", "light" or "dark". "system" is a real value rather than
	// the absence of one, because following the system is a choice a person can
	// return to.
	Theme string `yaml:"theme" json:"theme"`
	// Language is a two-letter code: "en", "de", "es" or "fr".
	//
	// It sits here rather than in its own section because it is the same kind of
	// setting as the theme — how the application presents itself, rather than what
	// it does. Both surfaces read it: the window translates its own text, and the
	// menu bar is redrawn through the settings watcher, so switching language
	// changes both without a relaunch.
	//
	// An unrecognised or empty value falls back to English rather than failing.
	// A settings file written by a later version, or edited by hand, should not
	// leave someone with an application that will not start.
	Language string `yaml:"language" json:"language"`
}

// Languages are the codes the application has translations for, in the order a
// picker should offer them.
var Languages = []string{"en", "de", "es", "fr"}

// Language returns the configured language, or English if it is unset or is not
// one this build carries.
func (c Config) Language() string {
	for _, code := range Languages {
		if c.Appearance.Language == code {
			return code
		}
	}
	return "en"
}

// Window is the size the window opens at.
type Window struct {
	Width  int `yaml:"width" json:"width"`
	Height int `yaml:"height" json:"height"`
}

// Refresh is how often each surface re-reads state it did not change itself.
//
// Both exist because snapshots are taken by things that are not this window: the
// menu bar, a launchd agent, and the bulk-deletion tripwire. Neither number is
// about being live — the underlying state moves on the schedule's interval, hours
// rather than seconds — so they are about noticing, and a slow machine or a large
// snapshot count is a reason someone might want them longer.
type Refresh struct {
	MenuBarSeconds int `yaml:"menu_bar_seconds" json:"menu_bar_seconds"`
	WindowSeconds  int `yaml:"window_seconds" json:"window_seconds"`
}

// Paths is where this application keeps things.
//
// Empty means the default location, which is what almost everyone wants; the
// fields exist for the machine where /Users is not where the data lives. They are
// resolved rather than used raw, so "~/somewhere" behaves as written.
type Paths struct {
	MountRoot   string `yaml:"mount_root" json:"mount_root"`
	Log         string `yaml:"log" json:"log"`
	TripwireLog string `yaml:"tripwire_log" json:"tripwire_log"`
}

// Defaults are what a machine with no configuration file should behave like, and
// they match the defaults the schedule screen offers: six hours of interval and a
// fortnight of depth, aimed at days of protection against a deletion rather than
// intra-day granularity.
func Defaults() Config {
	return Config{
		Schedule: Schedule{IntervalHours: 6, RetentionDays: 14, Policy: "flat"},
		// Off, with nothing to watch, until someone says what to watch.
		//
		// It was on by default and watching the whole home directory, which cost
		// far more than it was worth: most of the bursts it caught were ~/Library
		// doing its housekeeping, and each one pinned another whole-volume snapshot
		// on the disk. A protection that is mostly false positives is not a
		// protection, it is a disk-space leak with notifications.
		//
		// So the default is now no directories and not installed, and the Health
		// screen asks for the directories before it offers to install anything.
		// An existing settings file keeps whatever it already says.
		Tripwire: Tripwire{
			Enabled: false,
			// The setting every build before this one had, named.
			Sensitivity: string(watch.Balanced),
			// Nothing until someone names something. There is no sensible guess:
			// the whole point of the setting is that only the person using the
			// machine knows which of their directories holds work they could not
			// reproduce.
			//
			// Empty rather than nil, so that the defaults survive a round trip
			// through the file: YAML writes both as "watch: []" and reads it back
			// as an empty slice, and a nil here would make Defaults() and a
			// freshly written settings file compare unequal.
			Watch: []string{},
			// Machine-managed churn, not anybody's documents. Deliberately short:
			// every entry here is a place this application will stay quiet about,
			// so it holds only things no one would ask to recover.
			Ignore: []string{
				"/Library/Caches/",
				"/Caches/",
				"/private/var/folders/",
				"/.Trash/",
			},
		},
		Appearance: Appearance{Theme: "system", Language: "en"},
		Logging:    Logging{Verbose: false},
		Window:     Window{Width: 1180, Height: 780},
		Refresh:    Refresh{MenuBarSeconds: 60, WindowSeconds: 30},
		Paths:      Paths{}, // empty: see Paths, and Resolve below
		// Empty rather than a helpful default. Skipping .git by default would hide
		// a real loss from somebody who wanted it, and the window offers the usual
		// suspects as one-click suggestions instead.
		ChangeDetection: ChangeDetection{Ignore: []string{}},
	}
}

// Dir is the directory holding the configuration file.
//
// XDG_CONFIG_HOME is honoured where it is set, both because it is the convention
// this path imitates and because it makes the whole package testable without
// writing to a real home directory.
func Dir() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "snapshotter"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot find the home directory: %w", err)
	}
	return filepath.Join(home, ".config", "snapshotter"), nil
}

// Path is the configuration file itself.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads the configuration, falling back to the defaults for anything absent.
//
// A missing file is not an error: the first run of a fresh installation has none,
// and refusing to start over it would be absurd. A file that exists and cannot be
// parsed IS an error, but the defaults are returned alongside it — the caller can
// say so without the application becoming unusable, and nothing overwrites the
// broken file until someone deliberately saves.
func Load() (Config, error) {
	cfg := Defaults()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config: reading %s: %w", path, err)
	}
	// Unmarshalling into the defaults leaves absent keys at their default rather
	// than at Go's zero value, so a file written by an older version that did not
	// know about a setting does not silently set it to zero.
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("config: %s is not valid YAML: %w", path, err)
	}
	return cfg, nil
}

// Save writes the configuration, creating the directory if needed.
//
// Written to a temporary file and renamed, because rename is atomic within a
// directory: a crash or a full disk half way through leaves the previous
// configuration intact rather than a truncated file that Load would then reject.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	// 0700 rather than 0755: this is one person's settings in their own home.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: creating %s: %w", dir, err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: encoding: %w", err)
	}
	data = append([]byte(header), data...)

	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("config: creating a temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("config: writing %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("config: setting permissions on %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: replacing %s: %w", path, err)
	}
	return nil
}

// header is written above the settings so that someone opening the file knows
// what it is and that editing it is allowed.
const header = `# Snapshotter settings. Edit freely: a running Snapshotter notices the change
# and applies it, and it writes this file back when a setting changes in the window.
#
# Two things do not change under a running application, because they are not
# Snapshotter's to change: a snapshot that is already mounted stays where it is,
# and an installed scheduled task keeps writing to the log named in the copy
# launchd holds until that task is installed again.
#
# This records what was ASKED FOR. Whether the schedule is actually installed and
# running is a question for launchd, which the Health screen answers.
`

// ResolvePath expands a configured path into an absolute one, falling back to a
// default when nothing is configured.
//
// A leading ~ is expanded because this file is meant to be edited by hand, and
// nothing expands it there — a literal "~/x" directory appearing in the working
// directory is a confusing way to learn that.
func ResolvePath(configured, fallback string) string {
	if configured == "" {
		return fallback
	}
	if configured == "~" || strings.HasPrefix(configured, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(configured, "~"), "/"))
		}
	}
	return configured
}

// WatchRoots is the directories the tripwire should watch, expanded and cleaned.
//
// Empty means watch nothing, which is a real answer rather than a missing one:
// the tripwire watches what it is told to and there is no fallback. It used to
// fall back to the whole home directory, and a fallback that broad turns any
// failure to read this list — a typo, an unreadable file — into watching
// everything, which is the behaviour this setting exists to end.
//
// Duplicates are dropped and "~" is expanded, because this file is meant to be
// edited by hand and neither of those should need thinking about.
func (t Tripwire) WatchRoots() []string {
	seen := make(map[string]bool, len(t.Watch))
	out := make([]string, 0, len(t.Watch))
	for _, dir := range t.Watch {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dir = filepath.Clean(ResolvePath(dir, ""))
		// Absolute or nothing. A relative path means nothing to a launchd agent,
		// which starts wherever launchd starts it, and quietly watching the wrong
		// directory is worse than watching none.
		if !filepath.IsAbs(dir) {
			continue
		}
		// "/" would watch the entire disk, which is worse than what this replaced:
		// no ignore list survives every volume, cache and package manager at once.
		if dir == string(filepath.Separator) {
			continue
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// MenuBarRefresh and WindowRefresh are the intervals as durations, with anything
// absent or nonsensical falling back to the default. A zero or negative interval
// would mean a timer that never fires or fires continuously, and neither is a
// thing anyone is asking for by typing 0.
func (c Config) MenuBarRefresh() time.Duration {
	return positiveSeconds(c.Refresh.MenuBarSeconds, Defaults().Refresh.MenuBarSeconds)
}

func (c Config) WindowRefresh() time.Duration {
	return positiveSeconds(c.Refresh.WindowSeconds, Defaults().Refresh.WindowSeconds)
}

func positiveSeconds(v, fallback int) time.Duration {
	if v <= 0 {
		v = fallback
	}
	return time.Duration(v) * time.Second
}

// WindowSize is the configured size, refusing anything too small to use. A window
// 20 pixels wide is not a preference, it is a typo, and it would be very hard to
// correct from inside the application.
func (c Config) WindowSize() (width, height int) {
	const minimum = 480
	width, height = c.Window.Width, c.Window.Height
	if width < minimum {
		width = Defaults().Window.Width
	}
	if height < minimum {
		height = Defaults().Window.Height
	}
	return width, height
}
