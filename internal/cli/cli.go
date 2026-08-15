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
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"snapshotter/internal/apfs"
	"snapshotter/internal/config"
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
			summary: "list the snapshots of the data volume, newest first",
			usage:   "list",
			run:     runList,
		},
		"status": {
			summary: "say whether this Mac has usable restore points",
			usage:   "status",
			run:     runStatus,
		},
		"take": {
			summary: "take a snapshot now",
			usage:   "take",
			run:     runTake,
		},
		"run": {
			summary: "take a snapshot, then run a command",
			usage:   "run -- <command> [args...]",
			run:     runRun,
		},
		"version": {
			summary: "print the version of this build",
			usage:   "version",
			run:     runVersion,
		},
		"config": {
			summary: "show, read or change the settings",
			usage:   "config [--write | keys | get <key> | set <key> <value>]",
			run:     runConfig,
		},
	}
}

// IsCommand reports whether the first argument names a command, so main can
// tell `snapshotter list` from a bare `snapshotter` that should open a window.
func IsCommand(arg string) bool {
	if arg == "help" || arg == "-h" || arg == "--help" {
		return true
	}
	_, ok := commands()[arg]
	return ok
}

// Run dispatches one command and returns the process exit code.
func Run(ctx context.Context, e Env, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		writeHelp(e.Out)
		return 0
	}
	cmd, ok := commands()[args[0]]
	if !ok {
		fmt.Fprintf(e.Err, "snapshotter: no such command %q\n\n", args[0])
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
	fmt.Fprintln(w, "Browse, compare and schedule APFS local snapshots.")
	fmt.Fprintln(w, "\nUsage:\n  snapshotter                 open the window\n  snapshotter <command>")
	fmt.Fprintln(w, "\nCommands:")

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

	fmt.Fprintln(w, "\nThe scheduled task and the bulk-deletion watcher are run by launchd as")
	fmt.Fprintln(w, "--take-snapshot and --watch. Install both from the window.")
}

func runList(ctx context.Context, e Env, _ []string) error {
	snaps, err := apfs.List(ctx, e.Runner, e.Volume)
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		fmt.Fprintln(e.Out, "No snapshots. Nothing can be rolled back to.")
		return nil
	}

	// Flags decorate rather than replace the listing, so a failure to read them
	// costs detail and not the answer.
	details, _ := apfs.Details(ctx, e.Runner, e.Volume)

	tw := tabwriter.NewWriter(e.Out, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "DATE\tAGE\tFLAGS")
	for _, s := range snaps {
		var flags []string
		if d, ok := details[s.Name]; ok {
			if d.Purgeable {
				flags = append(flags, "purgeable")
			}
			if d.LimitsContainer {
				flags = append(flags, "pinning-container")
			}
		}
		if len(flags) == 0 {
			flags = []string{"-"}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Stamp, age(e.Now().Sub(s.Taken)), strings.Join(flags, ","))
	}
	return tw.Flush()
}

func runStatus(ctx context.Context, e Env, _ []string) error {
	snaps, err := apfs.List(ctx, e.Runner, e.Volume)
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		fmt.Fprintln(e.Out, "No snapshots — nothing to roll back to.")
		return nil
	}

	newest, oldest := snaps[0], snaps[len(snaps)-1]
	fmt.Fprintf(e.Out, "%d snapshot(s), covering %s\n", len(snaps), coverage(newest.Taken.Sub(oldest.Taken)))
	fmt.Fprintf(e.Out, "newest  %s (%s)\n", newest.Stamp, age(e.Now().Sub(newest.Taken)))
	fmt.Fprintf(e.Out, "oldest  %s (%s)\n", oldest.Stamp, age(e.Now().Sub(oldest.Taken)))

	// A configured Time Machine destination silently changes what retention
	// means, so it is reported here rather than left to surprise someone.
	if tm := apfs.DestinationInfo(ctx, e.Runner); tm.HasDestination {
		fmt.Fprintln(e.Out, "\nTime Machine has a destination configured, so backupd thins these to")
		fmt.Fprintln(e.Out, "roughly 24 hours. Any longer retention will not hold.")
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
		return fmt.Errorf("run: nothing to run (usage: snapshotter run -- <command> [args...])")
	}

	snap, err := apfs.Create(ctx, e.Runner)
	if err != nil {
		return fmt.Errorf("run: refusing to run without a restore point: %w", err)
	}
	fmt.Fprintf(e.Err, "snapshotter: restore point %s\n", snap.Stamp)

	if err := e.Exec(ctx, args[0], args[1:]); err != nil {
		fmt.Fprintf(e.Err, "snapshotter: %s failed; the state before it is in snapshot %s\n", args[0], snap.Stamp)
		return err
	}
	return nil
}

// age words a duration for a listing: short, and never more precise than it is
// accurate.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func coverage(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%.0f days", d.Hours()/24)
	case d >= time.Hour:
		return fmt.Sprintf("%.0f hours", d.Hours())
	default:
		return "under an hour"
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
			return fmt.Errorf("config: unknown option %q (usage: snapshotter config [--write])", arg)
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
		return fmt.Errorf("config: %s already exists; edit it, or delete it first to start again", path)
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
		fmt.Fprintf(e.Out, "wrote the defaults to %s\n", path)
		return nil
	default:
		fmt.Fprintf(e.Out, "%s does not exist, so the defaults are in use.\n", path)
		fmt.Fprintln(e.Out, "Write them there to start editing: snapshotter config --write")
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
			return fmt.Errorf("config: keys takes no arguments")
		}
		for _, k := range config.Keys() {
			fmt.Fprintln(e.Out, k)
		}
		return nil

	case "get":
		if len(args) != 2 {
			return fmt.Errorf("config: get needs one key (usage: snapshotter config get <key>)")
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
			return fmt.Errorf("config: set needs a key and a value (usage: snapshotter config set <key> <value>)")
		}
		// A file that will not parse is not overwritten here either. Load reports
		// the error and hands back the defaults, and saving those would silently
		// discard whatever the file was trying to say.
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("config: %w (fix the file before setting anything)", err)
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
		return fmt.Errorf("config: %q is not a config command (try: keys, get, set)", args[0])
	}
}
