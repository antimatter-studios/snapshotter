package services

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"snapshotter/internal/config"
	"snapshotter/internal/watch"
)

// ConfigService gives the window the settings file.
//
// It exists so that preferences survive the installation they were set in. The
// theme used to live in the webview's localStorage, which is per-installation by
// construction: set dark in a development build, install the release, and it was
// light again with nothing to explain why.
type ConfigService struct{}

func NewConfigService() *ConfigService { return &ConfigService{} }

// ConfigView is the settings plus where they are stored, because a settings screen
// that will not tell you which file it is editing is a settings screen you cannot
// fix by hand.
type ConfigView struct {
	Path   string        `json:"path"`
	Config config.Config `json:"config"`
	// Error is a configuration file that exists and could not be read. The
	// defaults are returned alongside it rather than nothing at all, so the window
	// still works while saying what is wrong.
	Error string `json:"error"`
}

// Get reads the configuration.
func (c *ConfigService) Get() ConfigView {
	view := ConfigView{}
	if path, err := config.Path(); err == nil {
		view.Path = path
	}
	cfg, err := config.Load()
	view.Config = cfg
	if err != nil {
		view.Error = err.Error()
	}
	return view
}

// SetTheme stores the appearance choice and nothing else.
//
// Narrow on purpose. A general "write this whole config" from the window would
// let a stale copy of the settings — one read before the file changed on disk —
// overwrite everything else in it. Each setting the interface can change gets its
// own call, so concurrent edits can only collide on the same field.
func (c *ConfigService) SetTheme(theme string) error {
	switch theme {
	case "system", "light", "dark":
	default:
		return fmt.Errorf("services: %q is not a theme", theme)
	}
	cfg, err := config.Load()
	if err != nil {
		// Saving on top of a file that could not be parsed would destroy whatever
		// the person had written there, and the only thing being changed is a colour
		// scheme. Refusing is the smaller loss.
		return fmt.Errorf("not changing the theme: %w", err)
	}
	cfg.Appearance.Theme = theme
	return config.Save(cfg)
}

// SetLanguage records which language both surfaces should speak.
//
// Written to the settings file rather than only to the window, because the menu
// bar is drawn in Go and reads the same file: the settings watcher redraws it,
// so a language chosen in the window reaches the menu bar without a relaunch.
func (c *ConfigService) SetLanguage(code string) error {
	if !slices.Contains(config.Languages, code) {
		return fmt.Errorf("services: %q is not a language this build carries", code)
	}
	cfg, err := config.Load()
	if err != nil {
		// The same reasoning as the theme: saving over a file that could not be
		// parsed would destroy whatever is in it, to change which words are shown.
		return fmt.Errorf("not changing the language: %w", err)
	}
	cfg.Appearance.Language = code
	return config.Save(cfg)
}

// SetTripwireSensitivity records how readily a burst of deletions counts as one
// worth snapshotting.
//
// It takes effect on the watcher's next run rather than immediately: the tripwire
// is a separate process that launchd restarts, and it reads this at startup. That
// is worth saying in the interface, because a setting that appears to apply and
// does not is worse than one that says when it will.
func (c *ConfigService) SetTripwireSensitivity(name string) error {
	if !watch.Known(watch.Sensitivity(name)) {
		return fmt.Errorf("services: %q is not a sensitivity", name)
	}
	cfg, err := config.Load()
	if err != nil {
		// The same reasoning as the theme and the language: saving over a file that
		// could not be parsed would destroy whatever is in it, to change one setting.
		return fmt.Errorf("not changing the sensitivity: %w", err)
	}
	cfg.Tripwire.Sensitivity = name
	return config.Save(cfg)
}

// TripwireSensitivity is one setting on offer, with the count it stands for.
//
// The count is carried rather than worded here: "75 files" means something next to
// a name, and the window can put the two together in its own language.
type TripwireSensitivity struct {
	ID string `json:"id"`
	// Deletions is how many inside the window count as a burst.
	Deletions int `json:"deletions"`
	// WindowSeconds is the same for every setting, and is here so the window can
	// say "within five seconds" without knowing the number itself.
	WindowSeconds int `json:"windowSeconds"`
}

// TripwireSensitivities are the settings on offer, coarsest first, and which one
// is in force.
func (c *ConfigService) TripwireSensitivities() ([]TripwireSensitivity, string) {
	out := make([]TripwireSensitivity, 0, len(watch.Sensitivities))
	for _, s := range watch.Sensitivities {
		out = append(out, TripwireSensitivity{
			ID:            string(s),
			Deletions:     watch.ThresholdFor(s),
			WindowSeconds: int(watch.DefaultWindow / time.Second),
		})
	}

	// What is in force, resolved the same way the watcher resolves it, so the
	// dropdown cannot show a different answer from the one being used.
	current := string(watch.Balanced)
	if cfg, err := config.Load(); err == nil && watch.Known(watch.Sensitivity(cfg.Tripwire.Sensitivity)) {
		current = cfg.Tripwire.Sensitivity
	}
	return out, current
}

// IgnoreFolder stops the bulk-deletion tripwire counting deletions in a folder.
//
// The reason for wanting this arrives while looking at a warning that should not
// have happened — a browser clearing its cache, a build directory being emptied —
// so the interface offers it there rather than in a settings screen someone would
// have to go and find.
//
// The folder is stored with separators around it, which is what makes it a
// fragment rather than a prefix: "/Caches/" matches that folder wherever it
// appears, and a folder given here matches itself and anything under it without
// matching a sibling whose name merely starts the same way.
func (c *ConfigService) IgnoreFolder(folder string) (ConfigView, error) {
	fragment := asFragment(folder)
	if fragment == "" {
		return c.Get(), fmt.Errorf("services: %q is not a folder", folder)
	}

	cfg, err := config.Load()
	if err != nil {
		// Same reasoning as the theme: writing over a file that will not parse
		// destroys whatever someone wrote there, to add one line.
		return c.Get(), fmt.Errorf("not changing what is ignored: %w", err)
	}
	for _, existing := range cfg.Tripwire.Ignore {
		if existing == fragment {
			return c.Get(), nil // already silenced; not an error
		}
	}
	cfg.Tripwire.Ignore = append(cfg.Tripwire.Ignore, fragment)
	if err := config.Save(cfg); err != nil {
		return c.Get(), err
	}
	return c.Get(), nil
}

// StopIgnoringFolder undoes IgnoreFolder.
//
// Removing is as important as adding: an ignore list nobody can see or shorten is
// a list that quietly grows until the tripwire watches nothing, and the failure
// is silent by construction.
//
// Named for what it does to the ignore list rather than "WatchFolder", which it
// used to be called. There is now a list of directories the tripwire watches, and
// a method called WatchFolder that does not add to it is a trap.
func (c *ConfigService) StopIgnoringFolder(fragment string) (ConfigView, error) {
	cfg, err := config.Load()
	if err != nil {
		return c.Get(), fmt.Errorf("not changing what is ignored: %w", err)
	}

	kept := make([]string, 0, len(cfg.Tripwire.Ignore))
	for _, existing := range cfg.Tripwire.Ignore {
		if existing != fragment {
			kept = append(kept, existing)
		}
	}
	cfg.Tripwire.Ignore = kept
	if err := config.Save(cfg); err != nil {
		return c.Get(), err
	}
	return c.Get(), nil
}

// WatchDirectory adds a directory to the list the bulk-deletion tripwire watches.
//
// This is the list that decides what is watched at all. Before it there was one
// answer — the whole home directory — and the only control was an ignore list
// chasing whatever had most recently made a noise. Naming what to protect is both
// smaller and answerable: someone knows which of their directories holds work
// they could not reproduce.
//
// Stored as given, "~" and all, so that the settings file says back what was
// typed and keeps meaning it on a machine where the home directory moves. It is
// resolved when it is read.
func (c *ConfigService) WatchDirectory(dir string) (ConfigView, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return c.Get(), fmt.Errorf("services: no directory given")
	}
	// Checked against what it resolves to, stored as it was written.
	resolved := config.ResolvePath(dir, "")
	if !filepath.IsAbs(resolved) {
		return c.Get(), fmt.Errorf("services: %q is not a full path — start it with / or ~/", dir)
	}
	resolved = filepath.Clean(resolved)
	if resolved == string(filepath.Separator) {
		// Watching the whole disk is what this setting exists to stop, and a button
		// should not be able to reinstate it in one click.
		return c.Get(), fmt.Errorf("services: watching the whole disk is what naming directories is for")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		// Refused rather than accepted hopefully. A watched directory that is not
		// there is not watched, and the tripwire cannot say so afterwards without
		// someone reading its log — which nobody does until something is lost.
		return c.Get(), fmt.Errorf("services: cannot watch %s: %w", dir, err)
	}
	if !info.IsDir() {
		return c.Get(), fmt.Errorf("services: %s is a file, and the tripwire watches directories", dir)
	}

	cfg, err := config.Load()
	if err != nil {
		// Same reasoning as the theme: writing over a file that will not parse
		// destroys whatever someone wrote there, to add one line.
		return c.Get(), fmt.Errorf("not changing what is watched: %w", err)
	}
	for _, existing := range cfg.Tripwire.Watch {
		if existing == dir || filepath.Clean(config.ResolvePath(existing, "")) == resolved {
			return c.Get(), nil // already watched; not an error
		}
	}
	cfg.Tripwire.Watch = append(cfg.Tripwire.Watch, dir)
	if err := config.Save(cfg); err != nil {
		return c.Get(), err
	}
	return c.Get(), nil
}

// UnwatchDirectory takes a directory off the list the tripwire watches.
//
// Matched on what it resolves to as well as on how it was written, so a row shown
// as "~/projects" is removed by the button beside it whichever of the two forms
// the file happens to hold.
func (c *ConfigService) UnwatchDirectory(dir string) (ConfigView, error) {
	cfg, err := config.Load()
	if err != nil {
		return c.Get(), fmt.Errorf("not changing what is watched: %w", err)
	}

	target := filepath.Clean(config.ResolvePath(strings.TrimSpace(dir), ""))
	kept := make([]string, 0, len(cfg.Tripwire.Watch))
	for _, existing := range cfg.Tripwire.Watch {
		if existing == dir || filepath.Clean(config.ResolvePath(existing, "")) == target {
			continue
		}
		kept = append(kept, existing)
	}
	cfg.Tripwire.Watch = kept
	if err := config.Save(cfg); err != nil {
		return c.Get(), err
	}
	return c.Get(), nil
}

// WatchedDirectories is the list as a screen should show it: what was configured,
// and what it resolves to.
//
// Both, because they differ and the difference is the whole of some bug reports.
// "~/projects" is what someone typed and recognises; "/Users/them/projects" is
// what is actually being watched, and seeing it is how they find out that the
// directory they meant is somewhere else.
type WatchedDirectory struct {
	Configured string `json:"configured"`
	Resolved   string `json:"resolved"`
	// Missing says the directory is not there. A watched directory that does not
	// exist is not watched, and nothing else on the screen would say so.
	Missing bool `json:"missing"`
}

// WatchedDirectories lists what the tripwire is set to watch.
func (c *ConfigService) WatchedDirectories() []WatchedDirectory {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	out := make([]WatchedDirectory, 0, len(cfg.Tripwire.Watch))
	for _, dir := range cfg.Tripwire.Watch {
		resolved := filepath.Clean(config.ResolvePath(strings.TrimSpace(dir), ""))
		info, statErr := os.Stat(resolved)
		out = append(out, WatchedDirectory{
			Configured: dir,
			Resolved:   resolved,
			Missing:    statErr != nil || !info.IsDir(),
		})
	}
	return out
}

// asFragment turns a folder into the form the watcher matches against.
func asFragment(folder string) string {
	folder = strings.TrimSpace(folder)
	if folder == "" || folder == "/" {
		// "/" would match every path and silence the tripwire entirely, which is
		// not something a button should be able to do by accident.
		return ""
	}
	if !strings.HasPrefix(folder, "/") {
		folder = "/" + folder
	}
	if !strings.HasSuffix(folder, "/") {
		folder += "/"
	}
	return folder
}
