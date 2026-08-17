package menubar

import "fmt"

// TrayIcon is the glyph for the menu bar itself, one per health level.
//
// Each is the same ring drawn to a different extent — closed for ok, two thirds
// for warn, a bare crescent for bad — so the level reads in greyscale and to
// someone who cannot tell the green from the amber. Colour is the fast signal
// here, not the only one.
//
// These are deliberately NOT template images, which is the one thing that cannot
// be changed casually: a template is black plus alpha and macOS inverts it to suit
// the menu bar, discarding the colour entirely. Worse, Wails latches the template
// flag on the tray the first time it is set and never clears it, so a single
// SetTemplateIcon call anywhere would render every one of these as a black
// silhouette. The palette is mid-toned to hold up against a light and a dark menu
// bar without that help.
//
// The @2x files are the ones held here, not the 22px ones beside them in assets:
// Wails resizes whatever it is given to the status bar's thickness in points, so
// the pixels only decide how sharp it looks. 44px into a 22pt slot is exactly
// Retina.
//
// They live in this package rather than beside main because this is the package
// that draws menu bar imagery, and because go:embed cannot reach across
// directories — which was the only reason the entry point had to sit in the
// repository root.
func TrayIcon(level Level) []byte {
	name := map[Level]string{
		LevelOK:   "tray-ok-2x.png",
		LevelWarn: "tray-warn-2x.png",
		LevelBad:  "tray-error-2x.png",
	}[level]
	if name == "" {
		// An unrecognised level is treated as bad rather than ok, so a level added
		// to services and forgotten here shows up as something to look at instead
		// of silently reading as healthy.
		name = "tray-error-2x.png"
	}
	data, err := icons.ReadFile("icons/" + name)
	if err != nil {
		// Embedded at build time: a failure here is a broken binary, not a missing
		// file on someone's disk.
		panic(fmt.Sprintf("menubar: %s is not embedded: %v", name, err))
	}
	return data
}
