package schedule

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingRunner captures the launchctl calls without making any.
type recordingRunner struct {
	calls [][]string
	fail  map[string]bool
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	if r.fail != nil && len(args) > 0 && r.fail[args[0]] {
		return "no such service", os.ErrNotExist
	}
	return "", nil
}

func (r *recordingRunner) sawVerb(verb string) bool {
	for _, c := range r.calls {
		for _, a := range c {
			if a == verb {
				return true
			}
		}
	}
	return false
}

func newTripwire(t *testing.T, r *recordingRunner) *Tripwire {
	t.Helper()
	dir := t.TempDir()
	return &Tripwire{
		Runner:   r,
		AgentDir: dir,
		Program:  "/Applications/Snapshotter.app/Contents/MacOS/snapshotter",
		LogPath:  filepath.Join(dir, "logs", "tripwire.log"),
		UID:      501,
	}
}

func TestTripwireInstallWritesAKeepAlivePlist(t *testing.T) {
	r := &recordingRunner{}
	tw := newTripwire(t, r)

	if err := tw.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}

	body, err := os.ReadFile(tw.plistPath())
	if err != nil {
		t.Fatalf("no plist written: %v", err)
	}
	plist := string(body)

	// KeepAlive, not StartInterval: this one runs continuously and is useless
	// while it is not running. Getting these the wrong way round would give a
	// watcher that exits immediately and a schedule that never stops.
	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Error("no KeepAlive: launchd will not restart the watcher if it dies")
	}
	if strings.Contains(plist, "StartInterval") {
		t.Error("StartInterval is for the interval agent, not a continuous watcher")
	}
	if !strings.Contains(plist, "ThrottleInterval") {
		t.Error("no ThrottleInterval: a crash loop would spin")
	}
	if !strings.Contains(plist, "--watch") {
		t.Error("the plist does not pass --watch, so it would open a window instead")
	}
	if !strings.Contains(plist, TripwireLabel) {
		t.Errorf("the plist does not carry %s", TripwireLabel)
	}
	if !r.sawVerb("bootstrap") {
		t.Error("the agent was written but never loaded")
	}
}

// The two agents must not share a label, or installing one boots the other out.
func TestTripwireAndIntervalAgentHaveDifferentLabels(t *testing.T) {
	if TripwireLabel == Label {
		t.Fatal("the tripwire and the interval agent share a launchd label")
	}
	if !strings.HasPrefix(TripwireLabel, Label) {
		t.Errorf("TripwireLabel %q should be namespaced under %q", TripwireLabel, Label)
	}
}

func TestTripwireStatusReportsWhatIsOnDisk(t *testing.T) {
	r := &recordingRunner{}
	tw := newTripwire(t, r)

	st, err := tw.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed {
		t.Error("reported installed with no plist on disk")
	}

	if err := tw.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st, err = tw.Status(context.Background()); err != nil || !st.Installed {
		t.Fatalf("after Install: installed=%v err=%v", st.Installed, err)
	}
}

func TestTripwireUninstallRemovesThePlist(t *testing.T) {
	r := &recordingRunner{}
	tw := newTripwire(t, r)

	if err := tw.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tw.Uninstall(context.Background()); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(tw.plistPath()); !os.IsNotExist(err) {
		t.Error("the plist survived Uninstall")
	}
	// Uninstalling something already gone is not an error.
	if err := tw.Uninstall(context.Background()); err != nil {
		t.Errorf("second Uninstall: %v", err)
	}
}

// Our own agents must never be reported to the user as competing ones. The
// tripwire really does take snapshots, so the marker logic would be right about
// it and wrong about what it means.
func TestOurOwnAgentsAreNotConflicts(t *testing.T) {
	if !ours(Label + ".plist") {
		t.Error("the interval agent is not recognised as ours")
	}
	if !ours(TripwireLabel + ".plist") {
		t.Error("the tripwire is not recognised as ours, so it would be flagged as a conflict")
	}
	if ours("com.example.someone-else.plist") {
		t.Error("a third-party agent was treated as ours")
	}
}

func TestConflictScanIgnoresBothOfOurAgents(t *testing.T) {
	r := &recordingRunner{}
	dir := t.TempDir()

	// Both of ours, plus a genuine third-party snapshot agent.
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(Label+".plist", "<plist>tmutil localsnapshot</plist>")
	write(TripwireLabel+".plist", "<plist>tmutil --watch</plist>")
	write("com.someone.backup.plist", "<plist>tmutil localsnapshot</plist>")

	a := &Agent{Runner: r, AgentDir: dir, Program: "/x", LogPath: filepath.Join(dir, "l.log"), UID: 501}
	found, err := a.conflicts()
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0] != "com.someone.backup" {
		t.Fatalf("conflicts = %v, want only the third-party agent", found)
	}
}
