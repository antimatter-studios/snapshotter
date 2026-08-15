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
)

// Config is the whole of it. Kept flat and small on purpose: every field here has
// to be something a person would recognise as a choice they made.
type Config struct {
	Schedule   Schedule   `yaml:"schedule" json:"schedule"`
	Tripwire   Tripwire   `yaml:"tripwire" json:"tripwire"`
	Appearance Appearance `yaml:"appearance" json:"appearance"`
	Window     Window     `yaml:"window" json:"window"`
	Refresh    Refresh    `yaml:"refresh" json:"refresh"`
	Paths      Paths      `yaml:"paths" json:"paths"`
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
}

type Appearance struct {
	// Theme is "system", "light" or "dark". "system" is a real value rather than
	// the absence of one, because following the system is a choice a person can
	// return to.
	Theme string `yaml:"theme" json:"theme"`
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
		Schedule:   Schedule{IntervalHours: 6, RetentionDays: 14, Policy: "flat"},
		Tripwire:   Tripwire{Enabled: false},
		Appearance: Appearance{Theme: "system"},
		Window:     Window{Width: 1180, Height: 780},
		Refresh:    Refresh{MenuBarSeconds: 60, WindowSeconds: 30},
		Paths:      Paths{}, // empty: see Paths, and Resolve below
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
const header = `# Snapshotter settings. Edit freely; the application reads this file on the next
# refresh and writes it back when a setting changes in the window.
#
# This records what was asked for. Whether the schedule is actually installed and
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
