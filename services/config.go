package services

import (
	"fmt"

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
