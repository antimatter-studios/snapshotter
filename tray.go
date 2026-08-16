// The menu bar.
//
// It is the surface that stays visible with the window closed, which makes it the
// one most able to mislead: a window showing yesterday's state is a window someone
// closed, but a menu bar showing it is the application's own account of this Mac.
package main

import (
	"context"
	"log"
	"os/exec"
	"snapshotter/internal/config"
	"snapshotter/internal/menubar"
	"snapshotter/internal/version"
	"snapshotter/services"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// menuIsDark reports whether menus are currently being drawn on a dark
// background, so the coverage strip is drawn in a shade that shows up on it.
//
// Asked of the system each time the menu is rebuilt rather than cached: someone
// switching appearance expects the next menu to match, and this runs once a
// minute rather than once a frame. The key is absent entirely in light mode,
// which is why any error means light.
var menuIsDark = func() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	return err == nil && strings.Contains(string(out), "Dark")
}

// coverageWindow is how far back the menu's strip reaches. Two days is far
// enough to show a nightly rhythm and close enough that each hour is still wide
// enough to see.
const coverageWindow = 48 * time.Hour

// trayRefreshDefault is how often the menu bar recomputes when nothing is
// configured. The underlying state moves on the schedule's interval — hours, not
// seconds — so this is about noticing a snapshot taken elsewhere rather than about
// being live. config.Refresh.menu_bar_seconds overrides it.
const trayRefreshDefault = time.Minute

// installTray puts the state in the menu bar, where it is visible without the
// window being open.
//
// The window is where you act; the menu bar is where you find out whether you
// need to. That is the whole division: this menu says what is true and offers
// the two actions that need no context.
//
// scenarioName is empty in the ordinary case; when it is not, the menu says so
// before it says anything else. The menu bar is the surface that stays visible
// with the window closed, so it is the one most able to mislead.
// installTray returns a function that applies changed settings to the running
// tray, so editing the interval takes effect without a relaunch.
func installTray(app *application.App, status *services.StatusService, win application.Window, scenarioName string) func(config.Config) {
	cfg, _ := config.Load()

	// Guarded because the watcher writes it from its own goroutine while the
	// refresh loop below reads it.
	var mu sync.Mutex
	trayRefresh := cfg.MenuBarRefresh()
	interval := func() time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return trayRefresh
	}

	tray := app.SystemTray.New()
	// No icon is set here: every render sets one to match the level it just read,
	// and the first render happens below before anything is on screen.

	// A menu bar has room for very little, and under a scenario "which machine is
	// this describing" outranks everything else the label could say.
	label, product := "", "Snapshotter"
	if scenarioName != "" {
		label, product = "SIM ", "Snapshotter — SCENARIO "+scenarioName
	} else if !version.IsRelease() {
		// A build that was not stamped by the release pipeline. Two copies of this
		// application put two identical icons in the menu bar, and the only way to
		// tell which one is being clicked is to have marked one of them — which
		// matters because the copy in /Applications holds the Full Disk Access
		// grant and a working build does not.
		label, product = "DEV ", "Snapshotter — development build"
	}

	// Declared before it is assigned because the "take a snapshot" item redraws
	// the menu it lives in.
	var render func()
	render = func() {
		health, err := status.Check(context.Background())
		menu := application.NewMenu()

		// Nothing informational is disabled any more. A disabled item is drawn in
		// the grey macOS uses for "you cannot have this", which is the wrong thing
		// to say about the state of your machine — and it is the hardest text in
		// the menu to read, which is backwards, because it is the text worth
		// reading. Each of these opens the window instead, so it is honestly
		// clickable rather than merely enabled.
		reveal := func(*application.Context) { win.Show(); win.Focus() }

		if scenarioName != "" {
			menu.Add("SCENARIO " + scenarioName + " — invented state").OnClick(reveal)
			menu.AddSeparator()
		}

		if err != nil {
			// Failing to read the state is not the same as reading a bad state, and
			// the icon cannot say which, so the label keeps a mark of its own.
			tray.SetIcon(trayIconBad)
			tray.SetLabel(label + "⚠︎")
			tray.SetTooltip(product + ": " + err.Error())
			menu.Add("Could not read snapshot state").SetBitmap(trayIconBad).OnClick(reveal)
			menu.Add(err.Error()).OnClick(reveal)
		} else {
			tray.SetIcon(trayIcon(health.Level))
			tray.SetLabel(label + trayLabel(health))
			tray.SetTooltip(product + " — " + health.Headline)

			// A drawn dot rather than the menu bar's own icon: that icon is sized
			// for the menu bar, and at menu size it dominates every row beneath it.
			// The colour still ties the two together.
			headline := menu.Add(health.Headline).OnClick(reveal)
			if dot, dotErr := menubar.Status(menubar.Level(health.Level)); dotErr == nil {
				headline.SetBitmap(dot)
			}

			// When the snapshots are, rather than how many there are. A machine with
			// twelve snapshots taken in one hour is not covered, and no count says so.
			if snaps, listErr := services.NewSnapshotService(status.Deps).List(context.Background()); listErr == nil {
				taken := make([]time.Time, 0, len(snaps))
				for _, snap := range snaps {
					taken = append(taken, snap.Taken)
				}
				if strip, drawErr := menubar.Coverage(taken, time.Now(), coverageWindow, menuIsDark()); drawErr == nil {
					// The caption sits above the strip rather than beside it: macOS
					// draws an item's image before its label, so a labelled strip
					// reads as a picture with its caption trailing off to the right.
					//
					// It says what a mark means, because the strip alone does not.
					// Twenty-two snapshots can fill three marks — which is the whole
					// point of showing when rather than how many — but without the
					// unit written down it reads as a strip that has stopped
					// updating.
					menu.Add("Last two days (mark represents an hour)").OnClick(reveal)
					menu.Add("").SetBitmap(strip).OnClick(reveal)
				}
			}

			if health.Newest != nil {
				menu.Add("Newest: " + health.Newest.Format("Mon 2 Jan, 15:04")).OnClick(reveal)
			}
			if health.ScheduleInstalled && health.NextDue != nil {
				menu.Add("Next due: " + health.NextDue.Format("Mon 2 Jan, 15:04")).OnClick(reveal)
			}
			// Findings are the reason to have looked at all, so they sit above the
			// actions rather than below them. Each carries its level as an image,
			// which is what the prefix characters were standing in for.
			if len(health.Findings) > 0 {
				menu.AddSeparator()
				for _, f := range health.Findings {
					item := menu.Add(f.Title).SetTooltip(f.Detail).OnClick(reveal)
					// Keyed on what the finding is about, not how bad it is: three
					// warnings drawn with three identical warning icons say only
					// that there are three.
					if icon, err := menubar.Glyph(f.Kind, menubar.Level(f.Level)); err == nil {
						item.SetBitmap(icon)
					}
				}
			}
		}

		menu.AddSeparator()
		menu.Add("Take a snapshot now").OnClick(func(*application.Context) {
			if _, err := services.NewSnapshotService(status.Deps).TakeNow(context.Background()); err != nil {
				log.Printf("menu bar: taking a snapshot: %v", err)
			}
			render()
		})
		menu.Add("Open Snapshotter").OnClick(func(*application.Context) {
			win.Show()
			win.Focus()
		})
		menu.AddSeparator()
		menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })

		tray.SetMenu(menu)
	}

	render()
	go func() {
		// A timer read fresh each round rather than a fixed ticker: the interval
		// is a setting, and a setting that only applies at the next launch is a
		// setting someone has to be told about.
		for {
			time.Sleep(interval())
			render()
		}
	}()

	return func(next config.Config) {
		mu.Lock()
		changed := trayRefresh != next.MenuBarRefresh()
		trayRefresh = next.MenuBarRefresh()
		mu.Unlock()
		if changed {
			// Redrawn immediately so the new interval is visibly in force rather
			// than starting after one more wait at the old one.
			render()
		}
	}
}

// trayIcon is the glyph for a level. An unrecognised level is treated as bad
// rather than ok, so a level added to services and forgotten here shows up as
// something to look at instead of silently reading as healthy.
func trayIcon(level services.Level) []byte {
	switch level {
	case services.LevelOK:
		return trayIconOK
	case services.LevelWarn:
		return trayIconWarn
	default:
		return trayIconBad
	}
}

// trayLabel is what sits in the menu bar beside the icon, which leaves room for
// very little. The count is there because "how many restore points do I have" is
// answerable at a glance and worth answering. The level is not, because the icon
// it sits next to already carries that.
func trayLabel(h services.Health) string {
	return strconv.Itoa(h.SnapshotCount)
}

// findingPrefix marks the severity of an individual finding in the menu, where
// the level has no icon of its own to lean on and colour is not available.
func findingPrefix(level services.Level) string {
	switch level {
	case services.LevelBad:
		return "●"
	case services.LevelWarn:
		return "◐"
	default:
		return "○"
	}
}
