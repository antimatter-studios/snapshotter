// Package cli is the command line half of the application.
//
// The window is for looking at snapshots; this is for scripting them. It also
// makes the one thing the graphical interface cannot express: taking a snapshot
// immediately before running something risky, so the restore point exists at the
// moment it matters rather than up to six hours earlier.
//
// Nothing here needs privileges. Every command goes through tmutil, which is a
// client of backupd; mounting is the only privileged operation and no command
// here mounts anything.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"snapshotter/internal/i18n"
	"snapshotter/internal/manual"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/config"
	"snapshotter/internal/single"
	"snapshotter/internal/version"
)

// Env is everything a command talks to, so the commands can be tested without
// touching the machine.
type Env struct {
	Runner apfs.Runner
	Volume string
	Out    io.Writer
	Err    io.Writer
	// Now is the clock, swappable so relative ages are stable in tests.
	Now func() time.Time
	// Exec runs a subprocess for `run`. Swappable for the same reason.
	Exec func(ctx context.Context, name string, args []string) error
}

// SystemEnv is the Env the real binary uses.
func SystemEnv() Env {
	return Env{
		Runner: apfs.SystemRunner(),
		Volume: apfs.DataVolume,
		Out:    os.Stdout,
		Err:    os.Stderr,
		Now:    time.Now,
		Exec: func(ctx context.Context, name string, args []string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		},
	}
}

// commands maps a verb to its handler, and doubles as the source of the help
// text so a command cannot exist without being documented.
type command struct {
	summary string
	usage   string
	run     func(ctx context.Context, e Env, args []string) error
}

func commands() map[string]command {
	return map[string]command{
		"list": {
			summary: i18n.T("cli.desc.list"),
			usage:   "list",
			run:     runList,
		},
		"status": {
			summary: i18n.T("cli.desc.status"),
			usage:   "status",
			run:     runStatus,
		},
		"take": {
			summary: i18n.T("cli.desc.snapshot"),
			usage:   "take",
			run:     runTake,
		},
		"run": {
			summary: i18n.T("cli.desc.run"),
			usage:   "run -- <command> [args...]",
			run:     runRun,
		},
		"open": {
			summary: i18n.T("cli.desc.open"),
			usage:   "open",
			run:     runOpen,
		},
		"version": {
			summary: i18n.T("cli.desc.version"),
			usage:   "version",
			run:     runVersion,
		},
		"config": {
			summary: i18n.T("cli.desc.config"),
			usage:   "config [--write | keys | get <key> | set <key> <value>]",
			run:     runConfig,
		},
	}
}

// Run dispatches one command and returns the process exit code.
func Run(ctx context.Context, e Env, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		// `help <topic>` reads the manual; bare `help` lists what there is.
		if len(args) > 1 {
			return writeTopic(e, args[1])
		}
		writeHelp(e.Out)
		return 0
	}
	cmd, ok := commands()[args[0]]
	if !ok {
		fmt.Fprintf(e.Err, "snapshotter: %s\n\n", i18n.T("cli.noSuchCommand", "Name", strconv.Quote(args[0])))
		writeHelp(e.Err)
		return 2
	}
	if err := cmd.run(ctx, e, args[1:]); err != nil {
		fmt.Fprintf(e.Err, "snapshotter: %v\n", err)
		return 1
	}
	return 0
}

func writeHelp(w io.Writer) {
	fmt.Fprintln(w, i18n.T("cli.tagline"))
	// The topic form is named here, not only implied by the listing below. A
	// reader scanning the usage block should not have to infer that the pages are
	// reached by the same verb as the command list.
	fmt.Fprintln(w, "\n"+i18n.T("cli.usageHeading")+
		"\n  snapshotter                 "+i18n.T("cli.usageWindow")+
		"\n  snapshotter <command>"+
		"\n  snapshotter help <topic>    "+i18n.T("cli.usageTopic"))
	fmt.Fprintln(w, "\n"+i18n.T("cli.commandsHeading"))

	all := commands()
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)

	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	for _, name := range names {
		fmt.Fprintf(tw, "  %s\t%s\n", all[name].usage, all[name].summary)
	}
	tw.Flush()

	// The topics, under the commands. A manual nothing points at is a manual
	// nobody opens, and this listing is the only place a reader looks.
	topics := manual.All()
	if len(topics) > 0 {
		fmt.Fprintln(w, "\n"+i18n.T("cli.topicsHeading"))
		tw = tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
		for _, t := range topics {
			fmt.Fprintf(tw, "  snapshotter help %s\t%s\n", t.Name, t.Summary)
		}
		tw.Flush()
	}

	fmt.Fprintln(w, "\n"+i18n.T("cli.agentsNote"))
	fmt.Fprintln(w)
}

// writeTopic prints one manual page.
//
// The markdown is printed as it is written rather than rendered down to plain
// text. It reads perfectly well in a terminal, it survives being piped into
// something that does render it, and a renderer of our own would be a second
// markdown implementation to keep correct for no gain a reader would notice.
func writeTopic(e Env, name string) int {
	topic, ok := manual.Lookup(name)
	if !ok {
		fmt.Fprintf(e.Err, "snapshotter: %s\n", i18n.T("cli.noSuchTopic", "Name", strconv.Quote(name)))
		// A near miss is answered with the page rather than with a list of
		// everything: somebody who typed "mount" wants "mounting", not a menu.
		if near := manual.Suggest(name); len(near) > 0 {
			fmt.Fprintf(e.Err, "\n%s\n", i18n.T("cli.didYouMean"))
			for _, n := range near {
				fmt.Fprintf(e.Err, "  snapshotter help %s\n", n)
			}
			fmt.Fprintln(e.Err)
			return 2
		}
		fmt.Fprintln(e.Err)
		writeHelp(e.Err)
		return 2
	}
	fmt.Fprintln(e.Out, topic.Body)
	return 0
}

// runList prints every volume's snapshots, grouped by the disk they are on.
//
// It used to answer for the data volume alone, which stopped being the whole
// answer when `tmutil localsnapshot` turned out to write to every mounted APFS
// volume at once. Somebody wanting to know what was on an external disk had to
// leave this application and read diskutil, which is the one thing it exists to
// save them from.
//
// One volume is printed without a heading. A machine with a single disk needs no
// label saying which disk, and adding one would make every Mac look like it had
// something to disambiguate.
func runList(ctx context.Context, e Env, _ []string) error {
	vols, err := apfs.Volumes(ctx, e.Runner)
	if err != nil {
		return err
	}
	if len(vols) == 0 {
		fmt.Fprintln(e.Out, i18n.T("cli.noSnapshots"))
		return nil
	}

	tw := tabwriter.NewWriter(e.Out, 0, 8, 2, ' ', 0)
	for i, v := range vols {
		if len(vols) > 1 {
			if i > 0 {
				fmt.Fprintln(tw)
			}
			// The name a person calls the disk, and where it is mounted. Both,
			// because two disks can share a name and only the mount point says
			// which one this is.
			fmt.Fprintf(tw, "%s\t%s\t\n", v.Name, v.MountPoint)
		}
		fmt.Fprintln(tw, i18n.T("cli.colDate")+"\t"+i18n.T("cli.colAge")+"\t"+i18n.T("cli.colFlags"))
		for _, s := range v.Snapshots {
			var flags []string
			if s.Purgeable {
				flags = append(flags, "purgeable")
			}
			if s.LimitsContainer {
				flags = append(flags, "pinning-container")
			}
			if len(flags) == 0 {
				flags = []string{"-"}
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Stamp, age(e.Now().Sub(s.Taken)), strings.Join(flags, ","))
		}
	}
	return tw.Flush()
}

func runStatus(ctx context.Context, e Env, _ []string) error {
	snaps, err := apfs.List(ctx, e.Runner, e.Volume)
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		fmt.Fprintln(e.Out, i18n.T("cli.noSnapshotsShort"))
		return nil
	}

	newest, oldest := snaps[0], snaps[len(snaps)-1]
	fmt.Fprintln(e.Out, i18n.N("cli.snapshotsCovering", len(snaps), "Span", i18n.Span(newest.Taken.Sub(oldest.Taken).Hours())))
	fmt.Fprintf(e.Out, i18n.T("cli.newest")+"  %s (%s)\n", newest.Stamp, age(e.Now().Sub(newest.Taken)))
	fmt.Fprintf(e.Out, i18n.T("cli.oldest")+"  %s (%s)\n", oldest.Stamp, age(e.Now().Sub(oldest.Taken)))

	// A configured Time Machine destination silently changes what retention
	// means, so it is reported here rather than left to surprise someone.
	if tm := apfs.DestinationInfo(ctx, e.Runner); tm.HasDestination {
		fmt.Fprintln(e.Out, "\n"+apfs.ThinningWarning())
	}
	return nil
}

// runVersion answers "which build is this", which is the first question asked of
// any bug report and the last one a person should have to work out for
// themselves. It touches nothing: no snapshots, no privileges, no network — so it
// is also the cheapest way to confirm an installation is wired up at all.
func runVersion(_ context.Context, e Env, _ []string) error {
	fmt.Fprintln(e.Out, version.String())
	return nil
}

func runTake(ctx context.Context, e Env, _ []string) error {
	snap, err := apfs.Create(ctx, e.Runner)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, snap.Stamp)
	return nil
}

// runRun is the reason this package exists.
//
// A schedule gives a restore point up to its interval old. Something about to
// rewrite or delete a lot of files deserves one from a second ago, and only the
// person running that command knows it is about to happen.
//
// The command's exit status is passed through, so this can be dropped in front
// of anything without changing what the caller sees. It does not roll back on
// failure: undoing needs a mounted snapshot, and mounting needs privileges this
// deliberately avoids.
func runRun(ctx context.Context, e Env, args []string) error {
	// Everything after a bare "--" is the command, so its own flags are never
	// mistaken for ours.
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return errors.New("run: " + i18n.T("cli.runNothing") + " (usage: snapshotter run -- <command> [args...])")
	}

	snap, err := apfs.Create(ctx, e.Runner)
	if err != nil {
		return fmt.Errorf("run: "+i18n.T("cli.runNoRestorePoint")+": %w", err)
	}
	fmt.Fprintf(e.Err, "snapshotter: "+i18n.T("cli.restorePoint", "Name", "%s")+"\n", snap.Stamp)

	if err := e.Exec(ctx, args[0], args[1:]); err != nil {
		fmt.Fprintf(e.Err, "snapshotter: "+i18n.T("cli.commandFailed", "Command", "%s", "Name", "%s")+"\n", args[0], snap.Stamp)
		return err
	}
	return nil
}

// age words a duration for a listing: short, and never more precise than it is
// accurate.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T("cli.justNow")
	case d < time.Hour:
		return i18n.N("cli.minutesAgo", int(d.Minutes()))
	case d < 48*time.Hour:
		return i18n.N("cli.hoursAgo", int(d.Hours()))
	default:
		return i18n.N("cli.daysAgo", int(d.Hours()/24))
	}
}

// runConfig shows where the settings live and what is currently in them.
//
// It exists because the file is the supported way to change anything the window
// does not offer, and until now nothing told you where it was or what could go in
// it. An absent file is not a fault — everything falls back to the defaults — but
// it does leave you with nothing to edit, which is what --write fixes.
func runConfig(_ context.Context, e Env, args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return configSubcommand(e, args)
	}

	write := false
	for _, arg := range args {
		switch arg {
		case "--write":
			write = true
		default:
			return fmt.Errorf("config: "+i18n.T("cli.configUnknownOption", "Name", "%q")+" (usage: snapshotter config [--write])", arg)
		}
	}

	path, err := config.Path()
	if err != nil {
		return err
	}

	_, statErr := os.Stat(path)
	switch {
	case statErr == nil && write:
		// Refusing rather than merging: a rewrite would drop anything this build
		// does not know about, and settings files outlive the version that wrote
		// them.
		return fmt.Errorf("config: "+i18n.T("cli.configExists", "Path", "%s"), path)
	case statErr == nil:
		// Read back rather than printed from memory, so what is shown is what the
		// file says — including a value this build ignores.
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintln(e.Out, path)
		fmt.Fprintln(e.Out)
		fmt.Fprint(e.Out, string(body))
		return nil
	case !os.IsNotExist(statErr):
		return statErr
	case write:
		if err := config.Save(config.Defaults()); err != nil {
			return err
		}
		fmt.Fprintf(e.Out, i18n.T("cli.wroteDefaults", "Path", "%s")+"\n", path)
		return nil
	default:
		fmt.Fprintf(e.Out, i18n.T("cli.configMissing", "Path", "%s")+"\n", path)
		fmt.Fprintln(e.Out, i18n.T("cli.configWriteHint"))
		return nil
	}
}

// configSubcommand handles the forms that name a setting rather than the file.
//
// These exist for scripting and for tests: putting a machine into a known state
// should not mean hand-writing YAML, and reading a value back should not mean
// parsing it.
func configSubcommand(e Env, args []string) error {
	switch args[0] {
	case "keys":
		if len(args) != 1 {
			return errors.New("config: " + i18n.T("cli.keysNoArgs"))
		}
		for _, k := range config.Keys() {
			fmt.Fprintln(e.Out, k)
		}
		return nil

	case "get":
		if len(args) != 2 {
			return errors.New("config: " + i18n.T("cli.getNeedsKey") + " (usage: snapshotter config get <key>)")
		}
		// Loaded rather than read from the file directly, so an absent file
		// answers with the default rather than with an error: the default is what
		// the application would actually use.
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		value, err := config.Get(cfg, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(e.Out, value)
		return nil

	case "set":
		if len(args) != 3 {
			return errors.New("config: " + i18n.T("cli.setNeedsPair") + " (usage: snapshotter config set <key> <value>)")
		}
		// A file that will not parse is not overwritten here either. Load reports
		// the error and hands back the defaults, and saving those would silently
		// discard whatever the file was trying to say.
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("config: %w ("+i18n.T("cli.fixFileFirst")+")", err)
		}
		if err := config.Set(&cfg, args[1], args[2]); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		// Read back rather than echoing the argument, so what is printed is what
		// was stored — "6" for a field that holds 6.0, and nothing at all if the
		// write did not take.
		stored, err := config.Get(cfg, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Out, "%s = %s\n", args[1], stored)
		return nil

	default:
		return fmt.Errorf("config: "+i18n.T("cli.notAConfigCommand", "Name", "%q"), args[0])
	}
}

// runOpen shows the window, and shows the one already open rather than refusing.
//
// Typing the bare name is a question — it prints this help — so asking for the
// window needs a word of its own. It exists because the alternative was reaching
// for Finder or `open -a` to do something this command line can obviously do.
//
// Through LaunchServices rather than by starting the binary: it is what gives an
// application a Dock icon and a menu bar, and what brings a running copy forward
// instead of starting a second one that would be refused by the instance guard.
func runOpen(_ context.Context, e Env, _ []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// Resolved, unlike the check that decides whether a bare invocation is a
	// question or a launch. That one wants to know how the program was ADDRESSED;
	// this wants to know where it actually lives. On macOS os.Executable hands
	// back the path used to start the process, so run through Homebrew's symlink
	// it is /opt/homebrew/bin/snapshotter — which is not a bundle, and asking for
	// the window said so.
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	bundle, ok := single.BundleOf(self)
	if !ok {
		// A development build run straight from bin/ has no bundle. Guessing at one
		// would open whatever else on this machine is called Snapshotter.
		return errors.New(i18n.T("cli.notInstalled"))
	}
	if err := exec.Command("open", "-a", bundle).Run(); err != nil {
		return fmt.Errorf("%s: %w", bundle, err)
	}
	return nil
}
