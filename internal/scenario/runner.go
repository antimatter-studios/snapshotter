package scenario

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"snapshotter/internal/apfs"
)

// Runner is the fake. The application chooses its apfs.Runner in exactly one
// place, so this is the only thing about the type that has to be true.
var _ apfs.Runner = (*Runner)(nil)

// stampLayout is tmutil's snapshot date: zero-padded local time, which is why a
// lexical sort of stamps is also a chronological one and the listing below can
// be emitted in order without carrying one.
const stampLayout = "2006-01-02-150405"

// Runner answers the commands this application runs, in the shape the real tools
// answer them, and executes nothing.
//
// It is stateful, which is the difference between a scenario and a fixture:
// taking a snapshot from the menu bar adds one to the listing, installing the
// schedule makes launchctl print report it loaded, and pruning deletes. A screen
// driven into a state is still drivable from there.
type Runner struct {
	now func() time.Time

	mu sync.Mutex
	// snapshots is keyed by stamp, which is unique per snapshot because APFS
	// names a snapshot by its date to the second.
	snapshots map[string]fakeSnapshot
	// loaded is the set of launchd labels launchctl print will admit to.
	loaded      map[string]bool
	destination bool
	// commands records everything asked of the fake. It is the only evidence
	// that a scenario answered the whole machine rather than most of it.
	commands []string
}

// fakeSnapshot carries both forms of the identifier for the same reason
// apfs.Snapshot does: tmutil wants the bare stamp and diskutil prints the full
// name, and guessing which is which is the mistake that deletes a volume.
type fakeSnapshot struct {
	name  string
	stamp string
	spec  SnapshotSpec
}

// newRunner turns the spec's relative ages into stamps, once, at the moment the
// scenario starts. Recomputing them per command would make a snapshot's date
// drift while the window was open.
func newRunner(s Spec, at time.Time, now func() time.Time) (*Runner, error) {
	r := &Runner{
		now:         now,
		snapshots:   make(map[string]fakeSnapshot, len(s.Snapshots)),
		loaded:      make(map[string]bool, 2),
		destination: s.TimeMachine,
	}
	for i, snap := range s.Snapshots {
		stamp := at.Add(-snap.Age.Duration()).Format(stampLayout)
		if _, taken := r.snapshots[stamp]; taken {
			return nil, fmt.Errorf("scenario %s: snapshot %d lands on %s, which another already occupies; ages must differ by at least a second",
				s.Name, i, stamp)
		}
		name, err := apfs.NameForStamp(stamp)
		if err != nil {
			return nil, err
		}
		r.snapshots[stamp] = fakeSnapshot{name: name, stamp: stamp, spec: snap}
	}
	return r, nil
}

// Run answers one command.
func (r *Runner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, strings.Join(append([]string{name}, args...), " "))

	switch {
	case name == "tmutil" && len(args) == 2 && args[0] == "listlocalsnapshots":
		return r.listSnapshots(args[1]), nil
	case name == "tmutil" && len(args) == 1 && args[0] == "localsnapshot":
		return r.createSnapshot()
	case name == "tmutil" && len(args) == 2 && args[0] == "deletelocalsnapshots":
		return r.deleteSnapshot(args[1])
	case name == "tmutil" && len(args) == 1 && args[0] == "destinationinfo":
		return r.destinationInfo(), nil
	case name == "diskutil" && len(args) == 3 && args[0] == "apfs" && args[1] == "listSnapshots":
		return r.listDetails(args[2]), nil
	case name == "mount":
		return r.mounted(), nil
	case name == "launchctl" && len(args) == 2 && args[0] == "print":
		return r.print(args[1])
	case name == "launchctl" && len(args) == 3 && args[0] == "bootstrap":
		return r.bootstrap(args[2])
	case name == "launchctl" && len(args) == 2 && args[0] == "bootout":
		return r.bootout(args[1])
	}

	// An unmodelled command must fail rather than return empty success. Empty
	// output parses as "no snapshots" or "nothing configured", so a silent
	// answer would let the scenario stop describing what it claims to and look
	// like a finding instead of a gap in the fake.
	return "", fmt.Errorf("scenario: nothing here answers %s %s", name, strings.Join(args, " "))
}

// mounted answers mount(8) with the volumes a scenario has.
//
// The data volume and the sealed system volume, which is what any Mac has and
// no more: a scenario describes one machine's snapshots, and inventing an
// external disk here would put a row on the Health screen that no scenario asked
// for. The system volume earns its line by being the case worth getting right —
// it is APFS, it is mounted, and it holds only macOS's own sealed snapshot,
// which must not be counted or pruned as if it were ours.
func (r *Runner) mounted() string {
	return "/dev/disk1s3s1 on / (apfs, sealed, local, read-only, journaled)\n" +
		"/dev/disk1s1 on " + apfs.DataVolume + " (apfs, local, journaled, nobrowse)\n"
}

// deviceFor names a volume's device the way diskutil would. Stable per mount
// point, because the enumeration deduplicates on it.
func deviceFor(volume string) string {
	if volume == apfs.DataVolume {
		return "disk1s1"
	}
	return "disk1s3s1"
}

// commandsRun is the audit trail, copied so a caller cannot edit it.
func (r *Runner) commandsRun() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

// setLoaded forces what launchctl print will say, which is how a scenario
// expresses a plist launchd has on disk and has not loaded.
func (r *Runner) setLoaded(label string, loaded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if loaded {
		r.loaded[label] = true
		return
	}
	delete(r.loaded, label)
}

// stamps are the snapshots oldest first, which is the order tmutil lists them
// in. The application sorts them itself, and emitting them the way the real
// command does keeps that sort load-bearing.
func (r *Runner) stamps() []string {
	stamps := make([]string, 0, len(r.snapshots))
	for stamp := range r.snapshots {
		stamps = append(stamps, stamp)
	}
	sort.Strings(stamps)
	return stamps
}

// exitStatus is what os/exec reports for a command that ran and failed, which is
// all any caller here looks at.
func exitStatus(code int) error { return fmt.Errorf("exit status %d", code) }

func (r *Runner) listSnapshots(volume string) string {
	// The header names the volume and is not a snapshot. Every real caller
	// filters it by shape rather than by position, and emitting it is what keeps
	// that true.
	var b strings.Builder
	fmt.Fprintf(&b, "Snapshots for disk %s:\n", volume)
	for _, stamp := range r.stamps() {
		fmt.Fprintf(&b, "%s\n", r.snapshots[stamp].name)
	}
	return b.String()
}

func (r *Runner) createSnapshot() (string, error) {
	stamp := r.now().Format(stampLayout)
	if _, exists := r.snapshots[stamp]; exists {
		// A snapshot is named by its date to the second, so a second one in the
		// same second cannot exist. Real tmutil fails; so does this.
		return "Failed to create local snapshot\n", exitStatus(1)
	}
	name, err := apfs.NameForStamp(stamp)
	if err != nil {
		return "", err
	}
	r.snapshots[stamp] = fakeSnapshot{name: name, stamp: stamp}

	// Both lines are tmutil's own. The note is included because tmutil prints it
	// sometimes and not always, and the parser answers that by scanning the
	// whole output rather than the first line — which only stays tested if
	// something produces the note.
	return "NOTE: local snapshots are considered purgeable and may be removed at any time by deleted(8).\n" +
		"Created local snapshot with date: " + stamp + "\n", nil
}

func (r *Runner) deleteSnapshot(stamp string) (string, error) {
	if _, exists := r.snapshots[stamp]; !exists {
		return fmt.Sprintf("Failed to delete local snapshot '%s'\n", stamp), exitStatus(1)
	}
	delete(r.snapshots, stamp)
	return fmt.Sprintf("Deleted local snapshot '%s'\n", stamp), nil
}

// destinationInfo answers whether Time Machine owns these snapshots.
//
// Only the presence of a destination is read anywhere, so the configured block
// is fixed rather than described by the spec.
func (r *Runner) destinationInfo() string {
	if !r.destination {
		// DECISIONS.md records that tmutil exits non-zero with nothing
		// configured; on macOS 26 it exits zero and says so in the message. The
		// parser handles both, and the message is the harder case, so it is the
		// one faked here.
		return "tmutil: No destinations configured.\n"
	}
	return "====================================================\n" +
		"Name          : Backups\n" +
		"Kind          : Local\n" +
		"Mount Point   : /Volumes/Backups\n" +
		"ID            : 3F8B0A62-9C1D-4E77-9B0E-5B1A2C7D4E10\n"
}

// listDetails reproduces diskutil's indented block listing, including the
// tree-drawing characters.
//
// The last block is indented with spaces where the others use a pipe, and the
// container NOTE appears on one snapshot only. Both are quirks of the real
// output that the parser has to survive, so faking a tidier format would test
// the fake instead of the application.
func (r *Runner) listDetails(volume string) string {
	// Only the data volume has any. A scenario describes one machine's snapshots,
	// and this fake is asked about every mounted APFS volume now that pruning
	// covers all of them — so the others have to answer, and answer nothing.
	if volume != apfs.DataVolume {
		return fmt.Sprintf("No snapshots for %s\n", deviceFor(volume))
	}
	stamps := r.stamps()

	var b strings.Builder
	// The header names the device, and it IS read: two mount points can name one
	// volume, so the enumeration deduplicates on this. An arbitrary constant here
	// would merge every volume into one.
	fmt.Fprintf(&b, "Snapshots for %s (%d found)\n", deviceFor(volume), len(stamps))

	for i, stamp := range stamps {
		snap := r.snapshots[stamp]
		lead := "|   "
		if i == len(stamps)-1 {
			lead = "    "
		}
		b.WriteString("|\n")
		fmt.Fprintf(&b, "+-- %s\n", snapshotUUID(stamp))
		field(&b, lead, "Name", snap.name)
		// XID is a transaction number nothing here reads. It exists so the block
		// has the shape diskutil produces, and it rises with age because that is
		// what a transaction number does.
		field(&b, lead, "XID", fmt.Sprintf("%d", 18000000+i*1000))
		field(&b, lead, "Purgeable", yesNo(snap.spec.purgeable()))
		if snap.spec.LimitsContainer {
			// diskutil phrases this as prose rather than as a flag, and the
			// parser matches the distinctive part of the sentence.
			field(&b, lead, "NOTE", "This snapshot limits the minimum size of APFS Container disk1")
		}
	}
	return b.String()
}

// field writes one line of a diskutil block. The value column is fixed, and the
// parser tolerates any spacing, so this only has to look like the real thing to
// whoever reads the log.
func field(b *strings.Builder, lead, key, value string) {
	fmt.Fprintf(b, "%s%-13s%s\n", lead, key+":", value)
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// snapshotUUID is derived from the stamp so two runs of the same scenario print
// the same listing. Nothing parses it; diskutil heads each block with one, and
// leaving it out would let a parser that keyed on it pass here and fail for real.
func snapshotUUID(stamp string) string {
	h := fnv.New64a()
	h.Write([]byte(stamp))
	sum := h.Sum64()
	return fmt.Sprintf("%08X-%04X-4%03X-8%03X-%012X",
		uint32(sum>>32), uint16(sum>>16), uint16(sum)&0xfff, uint16(sum>>4)&0xfff, sum&0xffffffffffff)
}

// print answers launchctl print, whose exit status is the whole answer: the
// application reads whether the service is loaded and nothing else.
func (r *Runner) print(target string) (string, error) {
	label := labelFromTarget(target)
	if !r.loaded[label] {
		// launchctl's own words, so a developer reading the log sees what the
		// real command would have said.
		return fmt.Sprintf("Bad request.\nCould not find service %q in domain for user gui: %s\n",
			label, uidFromTarget(target)), exitStatus(113)
	}
	return fmt.Sprintf("%s = {\n\tactive count = 1\n\tstate = running\n}\n", label), nil
}

// bootstrap loads the label named inside the plist it is given, rather than one
// derived from the filename, so a scenario cannot load an agent the file does
// not describe.
func (r *Runner) bootstrap(plistPath string) (string, error) {
	label, err := labelInPlist(plistPath)
	if err != nil {
		return "Bootstrap failed: 5: Input/output error\nTry re-running the command as root for richer errors.\n", exitStatus(5)
	}
	if r.loaded[label] {
		// launchd refuses a label it already holds, which is why the real
		// installer boots out before it bootstraps. Refusing here keeps that
		// ordering load-bearing instead of incidental.
		return "Bootstrap failed: 5: Input/output error\n", exitStatus(5)
	}
	r.loaded[label] = true
	return "", nil
}

func (r *Runner) bootout(target string) (string, error) {
	label := labelFromTarget(target)
	if !r.loaded[label] {
		return "Boot-out failed: 3: No such process\n", exitStatus(3)
	}
	delete(r.loaded, label)
	return "", nil
}

// labelFromTarget reads the label out of a launchctl service target, which is
// gui/<uid>/<label>.
func labelFromTarget(target string) string {
	if idx := strings.LastIndex(target, "/"); idx >= 0 {
		return target[idx+1:]
	}
	return target
}

// uidFromTarget is only used to make the refusal message read like launchctl's.
func uidFromTarget(target string) string {
	parts := strings.Split(target, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "0"
}

var plistLabel = regexp.MustCompile(`<key>Label</key>\s*<string>([^<]+)</string>`)

func labelInPlist(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	match := plistLabel.FindSubmatch(data)
	if match == nil {
		return "", fmt.Errorf("scenario: %s has no Label", path)
	}
	return string(match[1]), nil
}
