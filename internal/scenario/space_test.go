package scenario

import "testing"

// The invented disk a scenario reports.
//
// It matters because the low-space finding is one of the few that cannot be
// produced on a real machine on demand: filling a disk to test the warning is not
// something anyone does twice. A scenario is how that path is exercised at all,
// so a scenario that quietly reports the real disk instead makes the test it
// exists for meaningless.

func TestAScenarioWithNoDiskSpecifiedUsesTheRealOne(t *testing.T) {
	// nil rather than zeroes: the caller reads nil as "ask the machine", and a
	// function returning 0 of 0 would present every scenario as a disk with no
	// space at all and trip the low-space finding in all of them.
	if got := (Spec{}).Space(); got != nil {
		total, free, _ := got("/")
		t.Errorf("an unspecified disk produced %d of %d rather than deferring", free, total)
	}
}

func TestASpecifiedDiskIsReportedWhateverIsAsked(t *testing.T) {
	space := Spec{VolumeBytes: 1000, FreeBytes: 40}.Space()
	if space == nil {
		t.Fatal("a specified disk was not reported")
	}

	for _, volume := range []string{"/", "/System/Volumes/Data", "/anything"} {
		total, free, err := space(volume)
		if err != nil {
			t.Errorf("%s: %v", volume, err)
		}
		if total != 1000 || free != 40 {
			t.Errorf("%s reported %d of %d", volume, free, total)
		}
	}
}

// Only one of the two given is enough to mean "invented": a scenario saying the
// disk is full has a free figure and may not bother with a total.
func TestEitherFigureAloneStillMeansInvented(t *testing.T) {
	if (Spec{FreeBytes: 40}).Space() == nil {
		t.Error("a scenario naming only the free space fell back to the real disk")
	}
	if (Spec{VolumeBytes: 1000}).Space() == nil {
		t.Error("a scenario naming only the total fell back to the real disk")
	}
}

// The words the banner uses for each state an agent can be in. The third is the
// one worth having: a job launchd has loaded with no plist behind it survives
// until the next reboot and then vanishes, which is not the same as installed.
func TestEachAgentStateIsDescribedDistinctly(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range []AgentSpec{
		{Installed: true, Loaded: true},
		{Installed: true, Loaded: false},
		{Installed: false, Loaded: true},
		{Installed: false, Loaded: false},
	} {
		got := describeAgent(spec)
		if got == "" {
			t.Errorf("%+v described as nothing", spec)
		}
		if seen[got] {
			t.Errorf("%+v shares its description with another state: %q", spec, got)
		}
		seen[got] = true
	}
}
