package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"snapshotter/internal/config"
	"strings"
	"testing"
	"time"
)

// Local, not UTC: tmutil writes its stamps in local time and apfs.ParseName
// reads them that way, so a UTC clock here would offset every age by the
// machine's zone and test the timezone rather than the formatting.
var now = time.Date(2026, 8, 14, 8, 0, 0, 0, time.Local)

// fakeRunner answers tmutil and diskutil without touching the machine.
type fakeRunner struct {
	snapshots []string
	details   string
	createErr error
	created   int
	calls     []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch {
	case name == "tmutil" && args[0] == "listlocalsnapshots":
		return "Snapshots for disk /:\n" + strings.Join(f.snapshots, "\n") + "\n", nil
	case name == "tmutil" && args[0] == "localsnapshot":
		if f.createErr != nil {
			return "", f.createErr
		}
		f.created++
		return "Created local snapshot with date: 2026-08-14-080000\n", nil
	case name == "tmutil" && args[0] == "destinationinfo":
		return "No destinations configured", errors.New("exit status 1")
	case name == "diskutil":
		return f.details, nil
	}
	return "", nil
}

func newEnv(r *fakeRunner) (Env, *bytes.Buffer, *bytes.Buffer) {
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	return Env{
		Runner: r,
		Volume: "/System/Volumes/Data",
		Out:    out,
		Err:    errBuf,
		Now:    func() time.Time { return now },
		Exec:   func(context.Context, string, []string) error { return nil },
	}, out, errBuf
}

func TestIsCommandDistinguishesVerbsFromABareInvocation(t *testing.T) {
	for _, v := range []string{"list", "status", "take", "run", "help", "--help"} {
		if !IsCommand(v) {
			t.Errorf("IsCommand(%q) = false", v)
		}
	}
	// A bare invocation must open the window, and the launchd flags must not be
	// swallowed by the command dispatcher.
	for _, v := range []string{"", "--take-snapshot", "--watch", "nonsense"} {
		if IsCommand(v) {
			t.Errorf("IsCommand(%q) = true; it would stop the window opening", v)
		}
	}
}

func TestHelpSucceedsAndUnknownCommandsDoNot(t *testing.T) {
	e, out, _ := newEnv(&fakeRunner{})
	if code := Run(context.Background(), e, []string{"help"}); code != 0 {
		t.Errorf("help exited %d", code)
	}
	if !strings.Contains(out.String(), "run -- <command>") {
		t.Errorf("help does not document run:\n%s", out.String())
	}

	e2, _, errBuf := newEnv(&fakeRunner{})
	if code := Run(context.Background(), e2, []string{"frobnicate"}); code != 2 {
		t.Errorf("unknown command exited %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "no such command") {
		t.Error("an unknown command did not say so")
	}
}

func TestListReportsFlagsPerSnapshot(t *testing.T) {
	r := &fakeRunner{
		snapshots: []string{
			"com.apple.TimeMachine.2026-08-14-003200.local",
			"com.apple.TimeMachine.2026-08-13-172036.local",
		},
		details: `+-- A
|   Name:        com.apple.TimeMachine.2026-08-13-172036.local
|   Purgeable:   Yes
|   NOTE:        This snapshot limits the minimum size of APFS Container disk3
+-- B
    Name:        com.apple.TimeMachine.2026-08-14-003200.local
    Purgeable:   Yes
`,
	}
	e, out, _ := newEnv(r)
	if code := Run(context.Background(), e, []string{"list"}); code != 0 {
		t.Fatalf("list exited %d", code)
	}
	got := out.String()

	if !strings.Contains(got, "pinning-container") {
		t.Error("the snapshot holding the container open is not flagged")
	}
	// Only the older one carries the NOTE; flagging both would name the wrong
	// snapshot as the one worth deleting.
	if strings.Count(got, "pinning-container") != 1 {
		t.Errorf("pinning-container appears %d times, want 1:\n%s", strings.Count(got, "pinning-container"), got)
	}
	if !strings.Contains(got, "7h ago") {
		t.Errorf("age is wrong:\n%s", got)
	}
}

func TestListSaysSoWhenThereAreNone(t *testing.T) {
	e, out, _ := newEnv(&fakeRunner{})
	Run(context.Background(), e, []string{"list"})
	if !strings.Contains(out.String(), "No snapshots") {
		t.Errorf("did not report an empty list:\n%s", out.String())
	}
}

func TestTakePrintsOnlyTheStampSoItCanBePipedInto(t *testing.T) {
	r := &fakeRunner{}
	e, out, _ := newEnv(r)
	if code := Run(context.Background(), e, []string{"take"}); code != 0 {
		t.Fatalf("take exited %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "2026-08-14-080000" {
		t.Errorf("stdout = %q, want the bare stamp", got)
	}
}

// The point of `run`: the restore point has to exist before the risky thing
// starts. If it cannot be made, the risky thing must not start at all.
func TestRunRefusesToRunTheCommandWhenTheSnapshotFails(t *testing.T) {
	r := &fakeRunner{createErr: errors.New("exit status 1")}
	e, _, errBuf := newEnv(r)

	ran := false
	e.Exec = func(context.Context, string, []string) error { ran = true; return nil }

	if code := Run(context.Background(), e, []string{"run", "--", "rm", "-rf", "everything"}); code != 1 {
		t.Errorf("exited %d, want 1", code)
	}
	if ran {
		t.Fatal("the command ran without a restore point")
	}
	if !strings.Contains(errBuf.String(), "refusing to run without a restore point") {
		t.Errorf("the reason was not reported:\n%s", errBuf.String())
	}
}

func TestRunSnapshotsBeforeTheCommand(t *testing.T) {
	r := &fakeRunner{}
	e, _, errBuf := newEnv(r)

	var createdWhenCommandRan int
	e.Exec = func(context.Context, string, []string) error {
		createdWhenCommandRan = r.created
		return nil
	}

	if code := Run(context.Background(), e, []string{"run", "--", "true"}); code != 0 {
		t.Fatalf("exited %d", code)
	}
	if createdWhenCommandRan != 1 {
		t.Error("the command ran before the snapshot was taken")
	}
	if !strings.Contains(errBuf.String(), "restore point 2026-08-14-080000") {
		t.Errorf("the restore point was not reported:\n%s", errBuf.String())
	}
}

// A wrapper that swallowed the exit status would break every script it is put
// in front of.
func TestRunPassesTheCommandsFailureThrough(t *testing.T) {
	r := &fakeRunner{}
	e, _, errBuf := newEnv(r)
	e.Exec = func(context.Context, string, []string) error { return fmt.Errorf("exit status 3") }

	if code := Run(context.Background(), e, []string{"run", "--", "false"}); code != 1 {
		t.Errorf("exited %d, want a failure", code)
	}
	// And it must say where the state before the failure went.
	if !strings.Contains(errBuf.String(), "2026-08-14-080000") {
		t.Errorf("did not name the snapshot holding the prior state:\n%s", errBuf.String())
	}
}

func TestRunWithoutACommandIsAnError(t *testing.T) {
	e, _, errBuf := newEnv(&fakeRunner{})
	if code := Run(context.Background(), e, []string{"run"}); code != 1 {
		t.Error("run with no command succeeded")
	}
	if code := Run(context.Background(), e, []string{"run", "--"}); code != 1 {
		t.Error("run with a bare -- succeeded")
	}
	if !strings.Contains(errBuf.String(), "nothing to run") {
		t.Errorf("unhelpful message:\n%s", errBuf.String())
	}
}

// The separator exists so the wrapped command's own flags are never read as
// ours.
func TestRunTreatsEverythingAfterTheSeparatorAsTheCommand(t *testing.T) {
	r := &fakeRunner{}
	e, _, _ := newEnv(r)

	var gotName string
	var gotArgs []string
	e.Exec = func(_ context.Context, name string, args []string) error {
		gotName, gotArgs = name, args
		return nil
	}
	Run(context.Background(), e, []string{"run", "--", "git", "clean", "-fdx"})

	if gotName != "git" || strings.Join(gotArgs, " ") != "clean -fdx" {
		t.Errorf("ran %q %v", gotName, gotArgs)
	}
}

// The command line is what the launchd agents and any script use, so its output
// is an interface: something parses it, or a person reads it at the moment they
// have lost a file.

func TestStatusSaysWhatIsThereAndWhatIsNot(t *testing.T) {
	r := &fakeRunner{snapshots: []string{
		"com.apple.TimeMachine.2026-08-14-060000.local",
		"com.apple.TimeMachine.2026-08-13-060000.local",
	}}
	e, out, _ := newEnv(r)

	if code := Run(context.Background(), e, []string{"status"}); code != 0 {
		t.Fatalf("status exited %d: %s", code, out)
	}
	text := out.String()
	for _, want := range []string{"2 snapshot", "newest", "oldest"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("status did not mention %q:\n%s", want, text)
		}
	}
}

// The state this application exists to get someone out of has to be unmistakable
// on the command line too, not only in the window.
func TestStatusOnAnUnprotectedMachineSaysSoPlainly(t *testing.T) {
	e, out, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"status"}); code != 0 {
		t.Fatalf("status exited %d", code)
	}
	if !strings.Contains(out.String(), "No snapshots") {
		t.Errorf("an empty machine did not say so:\n%s", out)
	}
}

func TestVersionPrintsSomethingAndNothingElse(t *testing.T) {
	e, out, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"version"}); code != 0 {
		t.Fatalf("version exited %d", code)
	}
	got := strings.TrimSpace(out.String())
	if got == "" {
		t.Error("version printed nothing")
	}
	if strings.Contains(got, "\n") {
		t.Errorf("version printed more than one line, which a script would have to parse: %q", got)
	}
}

// version must not need a machine: it is the first thing someone runs to check
// an installation works at all, and it should not depend on tmutil answering.
func TestVersionWorksWithoutAWorkingRunner(t *testing.T) {
	// A runner that knows nothing about any command: version must still answer.
	e, out, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"version"}); code != 0 {
		t.Fatalf("version needed a working machine: %d", code)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("version printed nothing")
	}
}

// Ages and spans are the whole of the list and status output, and they are read
// at a glance rather than parsed.
func TestAgeIsWordedForAGlance(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{90 * time.Minute, "1h ago"},
		{47 * time.Hour, "47h ago"},
	} {
		if got := age(tc.d); got != tc.want {
			t.Errorf("%v: want %q, got %q", tc.d, tc.want, got)
		}
	}
	// Beyond two days it should stop counting hours, whatever wording it uses.
	if got := age(72 * time.Hour); strings.Contains(got, "72h") {
		t.Errorf("three days was still reported in hours: %q", got)
	}
}

func TestCoverageIsWordedInTheLargestHonestUnit(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "under an hour"},
		{5 * time.Hour, "5 hours"},
		{47 * time.Hour, "47 hours"},
		{48 * time.Hour, "2 days"},
	} {
		if got := coverage(tc.d); got != tc.want {
			t.Errorf("%v: want %q, got %q", tc.d, tc.want, got)
		}
	}
}

// The settings file is the supported way to change what the window does not
// offer, so the command that finds it has to be honest about three states: no
// file (defaults apply), a file (show what it says), and a file it must not
// overwrite.

func TestConfigSaysWhereTheSettingsWouldGoWhenThereAreNone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, out, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"config"}); code != 0 {
		t.Fatalf("config failed with no settings file: %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "config.yaml") {
		t.Errorf("did not name the file it was looking for: %q", got)
	}
	// An absent file is not a fault, and saying so is the whole point: the
	// application works without one.
	if !strings.Contains(got, "defaults") {
		t.Errorf("did not say the defaults are in use: %q", got)
	}
	if !strings.Contains(got, "--write") {
		t.Errorf("did not say how to create one: %q", got)
	}
}

func TestConfigWritesTheDefaultsAndThenShowsThem(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	e, out, _ := newEnv(&fakeRunner{})
	if code := Run(context.Background(), e, []string{"config", "--write"}); code != 0 {
		t.Fatalf("config --write failed: %d", code)
	}

	path := filepath.Join(dir, "snapshotter", "config.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nothing was written: %v", err)
	}
	// What it wrote must be what the application reads back, or the file is a
	// decoration rather than settings.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("the application cannot read what it just wrote: %v", err)
	}
	if !reflect.DeepEqual(cfg, config.Defaults()) {
		t.Errorf("what was written is not the defaults:\n got %+v\nwant %+v", cfg, config.Defaults())
	}

	out.Reset()
	if code := Run(context.Background(), e, []string{"config"}); code != 0 {
		t.Fatalf("config failed with a settings file present: %d", code)
	}
	shown := out.String()
	if !strings.Contains(shown, path) {
		t.Errorf("did not name the file: %q", shown)
	}
	if !strings.Contains(shown, "interval_hours") {
		t.Errorf("did not show what the file says: %q", shown)
	}
}

// Settings files outlive the version that wrote them, so a rewrite would drop
// anything this build does not recognise.
func TestConfigRefusesToOverwriteSettingsThatAlreadyExist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "snapshotter", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := "appearance:\n    theme: dark\nsomething_from_a_later_version: 42\n"
	if err := os.WriteFile(path, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}

	e, _, errOut := newEnv(&fakeRunner{})
	if code := Run(context.Background(), e, []string{"config", "--write"}); code == 0 {
		t.Error("overwrote settings that were already there")
	}
	if !strings.Contains(errOut.String(), "already exists") {
		t.Errorf("did not say why it refused: %q", errOut.String())
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != mine {
		t.Errorf("the file was modified despite the refusal:\n%s", body)
	}
}

func TestConfigRejectsAnOptionItDoesNotKnow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, _, errOut := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"config", "--wipe"}); code == 0 {
		t.Error("an unknown option was accepted")
	}
	if !strings.Contains(errOut.String(), "--wipe") {
		t.Errorf("did not name the offending option: %q", errOut.String())
	}
}

// get and set are the scripting surface: a machine can be put into a known state
// and read back without a window and without hand-writing YAML.

func TestConfigSetWritesAndGetReadsItBack(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, out, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"config", "set", "schedule.interval_hours", "3"}); code != 0 {
		t.Fatalf("set failed: %d", code)
	}
	if !strings.Contains(out.String(), "3") {
		t.Errorf("set did not report what it stored: %q", out.String())
	}

	// A separate invocation, which is what a script does: the value has to have
	// reached the file rather than a variable.
	out.Reset()
	if code := Run(context.Background(), e, []string{"config", "get", "schedule.interval_hours"}); code != 0 {
		t.Fatalf("get failed: %d", code)
	}
	if strings.TrimSpace(out.String()) != "3" {
		t.Errorf("want 3, got %q", out.String())
	}
}

// Reading a setting on a machine with no settings file must answer with the
// default rather than fail — that is the value the application would use.
func TestConfigGetAnswersFromTheDefaultsWhenThereIsNoFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, out, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"config", "get", "appearance.theme"}); code != 0 {
		t.Fatalf("get failed with no file: %d", code)
	}
	want, err := config.Get(config.Defaults(), "appearance.theme")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != want {
		t.Errorf("want %q, got %q", want, out.String())
	}
}

// Setting one value must not disturb the others, or a script that changes the
// theme quietly resets the schedule.
func TestConfigSetLeavesEveryOtherSettingAlone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, _, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"config", "set", "schedule.retention_days", "30"}); code != 0 {
		t.Fatal("first set failed")
	}
	if code := Run(context.Background(), e, []string{"config", "set", "appearance.theme", "dark"}); code != 0 {
		t.Fatal("second set failed")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Schedule.RetentionDays != 30 {
		t.Errorf("the first setting was lost: %v", cfg.Schedule.RetentionDays)
	}
	if cfg.Appearance.Theme != "dark" {
		t.Errorf("the second setting did not take: %q", cfg.Appearance.Theme)
	}
	// Untouched fields keep their defaults rather than becoming zero.
	if cfg.Window.Width != config.Defaults().Window.Width {
		t.Errorf("an untouched setting was zeroed: %v", cfg.Window.Width)
	}
}

func TestConfigKeysListsWhatCanBeSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, out, _ := newEnv(&fakeRunner{})

	if code := Run(context.Background(), e, []string{"config", "keys"}); code != 0 {
		t.Fatalf("keys failed: %d", code)
	}
	for _, want := range config.Keys() {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%s missing from the listing", want)
		}
	}
}

func TestConfigRejectsBadUsage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, args := range [][]string{
		{"config", "get"},                             // no key
		{"config", "set", "appearance.theme"},         // no value
		{"config", "set", "window.width", "wide"},     // not a number
		{"config", "get", "schedule.every_fortnight"}, // no such setting
		{"config", "sideways"},                        // no such subcommand
		{"config", "keys", "extra"},                   // keys takes nothing
	} {
		e, _, errOut := newEnv(&fakeRunner{})
		if code := Run(context.Background(), e, args); code == 0 {
			t.Errorf("%v was accepted", args)
		}
		if errOut.Len() == 0 {
			t.Errorf("%v failed without saying why", args)
		}
	}
}
