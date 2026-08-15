package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
