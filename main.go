// Snapshotter browses APFS local snapshots, compares them against the live
// disk, and keeps them being taken on a schedule.
//
// macOS only schedules local snapshots when Time Machine has a destination
// configured. Without a backup disk nothing takes them, and nothing browses
// them either: Time Machine's own interface needs a configured destination,
// and mounting a snapshot by hand needs root. This application fills that gap.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/cli"
	"snapshotter/internal/config"
	"snapshotter/internal/elevate"
	"snapshotter/internal/menubar"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/notify"
	"snapshotter/internal/scenario"
	"snapshotter/internal/schedule"
	"snapshotter/internal/watch"
	"snapshotter/services"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

// The menu bar glyphs, one per health level. Each is the same ring drawn to a
// different extent — closed for ok, two thirds for warn, a bare crescent for bad
// — so the level reads in greyscale and to someone who cannot tell the green from
// the amber. Colour is the fast signal here, not the only one.
//
// These are deliberately NOT template images, which is the one thing that cannot
// be changed casually: a template is black plus alpha and macOS inverts it to
// suit the menu bar, discarding the colour entirely. Worse, Wails latches the
// template flag on the tray the first time it is set and never clears it, so a
// single SetTemplateIcon call anywhere would render every one of these as a black
// silhouette. The palette is mid-toned to hold up against a light and a dark menu
// bar without that help.
//
// The @2x files are the ones embedded, not the 22px ones beside them: Wails
// resizes whatever it is given to the status bar's thickness in points, so the
// pixels only decide how sharp it looks. 44px into a 22pt slot is exactly Retina.
var (
	//go:embed assets/icons/tray-ok-2x.png
	trayIconOK []byte
	//go:embed assets/icons/tray-warn-2x.png
	trayIconWarn []byte
	//go:embed assets/icons/tray-error-2x.png
	trayIconBad []byte
)

func main() {
	// Before flag.Parse, and before anything else at all.
	//
	// The elevated helper carries its own flag set, so letting this program's
	// flag.Parse see -helper-root would abort on an unknown flag. It also wants
	// none of what follows: no scenario, no paths, no window. It is root, it does
	// one thing, and it exits — the less it touches the smaller the privileged
	// surface is.
	if mountmgr.IsHelperInvocation(os.Args[1:]) {
		if err := mountmgr.RunHelper(context.Background(), os.Args[1:], os.Stderr); err != nil {
			// Bare, and to stderr: the unprivileged parent reads this text to tell a
			// TCC refusal from an ordinary failure, so log's timestamp prefix would
			// only get in the way of matching it.
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	takeSnapshot := flag.Bool("take-snapshot", false,
		"take a snapshot, prune expired ones, and exit; this is what the scheduled task runs")
	watchMode := flag.Bool("watch", false,
		"watch for bulk deletion and take a snapshot when one starts; runs until stopped")
	flag.Parse()

	// Resolved before anything below and shared by every entry point, so the
	// command line, the launchd modes and the window cannot end up describing
	// different machines.
	//
	// A scenario that was asked for and could not be built is fatal. Falling back
	// to real state would be the one failure this mode must never have: the whole
	// point is that what is on screen is known, and quietly showing the real
	// machine instead would be indistinguishable from a scenario that lies.
	sim, err := scenario.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	runner := apfs.SystemRunner()
	if sim != nil {
		runner = sim.Runner
		for _, line := range sim.Banner() {
			log.Print(line)
		}
	}

	// A verb means the command line; a bare invocation means the window. The
	// launchd agents still use the original flags, because their installed
	// plists name them and changing that would orphan an installed agent.
	if rest := flag.Args(); len(rest) > 0 && cli.IsCommand(rest[0]) {
		env := cli.SystemEnv()
		env.Runner = runner
		os.Exit(cli.Run(context.Background(), env, rest))
	}

	paths, err := resolvePaths()
	if err != nil {
		log.Fatal(err)
	}

	if *takeSnapshot {
		if err := runScheduledSnapshot(context.Background(), runner); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *watchMode {
		if err := runWatch(context.Background(), runner); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := runWindow(paths, runner, sim); err != nil {
		log.Fatal(err)
	}
}

// paths are the locations the application reads and writes.
type paths struct {
	// mountRoot holds one directory per mounted snapshot. It sits under the
	// user's own Application Support so the directories can be created without
	// authorization; only the mount itself needs it.
	mountRoot string
	agentDir  string
	logPath   string
	// tripwireLogPath is separate from logPath because the two agents write
	// continuously and occasionally respectively; interleaving them would make
	// both harder to read at the moment either one matters.
	tripwireLogPath string
	program         string
}

func resolvePaths() (paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths{}, fmt.Errorf("cannot find the home directory: %w", err)
	}
	program, err := os.Executable()
	if err != nil {
		return paths{}, fmt.Errorf("cannot find this program's path: %w", err)
	}
	// Configured locations win where they are set; the defaults are what almost
	// everyone wants and what every previous version used, so an absent setting
	// changes nothing. agentDir is not configurable: launchd looks in exactly one
	// place, and a plist anywhere else is a file nobody reads.
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		log.Printf("configuration: %v (continuing with defaults)", cfgErr)
	}
	return paths{
		mountRoot: config.ResolvePath(cfg.Paths.MountRoot,
			filepath.Join(home, "Library", "Application Support", "Snapshotter", "mounts")),
		agentDir: filepath.Join(home, "Library", "LaunchAgents"),
		logPath: config.ResolvePath(cfg.Paths.Log,
			filepath.Join(home, "Library", "Logs", "snapshotter.log")),
		tripwireLogPath: config.ResolvePath(cfg.Paths.TripwireLog,
			filepath.Join(home, "Library", "Logs", "snapshotter-tripwire.log")),
		program: program,
	}, nil
}

// runScheduledSnapshot is the whole of the scheduled task: take one snapshot,
// drop the ones past the retention window, and report what happened to the log
// launchd captures. It needs no privileges, because tmutil asks backupd to do
// the work.
func runScheduledSnapshot(ctx context.Context, runner apfs.Runner) error {
	snap, err := apfs.Create(ctx, runner)
	if err != nil {
		// A scheduled run that fails is invisible: launchd keeps the output and
		// nobody reads a log until something has already been lost.
		if nerr := notify.Send(ctx, "Scheduled snapshot failed", err.Error()); nerr != nil {
			log.Printf("could not post a notification: %v", nerr)
		}
		return err
	}
	log.Printf("created %s", snap.Stamp)

	// The plist carries the policy, so the schedule prunes by whatever it was
	// installed with rather than by whatever this binary's default happens to be.
	policy, err := schedule.PolicyFromEnv()
	if err != nil {
		// A policy this build cannot read prunes NOTHING rather than pruning on a
		// guess. Keeping too much is corrected by the next run; deleting too much
		// is not correctable at all, because a snapshot records a past state of the
		// disk and cannot be recreated.
		log.Print(err)
	}
	pruned, err := schedule.PruneByPolicy(ctx, runner, apfs.DataVolume, policy, time.Now())
	for _, p := range pruned {
		log.Printf("pruned %s", p.Stamp)
	}
	if err != nil {
		return err
	}

	remaining, err := apfs.List(ctx, runner, apfs.DataVolume)
	if err != nil {
		return err
	}
	log.Printf("holding %d snapshots, keeping %s", len(remaining), policy.Describe())
	return nil
}

// runWatch is the tripwire: it watches the home directory and takes a snapshot
// as soon as something starts deleting in bulk.
//
// It cannot prevent a deletion. FSEvents reports what has already happened, so
// by the time a removal is seen that file is gone. What it prevents is a
// deletion running to completion unwitnessed — trip at the two-hundredth file
// of ten thousand and the rest are still recoverable.
//
// Like the scheduled task it needs no privileges, because tmutil asks backupd
// to do the work.
func runWatch(ctx context.Context, runner apfs.Runner) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find the home directory: %w", err)
	}

	w := watch.New([]string{home}, func(ctx context.Context) error {
		snap, err := apfs.Create(ctx, runner)
		if err != nil {
			if nerr := notify.Send(ctx, "Bulk deletion detected", "Could not take a snapshot: "+err.Error()); nerr != nil {
				log.Printf("could not post a notification: %v", nerr)
			}
			return err
		}
		log.Printf("created %s", snap.Stamp)
		// Worth interrupting for: something is deleting in bulk, and the user
		// may not have asked for it.
		if nerr := notify.Send(ctx, "Something is deleting a lot of files",
			"Took a snapshot at "+snap.Taken.Format("15:04")+". Whatever is still on disk can be restored."); nerr != nil {
			log.Printf("could not post a notification: %v", nerr)
		}
		return nil
	})
	w.Log = log.Printf
	return w.Run(ctx)
}

// Two values the native window and the web view have to agree on. There is no
// mechanism that could make them share one definition — one is compiled into a
// Go binary, the other is parsed by WebKit — so each side keeps its own copy and
// main_style_test.go reads the stylesheet and fails if the two drift apart.
const (
	// titleBarHeight is the strip of window the traffic lights sit in. The header
	// in styles.css pads its top by the same amount; less, and the buttons
	// overlap the title.
	titleBarHeight = 50

	// windowBackgroundHex is what --bg is set to in styles.css. It is what the
	// window shows in the moment between appearing and the web view painting, so
	// a mismatch is a flash of the wrong colour on every launch.
	windowBackgroundHex = "#121418"
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

// windowBackground is windowBackgroundHex as the components Wails wants.
var windowBackground = struct{ r, g, b uint8 }{0x12, 0x14, 0x18}

// setup is what a scenario decides before any service exists: where things are
// written, what the window is called, and who answers for disk space.
type setup struct {
	paths paths
	// scenario is empty on a real machine, and is the scenario's name otherwise.
	// Every surface that has to say "this is invented" reads it.
	scenario string
	// space is nil unless a scenario has an opinion about disk space, in which
	// case StatusService asks the kernel as usual.
	space func(string) (uint64, uint64, error)
}

// setupFor redirects the paths a scenario must not write to for real.
//
// The plists a scenario claims are installed have to exist somewhere, and it
// must not be the real ~/Library/LaunchAgents: a plist written there would
// outlive the run and start taking real snapshots on a real timer. So the agent,
// log and mount directories all move into the scenario's own sandbox, while
// program stays the real binary — the plists naming it is what makes reading
// them back worth anything.
func setupFor(ctx context.Context, p paths, sim *scenario.Scenario) (setup, error) {
	if sim == nil {
		return setup{paths: p}, nil
	}

	box, err := sim.Sandbox(ctx)
	if err != nil {
		return setup{}, err
	}
	p.agentDir, p.logPath, p.tripwireLogPath, p.mountRoot = box.AgentDir, box.LogPath, box.TripwireLogPath, box.MountRoot
	log.Printf("scenario %s: sandbox is %s", sim.Spec.Name, box.Dir)

	return setup{paths: p, scenario: sim.Spec.Name, space: sim.Spec.Space()}, nil
}

// buildDeps assembles everything the services share.
func buildDeps(s setup, runner apfs.Runner) services.Deps {
	p := s.paths

	// Mounting needs root and Full Disk Access, and is refused outright on a
	// machine without both. The fake stands in so the browse and compare
	// surface can be worked on regardless; nothing else changes.
	// p.program, not os.Args[0]: it has to be the bundle's executable, because
	// that is the identity the Full Disk Access grant is attached to.
	var mounts services.Mounter = mountmgr.New(elevate.Osascript{}, apfs.DataVolume, p.mountRoot, p.program)
	faking, fakeSeed := false, ""
	if fake, ok := mountmgr.FakeFromEnv(p.mountRoot); ok {
		mounts, faking, fakeSeed = fake, true, fake.Seed
		log.Printf("mounts are simulated: %s is set, seeding from %s", mountmgr.FakeEnabled, fake.Seed)
	}

	return services.Deps{
		Runner: runner,
		Mounts: mounts,
		Agent: &schedule.Agent{
			Runner:   runner,
			AgentDir: p.agentDir,
			Program:  p.program,
			LogPath:  p.logPath,
			UID:      os.Getuid(),
		},
		Tripwire: &schedule.Tripwire{
			Runner:   runner,
			AgentDir: p.agentDir,
			Program:  p.program,
			LogPath:  p.tripwireLogPath,
			UID:      os.Getuid(),
		},
		Volume:   apfs.DataVolume,
		Faking:   faking,
		FakeSeed: fakeSeed,
		Scenario: s.scenario,
		Space:    s.space,
	}
}

func runWindow(p paths, runner apfs.Runner, sim *scenario.Scenario) error {
	s, err := setupFor(context.Background(), p, sim)
	if err != nil {
		return err
	}
	scenarioName := s.scenario

	deps := buildDeps(s, runner)
	status := services.NewStatusService(deps)

	server, err := serverOptions()
	if err != nil {
		return err
	}

	app := application.New(application.Options{
		Name:        "Snapshotter",
		Description: "Browse, compare and schedule APFS local snapshots",
		Server:      server,
		Services: []application.Service{
			application.NewService(services.NewSnapshotService(deps)),
			application.NewService(services.NewBrowseService(deps)),
			application.NewService(services.NewDiffService(deps)),
			application.NewService(services.NewRestoreService(deps)),
			application.NewService(services.NewScheduleService(deps)),
			application.NewService(services.NewSearchService(deps)),
			application.NewService(status),
			application.NewService(services.NewConfigService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// A scenario has to be unmistakable, and the title bar is the one label that
	// is always visible and cannot be scrolled past. It is also the strongest
	// statement main.go can make on its own: the in-page banner needs a field on
	// services.Deps, which docs/AUTOMATION.md sets out.
	title := "Snapshotter"
	if scenarioName != "" {
		title = "Snapshotter — SCENARIO " + scenarioName + " (invented state, not this Mac)"
	}

	// One read for the window's own preferences. A broken configuration file has
	// already been reported by resolvePaths, so this quietly takes the defaults
	// rather than saying the same thing twice.
	cfg, _ := config.Load()
	windowWidth, windowHeight := cfg.WindowSize()

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  title,
		Width:  windowWidth,
		Height: windowHeight,
		// Mounted snapshots are deliberately left attached on quit. Detaching
		// needs root, and a password prompt on the way out is a poor trade for
		// tidying something read-only and harmless.
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: titleBarHeight,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(windowBackground.r, windowBackground.g, windowBackground.b),
		URL:              "/",
	})

	// Built once the application is running rather than before it. Creating the
	// tray during setup means calling SetMenu and SetLabel on something the
	// platform layer has not finished making, and the started event is the
	// point at which it has.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		// Before the tray, so the first menu it draws reflects a repaired machine
		// rather than the broken one it started from.
		//
		// A launchd job does not survive everything: upgrading through Homebrew
		// unloads both agents before staging the new version, and the loss is
		// silent — the settings still record the interval that was chosen and
		// nothing is taking snapshots. The settings are the intent; launchd is the
		// current state; this reconciles the second to the first.
		if restored, err := services.NewScheduleService(deps).Restore(context.Background()); err != nil {
			log.Printf("restoring what was configured: %v", err)
		} else if restored.Any() {
			what := "the schedule"
			if restored.Schedule && restored.Tripwire {
				what = "the schedule and the bulk-deletion watcher"
			} else if restored.Tripwire {
				what = "the bulk-deletion watcher"
			}
			log.Printf("restored %s from the settings file", what)
			if nerr := notify.Send(context.Background(), "Snapshotter restored your schedule",
				"Something had removed "+what+", most likely an upgrade. It is running again."); nerr != nil {
				log.Printf("could not post a notification: %v", nerr)
			}
		}

		installTray(app, status, win, scenarioName)
		if deps.Faking {
			openEveryFakeMount(deps)
		}
	})
	return app.Run()
}

// serverAddrEnv sets where the headless server build listens.
const serverAddrEnv = "SNAPSHOTTER_SERVER_ADDR"

// serverOptions configures the headless server build — the same services and the
// same frontend, served over HTTP with no native window.
//
// It exists because the interface cannot otherwise be driven by code at all:
// accessibility presses do not reach into a WKWebView, so no test and no script
// can press anything in the real window. Only a binary built with `-tags server`
// reads any of this; the desktop build ignores ServerOptions entirely, so setting
// it unconditionally costs nothing.
//
// The default binds localhost, and an address with no host is read as localhost
// rather than as every interface. The bindings are the whole application:
// anything that can reach the port can take and delete this machine's snapshots
// without a password, so ":8080" quietly meaning 0.0.0.0 would be a poor thing to
// acquire by accident. Wails reads WAILS_SERVER_HOST and WAILS_SERVER_PORT after
// this and they win, which is worth knowing when the port is not the one asked
// for.
func serverOptions() (application.ServerOptions, error) {
	addr := os.Getenv(serverAddrEnv)
	if addr == "" {
		addr = "localhost:8080"
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return application.ServerOptions{}, fmt.Errorf("%s=%q is not host:port: %w", serverAddrEnv, addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return application.ServerOptions{}, fmt.Errorf("%s=%q does not name a port", serverAddrEnv, addr)
	}
	if host == "" {
		host = "localhost"
	}
	return application.ServerOptions{Host: host, Port: port}, nil
}

// openEveryFakeMount populates every fake mountpoint as soon as the application
// starts.
//
// A real mount is deliberately manual, because each one costs an authorization
// prompt. A fake one costs a directory clone, so making the developer click
// through the same flow to reach the screens they are working on buys nothing.
func openEveryFakeMount(deps services.Deps) {
	ctx := context.Background()
	snaps, err := apfs.List(ctx, deps.Runner, deps.Volume)
	if err != nil {
		log.Printf("fake mounts: listing snapshots: %v", err)
		return
	}
	names := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		names = append(names, snap.Name)
	}
	if err := deps.Mounts.Mount(ctx, names); err != nil {
		log.Printf("fake mounts: %v", err)
		return
	}
	log.Printf("fake mounts: opened %d snapshot(s)", len(names))
}

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
func installTray(app *application.App, status *services.StatusService, win application.Window, scenarioName string) {
	// Read once when the tray is built rather than on every tick: a person editing
	// the file expects the next launch to honour it, not the next minute.
	cfg, _ := config.Load()
	trayRefresh := cfg.MenuBarRefresh()

	tray := app.SystemTray.New()
	// No icon is set here: every render sets one to match the level it just read,
	// and the first render happens below before anything is on screen.

	// A menu bar has room for very little, and under a scenario "which machine is
	// this describing" outranks everything else the label could say.
	label, product := "", "Snapshotter"
	if scenarioName != "" {
		label, product = "SIM ", "Snapshotter — SCENARIO "+scenarioName
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
					menu.Add("Last two days").OnClick(reveal)
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
		for range time.Tick(trayRefresh) {
			render()
		}
	}()
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
