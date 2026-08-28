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
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"snapshotter/frontend"
	"snapshotter/internal/boot"
	"snapshotter/internal/i18n"
	"strconv"

	"snapshotter/internal/apfs"
	"snapshotter/internal/changedb"
	"snapshotter/internal/cli"
	"snapshotter/internal/config"
	"snapshotter/internal/elevate"
	"snapshotter/internal/mountmgr"
	"snapshotter/internal/notify"
	"snapshotter/internal/scenario"
	"snapshotter/internal/schedule"
	"snapshotter/internal/single"
	"snapshotter/internal/verdict"
	"snapshotter/services"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
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

	// A flag set of this program's own, rather than the global one, so that a
	// command line this does not understand reaches the command line dispatcher
	// instead of being answered here.
	//
	// The global flag set answers -h itself, with a usage message listing the two
	// flags below and nothing else — so `snapshotter --help` described the launchd
	// agents' internal flags and never mentioned a single command. It also exits
	// on anything it does not recognise, which is why an unknown flag never got the
	// chance to be reported as an unknown command.
	//
	// ContinueOnError with the output discarded turns both of those into a value
	// this function can act on: flag.ErrHelp for -h, some other error for an
	// unrecognised flag, and either way the argument list is passed on intact.
	flags := flag.NewFlagSet("snapshotter", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	takeSnapshot := flags.Bool("take-snapshot", false,
		"take a snapshot, prune expired ones, and exit; this is what the scheduled task runs")
	watchMode := flags.Bool("watch", false,
		"watch for bulk deletion and take a snapshot when one starts; runs until stopped")
	argv := os.Args[1:]
	parseErr := flags.Parse(argv)

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

	// Before any branch below, so every path this process can take — the command
	// line, either agent, the window — speaks the configured language and honours
	// the verbose setting. Each used to apply its own subset, and two of them were
	// missing a piece.
	//
	// Here rather than inside each entry point: cli.Run is called directly by its
	// own tests, and reading the developer's settings file there made those tests
	// depend on whichever language the machine happened to be set to.
	boot.ApplyFromFile()

	// Anything on the command line means the command line; a BARE invocation means
	// the window. The launchd agents still use the original flags, because their
	// installed plists name them and changing that would orphan an installed agent.
	//
	// Anything, rather than anything this build recognises. A verb it did not know
	// used to fall through to here and open a window: `snapshotter health` — which
	// is not a command — silently launched the application, and the only thing that
	// said so was the one-window guard refusing the second copy. A mistyped verb
	// has to be told it was mistyped, and cli.Run already does that, in every
	// language and with the list of real commands beside it.
	//
	// parseErr covers the arguments that never became flag.Args() at all: -h, and
	// any flag this build does not have. Those are answered by the same dispatcher,
	// from the original argument list, so the help someone asked for is the help
	// about commands rather than about two flags meant for launchd.
	if rest := flags.Args(); len(rest) > 0 || parseErr != nil {
		if parseErr != nil {
			rest = argv
		}
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
	// One window at a time, whatever it was built from.
	//
	// Two copies put two identical icons in the menu bar, and only the one in
	// /Applications holds the Full Disk Access grant — so the second looks the same
	// and cannot mount anything. Worse, a copy left running is invisible: one sat
	// at 300% CPU for nineteen hours on the author's machine before anyone noticed,
	// because nothing about it looked different.
	//
	// This is held for as long as the window is, and released by the process ending
	// however it ends. The command line and both agents return before reaching
	// here, so none of them is blocked by a window being open — they take no icon
	// and they are the things that must keep working.
	configDir, dirErr := config.Dir()
	if dirErr != nil {
		log.Fatal(dirErr)
	}
	releaseWindow, err := single.Hold(single.Path(configDir))
	if err != nil {
		log.Fatal(err)
	}
	defer releaseWindow()

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

	// Any other volume's snapshots mount under a directory of their own, named
	// for the volume. Two volumes' snapshots of the same moment share a date, so
	// they would otherwise share a mountpoint and the second would land on top of
	// the first. The data volume keeps the bare directory it has always used, so
	// upgrading does not orphan mounts that are already attached.
	//
	// Nil while mounts are simulated: the fake has one seed directory and no
	// notion of a second volume, and inventing one would put rows on screen that
	// describe nothing.
	mountsOn := func(volume, device string) services.Mounter {
		return mountmgr.New(elevate.Osascript{}, volume, filepath.Join(p.mountRoot, device), p.program)
	}
	if faking {
		mountsOn = nil
	}

	return services.Deps{
		Runner:   runner,
		Mounts:   mounts,
		MountsOn: mountsOn,
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
		Volume: apfs.DataVolume,
		// The window asks which volumes exist constantly — every path it
		// translates needs to know — and enumerating them is twenty-odd
		// subprocesses. One cache for its lifetime, short enough that a disk
		// plugged in appears without a relaunch.
		VolumeCache: apfs.NewCache(0),
		Faking:      faking,
		FakeSeed:    fakeSeed,
		Scenario:    s.scenario,
		// One cache for the window's lifetime, kept honest by the watch started
		// in runWindow.
		//
		// Half of it is written to disk and half is not, and the line between them
		// is what each half would be promising. A recorded DIFFERENCE is safe to
		// keep for ever because it is re-checked every time it is used — still
		// different, and the folder still differs. A recorded SAMENESS would be a
		// claim about everything that happened while this application was not
		// running, which nothing can keep, so it is never written.
		Verdicts: verdict.New(),
		Space:    s.space,
	}
}

// openChangeTable attaches the change_detection table, if it will open.
//
// Failure is not fatal and is not even reported to the window. Everything the
// table holds is an optimisation: without it every folder is walked, which is
// what this application did before it existed — slow rather than wrong.
func openChangeTable() *changedb.Store {
	path, err := changedb.Path()
	if err != nil {
		log.Printf("recorded changes will not be kept between runs: %v", err)
		return nil
	}
	store, err := changedb.Open(path)
	if err != nil {
		log.Printf("recorded changes will not be kept between runs: %v", err)
		return nil
	}
	return store
}

func runWindow(p paths, runner apfs.Runner, sim *scenario.Scenario) error {
	s, err := setupFor(context.Background(), p, sim)
	if err != nil {
		return err
	}
	scenarioName := s.scenario

	deps := buildDeps(s, runner)
	// Recorded differences, kept between runs. Closed when the window is, and the
	// application runs without it if it will not open.
	if store := openChangeTable(); store != nil {
		defer store.Close()
		deps.Changes = store
		deps.Verdicts.Persist(store)
	}
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
			Handler: application.AssetFileServerFS(frontend.Assets),
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
		restored, restoreErr := services.NewScheduleService(deps).Restore(context.Background())
		if restoreErr != nil {
			// Said out loud, not only logged. This is the branch where the machine
			// is left unprotected, and it used to be the quiet one: putting the
			// agents back posted a notification, failing to put them back wrote a
			// line to a log file nobody opens. The reader was told when nothing was
			// wrong and told nothing when something was.
			log.Printf("restoring what was configured: %v", restoreErr)
			if nerr := notify.Send(context.Background(),
				i18n.T("notify.restoreFailed.title"),
				i18n.T("notify.restoreFailed.body", "Error", restoreErr.Error())); nerr != nil {
				log.Printf("could not post a notification: %v", nerr)
			}
		}
		// Whatever did come back is still worth saying, even if the other half
		// did not.
		if restored.Any() {
			what := i18n.T("notify.what.schedule")
			if restored.Schedule && restored.Tripwire {
				what = i18n.T("notify.what.both")
			} else if restored.Tripwire {
				what = i18n.T("notify.what.tripwire")
			}
			log.Printf("restored %s from the settings file", what)
			if nerr := notify.Send(context.Background(), i18n.T("notify.scheduleRestored.title"),
				i18n.T("notify.scheduleRestored.body", "what", what)); nerr != nil {
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
		//
		// Every volume holding snapshots, not just the home directory. Snapshots
		// on another volume can be browsed, so a verdict about one of its folders
		// can go stale — and watching only home meant it stayed stale until the
		// window was reopened, which reads as a comparison that is simply wrong.
		if deps.Verdicts != nil {
			go watchEveryVolume(context.Background(), deps)
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
