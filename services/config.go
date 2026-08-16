package services

import (
	"fmt"
	"slices"
	"strings"

	"snapshotter/internal/config"
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

// WatchFolder undoes IgnoreFolder.
//
// Removing is as important as adding: an ignore list nobody can see or shorten is
// a list that quietly grows until the tripwire watches nothing, and the failure
// is silent by construction.
func (c *ConfigService) WatchFolder(fragment string) (ConfigView, error) {
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
