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

	"snapshotter/internal/apfs"
	"snapshotter/internal/cli"
	"snapshotter/internal/config"
	"snapshotter/internal/elevate"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/notify"
	"snapshotter/internal/scenario"
	"snapshotter/internal/schedule"
	"snapshotter/internal/verdict"
	"snapshotter/internal/version"
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
	// A build that is not the installed one announces itself and refuses to run a
	// second window.
	//
	// Two copies put two identical icons in the menu bar, and only the one in
	// /Applications holds the Full Disk Access grant — so the working build looks
	// the same and cannot mount anything. Worse, a copy left running is invisible:
	// one of these sat at 300% CPU for nineteen hours on the author's machine
	// before anyone noticed, because nothing about it looked different.
	if err := refuseIfInstalledCopyIsRunning(); err != nil {
		log.Fatal(err)
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
		// One cache for the window's lifetime, kept honest by the watch started
		// in runWindow. Nothing is written to disk: a cache that outlived the
		// process would have to be right about everything that happened while it
		// was gone, and a cold start costs one walk, which is what every start
		// costs today.
		Verdicts: verdict.New(),
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

		applyToTray := installTray(app, status, win, scenarioName)
		if deps.Faking {
			openEveryFakeMount(deps)
		}

		// Editing the settings file and being told to relaunch is a poor answer
		// when nothing about the change requires it. Everything that can be
		// applied to a running application is applied here; what cannot is named
		// in applySettings so the limit is written down rather than discovered.
		go watchSettings(context.Background(), s.paths, deps, win, applyToTray)

		// Browsing asks for a folder's verdict constantly, and the answer only
		// stops being true when the live disk moves.
		if home, herr := os.UserHomeDir(); herr == nil && deps.Verdicts != nil {
			go watchForChanges(context.Background(), home, deps.Verdicts)
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

// installedApp is where the released copy lives once Homebrew has staged it.
const installedApp = "/Applications/Snapshotter.app/Contents/MacOS/snapshotter"

// refuseIfInstalledCopyIsRunning stops a development build joining a menu bar
// that already has one.
//
// Only for unstamped builds: two RELEASED copies cannot happen — Homebrew
// replaces the one in /Applications — and refusing to start a second copy of a
// binary someone deliberately invoked would be an odd thing for a released
// application to do.
func refuseIfInstalledCopyIsRunning() error {
	if version.IsRelease() || os.Getenv("SNAPSHOTTER_ALLOW_SECOND_COPY") == "1" {
		return nil
	}
	self, err := os.Executable()
	if err != nil || self == installedApp {
		return nil
	}
	// pgrep rather than reading /proc, which macOS does not have. A non-zero exit
	// means nothing matched, which is the ordinary case.
	if err := exec.Command("pgrep", "-f", installedApp).Run(); err != nil {
		return nil
	}
	return fmt.Errorf(
		"the installed Snapshotter is already running, and a second copy would put an "+
			"identical icon in the menu bar that cannot mount anything — only the copy in "+
			"/Applications holds the Full Disk Access grant.\n"+
			"Quit it first, or run this one with SNAPSHOTTER_ALLOW_SECOND_COPY=1 if that is "+
			"what you meant.\nThis build: %s", self)
}
