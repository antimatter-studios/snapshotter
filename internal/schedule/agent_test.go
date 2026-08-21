package schedule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls []string
	fail  map[string]bool
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, cmd)
	for prefix := range f.fail {
		if strings.HasPrefix(cmd, prefix) {
			return "not loaded", errors.New("exit status 1")
		}
	}
	return "", nil
}

func (f *fakeRunner) ranPrefix(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func newAgent(t *testing.T, r *fakeRunner) *Agent {
	t.Helper()
	dir := t.TempDir()
	return &Agent{
		Runner:   r,
		AgentDir: filepath.Join(dir, "LaunchAgents"),
		Program:  "/Applications/Snapshotter.app/Contents/MacOS/snapshotter",
		LogPath:  filepath.Join(dir, "Logs", "snapshotter.log"),
		UID:      501,
	}
}

func TestInstallWritesAPlistLaunchdCanRead(t *testing.T) {
	r := &fakeRunner{}
	a := newAgent(t, r)

	cfg := Config{Interval: 6 * time.Hour, Retention: 14 * 24 * time.Hour}
	if err := a.Install(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(a.plistPath())
	if err != nil {
		t.Fatal(err)
	}
	plist := string(data)
	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<integer>21600</integer>",
		"<key>" + retentionEnv + "</key>",
		"<string>336</string>",
		"--take-snapshot",
		"<key>RunAtLoad</key>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
}

// launchd refuses to bootstrap a label it already knows, so a reinstall has to
// boot the old one out first.
func TestInstallBootsOutBeforeBootstrapping(t *testing.T) {
	r := &fakeRunner{}
	a := newAgent(t, r)

	if err := a.Install(context.Background(), DefaultConfig); err != nil {
		t.Fatal(err)
	}
	var bootout, bootstrap int
	for i, c := range r.calls {
		if strings.HasPrefix(c, "launchctl bootout") {
			bootout = i + 1
		}
		if strings.HasPrefix(c, "launchctl bootstrap") {
			bootstrap = i + 1
		}
	}
	if bootout == 0 || bootstrap == 0 {
		t.Fatalf("expected both bootout and bootstrap: %v", r.calls)
	}
	if bootout > bootstrap {
		t.Errorf("bootout must come first: %v", r.calls)
	}
}

// A retention window shorter than the interval would delete each snapshot
// almost as it was taken, leaving nothing to restore from.
func TestInstallRefusesRetentionShorterThanInterval(t *testing.T) {
	a := newAgent(t, &fakeRunner{})
	err := a.Install(context.Background(), Config{Interval: 24 * time.Hour, Retention: time.Hour})
	if err == nil {
		t.Fatal("accepted a retention window shorter than the interval")
	}
	if !strings.Contains(err.Error(), "retention") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestInstallRefusesAbsurdlyShortIntervals(t *testing.T) {
	a := newAgent(t, &fakeRunner{})
	if err := a.Install(context.Background(), Config{Interval: time.Second, Retention: time.Hour}); err == nil {
		t.Error("accepted a one-second interval")
	}
}

func TestStatusReadsBackTheInstalledSchedule(t *testing.T) {
	r := &fakeRunner{}
	a := newAgent(t, r)
	cfg := Config{Interval: 3 * time.Hour, Retention: 7 * 24 * time.Hour}
	if err := a.Install(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed {
		t.Error("did not report the agent as installed")
	}
	if !st.Loaded {
		t.Error("did not report the agent as loaded")
	}
	if st.Config.Interval != 3*time.Hour {
		t.Errorf("want a 3h interval, got %s", st.Config.Interval)
	}
	if st.Config.Retention != 7*24*time.Hour {
		t.Errorf("want a 7d retention, got %s", st.Config.Retention)
	}
}

func TestStatusReportsNotLoadedWhenLaunchctlHasNothing(t *testing.T) {
	r := &fakeRunner{fail: map[string]bool{"launchctl print": true}}
	a := newAgent(t, r)
	if err := os.MkdirAll(a.AgentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed || st.Loaded {
		t.Errorf("reported a schedule that is not there: %+v", st)
	}
}

// Two agents both running tmutil localsnapshot would double the snapshot rate
// and apply two different retention windows to one shared set.
func TestStatusReportsAConflictingSnapshotAgent(t *testing.T) {
	r := &fakeRunner{}
	a := newAgent(t, r)
	if err := os.MkdirAll(a.AgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(a.AgentDir, "com.christhomas.apfs-snapshot.plist")
	if err := os.WriteFile(other, []byte("<string>/Users/someone/bin/apfs-snapshot</string>"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Conflicts) != 1 || st.Conflicts[0] != "com.christhomas.apfs-snapshot" {
		t.Errorf("want the standalone agent reported, got %v", st.Conflicts)
	}
}

func TestStatusIgnoresUnrelatedAgents(t *testing.T) {
	r := &fakeRunner{}
	a := newAgent(t, r)
	if err := os.MkdirAll(a.AgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(a.AgentDir, "com.example.updater.plist")
	if err := os.WriteFile(unrelated, []byte("<string>/usr/bin/true</string>"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Conflicts) != 0 {
		t.Errorf("reported an unrelated agent as a conflict: %v", st.Conflicts)
	}
}

func TestUninstallUnloadsAndRemoves(t *testing.T) {
	r := &fakeRunner{}
	a := newAgent(t, r)
	if err := a.Install(context.Background(), DefaultConfig); err != nil {
		t.Fatal(err)
	}
	if err := a.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.plistPath()); !os.IsNotExist(err) {
		t.Error("plist still present")
	}
	if !r.ranPrefix("launchctl bootout") {
		t.Errorf("did not unload the agent: %v", r.calls)
	}
}

func TestUninstallIsSafeWhenNothingIsInstalled(t *testing.T) {
	a := newAgent(t, &fakeRunner{})
	if err := a.Uninstall(context.Background()); err != nil {
		t.Errorf("failed on an absent agent: %v", err)
	}
}

// The plist is what launchd will actually run, so the policy has to be in it —
// and the flat hours value has to stay in it too, at the policy's horizon, so a
// build that predates tiering prunes at the far edge of the policy rather than
// inside it.
func TestInstallWritesThePolicyAndAFlatValueAtItsHorizon(t *testing.T) {
	r := &fakeRunner{}
	a := newAgent(t, r)
	preset := Presets(6*time.Hour, 14*day)[0]

	cfg := Config{Interval: 6 * time.Hour, Policy: preset.Policy}
	if err := a.Install(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(a.plistPath())
	if err != nil {
		t.Fatal(err)
	}
	plist := string(data)
	for _, want := range []string{
		"<key>" + policyEnv + "</key>",
		"<string>" + preset.Policy.String() + "</string>",
		"<key>" + retentionEnv + "</key>",
		// 2184 hours is the 13-week horizon, in hours.
		"<string>2184</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
}

func TestStatusReadsBackAnInstalledPolicy(t *testing.T) {
	a := newAgent(t, &fakeRunner{})
	preset := Presets(6*time.Hour, 14*day)[1]
	if err := a.Install(context.Background(), Config{Interval: 6 * time.Hour, Policy: preset.Policy}); err != nil {
		t.Fatal(err)
	}

	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Config.Policy.Equal(preset.Policy) {
		t.Errorf("read back %+v, want %+v", st.Config.Policy.Bands(), preset.Policy.Bands())
	}
	// Retention is the horizon, so the panels reading that field keep meaning
	// something once a policy is installed.
	if st.Config.Retention != preset.Policy.Horizon() {
		t.Errorf("retention %s, want the horizon %s", st.Config.Retention, preset.Policy.Horizon())
	}
}

// An agent installed before tiered retention existed carries only the flat
// hours. It has to keep working, and to keep meaning exactly what it meant:
// everything kept for that long, no thinning.
func TestAnAgentInstalledBeforeTieringIsReadAsTheEquivalentFlatPolicy(t *testing.T) {
	old := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>StartInterval</key>
	<integer>21600</integer>
	<key>EnvironmentVariables</key>
	<dict>
		<key>` + retentionEnv + `</key>
		<string>168</string>
	</dict>
</dict>
</plist>`

	cfg := parseConfig(old)
	if cfg.Interval != 6*time.Hour {
		t.Errorf("interval %s, want 6h", cfg.Interval)
	}
	if cfg.Retention != 168*time.Hour {
		t.Errorf("retention %s, want 168h", cfg.Retention)
	}
	if !cfg.EffectivePolicy().IsFlat() {
		t.Errorf("read as %+v, want a flat window", cfg.EffectivePolicy().Bands())
	}
	if got := cfg.EffectivePolicy().Horizon(); got != 168*time.Hour {
		t.Errorf("the flat policy reaches %s, want 168h", got)
	}
}

// A plist carrying a policy this build cannot read must not be pruned against a
// guess: not pruning is corrected by the next run, over-pruning is not
// correctable at all.
func TestAnUnreadablePolicyPrunesNothing(t *testing.T) {
	t.Setenv(retentionEnv, "336")
	t.Setenv(policyEnv, "every other tuesday")

	policy, err := PolicyFromEnv()
	if err == nil {
		t.Fatal("accepted an unreadable policy")
	}
	if policy.Horizon() != 0 {
		t.Errorf("fell back to a policy that prunes: %+v", policy.Bands())
	}
}

func TestPolicyFromEnvReadsTheFlatValueWhenNoPolicyIsSet(t *testing.T) {
	t.Setenv(retentionEnv, "72")
	t.Setenv(policyEnv, "")

	policy, err := PolicyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Equal(FlatPolicy(72 * time.Hour)) {
		t.Errorf("got %+v, want a flat 72h window", policy.Bands())
	}
}

func TestPolicyFromEnvReadsAPolicy(t *testing.T) {
	t.Setenv(policyEnv, Presets(6*time.Hour, 14*day)[0].Policy.String())

	policy, err := PolicyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Equal(Presets(6*time.Hour, 14*day)[0].Policy) {
		t.Errorf("got %+v, want %+v", policy.Bands(), Presets(6*time.Hour, 14*day)[0].Policy.Bands())
	}
}

// The same refusal as for a flat window, on the reach a tiered policy actually
// has rather than on whatever the Retention field happens to hold.
func TestInstallRefusesAPolicyThatReachesLessFarThanTheInterval(t *testing.T) {
	a := newAgent(t, &fakeRunner{})
	err := a.Install(context.Background(), Config{
		Interval: 24 * time.Hour,
		Policy:   Policy{Tiers: []Tier{{Every: time.Hour, For: 6 * time.Hour}}},
	})
	if err == nil {
		t.Fatal("accepted a policy that prunes faster than snapshots are taken")
	}
	if !strings.Contains(err.Error(), "retention") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestRetentionFromEnvFallsBackToTheDefault(t *testing.T) {
	t.Setenv(retentionEnv, "")
	if got := RetentionFromEnv(); got != DefaultConfig.Retention {
		t.Errorf("want the default, got %s", got)
	}
	t.Setenv(retentionEnv, "nonsense")
	if got := RetentionFromEnv(); got != DefaultConfig.Retention {
		t.Errorf("want the default for junk, got %s", got)
	}
	t.Setenv(retentionEnv, "72")
	if got := RetentionFromEnv(); got != 72*time.Hour {
		t.Errorf("want 72h, got %s", got)
	}
}

func TestRenderEscapesPathsForXML(t *testing.T) {
	a := newAgent(t, &fakeRunner{})
	a.Program = "/Applications/Snap & Shot.app/Contents/MacOS/snapshotter"

	plist, err := a.render(DefaultConfig)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plist, "Snap & Shot") {
		t.Error("ampersand not escaped, which would produce an unparseable plist")
	}
	if !strings.Contains(plist, "Snap &amp; Shot") {
		t.Errorf("wrong escaping:\n%s", plist)
	}
}

// System Settings lists background items under whatever it can attribute them
// to. With nothing to go on it uses the name on the signing certificate, so
// "App Background Activity" showed a person's name — "Chris Thomas" — against two
// items, with no way for the user to tell what they were or that they belonged
// to Snapshotter.
func TestThePlistSaysWhichApplicationItBelongsTo(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{
		Runner: &recordingRunner{}, AgentDir: dir,
		Program: "/Applications/Snapshotter.app/Contents/MacOS/snapshotter",
		LogPath: filepath.Join(dir, "log"), UID: os.Getuid(),
	}

	plist, err := a.render(DefaultConfig)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(plist, "<key>AssociatedBundleIdentifiers</key>") {
		t.Error("the plist does not say which application it belongs to")
	}
	if !strings.Contains(plist, BundleID) {
		t.Errorf("the bundle identifier is missing: %s", BundleID)
	}
	// The label and the identifier are the same string for this agent and
	// different for the tripwire, so the identifier must not be derived from the
	// label.
	if BundleID != "com.christhomas.snapshotter" {
		t.Errorf("the bundle identifier moved to %q; the Full Disk Access grant "+
			"and the cask's uninstall stanza are both keyed on the old one", BundleID)
	}
}
